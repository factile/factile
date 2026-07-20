package gitsource

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestResolveDefaultHeadNamedRefTagAndPinnedRevision(t *testing.T) {
	ctx := context.Background()
	fixture := newResolutionFixture(t)
	cache, err := OpenCache(resolveGitSourceWorkspace(t, writeGitSourceRoot(t)), fixture.runner)
	if err != nil {
		t.Fatal(err)
	}

	head, err := cache.Resolve(ctx, Intent{MountPath: "/coding", Source: fixture.remote})
	if err != nil {
		t.Fatal(err)
	}
	assertResolutionContent(t, head, fixture.mainRevision, "main v1")
	if head.SelectorMode != SelectorHead || head.Updated {
		t.Fatalf("unexpected HEAD resolution: %#v", head)
	}
	repeated, err := cache.Resolve(ctx, Intent{MountPath: "/coding", Source: fixture.remote})
	if err != nil {
		t.Fatal(err)
	}
	if repeated.SourcePath != head.SourcePath || repeated.Revision != head.Revision || repeated.Updated {
		t.Fatalf("same revision was not reused: first=%#v repeated=%#v", head, repeated)
	}

	branch, err := cache.Resolve(ctx, Intent{MountPath: "/coding", Source: fixture.remote, Ref: "feature"})
	if err != nil {
		t.Fatal(err)
	}
	assertResolutionContent(t, branch, fixture.featureRevision, "feature v1")
	if branch.SelectorMode != SelectorRef || branch.Ref != "feature" || !branch.Updated {
		t.Fatalf("unexpected branch resolution: %#v", branch)
	}

	tag, err := cache.Resolve(ctx, Intent{MountPath: "/coding", Source: fixture.remote, Ref: "v1"})
	if err != nil {
		t.Fatal(err)
	}
	assertResolutionContent(t, tag, fixture.mainRevision, "main v1")

	pinned, err := cache.Resolve(ctx, Intent{MountPath: "/pinned", Source: fixture.remote, Revision: fixture.featureRevision})
	if err != nil {
		t.Fatal(err)
	}
	assertResolutionContent(t, pinned, fixture.featureRevision, "feature v1")
	upperPinned, err := cache.Resolve(ctx, Intent{MountPath: "/pinned-upper", Source: fixture.remote, Revision: strings.ToUpper(fixture.featureRevision)})
	if err != nil {
		t.Fatal(err)
	}
	assertResolutionContent(t, upperPinned, fixture.featureRevision, "feature v1")
	if upperPinned.Status.IntentRevision != fixture.featureRevision {
		t.Fatalf("uppercase SHA-1 pin was not normalized: %#v", upperPinned.Status)
	}
	offlineRemote := fixture.remotePath + ".offline"
	if err := os.Rename(fixture.remotePath, offlineRemote); err != nil {
		t.Fatal(err)
	}
	cached, err := cache.Resolve(ctx, Intent{MountPath: "/pinned", Source: fixture.remote, Revision: fixture.featureRevision})
	if err != nil {
		t.Fatalf("cached pin required network access: %v", err)
	}
	if cached.SourcePath != pinned.SourcePath || cached.Revision != pinned.Revision {
		t.Fatalf("cached pin changed: first=%#v cached=%#v", pinned, cached)
	}

	if _, err := os.Stat(filepath.Join(cached.SourcePath, ".git")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("snapshot contains Git metadata: %v", err)
	}
	if info, err := os.Stat(filepath.Join(cached.SourcePath, "overview.md")); err != nil || info.Mode().Perm()&0o222 != 0 {
		t.Fatalf("snapshot file is not read-only: info=%v err=%v", info, err)
	}
	status, err := fixture.runner.Run(ctx, "", "-C", fixture.workPath, "status", "--porcelain")
	if err != nil || strings.TrimSpace(string(status)) != "" {
		t.Fatalf("source worktree was mutated: %s %v", status, err)
	}
}

