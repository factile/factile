package vfs

import (
	"errors"
	"os"
	"path"
	"path/filepath"
	"strings"
)

// StatePath returns a path below the workspace's private state directory
// without creating it.
func StatePath(context WorkspaceContext, relative string) (string, error) {
	workspaceDir, err := canonicalDir(context.WorkspaceDir)
	if err != nil || workspaceDir != filepath.Clean(context.WorkspaceDir) {
		return "", unsafeStatePath(context, relative)
	}
	expectedStateDir := filepath.Join(workspaceDir, StateDirname)
	if filepath.Clean(context.StateDir) != expectedStateDir {
		return "", unsafeStatePath(context, relative)
	}
	if relative == "" {
		return expectedStateDir, nil
	}
	if filepath.IsAbs(relative) || isWindowsAbsolute(relative) || strings.Contains(relative, `\`) || path.Clean(relative) != relative {
		return "", unsafeStatePath(context, relative)
	}
	for _, segment := range strings.Split(relative, "/") {
		if segment == "" || segment == "." || segment == ".." {
			return "", unsafeStatePath(context, relative)
		}
	}
	target := filepath.Join(expectedStateDir, filepath.FromSlash(relative))
	contained, err := pathContainedBy(expectedStateDir, target)
	if err != nil || !contained {
		return "", unsafeStatePath(context, relative)
	}
	return target, nil
}

// EnsureStateDirectory creates one private directory path below StateDir. It
// validates each component independently so a symlink cannot redirect state
// writes outside the workspace.
func EnsureStateDirectory(context WorkspaceContext, relative string) (string, error) {
	target, err := StatePath(context, relative)
	if err != nil {
		return "", err
	}
	relativeTarget, err := filepath.Rel(context.WorkspaceDir, target)
	if err != nil {
		return "", unsafeStatePath(context, relative)
	}
	current := context.WorkspaceDir
	for _, segment := range strings.Split(relativeTarget, string(filepath.Separator)) {
		if segment == "" || segment == "." {
			continue
		}
		current = filepath.Join(current, segment)
		info, statErr := os.Lstat(current)
		if errors.Is(statErr, os.ErrNotExist) {
			if mkdirErr := os.Mkdir(current, 0o700); mkdirErr != nil && !errors.Is(mkdirErr, os.ErrExist) {
				return "", mkdirErr
			}
			info, statErr = os.Lstat(current)
		}
		if statErr != nil || info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
			return "", unsafeStatePath(context, relative)
		}
		if err := os.Chmod(current, 0o700); err != nil {
			return "", err
		}
	}
	return target, nil
}

func unsafeStatePath(context WorkspaceContext, relative string) error {
	return layoutError(
		ErrInvalidWorkspace,
		"Workspace state path is unsafe.",
		map[string]string{"state_dir": context.StateDir, "relative_path": relative},
	)
}
