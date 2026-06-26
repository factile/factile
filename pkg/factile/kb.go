package factile

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/factile/factile/pkg/catalog"
	"github.com/factile/factile/pkg/storage"
	"github.com/factile/factile/pkg/vfs"
)

type catalogPaths struct {
	workDir     string
	libraryPath string
}

func (w *LocalWorkspace) ListKnowledgeBases(ctx context.Context) (KnowledgeBaseListResult, error) {
	_ = ctx
	paths, err := w.catalogPaths()
	if err != nil {
		return KnowledgeBaseListResult{}, err
	}
	library, err := loadLibraryAllowMissing(paths.libraryPath)
	if err != nil {
		return KnowledgeBaseListResult{}, catalogLoadError(err)
	}
	summaries := make([]KnowledgeBaseSummary, 0, len(library.KnowledgeBases))
	for _, ref := range library.KnowledgeBases {
		summaries = append(summaries, knowledgeBaseSummary(ref))
	}
	return KnowledgeBaseListResult{KnowledgeBases: summaries}, nil
}

func (w *LocalWorkspace) InspectKnowledgeBase(ctx context.Context, inputPath string) (KnowledgeBaseResult, error) {
	_ = ctx
	paths, ref, filename, err := w.findKnowledgeBase(inputPath)
	if err != nil {
		return KnowledgeBaseResult{}, err
	}
	kb, err := catalog.LoadKnowledgeBaseFile(filename)
	if err != nil {
		return KnowledgeBaseResult{}, catalogLoadError(err)
	}
	return KnowledgeBaseResult{KnowledgeBase: kb, Catalog: displayCatalogPath(paths.workDir, filename, ref.Catalog)}, nil
}

func (w *LocalWorkspace) CreateKnowledgeBase(ctx context.Context, inputPath string, input KnowledgeBaseCreateInput) (KnowledgeBaseResult, error) {
	_ = ctx
	kbPath, err := vfs.ValidateMountPath(inputPath)
	if err != nil {
		return KnowledgeBaseResult{}, NormalizeError(err)
	}
	paths, err := w.catalogPaths()
	if err != nil {
		return KnowledgeBaseResult{}, err
	}
	id := catalogIDFromPath(kbPath)
	catalogName := filepath.ToSlash(filepath.Join("knowledge-bases", id+".toml"))
	kbFile := filepath.Join(filepath.Dir(paths.libraryPath), filepath.FromSlash(catalogName))
	title := input.Title
	if title == "" {
		title = titleFromPath(kbPath) + " Knowledge Base"
	}
	kb := catalog.KnowledgeBase{
		ID:          id,
		Path:        kbPath,
		Title:       title,
		Description: input.Description,
	}
	ref := catalog.KnowledgeBaseRef{
		ID:          id,
		Path:        kbPath,
		Catalog:     catalogName,
		Title:       title,
		Description: input.Description,
	}
	err = storage.WithFileLocks([]string{paths.libraryPath, kbFile}, func() error {
		library, err := loadLibraryAllowMissing(paths.libraryPath)
		if err != nil {
			return catalogLoadError(err)
		}
		for _, existing := range library.KnowledgeBases {
			if existing.ID == ref.ID {
				return errorf(ErrValidationFailed, "Knowledge base id already exists: %s", ref.ID)
			}
			if sameFactilePath(existing.Path, ref.Path) {
				return errorf(ErrValidationFailed, "Knowledge base path already exists: %s", ref.Path)
			}
		}
		if _, err := os.Stat(kbFile); err == nil {
			return errorf(ErrValidationFailed, "Knowledge base catalog already exists: %s", catalogName)
		} else if err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		library.KnowledgeBases = append(library.KnowledgeBases, ref)
		if err := catalog.WriteKnowledgeBaseFile(kbFile, kb); err != nil {
			return catalogLoadError(err)
		}
		if err := catalog.WriteLibraryFile(paths.libraryPath, library); err != nil {
			return catalogLoadError(err)
		}
		return nil
	})
	if err != nil {
		return KnowledgeBaseResult{}, NormalizeError(err)
	}
	return KnowledgeBaseResult{KnowledgeBase: kb, Catalog: catalogName, Action: "created"}, nil
}

