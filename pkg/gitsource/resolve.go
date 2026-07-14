package gitsource

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/factile/factile/pkg/vfs"
)

var (
	ErrInvalidIntent           = errors.New("invalid Git mount intent")
	ErrGitSourceLocked         = errors.New("Git sources are read-only")
	ErrSnapshotSymlink         = errors.New("Git snapshot contains a symlink")
	ErrRemoteSourceUnavailable = errors.New("Git source is unavailable and no cached snapshot exists")
	ErrRevisionNotAvailable    = errors.New("requested Git ref or revision is not available")
)

const (
	SelectorHead     = "head"
	SelectorRef      = "ref"
	SelectorRevision = "revision"
	refreshInterval  = 24 * time.Hour
)

type Intent struct {
	MountPath   string
	Source      string
	Version     string
	Ref         string
	Revision    string
	VersionSet  bool
	RefSet      bool
	RevisionSet bool
	Writable    bool
}

type Resolution struct {
	SourcePath   string             `json:"source_path"`
	Revision     string             `json:"revision"`
	SelectorMode string             `json:"selector_mode"`
	Ref          string             `json:"ref,omitempty"`
	Updated      bool               `json:"updated"`
	Status       vfs.SourceStatus   `json:"status"`
	Warning      *vfs.SourceWarning `json:"warning,omitempty"`
}

type RefreshResult struct {
	MountPath string             `json:"mount_path"`
	Outcome   string             `json:"outcome"`
	Status    vfs.SourceStatus   `json:"status"`
	Warning   *vfs.SourceWarning `json:"warning,omitempty"`
}

type selector struct {
	mode     string
	ref      string
	revision string
}

var fullRevisionPattern = regexp.MustCompile(`^[0-9A-Fa-f]{40}$`)

func (c *Cache) Resolve(ctx context.Context, intent Intent) (Resolution, error) {
	resolution, _, err := c.resolve(ctx, intent, false)
	return resolution, err
}

func (c *Cache) Refresh(ctx context.Context, intent Intent) (RefreshResult, error) {
	_, result, err := c.resolve(ctx, intent, true)
	return result, err
}

func (c *Cache) Status(intent Intent) (vfs.SourceStatus, error) {
	selected, err := validateIntent(intent)
	if err != nil {
		return vfs.SourceStatus{}, err
	}
	entry, err := c.entryPaths(intent.MountPath, intent.Source)
	if err != nil {
		return vfs.SourceStatus{}, err
	}
	state, err := c.ReadState(entry)
	if errors.Is(err, os.ErrNotExist) {
		state = initialState(entry)
	} else if errors.Is(err, ErrInvalidCache) {
		state = initialState(entry)
		setStateSelector(&state, selected)
		state.LastErrorCode = "validation_failed"
	} else if err != nil {
		return vfs.SourceStatus{}, err
	}
	status, _ := c.statusFromState(entry, state, selected, c.nowUTC())
	return status, nil
}

