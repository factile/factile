package factile

import (
	"bytes"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strings"
	"unicode"

	"github.com/factile/factile/internal/atomicfile"
	"github.com/factile/factile/pkg/graph"
	"github.com/factile/factile/pkg/okf"
	"github.com/factile/factile/pkg/patch"
	"github.com/factile/factile/pkg/revision"
	"github.com/factile/factile/pkg/storage"
	"github.com/factile/factile/pkg/vfs"
)

type MovePlan struct {
	OldPath string       `json:"old_path"`
	NewPath string       `json:"new_path"`
	Plan    string       `json:"plan"`
	Applied bool         `json:"applied"`
	KeepOld bool         `json:"keep_old,omitempty"`
	Changes []MoveChange `json:"changes"`
}

type MoveChange struct {
	Path string `json:"path"`
	Diff string `json:"diff"`
}

type moveFile struct {
	path, filename string
	before, after  []byte
	stub           []byte
	mode           os.FileMode
}

// MoveWithLinks previews a move or applies an unchanged preview.
func (w *LocalWorkspace) MoveWithLinks(oldPath, newPath, expectedPlan string, keepOld ...bool) (MovePlan, error) {
	_, oldTarget, err := w.resolveExistingConceptWrite(oldPath)
	if err != nil {
		return MovePlan{}, err
	}
	mounts, newTarget, err := w.resolveForConceptWrite(newPath)
	if err != nil {
		return MovePlan{}, err
	}
	if oldTarget.Mount.MountPath != newTarget.Mount.MountPath {
		return MovePlan{}, NewError(ErrInvalidPath, "Move must stay within one bundle")
	}
	if okf.IsReservedFile(oldTarget.ConceptID+".md") || okf.IsReservedFile(newTarget.ConceptID+".md") {
		return MovePlan{}, NewError(ErrInvalidPath, "Move requires ordinary document paths")
	}
	if err := w.ensureWritable(oldTarget.Mount); err != nil {
		return MovePlan{}, err
	}
	if err := checkMoveDestination(mounts, newTarget); err != nil {
		return MovePlan{}, err
	}
	keep := len(keepOld) != 0 && keepOld[0]
	if keep && oldTarget.Mount.OKFVersion != "0.3" {
		return MovePlan{}, NewError(ErrInvalidPath, "Keeping a portable successor requires an OKF 0.3 bundle")
	}
	store, err := storeForMount(oldTarget.Mount)
	if err != nil {
		return MovePlan{}, NormalizeError(err)
	}
	destination, err := store.ConceptFile(newTarget.ConceptID)
	if err != nil {
		return MovePlan{}, NormalizeError(err)
	}
	result, planned, err := buildMovePlan(store, mounts, oldTarget, newTarget, keep)
	if err != nil || expectedPlan == "" {
		return result, NormalizeError(err)
	}
	if result.Plan != expectedPlan {
		return MovePlan{}, NewError(ErrRevisionMismatch, "Documents changed since the move preview. Preview again.")
	}
	files := []string{destination, strings.TrimSuffix(destination, ".md")}
	for parent := filepath.Dir(destination); parent != store.Root; parent = filepath.Dir(parent) {
		files = append(files, parent+".md")
	}
	for _, item := range planned {
		files = append(files, item.filename)
	}
	err = w.withWorkspaceLocks(files, func() error {
		currentMounts, err := w.mounts()
		if err != nil {
			return err
		}
		if err := checkMoveDestination(currentMounts, newTarget); err != nil {
			return err
		}
		latest, changes, err := buildMovePlan(store, currentMounts, oldTarget, newTarget, keep)
		if err != nil {
			return err
		}
		if latest.Plan != expectedPlan {
			return NewError(ErrRevisionMismatch, "Documents changed since the move preview. Preview again.")
		}
		if err := checkMoveDestination(currentMounts, newTarget); err != nil {
			return err
		}
		return applyMovePlan(destination, changes, oldTarget.Path)
	})
	if err != nil {
		return MovePlan{}, NormalizeError(err)
	}
	result.Applied = true
	return result, nil
}

