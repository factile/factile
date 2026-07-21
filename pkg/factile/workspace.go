package factile

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"github.com/factile/factile/pkg/contextpack"
	"github.com/factile/factile/pkg/gitsource"
	graphpkg "github.com/factile/factile/pkg/graph"
	"github.com/factile/factile/pkg/okf"
	patchpkg "github.com/factile/factile/pkg/patch"
	"github.com/factile/factile/pkg/revision"
	searchpkg "github.com/factile/factile/pkg/search"
	"github.com/factile/factile/pkg/storage"
	"github.com/factile/factile/pkg/vfs"
)

type WorkspaceOptions struct {
	Workspace string
	// Deprecated: Root Layout v2 does not support --mount-file composition.
	MountFile string
	// Deprecated: Root Layout v2 uses Workspace for exact selection.
	Root     string
	WorkDir  string
	ReadOnly bool
}

type LocalWorkspace struct {
	opts         WorkspaceOptions
	resolveOnce  sync.Once
	workspace    vfs.WorkspaceContext
	workspaceErr error
}

func NewWorkspace(opts WorkspaceOptions) *LocalWorkspace {
	return &LocalWorkspace{opts: opts}
}

func (w *LocalWorkspace) resolvedWorkspace() (vfs.WorkspaceContext, error) {
	w.resolveOnce.Do(func() {
		if w.opts.Root != "" {
			w.workspaceErr = &vfs.Error{
				Code:    vfs.ErrInvalidWorkspace,
				Message: "Legacy root selection does not select a Factile workspace.",
			}
			return
		}
		if w.opts.MountFile != "" {
			w.workspaceErr = &vfs.Error{
				Code:    vfs.ErrInvalidWorkspace,
				Message: "Legacy mount-file composition does not select a Factile workspace.",
			}
			return
		}
		w.workspace, w.workspaceErr = vfs.ResolveWorkspace(vfs.ResolveWorkspaceOptions{
			Workspace: w.opts.Workspace,
			WorkDir:   w.opts.WorkDir,
		})
	})
	return w.workspace, w.workspaceErr
}

func (w *LocalWorkspace) List(ctx context.Context, inputPath string, opts ListOptions) (ListResult, error) {
	if inputPath == "" {
		inputPath = "/"
	}
	normalized, err := vfs.NormalizePath(inputPath)
	if err != nil {
		return ListResult{}, err
	}
	if strings.TrimSpace(opts.View) != "" {
		return w.listForView(ctx, normalized, opts)
	}
	mounts, err := w.mounts()
	if err != nil {
		return ListResult{}, NormalizeError(err)
	}
	folders := immediateMountFolders(mounts, normalized)
	if normalized == "/" {
		if mount, ok := mountByPath(mounts, "/"); ok {
			if err := ensureReadable(mount); err != nil {
				return ListResult{}, err
			}
			var documents []DocumentSummary
			folders, documents, err = w.listLocalEntries(mount, "", folders)
			if err != nil {
				return ListResult{}, err
			}
			sortFolderSummaries(folders)
			sortDocumentSummaries(documents)
			return w.listResult(ctx, normalized, folders, documents, opts)
		}
		return w.listResult(ctx, normalized, folders, nil, opts)
	}
	mounts, err = w.mountsForTarget(ctx, normalized)
	if err != nil {
		return ListResult{}, err
	}

	target, err := vfs.Resolve(mounts, normalized)
	if err != nil {
		if len(folders) > 0 {
			return w.listResult(ctx, normalized, folders, nil, opts)
		}
		return ListResult{}, NormalizeError(err)
	}
	if err := ensureReadable(target.Mount); err != nil {
		return ListResult{}, err
	}
	if target.Kind == TargetConcept {
		return ListResult{}, errorf(ErrPathIsNotBundle, "Path is a concept, not a listable path: %s", target.Path)
	}
	if target.Kind == TargetPath && !target.Exists {
		return ListResult{}, errorf(ErrMountNotFound, "Path not found: %s", target.Path)
	}
	folders, documents, err := w.listLocalEntries(target.Mount, target.ConceptID, folders)
	if err != nil {
		return ListResult{}, err
	}
	sortFolderSummaries(folders)
	sortDocumentSummaries(documents)
	return w.listResult(ctx, target.Path, folders, documents, opts)
}

func (w *LocalWorkspace) listLocalEntries(mount vfs.Mount, prefix string, folders []FolderSummary) ([]FolderSummary, []DocumentSummary, error) {
	store, err := storage.NewLocal(mount.SourcePath)
	if err != nil {
		return nil, nil, NormalizeError(err)
	}
	ids, err := store.ListConceptIDs(prefix)
	if err != nil {
		return nil, nil, NormalizeError(err)
	}
	prefix = strings.Trim(prefix, "/")
	seenFolders := map[string]bool{}
	for _, folder := range folders {
		seenFolders[folder.Path] = true
	}
	var documents []DocumentSummary
	for _, id := range ids {
		rest := id
		if prefix != "" {
			rest = strings.TrimPrefix(id, prefix+"/")
		}
		if strings.Contains(rest, "/") {
			first := strings.Split(rest, "/")[0]
			child := mount.MountPath
			if prefix != "" {
				child = cleanVirtualJoin(child, prefix)
			}
			child = cleanVirtualJoin(child, first)
			if !seenFolders[child] {
				seenFolders[child] = true
				folders = append(folders, FolderSummary{Path: child, Title: titleFromPath(child)})
			}
			continue
		}
		summary, err := w.summaryForID(store, mount, id)
		if err == nil {
			documents = append(documents, documentSummaryFromConcept(summary))
		}
	}
	return folders, documents, nil
}

func (w *LocalWorkspace) listForView(ctx context.Context, normalized string, opts ListOptions) (ListResult, error) {
	if _, target, err := w.resolveReadable(ctx, normalized); err == nil && target.Kind == TargetConcept {
		return ListResult{}, errorf(ErrPathIsNotBundle, "Path is a concept, not a listable path: %s", target.Path)
	}
	scoped, err := w.scopeForView(ctx, normalized, opts.View)
	if err != nil {
		return ListResult{}, err
	}
	folders, documents := listEntriesFromScope(normalized, scoped)
	return w.listResult(ctx, normalized, folders, documents, opts)
}

func listEntriesFromScope(current string, scoped scopedSet) ([]FolderSummary, []DocumentSummary) {
	seenFolders := map[string]bool{}
	var folders []FolderSummary
	addFolder := func(path string) {
		if seenFolders[path] {
			return
		}
		seenFolders[path] = true
		folders = append(folders, FolderSummary{Path: path, Title: titleFromPath(path)})
	}
	for _, visiblePath := range scoped.Paths {
		if child, ok := immediateChildPath(current, visiblePath); ok {
			addFolder(child)
		}
	}
	seenDocuments := map[string]bool{}
	var documents []DocumentSummary
	for _, item := range scoped.Concepts {
		entry, document, ok := immediateConceptEntry(current, item.Concept.Path)
		if !ok {
			continue
		}
		if !document {
			addFolder(entry)
			continue
		}
		if seenDocuments[entry] {
			continue
		}
		seenDocuments[entry] = true
		documents = append(documents, documentSummaryFromConcept(item.Summary))
	}
	sortFolderSummaries(folders)
	sortDocumentSummaries(documents)
	return folders, documents
}

func (w *LocalWorkspace) Read(ctx context.Context, inputPath string, opts ReadOptions) (ConceptResult, error) {
	_, target, err := w.resolveReadable(ctx, inputPath)
	if err != nil {
		return ConceptResult{}, err
	}
	if target.Kind != TargetConcept {
		return ConceptResult{}, errorf(ErrConceptNotFound, "Concept not found: %s", target.Path)
	}
	if err := ensureReadable(target.Mount); err != nil {
		return ConceptResult{}, err
	}
	concept, err := w.readConcept(target.Mount, target.ConceptID)
	if err != nil {
		return ConceptResult{}, err
	}
	return ConceptResult{Concept: concept}, nil
}

func (w *LocalWorkspace) Search(ctx context.Context, inputPath string, query string, opts SearchOptions) (SearchResults, error) {
	if strings.TrimSpace(query) == "" {
		return SearchResults{}, errorf(ErrInvalidPath, "Search query must not be empty")
	}
	scope, err := w.scopeWithView(ctx, inputPath, opts.View)
	if err != nil {
		return SearchResults{}, err
	}
	fields := make([]searchpkg.Fields, 0, len(scope.Concepts))
	for _, item := range scope.Concepts {
		fields = append(fields, searchpkg.Fields{
			Path:        item.Concept.Path,
			ConceptID:   item.Concept.ConceptID,
			Title:       okf.StringField(item.Concept.Frontmatter, "title"),
			Description: okf.StringField(item.Concept.Frontmatter, "description"),
			Tags:        okf.StringSliceField(item.Concept.Frontmatter, "tags"),
			Resource:    okf.StringField(item.Concept.Frontmatter, "resource"),
			Body:        item.Concept.Markdown,
		})
	}
	scored := searchpkg.Score(query, fields)
	results := make([]SearchResult, 0, len(scored))
	for _, item := range scored {
		results = append(results, SearchResult{
			Concept: scope.Summaries[item.Index],
			Score:   item.Score,
			Snippet: item.Snippet,
		})
	}
	return SearchResults{Path: scope.Path, Query: query, Results: results}, nil
}

