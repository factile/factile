package gitsource

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestLazyFreshnessExplicitRefreshAndStaleFallback(t *testing.T) {
	ctx := context.Background()
	fixture := newResolutionFixture(t)
	var fetches atomic.Int32
	runner := fixture.runner
	runner.command = countingGitFactory(&fetches, nil, nil)
	cache, err := OpenCache(resolveGitSourceWorkspace(t, writeGitSourceRoot(t)), runner)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 7, 13, 0, 0, 0, 0, time.UTC)
	cache.now = func() time.Time { return now }
	intent := Intent{MountPath: "/coding", Source: fixture.remote, Ref: "main"}

	initial, err := cache.Resolve(ctx, intent)
	if err != nil {
		t.Fatal(err)
	}
	if fetches.Load() != 1 || initial.Status.LastAttemptAt != now.Format(time.RFC3339Nano) || initial.Status.RefreshDue {
		t.Fatalf("unexpected initial resolution: fetches=%d %#v", fetches.Load(), initial)
	}

	now = now.Add(24*time.Hour - time.Nanosecond)
	beforeBoundary, err := cache.Resolve(ctx, intent)
	if err != nil {
		t.Fatal(err)
	}
	if fetches.Load() != 1 || beforeBoundary.Status.RefreshDue {
		t.Fatalf("refresh occurred before 24 hours: fetches=%d %#v", fetches.Load(), beforeBoundary.Status)
	}

	now = now.Add(time.Nanosecond)
	updatedRevision := fixture.commitOnBranch(t, "main", "main v2")
	atBoundary, err := cache.Resolve(ctx, intent)
	if err != nil {
		t.Fatal(err)
	}
	if fetches.Load() != 2 || atBoundary.Revision != updatedRevision || !atBoundary.Updated {
		t.Fatalf("eligible refresh did not update: fetches=%d %#v", fetches.Load(), atBoundary)
	}

	now = now.Add(time.Hour)
	explicit, err := cache.Refresh(ctx, intent)
	if err != nil {
		t.Fatal(err)
	}
	if fetches.Load() != 3 || explicit.Outcome != "unchanged" || explicit.Status.LastAttemptAt != now.Format(time.RFC3339Nano) {
		t.Fatalf("explicit refresh did not bypass interval: fetches=%d %#v", fetches.Load(), explicit)
	}

	now = now.Add(25 * time.Hour)
	offlinePath := fixture.remotePath + ".offline"
	if err := os.Rename(fixture.remotePath, offlinePath); err != nil {
		t.Fatal(err)
	}
	stale, err := cache.Resolve(ctx, intent)
	if err != nil {
		t.Fatal(err)
	}
	if fetches.Load() != 4 || !stale.Status.Stale || stale.Status.LastErrorCode != "remote_source_unavailable" || stale.Warning == nil {
		t.Fatalf("offline read did not use stale snapshot: fetches=%d %#v", fetches.Load(), stale)
	}
	repeated, err := cache.Resolve(ctx, intent)
	if err != nil {
		t.Fatal(err)
	}
	if fetches.Load() != 4 || !repeated.Status.Stale {
		t.Fatalf("failed refresh was retried immediately: fetches=%d %#v", fetches.Load(), repeated)
	}
	forcedStale, err := cache.Refresh(ctx, intent)
	if err != nil {
		t.Fatal(err)
	}
	if fetches.Load() != 5 || forcedStale.Outcome != "stale" || forcedStale.Warning == nil {
		t.Fatalf("explicit stale retry did not run: fetches=%d %#v", fetches.Load(), forcedStale)
	}
	beforeStatus := fetches.Load()
	status, err := cache.Status(intent)
	if err != nil || !status.Stale || fetches.Load() != beforeStatus {
		t.Fatalf("status was not offline: fetches=%d status=%#v err=%v", fetches.Load(), status, err)
	}
}