func TestResolveRejectsSHA256RevisionBeforeGitOrEntryMutation(t *testing.T) {
	root := writeGitSourceRoot(t)
	runner := NewRunner()
	runner.GitPath = filepath.Join(t.TempDir(), "missing-git")
	cache, err := OpenCache(resolveGitSourceWorkspace(t, root), runner)
	if err != nil {
		t.Fatal(err)
	}
	source := fileRemote(t, filepath.Join(t.TempDir(), "remote.git"))
	for _, tc := range []struct {
		name     string
		revision string
	}{
		{name: "lowercase", revision: strings.Repeat("a", 64)},
		{name: "uppercase", revision: strings.Repeat("A", 64)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			mountPath := "/" + tc.name
			_, err := cache.Resolve(context.Background(), Intent{
				MountPath:   mountPath,
				Source:      source,
				Revision:    tc.revision,
				RevisionSet: true,
			})
			if !errors.Is(err, ErrInvalidIntent) || !strings.Contains(err.Error(), "40-hex SHA-1") {
				t.Fatalf("64-hex revision error = %v, want SHA-1 validation error", err)
			}
			entry, entryErr := cache.entryPaths(mountPath, source)
			if entryErr != nil {
				t.Fatal(entryErr)
			}
			if _, statErr := os.Stat(entry.Dir); !errors.Is(statErr, os.ErrNotExist) {
				t.Fatalf("invalid revision created cache entry %s: %v", entry.Dir, statErr)
			}
		})
	}
}

func TestResolveRejectsURIQueryAndFragmentDelimitersBeforeGit(t *testing.T) {
	runner := NewRunner()
	gitCalls := 0
	runner.command = func(ctx context.Context, name string, args ...string) *exec.Cmd {
		gitCalls++
		return exec.CommandContext(ctx, name, args...)
	}
	cache, err := OpenCache(resolveGitSourceWorkspace(t, writeGitSourceRoot(t)), runner)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		mountPath string
		source    string
	}{
		{mountPath: "/native-query", source: "https://example.test/repository.git?"},
		{mountPath: "/native-fragment", source: "https://example.test/repository.git#"},
		{mountPath: "/git-plus-query", source: "git+https://example.test/repository.git?"},
		{mountPath: "/git-plus-fragment", source: "git+https://example.test/repository.git#"},
	} {
		_, err := cache.Resolve(context.Background(), Intent{MountPath: tc.mountPath, Source: tc.source})
		if !errors.Is(err, ErrInvalidCache) {
			t.Fatalf("source %q error = %v, want ErrInvalidCache", tc.source, err)
		}
	}
	if gitCalls != 0 {
		t.Fatalf("invalid URI delimiters invoked Git %d times", gitCalls)
	}
	entries, err := os.ReadDir(cache.base)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("invalid URI delimiters created cache entries: %v", entries)
	}
}