func (w *LocalWorkspace) Context(ctx context.Context, inputPath string, query string, opts ContextOptions) (ContextPack, error) {
	if opts.MaxTokens <= 0 {
		opts.MaxTokens = 4000
	}
	depth, err := normalizeLinkDepth(opts.Depth)
	if err != nil {
		return ContextPack{}, err
	}
	searchResults, err := w.Search(ctx, inputPath, query, SearchOptions{View: opts.View})
	if err != nil {
		return ContextPack{}, err
	}
	scope, err := w.scopeWithView(ctx, inputPath, opts.View)
	if err != nil {
		return ContextPack{}, err
	}
	byPath := map[string]scopedConcept{}
	for _, item := range scope.Concepts {
		byPath[item.Concept.Path] = item
	}
	var ordered []scopedConcept
	seen := map[string]bool{}
	add := func(p string) {
		if seen[p] {
			return
		}
		if item, ok := byPath[p]; ok {
			seen[p] = true
			ordered = append(ordered, item)
		}
	}
	for _, result := range searchResults.Results {
		add(result.Concept.Path)
	}
	if depth > 0 {
		for _, item := range append([]scopedConcept(nil), ordered...) {
			for _, link := range graphpkg.ExtractMarkdownLinks(item.Concept.Markdown) {
				if target, ok := graphpkg.ResolveLink(item.Concept.Path, link.Target); ok {
					add(target)
				}
			}
		}
		for _, item := range scope.Concepts {
			for _, link := range graphpkg.ExtractMarkdownLinks(item.Concept.Markdown) {
				target, ok := graphpkg.ResolveLink(item.Concept.Path, link.Target)
				if ok && seen[target] {
					add(item.Concept.Path)
				}
			}
		}
	}
	concepts := []Concept{}
	summaries := []ConceptSummary{}
	var omitted []OmittedItem
	remaining := opts.MaxTokens
	for _, item := range ordered {
		tokens := contextpack.EstimateTokens(item.Concept.Markdown)
		if tokens > remaining {
			omitted = append(omitted, OmittedItem{Path: item.Concept.Path, Reason: "token_budget"})
			continue
		}
		remaining -= tokens
		concepts = append(concepts, item.Concept)
		summaries = append(summaries, item.Summary)
	}
	return ContextPack{Path: scope.Path, Query: query, Concepts: concepts, Summaries: summaries, Omitted: omitted}, nil
}

func (w *LocalWorkspace) Graph(ctx context.Context, inputPath string, opts GraphOptions) (GraphResult, error) {
	depth, err := normalizeLinkDepth(opts.Depth)
	if err != nil {
		return GraphResult{}, err
	}
	scope, err := w.scopeWithView(ctx, inputPath, opts.View)
	if err != nil {
		return GraphResult{}, err
	}
	var target vfs.Target
	targetResolved := false
	_, resolved, err := w.resolveReadable(ctx, scope.Path)
	if err == nil {
		target = resolved
		targetResolved = true
	}
	allConcepts := scope.Concepts
	viewID := strings.TrimSpace(opts.View)
	if viewID == "" && targetResolved && target.Kind == TargetConcept {
		allConcepts, err = w.scopeForMount(target.Mount, "")
		if err != nil {
			return GraphResult{}, err
		}
	}
	byPath := map[string]scopedConcept{}
	for _, item := range allConcepts {
		byPath[item.Concept.Path] = item
	}
	nodeMap := map[string]ConceptSummary{}
	edges := []GraphEdge{}
	issues := []ValidationIssue{}
	addNode := func(summary ConceptSummary) {
		nodeMap[summary.Path] = summary
	}
	for _, item := range allConcepts {
		includeSource := !targetResolved || target.Kind != TargetConcept || item.Concept.Path == target.Path
		if includeSource {
			addNode(item.Summary)
		}
		if depth == 0 {
			continue
		}
		for _, link := range graphpkg.ExtractMarkdownLinks(item.Concept.Markdown) {
			targetPath, ok := graphpkg.ResolveLink(item.Concept.Path, link.Target)
			if !ok {
				continue
			}
			includeEdge := !targetResolved || target.Kind != TargetConcept || item.Concept.Path == target.Path || targetPath == target.Path
			if !includeEdge {
				continue
			}
			if target, exists := byPath[targetPath]; exists {
				addNode(item.Summary)
				addNode(target.Summary)
				edges = append(edges, GraphEdge{From: item.Concept.Path, To: targetPath, Kind: "markdown_link"})
			} else if viewID == "" {
				issues = append(issues, ValidationIssue{
					Severity:  "warning",
					Code:      "broken_link",
					Message:   "Broken Markdown link: " + link.Target,
					Path:      item.Concept.Path,
					ConceptID: item.Concept.ConceptID,
				})
			}
		}
	}
	nodes := make([]GraphNode, 0, len(nodeMap))
	for _, summary := range nodeMap {
		nodes = append(nodes, GraphNode{Concept: summary})
	}
	sort.Slice(nodes, func(i, j int) bool { return nodes[i].Concept.Path < nodes[j].Concept.Path })
	sort.Slice(edges, func(i, j int) bool {
		if edges[i].From == edges[j].From {
			return edges[i].To < edges[j].To
		}
		return edges[i].From < edges[j].From
	})
	return GraphResult{Path: scope.Path, Nodes: nodes, Edges: edges, Issues: issues}, nil
}

func normalizeLinkDepth(depth int) (int, error) {
	if depth < 0 || depth > 1 {
		return 0, NewError(ErrInvalidPath, "Depth must be 0 or 1 in Phase 1")
	}
	return depth, nil
}

func (w *LocalWorkspace) Validate(ctx context.Context, inputPath string, opts ValidateOptions) (ValidationResult, error) {
	if inputPath == "" {
		inputPath = "/"
	}
	normalized, err := vfs.NormalizePath(inputPath)
	if err != nil {
		return ValidationResult{}, NormalizeError(err)
	}
	if viewID := strings.TrimSpace(opts.View); viewID != "" {
		return w.validateViewScope(ctx, normalized, viewID)
	}
	metadataIssues, metadataBlocking, err := w.validateRootMetadata()
	if err != nil {
		return ValidationResult{}, err
	}
	if metadataBlocking {
		return ValidationResult{Path: normalized, Valid: false, Issues: metadataIssues}, nil
	}
	resultPath, concepts, issues, err := w.validatePathScope(ctx, normalized)
	if err != nil {
		return ValidationResult{}, err
	}
	issues = append(metadataIssues, issues...)
	issues = append(issues, linkIssues(concepts)...)
	return ValidationResult{Path: resultPath, Valid: !hasErrors(issues), Issues: issues}, nil
}

// ValidateRootBundle validates only the selected local root bundle. It does
// not hydrate or refresh mounted Git sources.
func (w *LocalWorkspace) ValidateRootBundle(ctx context.Context) (ValidationResult, error) {
	metadataIssues, metadataBlocking, err := w.validateRootMetadata()
	if err != nil {
		return ValidationResult{}, err
	}
	if metadataBlocking {
		return ValidationResult{Path: "/", Valid: false, Issues: metadataIssues}, nil
	}
	mounts, err := w.mounts()
	if err != nil {
		return ValidationResult{}, NormalizeError(err)
	}
	var root *vfs.Mount
	for i := range mounts {
		if mounts[i].MountPath == "/" {
			root = &mounts[i]
			break
		}
	}
	if root == nil {
		return ValidationResult{}, NewError(ErrMountNotFound, "Root bundle is not mounted")
	}
	if err := ensureReadable(*root); err != nil {
		return ValidationResult{}, err
	}
	concepts, issues, err := w.validateMountScope(*root, "")
	if err != nil {
		return ValidationResult{}, err
	}
	index, indexIssues := validateRootIndex(*root)
	if index != nil {
		concepts = append(concepts, *index)
	}
	issues = append(issues, indexIssues...)
	issues = append(metadataIssues, issues...)
	issues = append(issues, localRootLinkIssues(concepts, mounts)...)
	_ = ctx
	return ValidationResult{Path: "/", Valid: !hasErrors(issues), Issues: issues}, nil
}

func validateRootIndex(mount vfs.Mount) (*scopedConcept, []ValidationIssue) {
	filename := filepath.Join(mount.SourcePath, "index.md")
	info, err := os.Lstat(filename)
	if err != nil || !info.Mode().IsRegular() {
		return nil, []ValidationIssue{{
			Severity: "error",
			Code:     ErrValidationFailed,
			Message:  "Root bundle index.md is missing or is not a regular file",
			Path:     "/",
		}}
	}
	data, err := os.ReadFile(filename)
	if err != nil {
		return nil, []ValidationIssue{{Severity: "error", Code: ErrValidationFailed, Message: err.Error(), Path: "/"}}
	}
	doc, err := okf.ParseConcept("", data)
	if err != nil {
		return nil, []ValidationIssue{{Severity: "error", Code: ErrOKFParse, Message: "Invalid root index.md: " + err.Error(), Path: "/"}}
	}
	concept := conceptFromDoc(mount, doc, data)
	item := &scopedConcept{Concept: concept, Summary: summaryFromConcept(concept)}
	return item, validateDocument("/", doc)
}