func TestRefreshSetupFailureUsesCachedSnapshot(t *testing.T) {
	ctx := context.Background()
	fixture := newResolutionFixture(t)
	cache, err := OpenCache(resolveGitSourceWorkspace(t, writeGitSourceRoot(t)), fixture.runner)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 7, 13, 0, 0, 0, 0, time.UTC)
	cache.now = func() time.Time { return now }
	intent := Intent{MountPath: "/coding", Source: fixture.remote, Ref: "main"}
	initial, err := cache.Resolve(ctx, intent)
	if err != nil {
		t.Fatal(err)
	}

	var commands atomic.Int32
	missingRunner := NewRunner()
	missingRunner.GitPath = filepath.Join(t.TempDir(), "missing-git")
	missingRunner.command = func(ctx context.Context, name string, args ...string) *exec.Cmd {
		commands.Add(1)
		return exec.CommandContext(ctx, name, args...)
	}
	cache.runner = missingRunner
	now = now.Add(refreshInterval)
	stale, err := cache.Resolve(ctx, intent)
	if err != nil {
		t.Fatal(err)
	}
	if stale.SourcePath != initial.SourcePath || !stale.Status.Stale || stale.Status.LastErrorCode != "remote_source_unavailable" || stale.Warning == nil {
		t.Fatalf("setup failure did not retain the snapshot: %#v", stale)
	}
	if stale.Status.LastAttemptAt != now.Format(time.RFC3339Nano) {
		t.Fatalf("failed attempt time = %q, want %q", stale.Status.LastAttemptAt, now.Format(time.RFC3339Nano))
	}
	afterFailure := commands.Load()
	now = now.Add(time.Hour)
	repeated, err := cache.Resolve(ctx, intent)
	if err != nil {
		t.Fatal(err)
	}
	if !repeated.Status.Stale || commands.Load() != afterFailure {
		t.Fatalf("stale setup failure was retried early: commands=%d -> %d, %#v", afterFailure, commands.Load(), repeated)
	}
	forced, err := cache.Refresh(ctx, intent)
	if err != nil {
		t.Fatal(err)
	}
	if forced.Outcome != "stale" || forced.Warning == nil || commands.Load() != afterFailure+1 {
		t.Fatalf("explicit setup retry = commands=%d, %#v", commands.Load(), forced)
	}

	lastAttempt := forced.Status.LastAttemptAt
	now = now.Add(refreshInterval)
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := cache.Resolve(canceled, intent); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled setup error = %v", err)
	}
	status, err := cache.Status(intent)
	if err != nil || status.LastAttemptAt != lastAttempt {
		t.Fatalf("cancellation was recorded as a stale source attempt: %#v %v", status, err)
	}
}

func TestRefreshSetupCommandFailureUsesCachedSnapshot(t *testing.T) {
	ctx := context.Background()
	fixture := newResolutionFixture(t)
	cache, err := OpenCache(resolveGitSourceWorkspace(t, writeGitSourceRoot(t)), fixture.runner)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 7, 13, 0, 0, 0, 0, time.UTC)
	cache.now = func() time.Time { return now }
	intent := Intent{MountPath: "/coding", Source: fixture.remote}
	if _, err := cache.Resolve(ctx, intent); err != nil {
		t.Fatal(err)
	}
	t.Setenv("FACTILE_GIT_HELPER", "secret-error")
	cache.runner = helperRunner(5 * time.Second)
	now = now.Add(refreshInterval)
	resolved, err := cache.Resolve(ctx, intent)
	if err != nil {
		t.Fatal(err)
	}
	if !resolved.Status.Stale || resolved.Status.LastErrorCode != "remote_source_unavailable" || resolved.Warning == nil {
		t.Fatalf("Git command setup failure did not use stale snapshot: %#v", resolved)
	}
}