func TestResolveSHA256RepositoryReturnsStableNoSnapshotFailure(t *testing.T) {
	ctx := context.Background()
	runner := NewRunner()
	workPath := filepath.Join(t.TempDir(), "work")
	if _, err := runner.Run(ctx, "", "init", "--object-format=sha256", "--", workPath); err != nil {
		t.Skipf("installed Git cannot create a SHA-256 fixture: %v", err)
	}
	for _, setting := range [][]string{
		{"user.name", "Factile Test"},
		{"user.email", "factile@example.test"},
	} {
		gitRun(t, runner, workPath, "config", "--local", "--", setting[0], setting[1])
	}
	writeResolutionContent(t, workPath, "sha256 source")
	gitRun(t, runner, workPath, "add", "--", ".")
	gitRun(t, runner, workPath, "commit", "-m", "sha256 source")
	gitRun(t, runner, workPath, "branch", "-M", "main")
	if revision := gitOutput(t, runner, workPath, "rev-parse", "HEAD"); len(revision) != 64 {
		t.Fatalf("SHA-256 fixture revision has %d characters: %q", len(revision), revision)
	}
	remotePath := filepath.Join(t.TempDir(), "remote.git")
	gitRun(t, runner, "", "clone", "--bare", "--", workPath, remotePath)
	remote := fileRemote(t, remotePath)
	cache, err := OpenCache(resolveGitSourceWorkspace(t, writeGitSourceRoot(t)), runner)
	if err != nil {
		t.Fatal(err)
	}

	_, err = cache.Resolve(ctx, Intent{MountPath: "/sha256", Source: remote})
	if !errors.Is(err, ErrRemoteSourceUnavailable) || err.Error() != ErrRemoteSourceUnavailable.Error() {
		t.Fatalf("SHA-256 remote error = %v, want stable ErrRemoteSourceUnavailable", err)
	}
	entry, err := cache.Entry("/sha256", remote)
	if err != nil {
		t.Fatal(err)
	}
	state, err := cache.ReadState(entry)
	if err != nil {
		t.Fatal(err)
	}
	if state.ResolvedRevision != "" || state.SelectedSnapshot != "" || state.LastErrorCode != "remote_source_unavailable" {
		t.Fatalf("SHA-256 remote selected cache state: %#v", state)
	}
	status, err := cache.Status(Intent{MountPath: "/sha256", Source: remote})
	if err != nil {
		t.Fatal(err)
	}
	if status.SnapshotAvailable || status.SelectedRevision != "" || status.LastErrorCode != "remote_source_unavailable" {
		t.Fatalf("SHA-256 remote reported a usable snapshot: %#v", status)
	}
	if format := gitOutput(t, runner, entry.RepositoryPath, "rev-parse", "--show-object-format"); format != "sha1" {
		t.Fatalf("managed v1 repository object format = %q, want sha1", format)
	}
}