func (w *LocalWorkspace) validateRootMetadata() ([]ValidationIssue, bool, error) {
	context, err := w.resolvedWorkspace()
	if err != nil {
		return nil, false, NormalizeError(err)
	}
	root := context.RootBundleDir

	var issues []ValidationIssue
	blocking := false
	if _, err := vfs.LoadDescriptorMounts(root); err != nil {
		issues = append(issues, ValidationIssue{
			Severity: "error",
			Code:     ErrValidationFailed,
			Message:  "Invalid mount descriptor: " + err.Error(),
			Path:     "/",
		})
		blocking = true
	}

	viewsFile := filepath.Join(context.WorkspaceDir, "factile.views.toml")
	if fileExists(viewsFile) {
		if _, err := loadViewsFile(viewsFile); err != nil {
			issues = append(issues, ValidationIssue{
				Severity: "error",
				Code:     ErrValidationFailed,
				Message:  "Invalid views file: " + err.Error(),
				Path:     "/",
				Details:  map[string]any{"file": "factile.views.toml"},
			})
		}
	}
	return issues, blocking, nil
}

func (w *LocalWorkspace) validatePathScope(ctx context.Context, normalized string) (string, []scopedConcept, []ValidationIssue, error) {
	mounts, issues, invalidMounts, err := w.mountsForValidationScope(ctx, normalized)
	if err != nil {
		return "", nil, nil, err
	}
	var concepts []scopedConcept
	if normalized == "/" {
		for _, mount := range mounts {
			if invalidMounts[mount.MountPath] {
				continue
			}
			if err := ensureReadable(mount); err != nil {
				return "", nil, nil, err
			}
			items, mountIssues, err := w.validateMountScope(mount, "")
			if err != nil {
				return "", nil, nil, err
			}
			concepts = append(concepts, items...)
			issues = append(issues, mountIssues...)
		}
		return "/", concepts, issues, nil
	}
	target, err := vfs.Resolve(mounts, normalized)
	if err != nil {
		selected := mountsForVirtualPath(mounts, normalized)
		if len(selected) == 0 {
			return "", nil, nil, NormalizeError(err)
		}
		for _, mount := range selected {
			if invalidMounts[mount.MountPath] {
				continue
			}
			if err := ensureReadable(mount); err != nil {
				return "", nil, nil, err
			}
			items, mountIssues, err := w.validateMountScope(mount, "")
			if err != nil {
				return "", nil, nil, err
			}
			concepts = append(concepts, items...)
			issues = append(issues, mountIssues...)
		}
		return normalized, concepts, issues, nil
	}
	if invalidMounts[target.Mount.MountPath] {
		return target.Path, concepts, issues, nil
	}
	if err := ensureReadable(target.Mount); err != nil {
		return "", nil, nil, err
	}
	if target.Kind == TargetConcept {
		item, conceptIssues, err := w.validateConcept(target.Mount, target.ConceptID)
		if err != nil {
			return "", nil, nil, err
		}
		if item != nil {
			concepts = append(concepts, *item)
		}
		issues = append(issues, conceptIssues...)
	} else {
		if target.Kind == TargetPath && !target.Exists {
			return "", nil, nil, errorf(ErrMountNotFound, "Path not found: %s", target.Path)
		}
		items, scopeIssues, err := w.validateMountScope(target.Mount, target.ConceptID)
		if err != nil {
			return "", nil, nil, err
		}
		concepts = append(concepts, items...)
		issues = append(issues, scopeIssues...)
		for _, mount := range mountsForVirtualPath(mounts, normalized) {
			if invalidMounts[mount.MountPath] {
				continue
			}
			if err := ensureReadable(mount); err != nil {
				return "", nil, nil, err
			}
			items, mountIssues, err := w.validateMountScope(mount, "")
			if err != nil {
				return "", nil, nil, err
			}
			concepts = append(concepts, items...)
			issues = append(issues, mountIssues...)
		}
	}
	return target.Path, concepts, issues, nil
}

func (w *LocalWorkspace) validateViewScope(ctx context.Context, inputPath string, viewID string) (ValidationResult, error) {
	normalized, selectedPaths, err := w.selectedViewPaths(inputPath, viewID)
	if err != nil {
		return ValidationResult{}, err
	}
	seenConcepts := map[string]bool{}
	seenIssues := map[string]bool{}
	var concepts []scopedConcept
	issues := []ValidationIssue{}
	addIssue := func(issue ValidationIssue) {
		key := issue.Severity + "\x00" + issue.Code + "\x00" + issue.Path + "\x00" + issue.ConceptID + "\x00" + issue.Message
		if seenIssues[key] {
			return
		}
		seenIssues[key] = true
		issues = append(issues, issue)
	}
	for _, selectedPath := range selectedPaths {
		_, items, itemIssues, err := w.validatePathScope(ctx, selectedPath)
		if err != nil {
			return ValidationResult{}, err
		}
		for _, item := range items {
			if seenConcepts[item.Concept.Path] {
				continue
			}
			seenConcepts[item.Concept.Path] = true
			concepts = append(concepts, item)
		}
		for _, issue := range itemIssues {
			addIssue(issue)
		}
	}
	for _, issue := range linkIssuesWithinScopes(concepts, selectedPaths) {
		addIssue(issue)
	}
	return ValidationResult{Path: normalized, Valid: !hasErrors(issues), Issues: issues}, nil
}

func (w *LocalWorkspace) Create(ctx context.Context, inputPath string, input CreateConceptInput) (ConceptResult, error) {
	_, target, err := w.resolveForConceptWrite(inputPath)
	if err != nil {
		return ConceptResult{}, err
	}
	_ = ctx
	if err := w.ensureWritable(target.Mount); err != nil {
		return ConceptResult{}, err
	}
	if input.Type == "" {
		return ConceptResult{}, errorf(ErrValidationFailed, "Concept type is required")
	}
	store, err := storage.NewLocal(target.Mount.SourcePath)
	if err != nil {
		return ConceptResult{}, NormalizeError(err)
	}
	file, err := store.ConceptFile(target.ConceptID)
	if err != nil {
		return ConceptResult{}, NormalizeError(err)
	}
	doc := okf.Document{
		ConceptID: target.ConceptID,
		Frontmatter: map[string]any{
			"type":  input.Type,
			"title": input.Title,
		},
		Order:    []string{"type", "title"},
		Markdown: input.Markdown,
	}
	if input.Description != "" {
		doc.Frontmatter["description"] = input.Description
		doc.Order = append(doc.Order, "description")
	}
	if len(input.Tags) > 0 {
		doc.Frontmatter["tags"] = input.Tags
		doc.Order = append(doc.Order, "tags")
	}
	if input.Resource != "" {
		doc.Frontmatter["resource"] = input.Resource
		doc.Order = append(doc.Order, "resource")
	}
	data := okf.Serialize(doc)
	if issues := validateDocument(target.Path, doc); hasErrors(issues) {
		return ConceptResult{}, validationError(issues)
	}
	err = w.withWorkspaceLocks([]string{file}, func() error {
		return store.CreateExclusive(target.ConceptID, data)
	})
	if err != nil {
		return ConceptResult{}, NormalizeError(err)
	}
	concept, err := w.readConcept(target.Mount, target.ConceptID)
	if err != nil {
		return ConceptResult{}, err
	}
	return ConceptResult{Concept: concept}, nil
}

func (w *LocalWorkspace) Mkdir(ctx context.Context, inputPath string, opts MkdirOptions) (DirectoryResult, error) {
	_ = ctx
	mounts, target, err := w.resolveForDirectoryWrite(inputPath)
	if err != nil {
		return DirectoryResult{}, err
	}
	if err := w.ensureWritable(target.Mount); err != nil {
		return DirectoryResult{}, err
	}
	if err := w.ensureDirectoryParent(mounts, target); err != nil {
		return DirectoryResult{}, err
	}
	store, err := storage.NewLocal(target.Mount.SourcePath)
	if err != nil {
		return DirectoryResult{}, NormalizeError(err)
	}
	files := mkdirScaffoldFiles(target.Path, target.RelPath, opts)
	directoryTarget := filepath.Join(store.Root, filepath.FromSlash(target.RelPath))
	err = w.withWorkspaceLocks([]string{directoryTarget}, func() error {
		return store.CreateDirectoryScaffold(target.RelPath, files.storage)
	})
	if err != nil {
		if errors.Is(err, os.ErrExist) {
			return DirectoryResult{}, NewError(ErrPathAlreadyExists, "Path already exists: "+target.Path)
		}
		if errors.Is(err, os.ErrNotExist) {
			return DirectoryResult{}, errorf(ErrMountNotFound, "Parent path not found: %s", path.Dir(target.Path))
		}
		return DirectoryResult{}, NormalizeError(err)
	}
	return DirectoryResult{Directory: Directory{Path: target.Path, Created: true, Files: files.logical}}, nil
}