func TestMissingGitWithoutSnapshotRecordsAndSuppressesFailure(t *testing.T) {
	ctx := context.Background()
	var commands atomic.Int32
	runner := NewRunner()
	runner.GitPath = filepath.Join(t.TempDir(), "missing-git")
	runner.command = func(ctx context.Context, name string, args ...string) *exec.Cmd {
		commands.Add(1)
		return exec.CommandContext(ctx, name, args...)
	}
	cache, err := OpenCache(resolveGitSourceWorkspace(t, writeGitSourceRoot(t)), runner)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 7, 13, 0, 0, 0, 0, time.UTC)
	cache.now = func() time.Time { return now }
	intent := Intent{MountPath: "/missing", Source: fileRemote(t, filepath.Join(t.TempDir(), "remote.git"))}
	if _, err := cache.Resolve(ctx, intent); !errors.Is(err, ErrRemoteSourceUnavailable) {
		t.Fatalf("missing Git error = %v", err)
	}
	status, err := cache.Status(intent)
	if err != nil || status.SnapshotAvailable || status.LastAttemptAt != now.Format(time.RFC3339Nano) || status.LastErrorCode != "remote_source_unavailable" || status.RefreshDue {
		t.Fatalf("missing Git failure state = %#v, %v", status, err)
	}
	afterFailure := commands.Load()
	now = now.Add(time.Hour)
	if _, err := cache.Resolve(ctx, intent); !errors.Is(err, ErrRemoteSourceUnavailable) {
		t.Fatalf("suppressed missing Git error = %v", err)
	}
	if commands.Load() != afterFailure {
		t.Fatalf("missing Git was retried early: %d -> %d", afterFailure, commands.Load())
	}
	if _, err := cache.Refresh(ctx, intent); !errors.Is(err, ErrRemoteSourceUnavailable) {
		t.Fatalf("explicit missing Git error = %v", err)
	}
	if commands.Load() != afterFailure+1 {
		t.Fatalf("explicit missing Git retry count = %d, want %d", commands.Load(), afterFailure+1)
	}
}

func TestRefreshSetupFailureDoesNotHideUnsafeSnapshotState(t *testing.T) {
	ctx := context.Background()
	fixture := newResolutionFixture(t)
	cache, err := OpenCache(resolveGitSourceWorkspace(t, writeGitSourceRoot(t)), fixture.runner)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 7, 13, 0, 0, 0, 0, time.UTC)
	cache.now = func() time.Time { return now }
	intent := Intent{MountPath: "/coding", Source: fixture.remote}
	if _, err := cache.Resolve(ctx, intent); err != nil {
		t.Fatal(err)
	}
	entry, err := cache.Entry(intent.MountPath, intent.Source)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(t.TempDir(), filepath.Join(entry.SnapshotsPath, ".snapshot-hostile")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	missingRunner := NewRunner()
	missingRunner.GitPath = filepath.Join(t.TempDir(), "missing-git")
	cache.runner = missingRunner
	now = now.Add(refreshInterval)
	if _, err := cache.Resolve(ctx, intent); !errors.Is(err, ErrInvalidCache) {
		t.Fatalf("unsafe interrupted snapshot error = %v", err)
	}
}

