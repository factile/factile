package factile

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"

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
	Changes []MoveChange `json:"changes"`
}

type MoveChange struct {
	Path string `json:"path"`
	Diff string `json:"diff"`
}

type moveFile struct {
	path, filename string
	before, after  []byte
	mode           os.FileMode
}

// MoveWithLinks previews a move or applies an unchanged preview.
func (w *LocalWorkspace) MoveWithLinks(oldPath, newPath, expectedPlan string) (MovePlan, error) {
	_, oldTarget, err := w.resolveExistingConceptWrite(oldPath)
	if err != nil {
		return MovePlan{}, err
	}
	_, newTarget, err := w.resolveForConceptWrite(newPath)
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
	store, err := storage.NewLocal(oldTarget.Mount.SourcePath)
	if err != nil {
		return MovePlan{}, NormalizeError(err)
	}
	destination, err := store.ConceptFile(newTarget.ConceptID)
	if err != nil {
		return MovePlan{}, NormalizeError(err)
	}
	result, planned, err := buildMovePlan(store, oldTarget, newTarget)
	if err != nil || expectedPlan == "" {
		return result, NormalizeError(err)
	}
	if result.Plan != expectedPlan {
		return MovePlan{}, NewError(ErrRevisionMismatch, "Documents changed since the move preview. Preview again.")
	}
	files := []string{destination}
	for _, item := range planned {
		files = append(files, item.filename)
	}
	err = w.withWorkspaceLocks(files, func() error {
		latest, changes, err := buildMovePlan(store, oldTarget, newTarget)
		if err != nil {
			return err
		}
		if latest.Plan != expectedPlan {
			return NewError(ErrRevisionMismatch, "Documents changed since the move preview. Preview again.")
		}
		return applyMovePlan(destination, changes, oldTarget.Path)
	})
	if err != nil {
		return MovePlan{}, NormalizeError(err)
	}
	result.Applied = true
	return result, nil
}

func buildMovePlan(store storage.Local, oldTarget, newTarget vfs.Target) (MovePlan, []moveFile, error) {
	result := MovePlan{OldPath: oldTarget.Path, NewPath: newTarget.Path, Changes: []MoveChange{}}
	var planned []moveFile
	ids, err := store.ListDocumentIDs("")
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
		markdown := rewriteMoveLinks(doc.Markdown, id, from, to, result)
		next := data
		if markdown != doc.Markdown {
			next, err = okf.ReplaceMarkdown(id, data, markdown)
			if err != nil {
				return result, nil, err
			}
			result.Changes = append(result.Changes, MoveChange{Path: from, Diff: patch.Diff(from+".md", string(data), string(next))})
		} else if from == to {
			continue
		}
		info, err := os.Stat(filename)
		if err != nil {
			return result, nil, err
		}
		fmt.Fprintf(&fingerprint, "%s\x00%s\x00%s\x00", id, revision.DigestBytes(data), revision.DigestBytes(next))
		planned = append(planned, moveFile{path: from, filename: filename, before: data, after: next, mode: info.Mode().Perm()})
	}
	if !found {
		return result, nil, NewError(ErrConceptNotFound, "Source document disappeared")
	}
	result.Plan = revision.DigestBytes([]byte(fingerprint.String()))
	return result, planned, nil
}

func rewriteMoveLinks(markdown, id, from, to string, plan MovePlan) string {
	source := scopedConcept{Concept: Concept{Path: from, ConceptID: id}}
	mount := conceptMount(source.Concept)
	var body strings.Builder
	offset := 0
	for _, link := range graph.MarkdownTargets(markdown) {
		target, ok := resolveConceptLink(source, link.Link)
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
		body.WriteString(rewritten)
		offset = link.End
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