func (w *LocalWorkspace) Write(ctx context.Context, inputPath string, input WriteConceptInput) (ConceptResult, error) {
	_, target, err := w.resolveExistingConceptWrite(inputPath)
	if err != nil {
		return ConceptResult{}, err
	}
	_ = ctx
	if input.ExpectedRevision == "" {
		return ConceptResult{}, NewError(ErrRevisionRequired, "Expected revision is required")
	}
	if err := w.ensureWritable(target.Mount); err != nil {
		return ConceptResult{}, err
	}
	store, err := storage.NewLocal(target.Mount.SourcePath)
	if err != nil {
		return ConceptResult{}, NormalizeError(err)
	}
	file, err := store.ConceptFile(target.ConceptID)
	if err != nil {
		return ConceptResult{}, NormalizeError(err)
	}
	err = w.withWorkspaceLocks([]string{file}, func() error {
		data, _, err := store.ReadConcept(target.ConceptID)
		if err != nil {
			return err
		}
		current := revision.DigestBytes(data)
		if current != input.ExpectedRevision {
			return NewError(ErrRevisionMismatch, "Revision mismatch")
		}
		doc, err := okf.ParseConcept(target.ConceptID, data)
		if err != nil {
			return err
		}
		doc.Markdown = input.Markdown
		if issues := validateDocument(target.Path, doc); hasErrors(issues) {
			return validationError(issues)
		}
		return store.AtomicReplace(target.ConceptID, okf.Serialize(doc))
	})
	if err != nil {
		return ConceptResult{}, NormalizeError(err)
	}
	concept, err := w.readConcept(target.Mount, target.ConceptID)
	if err != nil {
		return ConceptResult{}, err
	}
	return ConceptResult{Concept: concept}, nil
}

func (w *LocalWorkspace) Patch(ctx context.Context, inputPath string, input PatchConceptInput) (ConceptResult, error) {
	_, target, err := w.resolveExistingConceptWrite(inputPath)
	if err != nil {
		return ConceptResult{}, err
	}
	_ = ctx
	if input.ExpectedRevision == "" {
		return ConceptResult{}, NewError(ErrRevisionRequired, "Expected revision is required")
	}
	if err := w.ensureWritable(target.Mount); err != nil {
		return ConceptResult{}, err
	}
	store, err := storage.NewLocal(target.Mount.SourcePath)
	if err != nil {
		return ConceptResult{}, NormalizeError(err)
	}
	file, err := store.ConceptFile(target.ConceptID)
	if err != nil {
		return ConceptResult{}, NormalizeError(err)
	}
	err = w.withWorkspaceLocks([]string{file}, func() error {
		data, _, err := store.ReadConcept(target.ConceptID)
		if err != nil {
			return err
		}
		if revision.DigestBytes(data) != input.ExpectedRevision {
			return NewError(ErrRevisionMismatch, "Revision mismatch")
		}
		doc, err := okf.ParseConcept(target.ConceptID, data)
		if err != nil {
			return err
		}
		for key, value := range input.Set {
			if _, exists := doc.Frontmatter[key]; !exists {
				doc.Order = append(doc.Order, key)
			}
			doc.Frontmatter[key] = value
		}
		for _, key := range input.DeleteKeys {
			delete(doc.Frontmatter, key)
		}
		for heading, body := range input.ReplaceSections {
			next, err := patchpkg.ReplaceSection(doc.Markdown, heading, body)
			if err != nil {
				return NewError(ErrSectionNotFound, err.Error())
			}
			doc.Markdown = next
		}
		for heading, body := range input.AppendSections {
			doc.Markdown = patchpkg.AppendSection(doc.Markdown, heading, body)
		}
		if input.ReplaceBody != nil {
			doc.Markdown = *input.ReplaceBody
		}
		if issues := validateDocument(target.Path, doc); hasErrors(issues) {
			return validationError(issues)
		}
		return store.AtomicReplace(target.ConceptID, okf.Serialize(doc))
	})
	if err != nil {
		return ConceptResult{}, NormalizeError(err)
	}
	concept, err := w.readConcept(target.Mount, target.ConceptID)
	if err != nil {
		return ConceptResult{}, err
	}
	return ConceptResult{Concept: concept}, nil
}

func (w *LocalWorkspace) Rename(ctx context.Context, oldPath string, newPath string, opts RenameOptions) (RenameResult, error) {
	_, target, err := w.resolveExistingConceptWrite(oldPath)
	if err != nil {
		return RenameResult{}, err
	}
	_, newTarget, err := w.resolveForConceptWrite(newPath)
	if err != nil {
		return RenameResult{}, err
	}
	_ = ctx
	if opts.ExpectedRevision == "" {
		return RenameResult{}, NewError(ErrRevisionRequired, "Expected revision is required")
	}
	if target.Mount.MountPath != newTarget.Mount.MountPath {
		return RenameResult{}, errorf(ErrInvalidPath, "Rename must stay within one mounted bundle")
	}
	if err := w.ensureWritable(target.Mount); err != nil {
		return RenameResult{}, err
	}
	store, err := storage.NewLocal(target.Mount.SourcePath)
	if err != nil {
		return RenameResult{}, NormalizeError(err)
	}
	oldFile, err := store.ConceptFile(target.ConceptID)
	if err != nil {
		return RenameResult{}, NormalizeError(err)
	}
	newFile, err := store.ConceptFile(newTarget.ConceptID)
	if err != nil {
		return RenameResult{}, NormalizeError(err)
	}
	var warnings []ValidationIssue
	err = w.withWorkspaceLocks([]string{oldFile, newFile}, func() error {
		data, _, err := store.ReadConcept(target.ConceptID)
		if err != nil {
			return err
		}
		if revision.DigestBytes(data) != opts.ExpectedRevision {
			return NewError(ErrRevisionMismatch, "Revision mismatch")
		}
		if _, err := os.Stat(newFile); err == nil {
			return NewError(ErrConceptAlreadyExist, "Destination concept already exists")
		}
		warnings = w.backlinkWarnings(target.Mount, target.Path)
		return store.RenameConcept(target.ConceptID, newTarget.ConceptID)
	})
	if err != nil {
		return RenameResult{}, NormalizeError(err)
	}
	concept, err := w.readConcept(target.Mount, newTarget.ConceptID)
	if err != nil {
		return RenameResult{}, err
	}
	return RenameResult{Concept: concept, Warnings: warnings}, nil
}

func (w *LocalWorkspace) Delete(ctx context.Context, inputPath string, opts DeleteOptions) (DeleteResult, error) {
	_, target, err := w.resolveExistingConceptWrite(inputPath)
	if err != nil {
		return DeleteResult{}, err
	}
	_ = ctx
	if opts.ExpectedRevision == "" {
		return DeleteResult{}, NewError(ErrRevisionRequired, "Expected revision is required")
	}
	if err := w.ensureWritable(target.Mount); err != nil {
		return DeleteResult{}, err
	}
	store, err := storage.NewLocal(target.Mount.SourcePath)
	if err != nil {
		return DeleteResult{}, NormalizeError(err)
	}
	file, err := store.ConceptFile(target.ConceptID)
	if err != nil {
		return DeleteResult{}, NormalizeError(err)
	}
	err = w.withWorkspaceLocks([]string{file}, func() error {
		data, _, err := store.ReadConcept(target.ConceptID)
		if err != nil {
			return err
		}
		if revision.DigestBytes(data) != opts.ExpectedRevision {
			return NewError(ErrRevisionMismatch, "Revision mismatch")
		}
		return store.DeleteConcept(target.ConceptID)
	})
	if err != nil {
		return DeleteResult{}, NormalizeError(err)
	}
	return DeleteResult{Path: target.Path, Deleted: true}, nil
}

func (w *LocalWorkspace) Deprecate(ctx context.Context, inputPath string, opts DeprecateOptions) (ConceptResult, error) {
	if opts.Reason == "" {
		return ConceptResult{}, errorf(ErrValidationFailed, "Deprecation reason is required")
	}
	return w.Patch(ctx, inputPath, PatchConceptInput{
		ExpectedRevision: opts.ExpectedRevision,
		Set: map[string]any{
			"deprecated":        true,
			"deprecated_reason": opts.Reason,
		},
		AppendSections: map[string]string{
			"Deprecation": opts.Reason,
		},
	})
}