func (c *Cache) resolve(ctx context.Context, intent Intent, force bool) (Resolution, RefreshResult, error) {
	selected, err := validateIntent(intent)
	if err != nil {
		return Resolution{}, RefreshResult{}, err
	}
	entry, err := c.Entry(intent.MountPath, intent.Source)
	if err != nil {
		return Resolution{}, RefreshResult{}, err
	}
	observedStatus, err := c.Status(intent)
	if err != nil {
		return Resolution{}, RefreshResult{}, err
	}
	if !force {
		if !resolutionDue(observedStatus, selected, c.nowUTC()) {
			if observedStatus.SnapshotAvailable {
				outcome := "not_due"
				if selected.mode == SelectorRevision {
					outcome = "pinned"
				}
				resolution := resolutionFromStatus(entry, observedStatus, false)
				return resolution, refreshResult(outcome, observedStatus), nil
			}
			if observedStatus.LastAttemptAt != "" && observedStatus.LastErrorCode != "" {
				return Resolution{}, RefreshResult{}, statusError(observedStatus)
			}
		}
	}
	if err := c.InitializeRepository(ctx, entry); err != nil {
		return c.completeSetupFailure(entry, selected, force, observedStatus, err)
	}
	observed, err := c.ReadState(entry)
	if err != nil {
		return Resolution{}, RefreshResult{}, err
	}
	observedAttempt := observed.LastAttemptAt
	observedRevision := observed.ResolvedRevision

	var resolution Resolution
	var refresh RefreshResult
	err = c.WithUpdateLock(entry, func() error {
		if err := cleanupInterruptedSnapshots(entry.SnapshotsPath, 4); err != nil {
			return err
		}
		state, err := c.ReadState(entry)
		if errors.Is(err, ErrInvalidCache) || errors.Is(err, os.ErrNotExist) {
			state = initialState(entry)
			state.Initialized = true
		} else if err != nil {
			return err
		}
		now := c.nowUTC()
		status, snapshot := c.statusFromState(entry, state, selected, now)
		if selected.mode == SelectorRevision && !force && status.SnapshotAvailable {
			resolution = resolutionFromStatus(entry, status, false)
			refresh = refreshResult("pinned", status)
			return nil
		}
		if !force && !status.SnapshotAvailable && status.LastErrorCode == "" &&
			selectedStateMatches(state, selected) && fullRevisionPattern.MatchString(state.ResolvedRevision) &&
			c.commitExists(ctx, entry, state.ResolvedRevision) {
			snapshot, _, materializeErr := c.materializeSnapshot(ctx, entry, state.ResolvedRevision)
			if materializeErr == nil {
				status, _ = c.statusFromState(entry, state, selected, now)
				resolution = resolutionFromStatus(entry, status, false)
				resolution.SourcePath = snapshot
				outcome := "not_due"
				if selected.mode == SelectorRevision {
					outcome = "pinned"
				}
				refresh = refreshResult(outcome, status)
				return nil
			}
		}
		if force && state.LastAttemptAt != observedAttempt {
			outcome := "unchanged"
			if status.Stale {
				outcome = "stale"
			} else if selected.mode == SelectorRevision {
				outcome = "pinned"
			} else if observedRevision != "" && observedRevision != status.SelectedRevision {
				outcome = "updated"
			}
			if !status.SnapshotAvailable {
				return statusError(status)
			}
			resolution = resolutionFromStatus(entry, status, outcome == "updated")
			refresh = refreshResult(outcome, status)
			return nil
		}
		if !force && !resolutionDue(status, selected, now) {
			if !status.SnapshotAvailable && status.LastAttemptAt != "" && status.LastErrorCode != "" {
				return statusError(status)
			}
			if status.SnapshotAvailable {
				resolution = resolutionFromStatus(entry, status, false)
				refresh = refreshResult("not_due", status)
				return nil
			}
		}

		previous := state.ResolvedRevision
		setStateSelector(&state, selected)
		revision, err := c.resolveRevision(ctx, entry, selected)
		if err == nil {
			snapshot, _, err = c.materializeSnapshot(ctx, entry, revision)
		}
		completedAt := c.nowUTC().Format(time.RFC3339Nano)
		state.LastAttemptAt = completedAt
		if err != nil {
			state.LastErrorCode = sourceErrorCode(err)
			if writeErr := c.WriteState(entry, state); writeErr != nil {
				return writeErr
			}
			if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
				return err
			}
			status, snapshot = c.statusFromState(entry, state, selected, c.nowUTC())
			if status.SnapshotAvailable {
				resolution = resolutionFromStatus(entry, status, false)
				refresh = refreshResult("stale", status)
				return nil
			}
			if state.LastErrorCode == "remote_source_unavailable" {
				return ErrRemoteSourceUnavailable
			}
			return err
		}
		state.LastSuccessAt = completedAt
		state.LastErrorCode = ""
		state.ResolvedRevision = revision
		state.SelectedSnapshot = revision
		state.SelectedMode = selected.mode
		state.SelectedRef = selected.ref
		state.SelectedRequest = selected.revision
		if err := c.WriteState(entry, state); err != nil {
			return err
		}
		status, _ = c.statusFromState(entry, state, selected, c.nowUTC())
		updated := previous != "" && previous != revision
		resolution = resolutionFromStatus(entry, status, updated)
		resolution.SourcePath = snapshot
		outcome := "unchanged"
		if selected.mode == SelectorRevision {
			outcome = "pinned"
		} else if previous == "" || previous != revision {
			outcome = "updated"
		}
		refresh = refreshResult(outcome, status)
		return nil
	})
	if err != nil {
		return Resolution{}, RefreshResult{}, err
	}
	return resolution, refresh, nil
}