func (w *LocalWorkspace) LinkBundle(ctx context.Context, knowledgeBasePath string, source string, bundlePath string, input BundleLinkInput) (BundleLinkResult, error) {
	_ = ctx
	if strings.TrimSpace(source) == "" {
		return BundleLinkResult{}, errorf(ErrValidationFailed, "Bundle source is required")
	}
	kind := input.Kind
	if kind == "" {
		kind = "local"
	}
	if kind != "local" || strings.HasPrefix(source, "factile://") {
		return BundleLinkResult{}, NewError(ErrUnsupportedSource, "Remote bundle sources are not implemented in Phase 1")
	}
	_, ref, filename, err := w.findKnowledgeBase(knowledgeBasePath)
	if err != nil {
		return BundleLinkResult{}, err
	}
	normalizedBundlePath, err := vfs.ValidateMountPath(bundlePath)
	if err != nil {
		return BundleLinkResult{}, NormalizeError(err)
	}
	id := catalogIDFromPath(normalizedBundlePath)
	title := input.Title
	if title == "" {
		title = titleFromPath(normalizedBundlePath)
	}
	link := catalog.BundleLink{
		ID:          id,
		Path:        normalizedBundlePath,
		Source:      source,
		Kind:        kind,
		Writable:    input.Writable,
		Title:       title,
		Description: input.Description,
	}
	err = storage.WithFileLock(filename, func() error {
		kb, err := catalog.LoadKnowledgeBaseFile(filename)
		if err != nil {
			return catalogLoadError(err)
		}
		for _, existing := range kb.Bundles {
			if existing.ID == link.ID {
				return errorf(ErrValidationFailed, "Bundle link id already exists: %s", link.ID)
			}
			if sameFactilePath(existing.Path, link.Path) {
				return errorf(ErrValidationFailed, "Bundle link path already exists: %s", link.Path)
			}
		}
		kb.Bundles = append(kb.Bundles, link)
		if err := catalog.WriteKnowledgeBaseFile(filename, kb); err != nil {
			return catalogLoadError(err)
		}
		return nil
	})
	if err != nil {
		return BundleLinkResult{}, NormalizeError(err)
	}
	return BundleLinkResult{KnowledgeBase: knowledgeBaseSummary(ref), Bundle: link, Action: "linked"}, nil
}

func (w *LocalWorkspace) UnlinkBundle(ctx context.Context, inputPath string) (BundleUnlinkResult, error) {
	_ = ctx
	bundlePath, err := vfs.ValidateMountPath(inputPath)
	if err != nil {
		return BundleUnlinkResult{}, NormalizeError(err)
	}
	ref, filename, err := w.findBundleLink(bundlePath)
	if err != nil {
		return BundleUnlinkResult{}, err
	}
	err = storage.WithFileLock(filename, func() error {
		kb, err := catalog.LoadKnowledgeBaseFile(filename)
		if err != nil {
			return catalogLoadError(err)
		}
		next := kb.Bundles[:0]
		removed := false
		for _, link := range kb.Bundles {
			if sameFactilePath(link.Path, bundlePath) {
				removed = true
				continue
			}
			next = append(next, link)
		}
		if !removed {
			return errorf(ErrMountNotFound, "Bundle link not found: %s", bundlePath)
		}
		kb.Bundles = next
		if err := catalog.WriteKnowledgeBaseFile(filename, kb); err != nil {
			return catalogLoadError(err)
		}
		return nil
	})
	if err != nil {
		return BundleUnlinkResult{}, NormalizeError(err)
	}
	return BundleUnlinkResult{KnowledgeBase: knowledgeBaseSummary(ref), BundlePath: bundlePath, Removed: true}, nil
}