func (w *LocalWorkspace) Mount(ctx context.Context, source string, mountPath string, opts MountOptions) (MountResult, error) {
	normalized, err := vfs.ValidateMountPath(mountPath)
	if err != nil {
		return MountResult{}, NormalizeError(err)
	}
	classification, err := vfs.ClassifySource(source)
	if err != nil {
		return MountResult{}, NormalizeError(err)
	}
	kind := classification.Kind
	if opts.Kind != "" && opts.Kind != kind {
		return MountResult{}, NewError(ErrUnsupportedSource, "Mount source kind does not match its syntax")
	}
	if opts.Writable && kind != vfs.SourceKindLocal {
		if kind == vfs.SourceKindGit {
			return MountResult{}, NewError(ErrSourceReadOnly, "Git sources are always read-only.")
		}
		return MountResult{}, NewError(ErrSourceReadOnly, "Only local sources can be mounted writable")
	}
	if kind == vfs.SourceKindLocal && (opts.RefSet || opts.Ref != "" || opts.RevisionSet || opts.Revision != "") {
		return MountResult{}, NewError(ErrValidationFailed, "Git selectors require a Git source")
	}
	if kind == vfs.SourceKindFactile {
		return MountResult{}, NewError(ErrUnsupportedSource, "Remote sources are not implemented in Phase 1")
	}
	context, err := w.resolvedWorkspace()
	if err != nil {
		return MountResult{}, NormalizeError(err)
	}
	root := context.RootBundleDir
	descriptorPath, err := vfs.MountDescriptorPath(root, normalized)
	if err != nil {
		return MountResult{}, NormalizeError(err)
	}
	var result MountResult
	err = w.withWorkspaceLocks([]string{descriptorPath}, func() error {
		if err := ensureRootMountTargetAvailable(root, normalized); err != nil {
			return err
		}
		sourcePath, err := w.resolveMountSource(ctx, context, source, normalized, kind, filepath.Dir(descriptorPath), opts)
		if err != nil {
			return err
		}
		validated, err := vfs.ValidateWorkspaceMount(context, vfs.Mount{
			MountPath:  normalized,
			Source:     source,
			Kind:       kind,
			SourcePath: sourcePath,
		})
		if err != nil {
			return err
		}
		sourcePath = validated.SourcePath
		resolved := applyMountMetadataDefaults(sourcePath, normalized, opts)
		mount := vfs.Mount{
			MountPath:    normalized,
			Source:       source,
			Writable:     resolved.Writable,
			Title:        resolved.Title,
			Description:  resolved.Description,
			WhenToUse:    resolved.WhenToUse,
			WhenNotToUse: resolved.WhenNotToUse,
			Version:      resolved.Version,
			Ref:          resolved.Ref,
			Revision:     resolved.Revision,
			VersionSet:   resolved.VersionSet,
			RefSet:       resolved.RefSet,
			RevisionSet:  resolved.RevisionSet,
			Trust:        resolved.Trust,
		}
		written, err := vfs.WriteMountDescriptorFile(root, mount)
		if err != nil {
			return err
		}
		result = MountResult{Mount: written}
		return nil
	})
	if err != nil {
		return MountResult{}, NormalizeError(err)
	}
	return result, nil
}

func (w *LocalWorkspace) Unmount(ctx context.Context, mountPath string, opts UnmountOptions) (UnmountResult, error) {
	_, _ = ctx, opts
	normalized, err := vfs.ValidateMountPath(mountPath)
	if err != nil {
		return UnmountResult{}, NormalizeError(err)
	}
	context, err := w.resolvedWorkspace()
	if err != nil {
		return UnmountResult{}, NormalizeError(err)
	}
	descriptorPath, err := vfs.MountDescriptorPath(context.RootBundleDir, normalized)
	if err != nil {
		return UnmountResult{}, NormalizeError(err)
	}
	result := UnmountResult{MountPath: normalized}
	err = w.withWorkspaceLocks([]string{descriptorPath}, func() error {
		if err := os.Remove(descriptorPath); err != nil {
			if errors.Is(err, os.ErrNotExist) {
				return nil
			}
			return err
		}
		result.Removed = true
		return nil
	})
	if err != nil {
		return UnmountResult{}, NormalizeError(err)
	}
	return result, nil
}

func (w *LocalWorkspace) ListMounts(ctx context.Context) (MountListResult, error) {
	mounts, err := w.mounts()
	if err != nil {
		return MountListResult{}, NormalizeError(err)
	}
	out := make([]vfs.Mount, 0, len(mounts))
	for _, mount := range mounts {
		if mount.MountPath == "/" {
			continue
		}
		if mount.Kind == vfs.SourceKindGit {
			status, statusErr := w.gitSourceStatus(mount)
			if statusErr != nil {
				status = invalidGitStatus(mount)
				mount.Source = status.Source
			}
			mount.SourceStatus = &status
		}
		out = append(out, mount)
	}
	return MountListResult{Mounts: out}, nil
}

func (w *LocalWorkspace) Refresh(ctx context.Context, mountPath string) (RefreshResult, error) {
	normalized, err := vfs.ValidateMountPath(mountPath)
	if err != nil {
		return RefreshResult{}, NormalizeError(err)
	}
	mounts, err := w.mounts()
	if err != nil {
		return RefreshResult{}, NormalizeError(err)
	}
	mount, ok := mountByPath(mounts, normalized)
	if !ok {
		return RefreshResult{}, NewError(ErrMountNotFound, "Mount not found: "+normalized)
	}
	if mount.Kind != vfs.SourceKindGit {
		return RefreshResult{}, NewError(ErrUnsupportedSource, "Only Git mounts can be refreshed")
	}
	workspace, err := w.resolvedWorkspace()
	if err != nil {
		return RefreshResult{}, NormalizeError(err)
	}
	cache, err := gitsource.OpenCache(workspace, gitsource.NewRunner())
	if err != nil {
		return RefreshResult{}, NormalizeError(err)
	}
	result, err := cache.Refresh(ctx, gitIntent(mount))
	if err != nil {
		return RefreshResult{}, normalizeGitSourceError(err)
	}
	return RefreshResult{MountPath: result.MountPath, Outcome: result.Outcome, Status: result.Status, Warning: result.Warning}, nil
}

func (w *LocalWorkspace) InspectBundle(ctx context.Context, source string) (BundleInspectResult, error) {
	_ = ctx
	if strings.HasPrefix(source, "factile://") {
		return BundleInspectResult{}, NewError(ErrUnsupportedSource, "Remote sources are not implemented in Phase 1")
	}
	store, err := storage.NewLocal(source)
	if err != nil {
		return BundleInspectResult{}, NormalizeError(err)
	}
	manifest, err := vfs.LoadManifest(store.Root)
	if err != nil || manifest.Bundle == nil {
		return BundleInspectResult{}, NormalizeError(&vfs.Error{
			Code:    vfs.ErrInvalidBundle,
			Message: "Source directory has no valid bundle factile.toml.",
			Details: map[string]string{"bundle": store.Root},
		})
	}
	ids, err := store.ListConceptIDs("")
	if err != nil {
		return BundleInspectResult{}, NormalizeError(err)
	}
	var concepts []ConceptSummary
	var issues []ValidationIssue
	mount := vfs.Mount{MountPath: "/inspect", Source: source, SourcePath: store.Root, Kind: "local", Writable: true}
	for _, id := range ids {
		summary, err := w.summaryForID(store, mount, id)
		if err != nil {
			issues = append(issues, ValidationIssue{Severity: "error", Code: ErrOKFParse, Message: err.Error(), ConceptID: id})
			continue
		}
		concepts = append(concepts, summary)
	}
	return BundleInspectResult{Source: source, Kind: "local", PlausibleOKF: len(ids) > 0 || fileExists(filepath.Join(store.Root, "index.md")), Concepts: concepts, Issues: issues}, nil
}

func (w *LocalWorkspace) FindBundles(ctx context.Context, startPath string) (BundleFindResult, error) {
	_ = ctx
	if startPath == "" {
		startPath = "."
	}
	abs, err := filepath.Abs(startPath)
	if err != nil {
		return BundleFindResult{}, NormalizeError(err)
	}
	var sources []string
	err = filepath.WalkDir(abs, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() {
			return nil
		}
		name := d.Name()
		if name == vfs.StateDirname || name == ".git" || (strings.HasPrefix(name, ".") && p != abs) {
			return filepath.SkipDir
		}
		manifest, manifestErr := vfs.LoadManifest(p)
		if manifestErr == nil && manifest.Bundle != nil {
			sources = append(sources, p)
		}
		return nil
	})
	if err != nil {
		return BundleFindResult{}, NormalizeError(err)
	}
	sort.Strings(sources)
	return BundleFindResult{StartPath: startPath, Sources: sources}, nil
}

type scopedConcept struct {
	Concept Concept
	Summary ConceptSummary
}

type scopedSet struct {
	Path      string
	Paths     []string
	Concepts  []scopedConcept
	Summaries []ConceptSummary
}

func (w *LocalWorkspace) scope(ctx context.Context, inputPath string) (scopedSet, error) {
	if inputPath == "" {
		inputPath = "/"
	}
	normalized, err := vfs.NormalizePath(inputPath)
	if err != nil {
		return scopedSet{}, NormalizeError(err)
	}
	mounts, err := w.mountsForScope(ctx, normalized)
	if err != nil {
		return scopedSet{}, NormalizeError(err)
	}
	var scoped scopedSet
	scoped.Path = normalized
	target, err := vfs.Resolve(mounts, normalized)
	if err != nil {
		selected := mountsForVirtualPath(mounts, normalized)
		if len(selected) == 0 {
			return scopedSet{}, NormalizeError(err)
		}
		for _, mount := range selected {
			if err := ensureReadable(mount); err != nil {
				return scopedSet{}, err
			}
			items, err := w.scopeForMount(mount, "")
			if err != nil {
				return scopedSet{}, err
			}
			scoped.Concepts = append(scoped.Concepts, items...)
		}
	} else if target.Kind == TargetVirtualRoot {
		for _, mount := range mounts {
			if err := ensureReadable(mount); err != nil {
				return scopedSet{}, err
			}
			items, err := w.scopeForMount(mount, "")
			if err != nil {
				return scopedSet{}, err
			}
			scoped.Concepts = append(scoped.Concepts, items...)
		}
	} else {
		if err := ensureReadable(target.Mount); err != nil {
			return scopedSet{}, err
		}
		if target.Kind == TargetConcept {
			concept, err := w.readConcept(target.Mount, target.ConceptID)
			if err != nil {
				return scopedSet{}, err
			}
			summary := summaryFromConcept(concept)
			scoped.Concepts = append(scoped.Concepts, scopedConcept{Concept: concept, Summary: summary})
			scoped.Summaries = append(scoped.Summaries, summary)
			return scoped, nil
		}
		prefix := target.ConceptID
		items, err := w.scopeForMount(target.Mount, prefix)
		if err != nil {
			return scopedSet{}, err
		}
		scoped.Concepts = append(scoped.Concepts, items...)
		for _, mount := range mountsForVirtualPath(mounts, normalized) {
			if err := ensureReadable(mount); err != nil {
				return scopedSet{}, err
			}
			items, err := w.scopeForMount(mount, "")
			if err != nil {
				return scopedSet{}, err
			}
			scoped.Concepts = append(scoped.Concepts, items...)
		}
	}
	sort.Slice(scoped.Concepts, func(i, j int) bool {
		return scoped.Concepts[i].Concept.Path < scoped.Concepts[j].Concept.Path
	})
	for _, item := range scoped.Concepts {
		scoped.Summaries = append(scoped.Summaries, item.Summary)
	}
	return scoped, nil
}