func TestFreshnessHandlesClockSkewAndSuppressesMissingCacheFailures(t *testing.T) {
	ctx := context.Background()
	fixture := newResolutionFixture(t)
	var fetches atomic.Int32
	runner := fixture.runner
	runner.command = countingGitFactory(&fetches, nil, nil)
	cache, err := OpenCache(resolveGitSourceWorkspace(t, writeGitSourceRoot(t)), runner)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 7, 13, 12, 0, 0, 0, time.UTC)
	cache.now = func() time.Time { return now }
	intent := Intent{MountPath: "/coding", Source: fixture.remote}
	if _, err := cache.Resolve(ctx, intent); err != nil {
		t.Fatal(err)
	}
	now = now.Add(-time.Hour)
	if _, err := cache.Resolve(ctx, intent); err != nil {
		t.Fatal(err)
	}
	if fetches.Load() != 1 {
		t.Fatalf("clock skew caused a refresh: %d", fetches.Load())
	}

	missing := Intent{MountPath: "/missing", Source: fileRemote(t, t.TempDir()+"/missing.git")}
	if _, err := cache.Resolve(ctx, missing); !errors.Is(err, ErrRemoteSourceUnavailable) {
		t.Fatalf("missing cache error = %v", err)
	}
	afterFailure := fetches.Load()
	if _, err := cache.Resolve(ctx, missing); !errors.Is(err, ErrRemoteSourceUnavailable) {
		t.Fatalf("suppressed missing cache error = %v", err)
	}
	if fetches.Load() != afterFailure {
		t.Fatalf("missing cache failure retried immediately: before=%d after=%d", afterFailure, fetches.Load())
	}
	if _, err := cache.Refresh(ctx, missing); !errors.Is(err, ErrRemoteSourceUnavailable) {
		t.Fatalf("explicit missing cache retry error = %v", err)
	}
	if fetches.Load() != afterFailure+1 {
		t.Fatalf("explicit missing cache retry did not fetch: before=%d after=%d", afterFailure, fetches.Load())
	}
}

func TestPinnedStatusAndRefreshRemainStable(t *testing.T) {
	ctx := context.Background()
	fixture := newResolutionFixture(t)
	var fetches atomic.Int32
	runner := fixture.runner
	runner.command = countingGitFactory(&fetches, nil, nil)
	cache, err := OpenCache(resolveGitSourceWorkspace(t, writeGitSourceRoot(t)), runner)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 7, 13, 0, 0, 0, 0, time.UTC)
	cache.now = func() time.Time { return now }
	intent := Intent{MountPath: "/pinned", Source: fixture.remote, Revision: fixture.featureRevision}
	if _, err := cache.Resolve(ctx, intent); err != nil {
		t.Fatal(err)
	}
	initialFetches := fetches.Load()
	now = now.Add(7 * 24 * time.Hour)
	status, err := cache.Status(intent)
	if err != nil || status.RefreshDue || status.SelectedRevision != fixture.featureRevision || fetches.Load() != initialFetches {
		t.Fatalf("unexpected pinned status: fetches=%d %#v %v", fetches.Load(), status, err)
	}
	if _, err := cache.Resolve(ctx, intent); err != nil {
		t.Fatal(err)
	}
	if fetches.Load() != initialFetches {
		t.Fatalf("pinned read fetched again: %d -> %d", initialFetches, fetches.Load())
	}
	refreshed, err := cache.Refresh(ctx, intent)
	if err != nil {
		t.Fatal(err)
	}
	if refreshed.Outcome != "pinned" || refreshed.Status.LastAttemptAt != now.Format(time.RFC3339Nano) || fetches.Load() != initialFetches {
		t.Fatalf("unexpected pinned refresh: fetches=%d %#v", fetches.Load(), refreshed)
	}
}