func TestResolveMovedRefAndFailureRetainsSelectedSnapshot(t *testing.T) {
	ctx := context.Background()
	fixture := newResolutionFixture(t)
	cache, err := OpenCache(resolveGitSourceWorkspace(t, writeGitSourceRoot(t)), fixture.runner)
	if err != nil {
		t.Fatal(err)
	}
	first, err := cache.Resolve(ctx, Intent{MountPath: "/coding", Source: fixture.remote, Ref: "main"})
	if err != nil {
		t.Fatal(err)
	}
	assertResolutionContent(t, first, fixture.mainRevision, "main v1")

	secondRevision := fixture.commitOnBranch(t, "main", "main v2")
	refreshed, err := cache.Refresh(ctx, Intent{MountPath: "/coding", Source: fixture.remote, Ref: "main"})
	if err != nil {
		t.Fatal(err)
	}
	if refreshed.Outcome != "updated" || refreshed.Status.SelectedRevision != secondRevision {
		t.Fatalf("moved ref refresh was not updated: %#v", refreshed)
	}
	second, err := cache.Resolve(ctx, Intent{MountPath: "/coding", Source: fixture.remote, Ref: "main"})
	if err != nil {
		t.Fatal(err)
	}
	assertResolutionContent(t, second, secondRevision, "main v2")
	if second.SourcePath == first.SourcePath {
		t.Fatalf("moved ref did not select a new immutable snapshot: first=%#v second=%#v", first, second)
	}
	assertFileContains(t, filepath.Join(first.SourcePath, "overview.md"), "main v1")

	entry, err := cache.Entry("/coding", fixture.remote)
	if err != nil {
		t.Fatal(err)
	}
	interrupted := filepath.Join(entry.SnapshotsPath, ".snapshot-interrupted")
	if err := os.MkdirAll(interrupted, 0o700); err != nil {
		t.Fatal(err)
	}
	if _, err := cache.Resolve(ctx, Intent{MountPath: "/coding", Source: fixture.remote, Ref: "missing"}); !errors.Is(err, ErrRevisionNotAvailable) {
		t.Fatalf("missing ref error = %v, want ErrRevisionNotAvailable", err)
	}
	if _, err := os.Stat(interrupted); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("interrupted snapshot was not cleaned up: %v", err)
	}
	state, err := cache.ReadState(entry)
	if err != nil {
		t.Fatal(err)
	}
	if state.ResolvedRevision != secondRevision || state.SelectedSnapshot != secondRevision {
		t.Fatalf("failed resolution replaced selected state: %#v", state)
	}
	assertResolutionContent(t, Resolution{SourcePath: filepath.Join(entry.SnapshotsPath, state.SelectedSnapshot), Revision: state.ResolvedRevision}, secondRevision, "main v2")

	gitRun(t, fixture.runner, fixture.workPath, "push", "--", fixture.remote, ":refs/heads/feature")
	if _, err := cache.Resolve(ctx, Intent{MountPath: "/coding", Source: fixture.remote, Ref: "feature"}); !errors.Is(err, ErrRevisionNotAvailable) {
		t.Fatalf("deleted ref error = %v, want ErrRevisionNotAvailable", err)
	}
	state, err = cache.ReadState(entry)
	if err != nil || state.ResolvedRevision != secondRevision {
		t.Fatalf("deleted ref replaced selected state: %#v %v", state, err)
	}

	objectsPath := filepath.Join(entry.RepositoryPath, "objects")
	objectsBackup := objectsPath + ".corrupt"
	if err := os.Rename(objectsPath, objectsBackup); err != nil {
		t.Fatal(err)
	}
	remoteOffline := fixture.remotePath + ".offline"
	if err := os.Rename(fixture.remotePath, remoteOffline); err != nil {
		t.Fatal(err)
	}
	if _, err := cache.Resolve(ctx, Intent{MountPath: "/coding", Source: fixture.remote, Ref: "main"}); err == nil {
		t.Fatal("corrupt cache with unavailable remote resolved")
	}
	state, err = cache.ReadState(entry)
	if err != nil || state.ResolvedRevision != secondRevision {
		t.Fatalf("corrupt cache failure replaced selected state: %#v %v", state, err)
	}
	assertFileContains(t, filepath.Join(entry.SnapshotsPath, state.SelectedSnapshot, "overview.md"), "main v2")
}