func (w *LocalWorkspace) scopeForMount(mount vfs.Mount, prefix string) ([]scopedConcept, error) {
	store, err := storage.NewLocal(mount.SourcePath)
	if err != nil {
		return nil, NormalizeError(err)
	}
	ids, err := store.ListConceptIDs(prefix)
	if err != nil {
		return nil, NormalizeError(err)
	}
	items := make([]scopedConcept, 0, len(ids))
	for _, id := range ids {
		concept, err := w.readConcept(mount, id)
		if err != nil {
			return nil, err
		}
		items = append(items, scopedConcept{Concept: concept, Summary: summaryFromConcept(concept)})
	}
	return items, nil
}

func (w *LocalWorkspace) readConcept(mount vfs.Mount, conceptID string) (Concept, error) {
	store, err := storage.NewLocal(mount.SourcePath)
	if err != nil {
		return Concept{}, NormalizeError(err)
	}
	data, _, err := store.ReadConcept(conceptID)
	if err != nil {
		return Concept{}, NormalizeError(err)
	}
	doc, err := okf.ParseConcept(conceptID, data)
	if err != nil {
		return Concept{}, NormalizeError(err)
	}
	return conceptFromDoc(mount, doc, data), nil
}

func (w *LocalWorkspace) summaryForID(store storage.Local, mount vfs.Mount, conceptID string) (ConceptSummary, error) {
	data, _, err := store.ReadConcept(conceptID)
	if err != nil {
		return ConceptSummary{}, err
	}
	doc, err := okf.ParseConcept(conceptID, data)
	if err != nil {
		return ConceptSummary{}, err
	}
	return summaryFromDoc(mount, doc, data), nil
}

func immediateMountFolders(mounts []vfs.Mount, current string) []FolderSummary {
	seen := map[string]bool{}
	var folders []FolderSummary
	for _, mount := range mounts {
		if mount.MountPath == current {
			continue
		}
		child, ok := immediateChildPath(current, mount.MountPath)
		if !ok || seen[child] {
			continue
		}
		seen[child] = true
		folders = append(folders, FolderSummary{Path: child, Title: titleFromPath(child)})
	}
	return folders
}

func mountsForVirtualPath(mounts []vfs.Mount, current string) []vfs.Mount {
	var selected []vfs.Mount
	for _, mount := range mounts {
		if current == "/" || strings.HasPrefix(mount.MountPath, current+"/") {
			selected = append(selected, mount)
		}
	}
	sort.Slice(selected, func(i, j int) bool {
		return selected[i].MountPath < selected[j].MountPath
	})
	return selected
}

func mountByPath(mounts []vfs.Mount, mountPath string) (vfs.Mount, bool) {
	for _, mount := range mounts {
		if mount.MountPath == mountPath {
			return mount, true
		}
	}
	return vfs.Mount{}, false
}

func mountMatchesPath(mountPath string, normalized string) bool {
	if mountPath == "/" {
		return normalized != "/"
	}
	return normalized == mountPath || strings.HasPrefix(normalized, mountPath+"/")
}

func mountRelativePath(mount vfs.Mount, normalized string) string {
	if mount.MountPath == "/" {
		return strings.TrimPrefix(normalized, "/")
	}
	return strings.TrimPrefix(normalized, mount.MountPath+"/")
}

func immediateChildPath(current string, candidate string) (string, bool) {
	if current == "/" {
		rest := strings.TrimPrefix(candidate, "/")
		if rest == "" {
			return "", false
		}
		first := strings.Split(rest, "/")[0]
		return "/" + first, true
	}
	if !strings.HasPrefix(candidate, current+"/") {
		return "", false
	}
	rest := strings.TrimPrefix(candidate, current+"/")
	if rest == "" {
		return "", false
	}
	first := strings.Split(rest, "/")[0]
	return current + "/" + first, true
}

func immediateConceptEntry(current string, candidate string) (string, bool, bool) {
	if current == "/" {
		rest := strings.TrimPrefix(candidate, "/")
		if rest == "" {
			return "", false, false
		}
		if !strings.Contains(rest, "/") {
			return candidate, true, true
		}
		first := strings.Split(rest, "/")[0]
		return "/" + first, false, true
	}
	if !strings.HasPrefix(candidate, current+"/") {
		return "", false, false
	}
	rest := strings.TrimPrefix(candidate, current+"/")
	if rest == "" {
		return "", false, false
	}
	if !strings.Contains(rest, "/") {
		return candidate, true, true
	}
	first := strings.Split(rest, "/")[0]
	return current + "/" + first, false, true
}

func documentSummaryFromConcept(summary ConceptSummary) DocumentSummary {
	return DocumentSummary{
		Path:        summary.Path,
		Type:        summary.Type,
		Title:       summary.Title,
		Description: summary.Description,
		Tags:        summary.Tags,
		Resource:    summary.Resource,
		Revision:    summary.Revision,
	}
}

func sortFolderSummaries(folders []FolderSummary) {
	sort.Slice(folders, func(i, j int) bool {
		return folders[i].Path < folders[j].Path
	})
}

func sortDocumentSummaries(documents []DocumentSummary) {
	sort.Slice(documents, func(i, j int) bool {
		return documents[i].Path < documents[j].Path
	})
}

func titleFromPath(p string) string {
	base := path.Base(p)
	base = strings.ReplaceAll(base, "-", " ")
	base = strings.ReplaceAll(base, "_", " ")
	parts := strings.Fields(base)
	for i, part := range parts {
		if part == "" {
			continue
		}
		parts[i] = strings.ToUpper(part[:1]) + part[1:]
	}
	return strings.Join(parts, " ")
}

func summaryFromConcept(concept Concept) ConceptSummary {
	return ConceptSummary{
		Path:        concept.Path,
		ConceptID:   concept.ConceptID,
		Type:        okf.StringField(concept.Frontmatter, "type"),
		Title:       okf.StringField(concept.Frontmatter, "title"),
		Description: okf.StringField(concept.Frontmatter, "description"),
		Tags:        okf.StringSliceField(concept.Frontmatter, "tags"),
		Resource:    okf.StringField(concept.Frontmatter, "resource"),
		Revision:    concept.Revision,
	}
}

func summaryFromDoc(mount vfs.Mount, doc okf.Document, data []byte) ConceptSummary {
	concept := conceptFromDoc(mount, doc, data)
	return summaryFromConcept(concept)
}

func conceptFromDoc(mount vfs.Mount, doc okf.Document, data []byte) Concept {
	return Concept{
		Path:        cleanVirtualJoin(mount.MountPath, doc.ConceptID),
		ConceptID:   doc.ConceptID,
		Revision:    revision.DigestBytes(data),
		Frontmatter: doc.Frontmatter,
		Markdown:    doc.Markdown,
	}
}

func validateScope(scope scopedSet) []ValidationIssue {
	var issues []ValidationIssue
	byPath := map[string]bool{}
	for _, item := range scope.Concepts {
		byPath[item.Concept.Path] = true
		issues = append(issues, validateDocument(item.Concept.Path, okf.Document{
			ConceptID:   item.Concept.ConceptID,
			Frontmatter: item.Concept.Frontmatter,
			Markdown:    item.Concept.Markdown,
		})...)
	}
	for _, item := range scope.Concepts {
		for _, link := range graphpkg.ExtractMarkdownLinks(item.Concept.Markdown) {
			target, ok := graphpkg.ResolveLink(item.Concept.Path, link.Target)
			if ok && !byPath[target] {
				issues = append(issues, ValidationIssue{
					Severity:  "warning",
					Code:      "broken_link",
					Message:   "Broken Markdown link: " + link.Target,
					Path:      item.Concept.Path,
					ConceptID: item.Concept.ConceptID,
				})
			}
		}
	}
	return issues
}

func (w *LocalWorkspace) validateMountScope(mount vfs.Mount, prefix string) ([]scopedConcept, []ValidationIssue, error) {
	store, err := storage.NewLocal(mount.SourcePath)
	if err != nil {
		return nil, nil, NormalizeError(err)
	}
	ids, err := store.ListConceptIDs(prefix)
	if err != nil {
		return nil, nil, NormalizeError(err)
	}
	var concepts []scopedConcept
	issues := []ValidationIssue{}
	for _, id := range ids {
		item, conceptIssues, err := w.validateConcept(mount, id)
		if err != nil {
			return nil, nil, err
		}
		if item != nil {
			concepts = append(concepts, *item)
		}
		issues = append(issues, conceptIssues...)
	}
	return concepts, issues, nil
}

