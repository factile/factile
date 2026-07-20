package factile

import (
	"context"
	"errors"
	"strings"

	"github.com/factile/factile/pkg/gitsource"
	"github.com/factile/factile/pkg/vfs"
)

func (w *LocalWorkspace) mountsForTarget(ctx context.Context, normalized string) ([]vfs.Mount, error) {
	mounts, err := w.mounts()
	if err != nil {
		return nil, NormalizeError(err)
	}
	selected := -1
	for index := range mounts {
		if mountMatchesPath(mounts[index].MountPath, normalized) &&
			(selected < 0 || len(mounts[index].MountPath) > len(mounts[selected].MountPath)) {
			selected = index
		}
	}
	if selected < 0 || mounts[selected].Kind != vfs.SourceKindGit {
		return mounts, nil
	}
	return w.hydrateMountIndexes(ctx, mounts, []int{selected})
}

func (w *LocalWorkspace) mountsForScope(ctx context.Context, normalized string) ([]vfs.Mount, error) {
	mounts, err := w.mounts()
	if err != nil {
		return nil, NormalizeError(err)
	}
	var indexes []int
	for index, mount := range mounts {
		if mount.Kind != vfs.SourceKindGit {
			continue
		}
		if normalized == "/" || mountMatchesPath(mount.MountPath, normalized) || strings.HasPrefix(mount.MountPath, normalized+"/") {
			indexes = append(indexes, index)
		}
	}
	return w.hydrateMountIndexes(ctx, mounts, indexes)
}

func (w *LocalWorkspace) mountsForValidationScope(ctx context.Context, normalized string) ([]vfs.Mount, []ValidationIssue, map[string]bool, error) {
	mounts, err := w.mounts()
	if err != nil {
		return nil, nil, nil, NormalizeError(err)
	}
	invalid := map[string]bool{}
	var issues []ValidationIssue
	var indexes []int
	for index, mount := range mounts {
		if mount.Kind != vfs.SourceKindGit || !mountIntersectsScope(mount.MountPath, normalized) {
			continue
		}
		if err := gitsource.ValidateMountIntent(gitIntent(mount)); err != nil {
			invalid[mount.MountPath] = true
			issues = append(issues, invalidGitMountIssue(mount.MountPath))
			continue
		}
		indexes = append(indexes, index)
	}
	if len(indexes) == 0 {
		return mounts, issues, invalid, nil
	}
	workspace, err := w.resolvedWorkspace()
	if err != nil {
		return nil, nil, nil, NormalizeError(err)
	}
	cache, err := gitsource.OpenCache(workspace, gitsource.NewRunner())
	if err != nil {
		normalizedErr := normalizeGitSourceError(err)
		if ErrorCode(normalizedErr) != ErrValidationFailed {
			return nil, nil, nil, normalizedErr
		}
		for _, index := range indexes {
			mountPath := mounts[index].MountPath
			invalid[mountPath] = true
			issues = append(issues, invalidGitMountIssue(mountPath))
		}
		return mounts, issues, invalid, nil
	}
	for _, index := range indexes {
		mount := mounts[index]
		resolution, err := cache.Resolve(ctx, gitIntent(mount))
		if err != nil {
			normalizedErr := normalizeGitSourceError(err)
			if ErrorCode(normalizedErr) == ErrValidationFailed {
				invalid[mount.MountPath] = true
				issues = append(issues, invalidGitMountIssue(mount.MountPath))
				continue
			}
			return nil, nil, nil, normalizedErr
		}
		mount.SourcePath = resolution.SourcePath
		mount.SourceStatus = &resolution.Status
		mounts[index] = mount
	}
	return mounts, issues, invalid, nil
}

func mountIntersectsScope(mountPath string, normalized string) bool {
	return normalized == "/" || mountMatchesPath(mountPath, normalized) || strings.HasPrefix(mountPath, normalized+"/")
}

func invalidGitMountIssue(mountPath string) ValidationIssue {
	return ValidationIssue{
		Severity: "error",
		Code:     ErrValidationFailed,
		Message:  "Git mount configuration is invalid",
		Path:     mountPath,
	}
}

func (w *LocalWorkspace) hydrateMountIndexes(ctx context.Context, mounts []vfs.Mount, indexes []int) ([]vfs.Mount, error) {
	if len(indexes) == 0 {
		return mounts, nil
	}
	workspace, err := w.resolvedWorkspace()
	if err != nil {
		return nil, NormalizeError(err)
	}
	cache, err := gitsource.OpenCache(workspace, gitsource.NewRunner())
	if err != nil {
		return nil, NormalizeError(err)
	}
	for _, index := range indexes {
		mount := mounts[index]
		resolution, err := cache.Resolve(ctx, gitIntent(mount))
		if err != nil {
			return nil, normalizeGitSourceError(err)
		}
		mount.SourcePath = resolution.SourcePath
		mount.SourceStatus = &resolution.Status
		mounts[index] = mount
	}
	return mounts, nil
}