func (w *LocalWorkspace) SetKnowledgeBaseView(ctx context.Context, knowledgeBasePath string, viewID string, input ViewInput) (ViewResult, error) {
	_ = ctx
	id, err := normalizeViewID(viewID)
	if err != nil {
		return ViewResult{}, err
	}
	_, ref, filename, err := w.findKnowledgeBase(knowledgeBasePath)
	if err != nil {
		return ViewResult{}, err
	}
	view := catalog.View{
		ID:           id,
		Title:        input.Title,
		Description:  input.Description,
		WhenToUse:    input.WhenToUse,
		WhenNotToUse: input.WhenNotToUse,
		Status:       input.Status,
	}
	action := ""
	err = storage.WithFileLock(filename, func() error {
		kb, err := catalog.LoadKnowledgeBaseFile(filename)
		if err != nil {
			return catalogLoadError(err)
		}
		bundleIDs, err := resolveViewBundleRefs(kb, id, input.Bundles)
		if err != nil {
			return err
		}
		view.Bundles = bundleIDs
		action = catalog.SetView(&kb, view)
		if err := catalog.WriteKnowledgeBaseFile(filename, kb); err != nil {
			return catalogLoadError(err)
		}
		return nil
	})
	if err != nil {
		return ViewResult{}, NormalizeError(err)
	}
	return ViewResult{KnowledgeBase: knowledgeBaseSummary(ref), View: view, Action: action}, nil
}

func (w *LocalWorkspace) DeleteKnowledgeBaseView(ctx context.Context, knowledgeBasePath string, viewID string) (ViewDeleteResult, error) {
	_ = ctx
	id, err := normalizeViewID(viewID)
	if err != nil {
		return ViewDeleteResult{}, err
	}
	_, ref, filename, err := w.findKnowledgeBase(knowledgeBasePath)
	if err != nil {
		return ViewDeleteResult{}, err
	}
	err = storage.WithFileLock(filename, func() error {
		kb, err := catalog.LoadKnowledgeBaseFile(filename)
		if err != nil {
			return catalogLoadError(err)
		}
		if !catalog.DeleteView(&kb, id) {
			return errorf(ErrValidationFailed, "View not found in Knowledge Base %s: %s", kb.Path, id)
		}
		if err := catalog.WriteKnowledgeBaseFile(filename, kb); err != nil {
			return catalogLoadError(err)
		}
		return nil
	})
	if err != nil {
		return ViewDeleteResult{}, NormalizeError(err)
	}
	return ViewDeleteResult{KnowledgeBase: knowledgeBaseSummary(ref), ViewID: id, Deleted: true}, nil
}

func (w *LocalWorkspace) catalogPaths() (catalogPaths, error) {
	workDir := w.opts.WorkDir
	if workDir == "" {
		var err error
		workDir, err = os.Getwd()
		if err != nil {
			return catalogPaths{}, err
		}
	}
	abs, err := filepath.Abs(workDir)
	if err != nil {
		return catalogPaths{}, err
	}
	return catalogPaths{workDir: abs, libraryPath: filepath.Join(abs, ".factile", "library.toml")}, nil
}

func (w *LocalWorkspace) findKnowledgeBase(inputPath string) (catalogPaths, catalog.KnowledgeBaseRef, string, error) {
	kbPath, err := vfs.ValidateMountPath(inputPath)
	if err != nil {
		return catalogPaths{}, catalog.KnowledgeBaseRef{}, "", NormalizeError(err)
	}
	paths, err := w.catalogPaths()
	if err != nil {
		return catalogPaths{}, catalog.KnowledgeBaseRef{}, "", err
	}
	library, err := catalog.LoadLibraryFile(paths.libraryPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return catalogPaths{}, catalog.KnowledgeBaseRef{}, "", errorf(ErrMountNotFound, "Knowledge base not found: %s", kbPath)
		}
		return catalogPaths{}, catalog.KnowledgeBaseRef{}, "", catalogLoadError(err)
	}
	for _, ref := range library.KnowledgeBases {
		if sameFactilePath(ref.Path, kbPath) {
			filename := ref.Catalog
			if !filepath.IsAbs(filename) {
				filename = filepath.Join(filepath.Dir(paths.libraryPath), filename)
			}
			return paths, ref, filename, nil
		}
	}
	return catalogPaths{}, catalog.KnowledgeBaseRef{}, "", errorf(ErrMountNotFound, "Knowledge base not found: %s", kbPath)
}