func TestResolveRejectsInvalidIntentAndRepositorySymlinks(t *testing.T) {
	fixture := newResolutionFixture(t)
	cache, err := OpenCache(resolveGitSourceWorkspace(t, writeGitSourceRoot(t)), fixture.runner)
	if err != nil {
		t.Fatal(err)
	}
	invalid := []Intent{
		{MountPath: "/coding", Source: fixture.remote, Writable: true},
		{MountPath: "/coding", Source: fixture.remote, Ref: "main", Revision: fixture.mainRevision},
		{MountPath: "/coding", Source: fixture.remote, Version: "v1"},
		{MountPath: "/coding", Source: fixture.remote, RefSet: true},
		{MountPath: "/coding", Source: fixture.remote, RevisionSet: true},
		{MountPath: "/coding", Source: fixture.remote, VersionSet: true},
		{MountPath: "/coding", Source: fixture.remote, Ref: "-upload-pack=evil"},
		{MountPath: "/coding", Source: fixture.remote, Ref: "refs/heads/bad ref"},
		{MountPath: "/coding", Source: fixture.remote, Revision: fixture.mainRevision[:12]},
	}
	for _, intent := range invalid {
		if _, err := cache.Resolve(context.Background(), intent); err == nil {
			t.Fatalf("invalid intent resolved: %#v", intent)
		}
	}

	if err := fixture.createSymlinkBranch(t); err != nil {
		t.Skipf("symlink fixture unavailable: %v", err)
	}
	good, err := cache.Resolve(context.Background(), Intent{MountPath: "/coding", Source: fixture.remote, Ref: "main"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := cache.Resolve(context.Background(), Intent{MountPath: "/coding", Source: fixture.remote, Ref: "symlink"}); !errors.Is(err, ErrSnapshotSymlink) {
		t.Fatalf("symlink commit error = %v", err)
	}
	entry, err := cache.Entry("/coding", fixture.remote)
	if err != nil {
		t.Fatal(err)
	}
	state, err := cache.ReadState(entry)
	if err != nil {
		t.Fatal(err)
	}
	if state.ResolvedRevision != good.Revision {
		t.Fatalf("symlink failure replaced selected state: %#v", state)
	}
}

func TestResolveEmptyRemoteAndMissingPinnedCommit(t *testing.T) {
	ctx := context.Background()
	runner := NewRunner()
	remotePath := filepath.Join(t.TempDir(), "empty.git")
	if _, err := runner.Run(ctx, "", "init", "--bare", "--", remotePath); err != nil {
		t.Fatal(err)
	}
	cache, err := OpenCache(resolveGitSourceWorkspace(t, writeGitSourceRoot(t)), runner)
	if err != nil {
		t.Fatal(err)
	}
	remote := fileRemote(t, remotePath)
	if _, err := cache.Resolve(ctx, Intent{MountPath: "/empty", Source: remote}); !errors.Is(err, ErrRevisionNotAvailable) {
		t.Fatalf("empty remote HEAD error = %v, want ErrRevisionNotAvailable", err)
	}
	if _, err := cache.Resolve(ctx, Intent{MountPath: "/empty", Source: remote, Revision: strings.Repeat("1", 40)}); !errors.Is(err, ErrRevisionNotAvailable) {
		t.Fatalf("missing pinned commit error = %v, want ErrRevisionNotAvailable", err)
	}
}

func TestDeletedSelectedRefUsesStaleSnapshot(t *testing.T) {
	ctx := context.Background()
	fixture := newResolutionFixture(t)
	cache, err := OpenCache(resolveGitSourceWorkspace(t, writeGitSourceRoot(t)), fixture.runner)
	if err != nil {
		t.Fatal(err)
	}
	intent := Intent{MountPath: "/coding", Source: fixture.remote, Ref: "feature"}
	resolved, err := cache.Resolve(ctx, intent)
	if err != nil {
		t.Fatal(err)
	}
	gitRun(t, fixture.runner, fixture.workPath, "push", "--", fixture.remote, ":refs/heads/feature")

	refreshed, err := cache.Refresh(ctx, intent)
	if err != nil {
		t.Fatal(err)
	}
	if refreshed.Outcome != "stale" || !refreshed.Status.Stale || refreshed.Status.LastErrorCode != "revision_not_available" || refreshed.Status.SelectedRevision != resolved.Revision {
		t.Fatalf("deleted ref did not retain a stale snapshot: %#v", refreshed)
	}
}

func TestCleanupInterruptedSnapshotsRejectsSymlinksWithoutTouchingTargets(t *testing.T) {
	snapshots := t.TempDir()
	outside := t.TempDir()
	if err := os.Chmod(outside, 0o755); err != nil {
		t.Fatal(err)
	}
	interrupted := filepath.Join(snapshots, ".snapshot-hostile")
	if err := os.Symlink(outside, interrupted); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}

	if err := cleanupInterruptedSnapshots(snapshots, 4); !errors.Is(err, ErrInvalidCache) {
		t.Fatalf("cleanup error = %v, want ErrInvalidCache", err)
	}
	info, err := os.Stat(outside)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o755 {
		t.Fatalf("outside target mode changed to %o", info.Mode().Perm())
	}
	if _, err := os.Lstat(interrupted); err != nil {
		t.Fatalf("hostile cache entry was changed: %v", err)
	}
}

func TestMakeTreeWritableRejectsNestedSymlinkWithoutTouchingTarget(t *testing.T) {
	root := t.TempDir()
	outside := filepath.Join(t.TempDir(), "outside.md")
	if err := os.WriteFile(outside, []byte("outside"), 0o444); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "linked.md")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}

	if err := makeTreeWritable(root); !errors.Is(err, ErrInvalidCache) {
		t.Fatalf("make writable error = %v, want ErrInvalidCache", err)
	}
	info, err := os.Stat(outside)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o444 {
		t.Fatalf("outside target mode changed to %o", info.Mode().Perm())
	}
}