func checkMoveDestination(mounts []vfs.Mount, destination vfs.Target) error {
	if len(mountsForVirtualPath(mounts, destination.Path)) != 0 {
		return errorf(ErrPathAlreadyExists, "Move destination contains a mount: %s", destination.Path)
	}
	target, err := vfs.Resolve(mounts, destination.Path)
	if err != nil {
		if ErrorCode(NormalizeError(err)) != ErrMountNotFound {
			return err
		}
	} else if target.Exists {
		return errorf(ErrPathAlreadyExists, "Move destination already exists: %s", destination.Path)
	} else if target.Mount.SourcePath != destination.Mount.SourcePath {
		return NewError(ErrRevisionMismatch, "Move destination source changed. Preview again.")
	}
	for parent := path.Dir(destination.Path); parent != destination.Mount.MountPath; parent = path.Dir(parent) {
		target, err := vfs.Resolve(mounts, parent)
		if err != nil {
			if ErrorCode(NormalizeError(err)) == ErrMountNotFound {
				continue
			}
			return err
		}
		if target.Kind == TargetConcept {
			return errorf(ErrPathAlreadyExists, "Move destination has a document as a parent: %s", parent)
		}
	}
	return nil
}

func buildMovePlan(store storage.Local, mounts []vfs.Mount, oldTarget, newTarget vfs.Target, keepOld bool) (MovePlan, []moveFile, error) {
	result := MovePlan{OldPath: oldTarget.Path, NewPath: newTarget.Path, KeepOld: keepOld, Changes: []MoveChange{}}
	var planned []moveFile
	mountedRoots := make(map[string]bool, len(mounts))
	for _, mount := range mounts {
		mountedRoots[mount.SourcePath] = true
	}
	nestedRoot := func(dir string) bool {
		if mountedRoots[dir] {
			return true
		}
		manifest, _ := vfs.LoadManifest(dir)
		return manifest.Bundle != nil || vfs.SelectOKF(vfs.Mount{SourcePath: dir}, true).OKFVersion != ""
	}
	for _, target := range []vfs.Target{oldTarget, newTarget} {
		for parent := path.Dir(target.ConceptID); parent != "."; parent = path.Dir(parent) {
			if nestedRoot(filepath.Join(store.Root, filepath.FromSlash(parent))) {
				return result, nil, errorf(ErrInvalidPath, "Move path belongs to a separate physical bundle: %s", target.Path)
			}
		}
	}
	ids, err := store.ListDocumentIDsSkipping(nestedRoot)
	if err != nil {
		return result, nil, err
	}
	found := false
	var fingerprint strings.Builder
	fmt.Fprintf(&fingerprint, "%s\x00%s\x00%s\x00", store.Root, result.OldPath, result.NewPath)
	for _, id := range ids {
		data, filename, err := store.ReadConcept(id)
		if err != nil {
			return result, nil, err
		}
		from := cleanVirtualJoin(oldTarget.Mount.MountPath, id)
		to := from
		if id == oldTarget.ConceptID {
			found = true
			to = newTarget.Path
		}
		doc, err := okf.ParseConcept(id, data)
		if err != nil {
			if from == oldTarget.Path || rewriteMoveLinks(string(data), id, from, to, result) != string(data) {
				return result, nil, errorf(ErrOKFParse, "%s: %v", from, err)
			}
			continue
		}
		markdown := doc.Markdown
		next := data
		if keepOld {
			rewrite := func(field, value string) string {
				return movedPortableReference(store, id, oldTarget.ConceptID, newTarget.ConceptID, field, value)
			}
			markdown = rewritePortableMoveMarkdown(doc.Markdown, rewrite)
			if fields := portableMoveFields(doc.Frontmatter, rewrite); len(fields) != 0 {
				next, err = okf.PatchFrontmatter(id, next, fields, nil)
				if err != nil {
					return result, nil, err
				}
			}
		} else {
			markdown = rewriteMoveLinks(markdown, id, from, to, result)
		}
		if markdown != doc.Markdown {
			next, err = okf.ReplaceMarkdown(id, next, markdown)
			if err != nil {
				return result, nil, err
			}
		}
		if !bytes.Equal(next, data) {
			result.Changes = append(result.Changes, MoveChange{Path: from, Diff: patch.Diff(from+".md", string(data), string(next))})
		} else if from == to {
			continue
		}
		info, err := os.Stat(filename)
		if err != nil {
			return result, nil, err
		}
		fmt.Fprintf(&fingerprint, "%s\x00%s\x00%s\x00", id, revision.DigestBytes(data), revision.DigestBytes(next))
		item := moveFile{path: from, filename: filename, before: data, after: next, mode: info.Mode().Perm()}
		if keepOld && from == oldTarget.Path {
			successor := escapedBundlePath(newTarget.ConceptID + ".md")
			fields := map[string]any{"type": doc.Frontmatter["type"], "status": "deprecated", "superseded_by": successor}
			if title, ok := doc.Frontmatter["title"].(string); ok {
				fields["title"] = title
			}
			item.stub = okf.Serialize(okf.Document{Frontmatter: fields, Markdown: "This concept moved to its [replacement](" + successor + ").\n"})
			result.Changes = append(result.Changes, MoveChange{Path: from, Diff: patch.Diff(from+".md", string(data), string(item.stub))})
			fmt.Fprintf(&fingerprint, "stub:%s\x00", revision.DigestBytes(item.stub))
		}
		planned = append(planned, item)
	}
	if !found {
		return result, nil, NewError(ErrConceptNotFound, "Source document disappeared")
	}
	result.Plan = revision.DigestBytes([]byte(fingerprint.String()))
	return result, planned, nil
}

