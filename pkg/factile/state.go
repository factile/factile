package factile

import (
	"crypto/sha256"
	"encoding/hex"
	"path/filepath"

	"github.com/factile/factile/pkg/storage"
	"github.com/factile/factile/pkg/vfs"
)

func (w *LocalWorkspace) withWorkspaceLocks(targets []string, fn func() error) error {
	if len(targets) == 0 {
		return fn()
	}
	context, err := w.resolvedWorkspace()
	if err != nil {
		return err
	}
	locksDir, err := vfs.EnsureStateDirectory(context, "locks")
	if err != nil {
		return err
	}
	lockTargets := make([]string, 0, len(targets))
	for _, target := range targets {
		absolute, err := filepath.Abs(target)
		if err != nil {
			return err
		}
		digest := sha256.Sum256([]byte(filepath.Clean(absolute)))
		lockTargets = append(lockTargets, filepath.Join(locksDir, hex.EncodeToString(digest[:])))
	}
	return storage.WithFileLocks(lockTargets, fn)
}
