package gitsource

import (
	"errors"
	"os"
	"path/filepath"

	"github.com/factile/factile/pkg/vfs"
)

// SelectionError describes a snapshot failure without exposing source or cache paths.
type SelectionError struct {
	Reason string
}

func (e *SelectionError) Error() string {
	switch e.Reason {
	case "missing_manifest":
		return "Git snapshot has no repository-root factile.toml."
	case "invalid_manifest":
		return "Git snapshot repository-root factile.toml is invalid."
	case "invalid_root":
		return "Git snapshot workspace root is invalid."
	case "missing_bundle":
		return "Git snapshot selected bundle is missing."
	case "invalid_bundle":
		return "Git snapshot selected bundle manifest is invalid."
	case "symlink":
		return "Git snapshot contains a symlink."
	default:
		return "Git source configuration is invalid."
	}
}

func selectionReason(err error) string {
	var selection *SelectionError
	if errors.As(err, &selection) {
		return selection.Reason
	}
	return ""
}

func validSelectionReason(reason string) bool {
	switch reason {
	case "missing_manifest", "invalid_manifest", "invalid_root", "missing_bundle", "invalid_bundle", "symlink":
		return true
	default:
		return false
	}
}

func snapshotBundle(base, snapshot string) (string, error) {
	if err := validateSnapshot(base, snapshot); err != nil {
		return "", err
	}
	return selectSnapshotBundle(snapshot)
}

func selectSnapshotBundle(snapshot string) (string, error) {
	manifest, err := vfs.LoadManifest(snapshot)
	if errors.Is(err, os.ErrNotExist) {
		return "", &SelectionError{Reason: "missing_manifest"}
	}
	if err != nil {
		return "", &SelectionError{Reason: "invalid_manifest"}
	}
	if manifest.Workspace == nil {
		return snapshot, nil
	}
	// Explicit selection never discovers a consuming workspace above the snapshot.
	workspace, err := vfs.ResolveWorkspace(vfs.ResolveWorkspaceOptions{Workspace: snapshot})
	if err == nil {
		return workspace.RootBundleDir, nil
	}
	var layout *vfs.Error
	if errors.As(err, &layout) && layout.Code == vfs.ErrInvalidBundle {
		_, bundleErr := vfs.LoadManifest(filepath.Join(snapshot, filepath.FromSlash(manifest.Workspace.Root)))
		if errors.Is(bundleErr, os.ErrNotExist) {
			return "", &SelectionError{Reason: "missing_bundle"}
		}
		return "", &SelectionError{Reason: "invalid_bundle"}
	}
	return "", &SelectionError{Reason: "invalid_root"}
}