func movedPortableReference(store storage.Local, id, oldID, newID, field, value string) string {
	kind, rel := okf.ResolvePath(id, value, field, "0.3", store.EntryKind)
	if kind != "file" && kind != "directory" {
		return value
	}
	if rel == oldID+".md" {
		rel = newID + ".md"
	} else if id != oldID || path.Dir(id) == path.Dir(newID) || strings.HasPrefix(value, "/") {
		return value
	}
	if suffix := strings.IndexAny(value, "?#"); suffix >= 0 {
		return escapedBundlePath(rel) + value[suffix:]
	}
	if strings.HasSuffix(value, "/") && rel != "" {
		rel += "/"
	}
	return escapedBundlePath(rel)
}

func escapedBundlePath(rel string) string {
	return (&url.URL{Path: "/" + rel}).EscapedPath()
}

func rewritePortableMoveMarkdown(markdown string, rewrite func(field, value string) string) string {
	var body strings.Builder
	offset := 0
	for _, link := range graph.MarkdownTargets(markdown) {
		next := rewrite("markdown", link.Target)
		if next != link.Target {
			body.WriteString(markdown[offset:link.Start])
			body.WriteString(next)
			offset = link.End
		}
	}
	body.WriteString(markdown[offset:])
	return body.String()
}

func portableMoveFields(fields map[string]any, rewrite func(field, value string) string) map[string]any {
	set := map[string]any{}
	for _, field := range []string{"resource", "computation", "superseded_by"} {
		if value, ok := fields[field].(string); ok {
			if next := rewrite(field, value); next != value {
				set[field] = next
			}
		}
	}
	object := func(field string, entry any) bool {
		values, _ := entry.(map[string]any)
		value, ok := values["resource"].(string)
		if !ok {
			return false
		}
		next := rewrite(field, value)
		if next == value {
			return false
		}
		values["resource"] = next
		return true
	}
	for _, field := range []string{"executor", "attester"} {
		if object(field+".resource", fields[field]) {
			set[field] = fields[field]
		}
	}
	for _, entry := range okf.MetadataItems(fields["sources"]) {
		if object("sources[].resource", entry) {
			set["sources"] = fields["sources"]
		}
	}
	return set
}