func (w *LocalWorkspace) validateConcept(mount vfs.Mount, conceptID string) (*scopedConcept, []ValidationIssue, error) {
	store, err := storage.NewLocal(mount.SourcePath)
	if err != nil {
		return nil, nil, NormalizeError(err)
	}
	data, _, err := store.ReadConcept(conceptID)
	if err != nil {
		return nil, nil, NormalizeError(err)
	}
	doc, err := okf.ParseConcept(conceptID, data)
	if err != nil {
		return nil, []ValidationIssue{{
			Severity:  "error",
			Code:      ErrOKFParse,
			Message:   err.Error(),
			Path:      cleanVirtualJoin(mount.MountPath, okf.NormalizeConceptID(conceptID)),
			ConceptID: okf.NormalizeConceptID(conceptID),
		}}, nil
	}
	concept := conceptFromDoc(mount, doc, data)
	item := scopedConcept{Concept: concept, Summary: summaryFromConcept(concept)}
	return &item, validateDocument(concept.Path, doc), nil
}

func linkIssues(concepts []scopedConcept) []ValidationIssue {
	byPath := map[string]bool{}
	for _, item := range concepts {
		byPath[item.Concept.Path] = true
	}
	return linkIssuesAgainst(concepts, byPath)
}

func localRootLinkIssues(concepts []scopedConcept, mounts []vfs.Mount) []ValidationIssue {
	byPath := map[string]bool{}
	for _, item := range concepts {
		byPath[item.Concept.Path] = true
	}
	rootSource := ""
	for _, mount := range mounts {
		if mount.MountPath == "/" {
			rootSource = mount.SourcePath
			break
		}
	}
	var issues []ValidationIssue
	for _, item := range concepts {
		for _, link := range graphpkg.ExtractMarkdownLinks(item.Concept.Markdown) {
			target, ok := graphpkg.ResolveLink(item.Concept.Path, link.Target)
			if !ok || byPath[target] || localReservedFileExists(rootSource, target) || coveredByNonRootMount(target, mounts) {
				continue
			}
			issues = append(issues, ValidationIssue{
				Severity:  "warning",
				Code:      "broken_link",
				Message:   "Broken Markdown link: " + link.Target,
				Path:      item.Concept.Path,
				ConceptID: item.Concept.ConceptID,
			})
		}
	}
	return issues
}

func localReservedFileExists(rootSource, target string) bool {
	if rootSource == "" || target == "/" || !okf.IsReservedFile(path.Base(target)+".md") {
		return false
	}
	filename := filepath.Join(rootSource, filepath.FromSlash(strings.TrimPrefix(target, "/"))+".md")
	info, err := os.Lstat(filename)
	return err == nil && info.Mode().IsRegular()
}

func coveredByNonRootMount(target string, mounts []vfs.Mount) bool {
	for _, mount := range mounts {
		if mount.MountPath != "/" && mountMatchesPath(mount.MountPath, target) {
			return true
		}
	}
	return false
}

func linkIssuesAgainst(concepts []scopedConcept, byPath map[string]bool) []ValidationIssue {
	var issues []ValidationIssue
	for _, item := range concepts {
		for _, link := range graphpkg.ExtractMarkdownLinks(item.Concept.Markdown) {
			target, ok := graphpkg.ResolveLink(item.Concept.Path, link.Target)
			if ok && !byPath[target] {
				issues = append(issues, ValidationIssue{
					Severity:  "warning",
					Code:      "broken_link",
					Message:   "Broken Markdown link: " + link.Target,
					Path:      item.Concept.Path,
					ConceptID: item.Concept.ConceptID,
				})
			}
		}
	}
	return issues
}

func linkIssuesWithinScopes(concepts []scopedConcept, scopes []string) []ValidationIssue {
	byPath := map[string]bool{}
	for _, item := range concepts {
		byPath[item.Concept.Path] = true
	}
	var issues []ValidationIssue
	for _, item := range concepts {
		for _, link := range graphpkg.ExtractMarkdownLinks(item.Concept.Markdown) {
			target, ok := graphpkg.ResolveLink(item.Concept.Path, link.Target)
			if !ok || !pathInAnyScope(target, scopes) || byPath[target] {
				continue
			}
			issues = append(issues, ValidationIssue{
				Severity:  "warning",
				Code:      "broken_link",
				Message:   "Broken Markdown link: " + link.Target,
				Path:      item.Concept.Path,
				ConceptID: item.Concept.ConceptID,
			})
		}
	}
	return issues
}

func pathInAnyScope(candidate string, scopes []string) bool {
	for _, scope := range scopes {
		if candidate == scope || strings.HasPrefix(candidate, scope+"/") {
			return true
		}
	}
	return false
}

func validateDocument(path string, doc okf.Document) []ValidationIssue {
	var issues []ValidationIssue
	if strings.TrimSpace(okf.StringField(doc.Frontmatter, "type")) == "" {
		issues = append(issues, ValidationIssue{
			Severity:  "error",
			Code:      "missing_type",
			Message:   "Concept frontmatter must include non-empty type",
			Path:      path,
			ConceptID: doc.ConceptID,
		})
	}
	return issues
}

type mkdirFiles struct {
	storage []storage.ScaffoldFile
	logical []string
}

func mkdirScaffoldFiles(logicalPath string, rel string, opts MkdirOptions) mkdirFiles {
	if opts.Bundle {
		opts.Log = true
		opts.Overview = true
	}
	title := strings.TrimSpace(opts.Title)
	if title == "" {
		title = titleFromPath(logicalPath)
	}
	var files mkdirFiles
	add := func(name string, data []byte) {
		files.storage = append(files.storage, storage.ScaffoldFile{Name: name, Data: data})
		files.logical = append(files.logical, path.Join(logicalPath, name))
	}
	add("index.md", mkdirIndexMarkdown(title, opts.Bundle))
	if opts.Log {
		add("log.md", mkdirLogMarkdown(title))
	}
	if opts.Overview {
		add("overview.md", mkdirOverviewMarkdown(rel, title))
	}
	return files
}

func mkdirIndexMarkdown(title string, bundle bool) []byte {
	if bundle {
		return []byte("---\nokf_version: \"0.1\"\ntitle: " + okf.FormatValue(title) + "\n---\n\n# " + title + "\n")
	}
	frontmatter := map[string]any{"title": title}
	order := []string{"title"}
	return okf.Serialize(okf.Document{
		Frontmatter: frontmatter,
		Order:       order,
		Markdown:    "# " + title + "\n",
	})
}

func mkdirLogMarkdown(title string) []byte {
	return okf.Serialize(okf.Document{
		Frontmatter: map[string]any{"title": title + " Log"},
		Order:       []string{"title"},
		Markdown:    "# " + title + " Log\n\n- Created directory scaffold.\n",
	})
}

func mkdirOverviewMarkdown(rel string, title string) []byte {
	overviewTitle := title + " Overview"
	return okf.Serialize(okf.Document{
		ConceptID: rel + "/overview",
		Frontmatter: map[string]any{
			"type":  "Reference",
			"title": overviewTitle,
		},
		Order:    []string{"type", "title"},
		Markdown: "# " + overviewTitle + "\n",
	})
}