func (c *Cache) completeSetupFailure(entry Entry, selected selector, force bool, observed vfs.SourceStatus, cause error) (Resolution, RefreshResult, error) {
	if !isOperationalGitError(cause) {
		return Resolution{}, RefreshResult{}, cause
	}
	var resolution Resolution
	var refresh RefreshResult
	var resultErr error
	err := c.WithUpdateLock(entry, func() error {
		if err := cleanupInterruptedSnapshots(entry.SnapshotsPath, 4); err != nil {
			return err
		}
		state, err := c.ReadState(entry)
		if errors.Is(err, os.ErrNotExist) {
			state = initialState(entry)
		} else if err != nil {
			return err
		}
		now := c.nowUTC()
		status, _ := c.statusFromState(entry, state, selected, now)
		if state.LastAttemptAt != observed.LastAttemptAt {
			if status.SnapshotAvailable {
				outcome := "not_due"
				updated := false
				if force {
					outcome = "unchanged"
					if status.Stale {
						outcome = "stale"
					} else if selected.mode == SelectorRevision {
						outcome = "pinned"
					} else if observed.SelectedRevision != "" && observed.SelectedRevision != status.SelectedRevision {
						outcome = "updated"
						updated = true
					}
				}
				resolution = resolutionFromStatus(entry, status, updated)
				refresh = refreshResult(outcome, status)
				return nil
			}
			if status.LastAttemptAt != "" && status.LastErrorCode != "" {
				resultErr = statusError(status)
				return nil
			}
		}

		setStateSelector(&state, selected)
		status, _ = c.statusFromState(entry, state, selected, now)
		if status.LastErrorCode == "validation_failed" {
			return ErrInvalidCache
		}
		state.LastAttemptAt = now.Format(time.RFC3339Nano)
		state.LastErrorCode = sourceErrorCode(cause)
		if err := c.WriteState(entry, state); err != nil {
			return err
		}
		status, _ = c.statusFromState(entry, state, selected, now)
		if status.SnapshotAvailable {
			resolution = resolutionFromStatus(entry, status, false)
			refresh = refreshResult("stale", status)
			return nil
		}
		resultErr = ErrRemoteSourceUnavailable
		return nil
	})
	if err != nil {
		return Resolution{}, RefreshResult{}, err
	}
	if resultErr != nil {
		return Resolution{}, RefreshResult{}, resultErr
	}
	return resolution, refresh, nil
}

func isOperationalGitError(err error) bool {
	return errors.Is(err, ErrGitUnavailable) || errors.Is(err, ErrGitTimeout) ||
		errors.Is(err, ErrGitCommand) || errors.Is(err, ErrRemoteSourceUnavailable)
}

func (c *Cache) statusFromState(entry Entry, state State, selected selector, now time.Time) (vfs.SourceStatus, string) {
	status := vfs.SourceStatus{
		MountPath:      entry.MountPath,
		Source:         entry.Source,
		Kind:           vfs.SourceKindGit,
		SelectorMode:   selected.mode,
		IntentRef:      selected.ref,
		IntentRevision: selected.revision,
	}
	if !stateMatchesSelector(state, selected) {
		status.RefreshDue = selected.mode != SelectorRevision
		return status, ""
	}
	status.LastAttemptAt = state.LastAttemptAt
	status.LastSuccessAt = state.LastSuccessAt
	status.LastErrorCode = state.LastErrorCode
	snapshot := ""
	if selectedStateMatches(state, selected) && state.SelectedSnapshot != "" && state.ResolvedRevision == state.SelectedSnapshot {
		candidate := filepath.Join(entry.SnapshotsPath, state.SelectedSnapshot)
		if err := validateSnapshot(entry.SnapshotsPath, candidate); err == nil {
			status.SnapshotAvailable = true
			status.SelectedRevision = state.ResolvedRevision
			snapshot = candidate
		} else if !errors.Is(err, os.ErrNotExist) {
			status.LastErrorCode = "validation_failed"
		}
	}
	status.RefreshDue = selected.mode != SelectorRevision && refreshDue(state.LastAttemptAt, now)
	status.Stale = status.SnapshotAvailable && state.LastErrorCode != ""
	if status.Stale {
		status.Warning = staleWarning(entry.MountPath)
	}
	return status, snapshot
}

func stateMatchesSelector(state State, selected selector) bool {
	return state.SelectorMode == selected.mode && state.Ref == selected.ref && state.RequestedRevision == selected.revision
}

