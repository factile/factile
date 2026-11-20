package factile

import (
	"bytes"
	"os"
	"strings"

	"github.com/factile/factile/internal/atomicfile"
	"github.com/factile/factile/pkg/okf"
	"github.com/factile/factile/pkg/revision"
	"github.com/factile/factile/pkg/storage"
	"github.com/factile/factile/pkg/vfs"
	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/text"
)

type AdoptResult struct {
	Applied bool        `json:"applied"`
	Files   []AdoptFile `json:"files"`
}

type AdoptFile struct {
	Path        string `json:"path"`
	Frontmatter string `json:"frontmatter,omitempty"`
}

// Adopt adds minimal frontmatter to selected Markdown in a writable bundle.
func (w *LocalWorkspace) Adopt(paths []string, typ string, apply bool) (AdoptResult, error) {
	result := AdoptResult{Applied: apply, Files: []AdoptFile{}}
	if len(paths) == 0 || strings.TrimSpace(typ) == "" {
		return result, NewError(ErrInvalidPath, "Adoption requires a type and at least one document path")
	}
	type candidate struct {
		target vfs.Target
		store  storage.Local
		file   string
		before []byte
		after  []byte
	}
	var candidates []candidate
	var files []string
	seen := map[string]bool{}
	for _, input := range paths {
		_, target, err := w.resolveExistingConceptWrite(input)
		if err != nil {
			return result, err
		}
		if err := w.ensureWritable(target.Mount); err != nil {
			return result, err
		}
		if okf.IsReservedFile(target.RelPath) {
			return result, errorf(ErrInvalidPath, "Reserved document %s is already supported as Markdown", target.Path)
		}
		store, err := storage.NewLocal(target.Mount.SourcePath)
		if err != nil {
			return result, NormalizeError(err)
		}
		file, err := store.ConceptFile(target.ConceptID)
		if err != nil {
			return result, NormalizeError(err)
		}
		if !seen[file] {
			seen[file] = true
			files = append(files, file)
			candidates = append(candidates, candidate{target: target, store: store, file: file})
		}
	}

	plan := func() error {
		for index := range candidates {
			item := &candidates[index]
			data, _, err := item.store.ReadConcept(item.target.ConceptID)
			if err != nil {
				return NormalizeError(err)
			}
			check := bytes.TrimPrefix(data, []byte("\xef\xbb\xbf"))
			if line, _, _ := bytes.Cut(check, []byte("\n")); bytes.Equal(bytes.TrimSuffix(line, []byte("\r")), []byte("---")) {
				if _, err := okf.ParseConcept(item.target.ConceptID, check); err != nil {
					return errorf(ErrOKFParse, "%s: %v", item.target.Path, err)
				}
				continue
			}
			title := firstMarkdownHeading(check)
			if title == "" {
				title = titleFromPath(item.target.Path)
			}
			header := okf.Serialize(okf.Document{
				Frontmatter: map[string]any{"type": typ, "title": title},
				Order:       []string{"type", "title"},
			})
			if bytes.Contains(data, []byte("\r\n")) {
				header = bytes.ReplaceAll(header, []byte("\n"), []byte("\r\n"))
			}
			item.before, item.after = data, append(append([]byte{}, header...), data...)
			result.Files = append(result.Files, AdoptFile{Path: item.target.Path, Frontmatter: string(header)})
		}
		return nil
	}
	if !apply {
		err := plan()
		return result, err
	}
	err := w.withWorkspaceLocks(files, func() error {
		if err := plan(); err != nil {
			return err
		}
		var written []string
		for _, item := range candidates {
			if item.after == nil {
				continue
			}
			current, _, err := item.store.ReadConcept(item.target.ConceptID)
			if err == nil && !bytes.Equal(current, item.before) {
				err = revisionMismatch(item.target.Path, revision.DigestBytes(item.before), revision.DigestBytes(current))
			}
			if err == nil {
				var info os.FileInfo
				info, err = os.Stat(item.file)
				if err == nil {
					err = atomicfile.Write(item.file, item.after, info.Mode().Perm())
				}
			}
			if err != nil {
				if len(written) > 0 {
					return errorf(ErrPartialFailure, "Adopted %s before failing at %s: %v", strings.Join(written, ", "), item.target.Path, err)
				}
				return NormalizeError(err)
			}
			written = append(written, item.target.Path)
		}
		return nil
	})
	return result, NormalizeError(err)
}

func firstMarkdownHeading(data []byte) string {
	var title string
	root := goldmark.DefaultParser().Parse(text.NewReader(data))
	_ = ast.Walk(root, func(node ast.Node, entering bool) (ast.WalkStatus, error) {
		if heading, ok := node.(*ast.Heading); entering && ok {
			title = strings.TrimSpace(string(heading.Text(data)))
			return ast.WalkStop, nil
		}
		return ast.WalkContinue, nil
	})
	return title
}