func (w *LocalWorkspace) findBundleLink(bundlePath string) (catalog.KnowledgeBaseRef, string, error) {
	paths, err := w.catalogPaths()
	if err != nil {
		return catalog.KnowledgeBaseRef{}, "", err
	}
	library, err := catalog.LoadLibraryFile(paths.libraryPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return catalog.KnowledgeBaseRef{}, "", errorf(ErrMountNotFound, "Bundle link not found: %s", bundlePath)
		}
		return catalog.KnowledgeBaseRef{}, "", catalogLoadError(err)
	}
	for _, ref := range library.KnowledgeBases {
		filename := ref.Catalog
		if !filepath.IsAbs(filename) {
			filename = filepath.Join(filepath.Dir(paths.libraryPath), filename)
		}
		kb, err := catalog.LoadKnowledgeBaseFile(filename)
		if err != nil {
			return catalog.KnowledgeBaseRef{}, "", catalogLoadError(err)
		}
		for _, link := range kb.Bundles {
			if sameFactilePath(link.Path, bundlePath) {
				return ref, filename, nil
			}
		}
	}
	return catalog.KnowledgeBaseRef{}, "", errorf(ErrMountNotFound, "Bundle link not found: %s", bundlePath)
}

func normalizeViewID(input string) (string, error) {
	id := strings.TrimSpace(input)
	if id == "" {
		return "", errorf(ErrValidationFailed, "View id is required")
	}
	for _, r := range id {
		if !isViewIDChar(r) {
			return "", errorf(ErrValidationFailed, "Invalid View id: %s", input)
		}
	}
	return id, nil
}

func isViewIDChar(r rune) bool {
	return (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' || r == '_'
}

func resolveViewBundleRefs(kb catalog.KnowledgeBase, viewID string, refs []string) ([]string, error) {
	byID := map[string]string{}
	byPath := map[string]string{}
	for _, bundle := range kb.Bundles {
		byID[bundle.ID] = bundle.ID
		if normalized, err := vfs.NormalizePath(bundle.Path); err == nil {
			byPath[normalized] = bundle.ID
		}
	}
	seen := map[string]bool{}
	bundleIDs := make([]string, 0, len(refs))
	for _, raw := range refs {
		ref := strings.TrimSpace(raw)
		if ref == "" {
			return nil, errorf(ErrValidationFailed, "View bundle id is required: %s", viewID)
		}
		id, ok := byID[ref]
		if !ok {
			if normalized, err := vfs.NormalizePath(ref); err == nil {
				id, ok = byPath[normalized]
			}
		}
		if !ok {
			return nil, errorf(ErrValidationFailed, "View references unknown bundle: %s in %s", ref, viewID)
		}
		if seen[id] {
			return nil, errorf(ErrValidationFailed, "Duplicate view bundle reference: %s in %s", id, viewID)
		}
		seen[id] = true
		bundleIDs = append(bundleIDs, id)
	}
	return bundleIDs, nil
}

func loadLibraryAllowMissing(filename string) (catalog.Library, error) {
	library, err := catalog.LoadLibraryFile(filename)
	if err == nil {
		return library, nil
	}
	if errors.Is(err, os.ErrNotExist) {
		return catalog.Library{
			ID:          "local",
			Title:       "Local Library",
			Description: "Knowledge bases available in this workspace.",
		}, nil
	}
	return catalog.Library{}, err
}

func knowledgeBaseSummary(ref catalog.KnowledgeBaseRef) KnowledgeBaseSummary {
	return KnowledgeBaseSummary{
		ID:          ref.ID,
		Path:        ref.Path,
		Catalog:     ref.Catalog,
		Title:       ref.Title,
		Description: ref.Description,
		Status:      ref.Status,
	}
}

func catalogIDFromPath(inputPath string) string {
	id := strings.Trim(inputPath, "/")
	id = strings.ReplaceAll(id, "/", "-")
	id = strings.ReplaceAll(id, "_", "-")
	if id == "" {
		return "root"
	}
	return id
}

func sameFactilePath(left string, right string) bool {
	normalizedLeft, err := vfs.NormalizePath(left)
	if err != nil {
		return false
	}
	normalizedRight, err := vfs.NormalizePath(right)
	if err != nil {
		return false
	}
	return normalizedLeft == normalizedRight
}

func displayCatalogPath(workDir string, filename string, fallback string) string {
	if fallback != "" {
		return filepath.ToSlash(fallback)
	}
	rel, err := filepath.Rel(workDir, filename)
	if err != nil {
		return filepath.ToSlash(filename)
	}
	return filepath.ToSlash(rel)
}

func catalogLoadError(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, os.ErrNotExist) {
		return err
	}
	return NewError(ErrValidationFailed, fmt.Sprintf("Invalid catalog: %v", err))
}