func selectedStateMatches(state State, selected selector) bool {
	return state.SelectedMode == selected.mode && state.SelectedRef == selected.ref && state.SelectedRequest == selected.revision
}

func setStateSelector(state *State, selected selector) {
	if !stateMatchesSelector(*state, selected) {
		state.LastAttemptAt = ""
		state.LastSuccessAt = ""
		state.LastErrorCode = ""
	}
	state.SelectorMode = selected.mode
	state.Ref = selected.ref
	state.RequestedRevision = selected.revision
}

func refreshDue(lastAttempt string, now time.Time) bool {
	if lastAttempt == "" {
		return true
	}
	attemptedAt, err := time.Parse(time.RFC3339Nano, lastAttempt)
	if err != nil {
		return true
	}
	if now.Before(attemptedAt) {
		return false
	}
	return !now.Before(attemptedAt.Add(refreshInterval))
}

func resolutionDue(status vfs.SourceStatus, selected selector, now time.Time) bool {
	if selected.mode != SelectorRevision {
		return status.RefreshDue
	}
	return !status.SnapshotAvailable && refreshDue(status.LastAttemptAt, now)
}

func resolutionFromStatus(entry Entry, status vfs.SourceStatus, updated bool) Resolution {
	return Resolution{
		SourcePath:   filepath.Join(entry.SnapshotsPath, status.SelectedRevision),
		Revision:     status.SelectedRevision,
		SelectorMode: status.SelectorMode,
		Ref:          status.IntentRef,
		Updated:      updated,
		Status:       status,
		Warning:      status.Warning,
	}
}

func refreshResult(outcome string, status vfs.SourceStatus) RefreshResult {
	warning := status.Warning
	status.Warning = nil
	return RefreshResult{MountPath: status.MountPath, Outcome: outcome, Status: status, Warning: warning}
}

func staleWarning(mountPath string) *vfs.SourceWarning {
	return &vfs.SourceWarning{Code: "stale_source", Message: "Git refresh failed; using the last successful snapshot.", MountPath: mountPath}
}

func sourceErrorCode(err error) string {
	if errors.Is(err, ErrInvalidIntent) || errors.Is(err, ErrInvalidCache) || errors.Is(err, ErrSnapshotSymlink) {
		return "validation_failed"
	}
	if errors.Is(err, ErrRevisionNotAvailable) {
		return "revision_not_available"
	}
	return "remote_source_unavailable"
}

func statusError(status vfs.SourceStatus) error {
	if status.LastErrorCode == "revision_not_available" {
		return ErrRevisionNotAvailable
	}
	return ErrRemoteSourceUnavailable
}

func validateIntent(intent Intent) (selector, error) {
	if intent.Writable {
		return selector{}, ErrGitSourceLocked
	}
	if _, err := vfs.ValidateMountPath(intent.MountPath); err != nil {
		return selector{}, err
	}
	refSet := intent.RefSet || intent.Ref != ""
	revisionSet := intent.RevisionSet || intent.Revision != ""
	versionSet := intent.VersionSet || intent.Version != ""
	if refSet && revisionSet {
		return selector{}, fmt.Errorf("%w: ref and revision cannot be combined", ErrInvalidIntent)
	}
	if versionSet {
		return selector{}, fmt.Errorf("%w: version is not a Git selector", ErrInvalidIntent)
	}
	if revisionSet {
		if intent.Revision == "" {
			return selector{}, fmt.Errorf("%w: revision must not be empty", ErrInvalidIntent)
		}
		if !fullRevisionPattern.MatchString(intent.Revision) {
			return selector{}, fmt.Errorf("%w: revision must be a full 40-hex SHA-1 commit identifier", ErrInvalidIntent)
		}
		return selector{mode: SelectorRevision, revision: strings.ToLower(intent.Revision)}, nil
	}
	if refSet {
		if intent.Ref == "" {
			return selector{}, fmt.Errorf("%w: ref must not be empty", ErrInvalidIntent)
		}
		if !validRef(intent.Ref) {
			return selector{}, fmt.Errorf("%w: ref is invalid", ErrInvalidIntent)
		}
		return selector{mode: SelectorRef, ref: intent.Ref}, nil
	}
	return selector{mode: SelectorHead}, nil
}