func rewriteMoveLinks(markdown, id, from, to string, plan MovePlan) string {
	source := scopedConcept{Concept: Concept{Path: from, ConceptID: id}}
	mount := conceptMount(source.Concept)
	var body strings.Builder
	offset := 0
	for _, link := range graph.MarkdownTargets(markdown) {
		target, ok := resolveConceptLink(source, link.Link, nil)
		if !ok {
			continue
		}
		destination := target
		if target == plan.OldPath {
			destination = plan.NewPath
		}
		absolute := strings.HasPrefix(link.Target, "/")
		if target == destination && (absolute || path.Dir(from) == path.Dir(to)) {
			continue
		}
		rewritten := strings.TrimPrefix(destination, mount)
		if !absolute {
			relative, err := filepath.Rel(filepath.FromSlash(path.Dir(to)), filepath.FromSlash(destination))
			if err != nil {
				continue
			}
			rewritten = filepath.ToSlash(relative)
		}
		if strings.HasSuffix(link.Target, ".md") {
			rewritten += ".md"
		}
		if rewritten == link.Target {
			continue
		}
		body.WriteString(markdown[offset:link.Start])
		offset = link.End
		if strings.IndexFunc(rewritten, unicode.IsSpace) >= 0 && markdown[link.Start-1] != '<' {
			offset = link.Start + len(link.Raw)
			body.WriteByte('<')
			body.WriteString(rewritten)
			body.WriteString(markdown[link.End:offset])
			body.WriteByte('>')
		} else {
			body.WriteString(rewritten)
		}
	}
	body.WriteString(markdown[offset:])
	return body.String()
}

func applyMovePlan(destination string, changes []moveFile, oldPath string) error {
	var source moveFile
	for _, item := range changes {
		if item.path == oldPath {
			source = item
		}
		if err := checkMoveFile(item.filename, item.before); err != nil {
			return err
		}
	}
	if err := os.MkdirAll(filepath.Dir(destination), 0o755); err != nil {
		return err
	}
	created, err := atomicfile.Create(destination, source.after, source.mode)
	if err != nil {
		return err
	}
	if !created {
		return NewError(ErrConceptAlreadyExist, "Move destination already exists")
	}
	var applied []moveFile
	apply := func() error {
		for _, change := range changes {
			if change.path == oldPath {
				continue
			}
			if err := rewriteMoveFile(change.filename, change.before, change.after, change.mode); err != nil {
				return err
			}
			applied = append(applied, change)
		}
		if err := checkMoveFile(source.filename, source.before); err != nil {
			return err
		}
		if source.stub != nil {
			return rewriteMoveFile(source.filename, source.before, source.stub, source.mode)
		}
		return os.Remove(source.filename)
	}
	if err := apply(); err != nil {
		var rollback error
		for i := len(applied) - 1; i >= 0; i-- {
			item := applied[i]
			rollback = errors.Join(rollback, rewriteMoveFile(item.filename, item.after, item.before, item.mode))
		}
		if check := checkMoveFile(destination, source.after); check != nil {
			rollback = errors.Join(rollback, check)
		} else {
			rollback = errors.Join(rollback, os.Remove(destination))
		}
		if rollback != nil {
			return errorf(ErrPartialFailure, "Move failed: %v. Rollback incomplete: %v", err, rollback)
		}
		return err
	}
	return nil
}

func checkMoveFile(filename string, expected []byte) error {
	info, err := os.Lstat(filename)
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return NewError(ErrUnsafeSourcePath, "Move requires a regular file: "+filename)
	}
	current, err := os.ReadFile(filename)
	if err != nil {
		return err
	}
	if !bytes.Equal(current, expected) {
		return NewError(ErrRevisionMismatch, "File changed during move: "+filename)
	}
	return nil
}

func rewriteMoveFile(filename string, before, after []byte, mode os.FileMode) error {
	if err := checkMoveFile(filename, before); err != nil {
		return err
	}
	return atomicfile.Write(filename, after, mode)
}
