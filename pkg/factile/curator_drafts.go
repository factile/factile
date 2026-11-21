package factile

import (
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/factile/factile/internal/atomicfile"
	"github.com/factile/factile/pkg/vfs"
)

type CuratorDraft struct {
	Path        string `json:"path"`
	Type        string `json:"type"`
	Title       string `json:"title"`
	Description string `json:"description"`
	Tags        string `json:"tags"`
	Markdown    string `json:"markdown"`
	Revision    string `json:"revision,omitempty"`
}

func (w *LocalWorkspace) LoadCuratorDraft(path, mode string) (*CuratorDraft, error) {
	filename, err := w.curatorDraftFile(path, mode)
	if err != nil {
		return nil, err
	}
	return readCuratorDraft(filename)
}

// StoreCuratorDraft discards a draft only if it still matches the editor's copy.
func (w *LocalWorkspace) StoreCuratorDraft(path, mode string, draft CuratorDraft, discard bool) error {
	filename, err := w.curatorDraftFile(path, mode)
	if err != nil {
		return err
	}
	return w.withWorkspaceLocks([]string{filename}, func() error {
		if discard {
			current, err := readCuratorDraft(filename)
			if err != nil || current == nil || *current != draft {
				return err
			}
			return os.Remove(filename)
		}
		data, err := json.Marshal(draft)
		if err != nil {
			return err
		}
		if _, err := atomicfile.Create(filepath.Join(filepath.Dir(filename), ".gitignore"), []byte("*\n"), 0o600); err != nil {
			return err
		}
		return atomicfile.Write(filename, data, 0o600)
	})
}

func (w *LocalWorkspace) curatorDraftFile(path, mode string) (string, error) {
	if w.opts.ReadOnly {
		return "", NewError(ErrSourceReadOnly, "Workspace is read-only")
	}
	if mode != "create" && mode != "edit" {
		return "", NewError(ErrInvalidPath, "Draft mode must be create or edit")
	}
	logicalPath, err := vfs.NormalizePath(path)
	if err != nil {
		return "", err
	}
	workspace, err := w.resolvedWorkspace()
	if err != nil {
		return "", err
	}
	dir, err := vfs.EnsureStateDirectory(workspace, "drafts")
	if err != nil {
		return "", err
	}
	key := sha256.Sum256([]byte(mode + "\x00" + logicalPath))
	return filepath.Join(dir, fmt.Sprintf("%x.json", key)), nil
}

func readCuratorDraft(filename string) (*CuratorDraft, error) {
	info, err := os.Lstat(filename)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, NewError(ErrInvalidWorkspace, "Draft state is not a regular file")
	}
	data, err := os.ReadFile(filename)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var draft CuratorDraft
	if err := json.Unmarshal(data, &draft); err != nil {
		return nil, err
	}
	return &draft, nil
}