// ValidateMountIntent validates Git-specific source, selector, and capability
// rules without creating cache state or contacting the remote.
func ValidateMountIntent(intent Intent) error {
	if _, err := validateIntent(intent); err != nil {
		return err
	}
	return ValidateSource(intent.Source)
}

func validRef(ref string) bool {
	if ref == "" || strings.HasPrefix(ref, "-") || strings.HasPrefix(ref, ".") ||
		strings.HasSuffix(ref, "/") || strings.HasSuffix(ref, ".") ||
		strings.Contains(ref, "..") || strings.Contains(ref, "@{") ||
		strings.Contains(ref, "//") || strings.ContainsAny(ref, " ~^:?*[\\") {
		return false
	}
	for _, part := range strings.Split(ref, "/") {
		if part == "" || strings.HasPrefix(part, ".") || strings.HasSuffix(part, ".lock") {
			return false
		}
	}
	for _, r := range ref {
		if r < 0x20 || r == 0x7f {
			return false
		}
	}
	return true
}

func (c *Cache) resolveRevision(ctx context.Context, entry Entry, selected selector) (string, error) {
	if selected.mode == SelectorRevision {
		if c.commitExists(ctx, entry, selected.revision) {
			return selected.revision, nil
		}
		if _, err := c.runner.Run(ctx, "", "-C", entry.RepositoryPath, "fetch", "--depth=1", "--no-tags", "--no-recurse-submodules", "--force", "--", "origin", selected.revision); err != nil {
			if reachable, probeErr := c.remoteReachable(ctx, entry); probeErr == nil && reachable {
				return "", ErrRevisionNotAvailable
			}
			return "", err
		}
		if !c.commitExists(ctx, entry, selected.revision) {
			return "", ErrRevisionNotAvailable
		}
		return selected.revision, nil
	}

	remoteRef := "HEAD"
	if selected.mode == SelectorRef {
		remoteRef = selected.ref
	}
	if _, err := c.runner.Run(ctx, "", "-C", entry.RepositoryPath, "fetch", "--depth=1", "--no-tags", "--no-recurse-submodules", "--force", "--", "origin", remoteRef); err != nil {
		if available, probeErr := c.remoteRefAvailable(ctx, entry, remoteRef); probeErr == nil && !available {
			return "", ErrRevisionNotAvailable
		}
		return "", err
	}
	output, err := c.runner.Run(ctx, "", "-C", entry.RepositoryPath, "rev-parse", "--verify", "FETCH_HEAD^{commit}")
	if err != nil {
		return "", ErrRevisionNotAvailable
	}
	revision := strings.ToLower(strings.TrimSpace(string(output)))
	if !fullRevisionPattern.MatchString(revision) || !c.commitExists(ctx, entry, revision) {
		return "", ErrRevisionNotAvailable
	}
	return revision, nil
}

func (c *Cache) remoteReachable(ctx context.Context, entry Entry) (bool, error) {
	_, err := c.runner.Run(ctx, "", "-C", entry.RepositoryPath, "ls-remote", "--refs", "origin")
	if err != nil {
		return false, err
	}
	return true, nil
}

func (c *Cache) remoteRefAvailable(ctx context.Context, entry Entry, ref string) (bool, error) {
	output, err := c.runner.Run(ctx, "", "-C", entry.RepositoryPath, "ls-remote", "origin", ref)
	if err != nil {
		return false, err
	}
	return strings.TrimSpace(string(output)) != "", nil
}

func (c *Cache) commitExists(ctx context.Context, entry Entry, revision string) bool {
	output, err := c.runner.Run(ctx, "", "-C", entry.RepositoryPath, "cat-file", "-t", revision)
	return err == nil && strings.TrimSpace(string(output)) == "commit"
}