func TestFailedPinnedAcquisitionRetriesAfterInterval(t *testing.T) {
	ctx := context.Background()
	fixture := newResolutionFixture(t)
	var fetches atomic.Int32
	runner := fixture.runner
	runner.command = countingGitFactory(&fetches, nil, nil)
	cache, err := OpenCache(resolveGitSourceWorkspace(t, writeGitSourceRoot(t)), runner)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 7, 13, 0, 0, 0, 0, time.UTC)
	cache.now = func() time.Time { return now }

	gitRun(t, fixture.runner, fixture.workPath, "checkout", "-b", "delayed", "main")
	writeResolutionContent(t, fixture.workPath, "delayed pin")
	gitRun(t, fixture.runner, fixture.workPath, "add", "--", ".")
	gitRun(t, fixture.runner, fixture.workPath, "commit", "-m", "delayed pin")
	delayedRevision := gitOutput(t, fixture.runner, fixture.workPath, "rev-parse", "HEAD")
	gitRun(t, fixture.runner, fixture.workPath, "checkout", "main")
	intent := Intent{MountPath: "/delayed", Source: fixture.remote, Revision: delayedRevision}
	if _, err := cache.Resolve(ctx, intent); !errors.Is(err, ErrRevisionNotAvailable) {
		t.Fatalf("initial unavailable pin error = %v", err)
	}
	initialFetches := fetches.Load()
	status, err := cache.Status(intent)
	if err != nil || status.RefreshDue || status.LastAttemptAt != now.Format(time.RFC3339Nano) || status.LastErrorCode != "revision_not_available" {
		t.Fatalf("initial pinned failure status = %#v, %v", status, err)
	}
	gitRun(t, fixture.runner, fixture.workPath, "push", "--", fixture.remote, "delayed:delayed")

	now = now.Add(refreshInterval - time.Nanosecond)
	if _, err := cache.Resolve(ctx, intent); !errors.Is(err, ErrRevisionNotAvailable) {
		t.Fatalf("pin retried before interval: %v", err)
	}
	if fetches.Load() != initialFetches {
		t.Fatalf("pin fetched before interval: %d -> %d", initialFetches, fetches.Load())
	}
	status, err = cache.Status(intent)
	if err != nil || status.RefreshDue {
		t.Fatalf("failed pin advertised refresh due: %#v, %v", status, err)
	}

	now = now.Add(time.Nanosecond)
	resolved, err := cache.Resolve(ctx, intent)
	if err != nil {
		t.Fatal(err)
	}
	if resolved.Revision != delayedRevision || resolved.Status.RefreshDue || fetches.Load() != initialFetches+1 {
		t.Fatalf("pin was not acquired exactly at the interval: fetches=%d %#v", fetches.Load(), resolved)
	}
	if err := os.RemoveAll(resolved.SourcePath); err != nil {
		t.Fatal(err)
	}
	afterAcquisition := fetches.Load()
	rehydrated, err := cache.Resolve(ctx, intent)
	if err != nil {
		t.Fatal(err)
	}
	if rehydrated.Revision != delayedRevision || fetches.Load() != afterAcquisition {
		t.Fatalf("pin did not rehydrate locally: fetches=%d %#v", fetches.Load(), rehydrated)
	}

	gitRun(t, fixture.runner, fixture.workPath, "checkout", "-b", "explicit", "main")
	writeResolutionContent(t, fixture.workPath, "explicit pin")
	gitRun(t, fixture.runner, fixture.workPath, "add", "--", ".")
	gitRun(t, fixture.runner, fixture.workPath, "commit", "-m", "explicit pin")
	explicitRevision := gitOutput(t, fixture.runner, fixture.workPath, "rev-parse", "HEAD")
	gitRun(t, fixture.runner, fixture.workPath, "checkout", "main")
	explicitIntent := Intent{MountPath: "/explicit", Source: fixture.remote, Revision: explicitRevision}
	if _, err := cache.Resolve(ctx, explicitIntent); !errors.Is(err, ErrRevisionNotAvailable) {
		t.Fatalf("initial explicit pin error = %v", err)
	}
	gitRun(t, fixture.runner, fixture.workPath, "push", "--", fixture.remote, "explicit:explicit")
	now = now.Add(time.Hour)
	beforeExplicit := fetches.Load()
	refreshed, err := cache.Refresh(ctx, explicitIntent)
	if err != nil {
		t.Fatal(err)
	}
	if refreshed.Outcome != "pinned" || refreshed.Status.SelectedRevision != explicitRevision || refreshed.Status.RefreshDue || fetches.Load() != beforeExplicit+1 {
		t.Fatalf("explicit pin retry = fetches=%d %#v", fetches.Load(), refreshed)
	}
}