func TestResolveDoesNotRunRemoteHooksInitializeSubmodulesOrDownloadLFS(t *testing.T) {
	ctx := context.Background()
	fixture := newResolutionFixture(t)
	gitRun(t, fixture.runner, fixture.workPath, "checkout", "-b", "adversarial", "main")
	if err := os.WriteFile(filepath.Join(fixture.workPath, ".gitattributes"), []byte("*.bin filter=lfs diff=lfs merge=lfs -text\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	pointer := "version https://git-lfs.github.com/spec/v1\noid sha256:" + strings.Repeat("1", 64) + "\nsize 123\n"
	if err := os.WriteFile(filepath.Join(fixture.workPath, "large.bin"), []byte(pointer), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(fixture.workPath, ".gitmodules"), []byte("[submodule \"vendor/dependency\"]\n\tpath = vendor/dependency\n\turl = https://example.invalid/dependency.git\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitRun(t, fixture.runner, fixture.workPath, "add", "--", ".gitattributes", ".gitmodules", "large.bin")
	gitRun(t, fixture.runner, fixture.workPath, "update-index", "--add", "--cacheinfo", "160000,"+fixture.featureRevision+",vendor/dependency")
	gitRun(t, fixture.runner, fixture.workPath, "commit", "-m", "adversarial content")
	// The LFS pointer deliberately has no local object. Ignore host-provided
	// push hooks while seeding the fixture; hooks are not behavior under test.
	gitRun(t, fixture.runner, fixture.workPath, "-c", "core.hooksPath="+t.TempDir(), "push", "--", fixture.remote, "adversarial:adversarial")

	marker := filepath.Join(t.TempDir(), "remote-hook-ran")
	hook := filepath.Join(fixture.remotePath, "hooks", "post-checkout")
	if err := os.WriteFile(hook, []byte("#!/bin/sh\ntouch '"+marker+"'\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	cache, err := OpenCache(resolveGitSourceWorkspace(t, writeGitSourceRoot(t)), fixture.runner)
	if err != nil {
		t.Fatal(err)
	}
	resolved, err := cache.Resolve(ctx, Intent{MountPath: "/adversarial", Source: fixture.remote, Ref: "adversarial"})
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(resolved.SourcePath, "large.bin"))
	if err != nil || string(data) != pointer {
		t.Fatalf("LFS pointer changed or downloaded: %q, %v", data, err)
	}
	submodulePath := filepath.Join(resolved.SourcePath, "vendor", "dependency")
	if entries, err := os.ReadDir(submodulePath); err != nil || len(entries) != 0 {
		t.Fatalf("submodule gitlink was initialized: entries=%v err=%v", entries, err)
	}
	if _, err := os.Stat(filepath.Join(submodulePath, ".git")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("submodule contains Git metadata: %v", err)
	}
	if _, err := os.Stat(marker); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("remote hook executed: %v", err)
	}
}

type resolutionFixture struct {
	runner          Runner
	workPath        string
	remotePath      string
	remote          string
	mainRevision    string
	featureRevision string
}

func newResolutionFixture(t *testing.T) *resolutionFixture {
	t.Helper()
	ctx := context.Background()
	runner := NewRunner()
	workPath := filepath.Join(t.TempDir(), "work")
	if _, err := runner.Run(ctx, "", "init", "--", workPath); err != nil {
		t.Fatal(err)
	}
	for _, setting := range [][]string{
		{"user.name", "Factile Test"},
		{"user.email", "factile@example.test"},
	} {
		if _, err := runner.Run(ctx, "", "-C", workPath, "config", "--local", "--", setting[0], setting[1]); err != nil {
			t.Fatal(err)
		}
	}
	writeResolutionContent(t, workPath, "main v1")
	gitRun(t, runner, workPath, "add", "--", ".")
	gitRun(t, runner, workPath, "commit", "-m", "main v1")
	gitRun(t, runner, workPath, "branch", "-M", "main")
	mainRevision := gitOutput(t, runner, workPath, "rev-parse", "HEAD")
	gitRun(t, runner, workPath, "tag", "v1")

	gitRun(t, runner, workPath, "checkout", "-b", "feature")
	writeResolutionContent(t, workPath, "feature v1")
	gitRun(t, runner, workPath, "add", "--", ".")
	gitRun(t, runner, workPath, "commit", "-m", "feature v1")
	featureRevision := gitOutput(t, runner, workPath, "rev-parse", "HEAD")
	gitRun(t, runner, workPath, "checkout", "main")

	remotePath := filepath.Join(t.TempDir(), "remote.git")
	gitRun(t, runner, "", "clone", "--bare", "--", workPath, remotePath)
	remote := fileRemote(t, remotePath)
	gitRun(t, runner, workPath, "push", "--", remote, "refs/heads/feature:refs/heads/feature")
	return &resolutionFixture{
		runner:          runner,
		workPath:        workPath,
		remotePath:      remotePath,
		remote:          remote,
		mainRevision:    mainRevision,
		featureRevision: featureRevision,
	}
}

func (f *resolutionFixture) commitOnBranch(t *testing.T, branch, content string) string {
	t.Helper()
	gitRun(t, f.runner, f.workPath, "checkout", branch)
	writeResolutionContent(t, f.workPath, content)
	gitRun(t, f.runner, f.workPath, "add", "--", ".")
	gitRun(t, f.runner, f.workPath, "commit", "-m", content)
	revision := gitOutput(t, f.runner, f.workPath, "rev-parse", "HEAD")
	gitRun(t, f.runner, f.workPath, "push", "--force", "--", f.remote, branch+":"+branch)
	return revision
}

func (f *resolutionFixture) createSymlinkBranch(t *testing.T) error {
	t.Helper()
	gitRun(t, f.runner, f.workPath, "checkout", "-b", "symlink", "main")
	if err := os.Symlink("overview.md", filepath.Join(f.workPath, "linked.md")); err != nil {
		return err
	}
	gitRun(t, f.runner, f.workPath, "add", "--", "linked.md")
	gitRun(t, f.runner, f.workPath, "commit", "-m", "symlink")
	gitRun(t, f.runner, f.workPath, "push", "--", f.remote, "symlink:symlink")
	gitRun(t, f.runner, f.workPath, "checkout", "main")
	return nil
}

func writeResolutionContent(t *testing.T, root, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(root, "factile.toml"), []byte("version = 2\n\n[bundle]\nname = \"git-fixture\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "overview.md"), []byte("# "+content+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
}

func assertResolutionContent(t *testing.T, resolution Resolution, revision, content string) {
	t.Helper()
	if resolution.Revision != revision || resolution.SourcePath == "" {
		t.Fatalf("unexpected resolution: %#v want revision %s", resolution, revision)
	}
	assertFileContains(t, filepath.Join(resolution.SourcePath, "overview.md"), content)
}

func assertFileContains(t *testing.T, path, content string) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), content) {
		t.Fatalf("%s missing %q: %s", path, content, data)
	}
}

func gitRun(t *testing.T, runner Runner, dir string, args ...string) {
	t.Helper()
	if _, err := runner.Run(context.Background(), dir, args...); err != nil {
		t.Fatalf("git %s: %v", strings.Join(args, " "), err)
	}
}

func gitOutput(t *testing.T, runner Runner, dir string, args ...string) string {
	t.Helper()
	output, err := runner.Run(context.Background(), dir, args...)
	if err != nil {
		t.Fatalf("git %s: %v", strings.Join(args, " "), err)
	}
	return strings.TrimSpace(string(output))
}