func (c *Cache) materializeSnapshot(ctx context.Context, entry Entry, revision string) (string, bool, error) {
	if err := ensurePrivateDirectory(entry.SnapshotsPath); err != nil {
		return "", false, err
	}
	target := filepath.Join(entry.SnapshotsPath, revision)
	if err := validateManagedPath(entry.SnapshotsPath, target); err != nil {
		return "", false, err
	}
	if err := validateSnapshot(entry.SnapshotsPath, target); err == nil {
		return target, false, nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return "", false, err
	}
	if err := c.rejectCommitSymlinks(ctx, entry, revision); err != nil {
		return "", false, err
	}
	temporary, err := os.MkdirTemp(entry.SnapshotsPath, ".snapshot-*")
	if err != nil {
		return "", false, err
	}
	defer func() {
		_ = makeTreeWritable(temporary)
		_ = os.RemoveAll(temporary)
	}()
	indexFile, err := os.CreateTemp(entry.Dir, ".index-*")
	if err != nil {
		return "", false, err
	}
	indexPath := indexFile.Name()
	if err := indexFile.Close(); err != nil {
		return "", false, err
	}
	if err := os.Remove(indexPath); err != nil {
		return "", false, err
	}
	defer os.Remove(indexPath)
	gitDir := "--git-dir=" + entry.RepositoryPath
	workTree := "--work-tree=" + temporary
	if _, err := c.runner.runWithManagedIndex(ctx, "", indexPath, gitDir, workTree, "read-tree", revision); err != nil {
		return "", false, err
	}
	if _, err := c.runner.runWithManagedIndex(ctx, "", indexPath, gitDir, workTree, "checkout-index", "--all", "--force"); err != nil {
		return "", false, err
	}
	if err := rejectTreeSymlinks(temporary, ""); err != nil {
		return "", false, err
	}
	if err := makeSnapshotReadOnly(temporary); err != nil {
		return "", false, err
	}
	if err := os.Rename(temporary, target); err != nil {
		if validateErr := validateSnapshot(entry.SnapshotsPath, target); validateErr == nil {
			return target, false, nil
		}
		return "", false, err
	}
	return target, true, nil
}

func (c *Cache) rejectCommitSymlinks(ctx context.Context, entry Entry, revision string) error {
	output, err := c.runner.Run(ctx, "", "-C", entry.RepositoryPath, "ls-tree", "-r", "-z", "--full-tree", revision)
	if err != nil {
		return err
	}
	for _, record := range strings.Split(string(output), "\x00") {
		if strings.HasPrefix(record, "120000 ") {
			return ErrSnapshotSymlink
		}
	}
	return nil
}

func validateSnapshot(base, snapshot string) error {
	if err := validateManagedPath(base, snapshot); err != nil {
		return err
	}
	if err := requireSafeDirectory(base); err != nil {
		return err
	}
	info, err := os.Lstat(snapshot)
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return fmt.Errorf("%w: snapshot is not a safe directory", ErrInvalidCache)
	}
	return rejectTreeSymlinks(snapshot, "")
}

func rejectTreeSymlinks(root, skip string) error {
	return filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if skip != "" && path == skip {
			return filepath.SkipDir
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return ErrSnapshotSymlink
		}
		return nil
	})
}

func makeSnapshotReadOnly(root string) error {
	var directories []string
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			directories = append(directories, path)
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		mode := os.FileMode(0o444)
		if info.Mode().Perm()&0o111 != 0 {
			mode = 0o555
		}
		return os.Chmod(path, mode)
	})
	if err != nil {
		return err
	}
	for index := len(directories) - 1; index >= 0; index-- {
		if err := os.Chmod(directories[index], 0o700); err != nil {
			return err
		}
	}
	return nil
}

func makeTreeWritable(root string) error {
	return filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("%w: interrupted snapshot contains a symlink", ErrInvalidCache)
		}
		if entry.IsDir() {
			return os.Chmod(path, 0o700)
		}
		return os.Chmod(path, 0o600)
	})
}

func cleanupInterruptedSnapshots(snapshotsPath string, limit int) error {
	info, err := os.Lstat(snapshotsPath)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return fmt.Errorf("%w: snapshots path is not a safe directory", ErrInvalidCache)
	}
	entries, err := os.ReadDir(snapshotsPath)
	if err != nil {
		return err
	}
	var interrupted []string
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".snapshot-") {
			if entry.Type()&os.ModeSymlink != 0 || !entry.IsDir() {
				return fmt.Errorf("%w: interrupted snapshot is not a safe directory", ErrInvalidCache)
			}
			interrupted = append(interrupted, filepath.Join(snapshotsPath, entry.Name()))
		}
	}
	sort.Strings(interrupted)
	if len(interrupted) > limit {
		interrupted = interrupted[:limit]
	}
	for _, path := range interrupted {
		if err := makeTreeWritable(path); err != nil {
			return err
		}
		if err := os.RemoveAll(path); err != nil {
			return err
		}
	}
	return nil
}