func loadRegistryForMutation(registryPath string) ([]vfs.Mount, error) {
	mounts, err := vfs.LoadRegistryFile(registryPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	return mounts, nil
}

func ensureRootMountTargetAvailable(root string, mountPath string) error {
	rel := strings.TrimPrefix(mountPath, "/")
	file := filepath.Join(root, filepath.FromSlash(rel)+".md")
	dir := filepath.Join(root, filepath.FromSlash(rel))
	if fileExists(file) || dirExistsLocal(dir) {
		return errorf(ErrAmbiguousTarget, "Path is both root path and mount: %s", mountPath)
	}
	return nil
}

func dirExistsLocal(name string) bool {
	info, err := os.Stat(name)
	return err == nil && info.IsDir()
}

func validationError(issues []ValidationIssue) *AppError {
	return &AppError{Code: ErrValidationFailed, Message: "Validation failed", Details: map[string]any{"issues": issues}}
}

func hasErrors(issues []ValidationIssue) bool {
	for _, issue := range issues {
		if issue.Severity == "error" {
			return true
		}
	}
	return false
}

func (w *LocalWorkspace) backlinkWarnings(mount vfs.Mount, oldPath string) []ValidationIssue {
	items, err := w.scopeForMount(mount, "")
	if err != nil {
		return nil
	}
	var warnings []ValidationIssue
	for _, item := range items {
		for _, link := range graphpkg.ExtractMarkdownLinks(item.Concept.Markdown) {
			target, ok := graphpkg.ResolveLink(item.Concept.Path, link.Target)
			if ok && target == oldPath {
				warnings = append(warnings, ValidationIssue{
					Severity:  "warning",
					Code:      "backlink_not_updated",
					Message:   "Rename does not update links in Phase 1",
					Path:      item.Concept.Path,
					ConceptID: item.Concept.ConceptID,
				})
			}
		}
	}
	return warnings
}

func (w *LocalWorkspace) resolve(inputPath string) ([]vfs.Mount, vfs.Target, error) {
	mounts, err := w.mounts()
	if err != nil {
		return nil, vfs.Target{}, NormalizeError(err)
	}
	target, err := vfs.Resolve(mounts, inputPath)
	if err != nil {
		return nil, vfs.Target{}, NormalizeError(err)
	}
	return mounts, target, nil
}

func (w *LocalWorkspace) resolveReadable(ctx context.Context, inputPath string) ([]vfs.Mount, vfs.Target, error) {
	normalized, err := vfs.NormalizePath(inputPath)
	if err != nil {
		return nil, vfs.Target{}, NormalizeError(err)
	}
	mounts, err := w.mountsForTarget(ctx, normalized)
	if err != nil {
		return nil, vfs.Target{}, err
	}
	target, err := vfs.Resolve(mounts, normalized)
	if err != nil {
		return nil, vfs.Target{}, NormalizeError(err)
	}
	return mounts, target, nil
}

func (w *LocalWorkspace) resolveForConceptWrite(inputPath string) ([]vfs.Mount, vfs.Target, error) {
	mounts, err := w.mounts()
	if err != nil {
		return nil, vfs.Target{}, NormalizeError(err)
	}
	normalized, err := vfs.NormalizePath(inputPath)
	if err != nil {
		return nil, vfs.Target{}, NormalizeError(err)
	}
	target, err := vfs.Resolve(mounts, normalized)
	if err == nil && target.Kind == TargetConcept {
		return nil, vfs.Target{}, NewError(ErrConceptAlreadyExist, "Concept already exists: "+normalized)
	}
	if err != nil {
		var vfsErr *vfs.Error
		if errors.As(err, &vfsErr) && vfsErr.Code != ErrMountNotFound {
			return nil, vfs.Target{}, NormalizeError(err)
		}
	}
	selected, conceptID, err := w.mountAndConceptID(mounts, normalized)
	if err != nil {
		return nil, vfs.Target{}, err
	}
	return mounts, vfs.Target{Kind: TargetConcept, Path: normalized, Mount: selected, ConceptID: conceptID, Exists: false}, nil
}

func (w *LocalWorkspace) resolveForDirectoryWrite(inputPath string) ([]vfs.Mount, vfs.Target, error) {
	mounts, err := w.mounts()
	if err != nil {
		return nil, vfs.Target{}, NormalizeError(err)
	}
	normalized, err := vfs.NormalizePath(inputPath)
	if err != nil {
		return nil, vfs.Target{}, NormalizeError(err)
	}
	if normalized == "/" {
		return nil, vfs.Target{}, errorf(ErrInvalidPath, "Directory path must not be /")
	}
	selected, rel, err := w.mountAndRelativePath(mounts, normalized)
	if err != nil {
		return nil, vfs.Target{}, err
	}
	if rel == "" {
		return nil, vfs.Target{}, NewError(ErrPathAlreadyExists, "Path already exists: "+normalized)
	}
	if selected.Kind != "" && selected.Kind != "local" {
		if err := w.ensureWritable(selected); err != nil {
			return nil, vfs.Target{}, err
		}
	}
	target, err := vfs.Resolve(mounts, normalized)
	if err != nil {
		return nil, vfs.Target{}, NormalizeError(err)
	}
	switch target.Kind {
	case TargetConcept:
		return nil, vfs.Target{}, NewError(ErrConceptAlreadyExist, "Concept already exists: "+normalized)
	case TargetPath:
		if target.Exists {
			return nil, vfs.Target{}, NewError(ErrPathAlreadyExists, "Path already exists: "+normalized)
		}
	case TargetBundle:
		return nil, vfs.Target{}, NewError(ErrPathAlreadyExists, "Path already exists: "+normalized)
	default:
		return nil, vfs.Target{}, errorf(ErrInvalidPath, "Invalid directory path: %s", normalized)
	}
	target.Mount = selected
	target.RelPath = rel
	target.ConceptID = rel
	return mounts, target, nil
}

func (w *LocalWorkspace) ensureDirectoryParent(mounts []vfs.Mount, target vfs.Target) error {
	parentPath := path.Dir(target.Path)
	if parentPath == "." {
		parentPath = "/"
	}
	if parentPath == "/" && target.Mount.MountPath == "/" {
		return nil
	}
	parent, err := vfs.Resolve(mounts, parentPath)
	if err != nil {
		return NormalizeError(err)
	}
	if parent.Kind == TargetVirtualRoot {
		if target.Mount.MountPath == "/" {
			return nil
		}
		return errorf(ErrMountNotFound, "Parent path not found: %s", parentPath)
	}
	if parent.Kind == TargetConcept {
		return errorf(ErrInvalidPath, "Parent path is a concept: %s", parentPath)
	}
	if !parent.Exists {
		return errorf(ErrMountNotFound, "Parent path not found: %s", parentPath)
	}
	if parent.Mount.MountPath != target.Mount.MountPath {
		return errorf(ErrMountNotFound, "Parent path not found in target source: %s", parentPath)
	}
	if parent.Kind != TargetPath && parent.Kind != TargetBundle {
		return errorf(ErrInvalidPath, "Parent path is not a directory: %s", parentPath)
	}
	return nil
}

func (w *LocalWorkspace) resolveExistingConceptWrite(inputPath string) ([]vfs.Mount, vfs.Target, error) {
	mounts, err := w.mounts()
	if err != nil {
		return nil, vfs.Target{}, NormalizeError(err)
	}
	normalized, err := vfs.NormalizePath(inputPath)
	if err != nil {
		return nil, vfs.Target{}, NormalizeError(err)
	}
	selected, _, err := w.mountAndRelativePath(mounts, normalized)
	if err != nil {
		return nil, vfs.Target{}, err
	}
	if selected.Kind == vfs.SourceKindGit {
		return nil, vfs.Target{}, w.ensureWritable(selected)
	}
	mounts, target, err := w.resolve(inputPath)
	if err != nil {
		return nil, vfs.Target{}, err
	}
	if target.Kind != TargetConcept {
		return nil, vfs.Target{}, errorf(ErrConceptNotFound, "Concept not found: %s", target.Path)
	}
	return mounts, target, nil
}

func (w *LocalWorkspace) mountAndConceptID(mounts []vfs.Mount, normalized string) (vfs.Mount, string, error) {
	selected, conceptID, err := w.mountAndRelativePath(mounts, normalized)
	if err != nil {
		return vfs.Mount{}, "", err
	}
	if conceptID == "" {
		return vfs.Mount{}, "", errorf(ErrPathIsNotConcept, "Path is not a concept: %s", normalized)
	}
	return selected, conceptID, nil
}

func (w *LocalWorkspace) mountAndRelativePath(mounts []vfs.Mount, normalized string) (vfs.Mount, string, error) {
	var selected *vfs.Mount
	for i := range mounts {
		mount := mounts[i]
		if mountMatchesPath(mount.MountPath, normalized) {
			if selected == nil || len(mount.MountPath) > len(selected.MountPath) {
				selected = &mounts[i]
			}
		}
	}
	if selected == nil {
		return vfs.Mount{}, "", NewError(ErrMountNotFound, "Mount not found for path: "+normalized)
	}
	if normalized == selected.MountPath {
		return *selected, "", nil
	}
	rel := mountRelativePath(*selected, normalized)
	if rel == "" || strings.Contains(rel, "..") {
		return vfs.Mount{}, "", errorf(ErrInvalidPath, "Invalid path: %s", normalized)
	}
	return *selected, rel, nil
}

func (w *LocalWorkspace) mounts() ([]vfs.Mount, error) {
	context, err := w.resolvedWorkspace()
	if err != nil {
		return nil, err
	}
	return vfs.LoadWorkspaceMounts(context)
}

func (w *LocalWorkspace) ensureWritable(mount vfs.Mount) error {
	if mount.Kind == vfs.SourceKindGit {
		return NewError(ErrSourceReadOnly, "Source is read-only: "+mount.MountPath)
	}
	if w.opts.ReadOnly || !mount.Writable {
		return NewError(ErrSourceReadOnly, "Source is read-only: "+mount.MountPath)
	}
	return ensureLocal(mount)
}

func ensureReadable(mount vfs.Mount) error {
	if mount.Kind != "" && mount.Kind != vfs.SourceKindLocal && mount.Kind != vfs.SourceKindGit {
		return NewError(ErrUnsupportedSource, "Unsupported source kind: "+mount.Kind)
	}
	if mount.SourcePath == "" {
		return NewError(ErrUnsupportedSource, "Source is not materialized: "+mount.MountPath)
	}
	return nil
}

func ensureLocal(mount vfs.Mount) error {
	if mount.Kind != "" && mount.Kind != "local" {
		return NewError(ErrUnsupportedSource, "Unsupported source kind: "+mount.Kind)
	}
	return nil
}

func fileExists(name string) bool {
	_, err := os.Stat(name)
	return err == nil
}

func cleanVirtualJoin(basePath, id string) string {
	return path.Clean(basePath + "/" + strings.TrimPrefix(id, "/"))
}

func asAppError(err error) *AppError {
	var app *AppError
	if errors.As(err, &app) {
		return app
	}
	return nil
}

var _ Workspace = (*LocalWorkspace)(nil)