func TestMissingSnapshotAndCorruptStateRecoverSafely(t *testing.T) {
	ctx := context.Background()
	fixture := newResolutionFixture(t)
	var fetches atomic.Int32
	runner := fixture.runner
	runner.command = countingGitFactory(&fetches, nil, nil)
	cache, err := OpenCache(resolveGitSourceWorkspace(t, writeGitSourceRoot(t)), runner)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 7, 13, 0, 0, 0, 0, time.UTC)
	cache.now = func() time.Time { return now }
	intent := Intent{MountPath: "/coding", Source: fixture.remote, Ref: "main"}
	resolved, err := cache.Resolve(ctx, intent)
	if err != nil {
		t.Fatal(err)
	}
	initialFetches := fetches.Load()
	if err := os.RemoveAll(resolved.SourcePath); err != nil {
		t.Fatal(err)
	}
	rehydrated, err := cache.Resolve(ctx, intent)
	if err != nil {
		t.Fatal(err)
	}
	if rehydrated.Revision != resolved.Revision || fetches.Load() != initialFetches {
		t.Fatalf("missing snapshot did not rehydrate locally: fetches=%d %#v", fetches.Load(), rehydrated)
	}

	entry, err := cache.Entry(intent.MountPath, intent.Source)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(entry.StatePath, []byte("not-json"), 0o600); err != nil {
		t.Fatal(err)
	}
	status, err := cache.Status(intent)
	if err != nil || status.LastErrorCode != "validation_failed" || status.SnapshotAvailable || fetches.Load() != initialFetches {
		t.Fatalf("corrupt status was not reported offline: fetches=%d %#v %v", fetches.Load(), status, err)
	}
	recovered, err := cache.Resolve(ctx, intent)
	if err != nil {
		t.Fatal(err)
	}
	if recovered.Revision != resolved.Revision || fetches.Load() != initialFetches+1 {
		t.Fatalf("corrupt state did not recover through one fetch: fetches=%d %#v", fetches.Load(), recovered)
	}
}

func TestConcurrentExplicitRefreshCoalescesOneFetch(t *testing.T) {
	ctx := context.Background()
	fixture := newResolutionFixture(t)
	var fetches atomic.Int32
	runner := fixture.runner
	cache, err := OpenCache(resolveGitSourceWorkspace(t, writeGitSourceRoot(t)), runner)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 7, 13, 0, 0, 0, 0, time.UTC)
	cache.now = func() time.Time { return now }
	intent := Intent{MountPath: "/coding", Source: fixture.remote, Ref: "main"}
	if _, err := cache.Resolve(ctx, intent); err != nil {
		t.Fatal(err)
	}

	entered := make(chan struct{})
	release := make(chan struct{})
	runner.command = countingGitFactory(&fetches, entered, release)
	cache.runner = runner
	now = now.Add(time.Hour)
	results := make(chan error, 2)
	var start sync.WaitGroup
	start.Add(2)
	for range 2 {
		go func() {
			start.Done()
			start.Wait()
			_, err := cache.Refresh(ctx, intent)
			results <- err
		}()
	}
	<-entered
	readDuringRefresh, err := cache.Resolve(ctx, intent)
	if err != nil {
		t.Fatalf("reader failed while refresh was in progress: %v", err)
	}
	assertResolutionContent(t, readDuringRefresh, fixture.mainRevision, "main v1")
	close(release)
	for range 2 {
		if err := <-results; err != nil {
			t.Fatal(err)
		}
	}
	if fetches.Load() != 1 {
		t.Fatalf("concurrent refreshes performed %d fetches", fetches.Load())
	}
}

func countingGitFactory(fetches *atomic.Int32, entered chan<- struct{}, release <-chan struct{}) commandFactory {
	var once sync.Once
	return func(ctx context.Context, name string, args ...string) *exec.Cmd {
		if containsExact(args, "fetch") {
			fetches.Add(1)
			if entered != nil {
				once.Do(func() {
					close(entered)
					<-release
				})
			}
		}
		return exec.CommandContext(ctx, name, args...)
	}
}