func normalizeGitSourceError(err error) error {
	switch {
	case errors.Is(err, gitsource.ErrGitSourceLocked):
		return NewError(ErrSourceReadOnly, "Git sources are always read-only.")
	case errors.Is(err, gitsource.ErrInvalidIntent):
		return NewError(ErrValidationFailed, "Git source selector is invalid.")
	case errors.Is(err, gitsource.ErrInvalidCache), errors.Is(err, gitsource.ErrSnapshotSymlink):
		return NewError(ErrValidationFailed, "Git source configuration is invalid")
	case errors.Is(err, gitsource.ErrGitUnavailable), errors.Is(err, gitsource.ErrGitTimeout), errors.Is(err, gitsource.ErrGitCommand):
		return NewError(ErrRemoteSourceUnavailable, "Git source is unavailable and no cached snapshot exists.")
	case errors.Is(err, gitsource.ErrRemoteSourceUnavailable):
		return NewError(ErrRemoteSourceUnavailable, "Git source is unavailable and no cached snapshot exists.")
	case errors.Is(err, gitsource.ErrRevisionNotAvailable):
		return NewError(ErrRevisionNotAvailable, "Requested Git ref or revision is not available.")
	default:
		return NormalizeError(err)
	}
}

func (w *LocalWorkspace) resolveMountSource(ctx context.Context, workspace vfs.WorkspaceContext, source, mountPath, kind, localBase string, opts MountOptions) (string, error) {
	if kind == vfs.SourceKindLocal {
		return resolveMountSourcePath(source, localBase)
	}
	intent := gitsource.Intent{
		MountPath:   mountPath,
		Source:      source,
		Version:     opts.Version,
		Ref:         opts.Ref,
		Revision:    opts.Revision,
		VersionSet:  opts.VersionSet,
		RefSet:      opts.RefSet,
		RevisionSet: opts.RevisionSet,
		Writable:    opts.Writable,
	}
	if err := gitsource.ValidateMountIntent(intent); err != nil {
		return "", normalizeGitSourceError(err)
	}
	cache, err := gitsource.OpenCache(workspace, gitsource.NewRunner())
	if err != nil {
		return "", NormalizeError(err)
	}
	if _, err := cache.Refresh(ctx, intent); err != nil {
		return "", normalizeGitSourceError(err)
	}
	resolution, err := cache.Resolve(ctx, intent)
	if err != nil {
		return "", normalizeGitSourceError(err)
	}
	return resolution.SourcePath, nil
}

func (w *LocalWorkspace) gitSourceStatus(mount vfs.Mount) (vfs.SourceStatus, error) {
	workspace, err := w.resolvedWorkspace()
	if err != nil {
		return vfs.SourceStatus{}, err
	}
	cache, err := gitsource.OpenCacheForStatus(workspace, gitsource.NewRunner())
	if err != nil {
		return vfs.SourceStatus{}, err
	}
	return cache.Status(gitIntent(mount))
}

func gitIntent(mount vfs.Mount) gitsource.Intent {
	return gitsource.Intent{
		MountPath:   mount.MountPath,
		Source:      mount.Source,
		Version:     mount.Version,
		Ref:         mount.Ref,
		Revision:    mount.Revision,
		VersionSet:  mount.VersionSet,
		RefSet:      mount.RefSet,
		RevisionSet: mount.RevisionSet,
		Writable:    mount.Writable,
	}
}

func invalidGitStatus(mount vfs.Mount) vfs.SourceStatus {
	mode := gitsource.SelectorHead
	if mount.RefSet || mount.Ref != "" {
		mode = gitsource.SelectorRef
	} else if mount.RevisionSet || mount.Revision != "" {
		mode = gitsource.SelectorRevision
	}
	return vfs.SourceStatus{
		MountPath:      mount.MountPath,
		Source:         safeGitSource(mount.Source),
		Kind:           vfs.SourceKindGit,
		SelectorMode:   mode,
		IntentRef:      mount.Ref,
		IntentRevision: mount.Revision,
		LastErrorCode:  ErrValidationFailed,
	}
}

func safeGitSource(source string) string {
	if err := gitsource.ValidateSource(source); err != nil {
		return "[redacted]"
	}
	return source
}
