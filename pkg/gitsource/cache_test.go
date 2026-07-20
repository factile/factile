package gitsource

import (
	"context"
	"encoding/json"
	"errors"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/factile/factile/pkg/vfs"
)

func TestCacheInitializesAndInspectsManagedBareRepository(t *testing.T) {
	ctx := context.Background()
	runner := NewRunner()
	root := writeGitSourceRoot(t)
	if _, err := runner.Run(ctx, "", "init", "--", root); err != nil {
		t.Fatal(err)
	}
	remotePath := filepath.Join(t.TempDir(), "remote.git")
	if _, err := runner.Run(ctx, "", "init", "--bare", "--", remotePath); err != nil {
		t.Fatal(err)
	}
	remote := fileRemote(t, remotePath)

	cache, err := OpenCache(resolveGitSourceWorkspace(t, root), runner)
	if err != nil {
		t.Fatal(err)
	}
	entry, err := cache.Entry("/coding", remote)
	if err != nil {
		t.Fatal(err)
	}
	again, err := cache.Entry("/coding", remote)
	if err != nil || entry != again {
		t.Fatalf("cache key was not deterministic: first=%#v second=%#v err=%v", entry, again, err)
	}
	other, err := cache.Entry("/other", remote)
	if err != nil || other.Key == entry.Key {
		t.Fatalf("mount identity did not affect cache key: entry=%#v other=%#v err=%v", entry, other, err)
	}
	if err := cache.InitializeRepository(ctx, entry); err != nil {
		t.Fatal(err)
	}
	if err := cache.InitializeRepository(ctx, entry); err != nil {
		t.Fatalf("repository initialization was not idempotent: %v", err)
	}
	info, err := cache.InspectRepository(ctx, entry)
	if err != nil {
		t.Fatal(err)
	}
	if !info.Initialized || info.Remote != remote {
		t.Fatalf("unexpected repository info: %#v", info)
	}
	state, err := cache.ReadState(entry)
	if err != nil {
		t.Fatal(err)
	}
	if !state.Initialized || state.Source != remote || state.MountPath != "/coding" || state.Key != entry.Key {
		t.Fatalf("unexpected cache state: %#v", state)
	}
	for _, path := range []string{filepath.Join(root, ".factile", "cache"), filepath.Dir(entry.Dir), entry.Dir} {
		info, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode().Perm() != 0o700 {
			t.Fatalf("cache directory %s mode = %o", path, info.Mode().Perm())
		}
	}
	if info, err := os.Stat(entry.StatePath); err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("cache state mode = %v, %v", info, err)
	}
	status, err := runner.Run(ctx, "", "-C", root, "status", "--porcelain", "--untracked-files=all", "--", ".factile/cache")
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(string(status)) != "" {
		t.Fatalf("generated cache is visible to Git status: %s", status)
	}
}

func TestCacheAnchorsToWorkspaceStateAcrossWorkingDirectories(t *testing.T) {
	workspace := filepath.Join(t.TempDir(), "workspace")
	rootBundle := filepath.Join(workspace, "docs")
	secondary := filepath.Join(workspace, "bundles", "reference")
	writeGitSourceTestFile(t, filepath.Join(workspace, "factile.toml"), "version = 2\n\n[workspace]\nroot = \"docs\"\n")
	writeGitSourceTestFile(t, filepath.Join(rootBundle, "factile.toml"), "version = 2\n\n[bundle]\nname = \"docs\"\n")
	writeGitSourceTestFile(t, filepath.Join(secondary, "factile.toml"), "version = 2\n\n[bundle]\nname = \"reference\"\n")
	deepRoot := filepath.Join(rootBundle, "guides", "deep")
	if err := os.MkdirAll(deepRoot, 0o755); err != nil {
		t.Fatal(err)
	}

	first, err := OpenCache(resolveGitSourceWorkspace(t, deepRoot), NewRunner())
	if err != nil {
		t.Fatal(err)
	}
	second, err := OpenCache(resolveGitSourceWorkspace(t, secondary), NewRunner())
	if err != nil {
		t.Fatal(err)
	}
	firstEntry, err := first.Entry("/reference", "https://example.test/reference.git")
	if err != nil {
		t.Fatal(err)
	}
	secondEntry, err := second.Entry("/reference", "https://example.test/reference.git")
	if err != nil {
		t.Fatal(err)
	}
	if firstEntry != secondEntry {
		t.Fatalf("CWD changed cache identity: first=%#v second=%#v", firstEntry, secondEntry)
	}
	wantState := filepath.Join(workspace, ".factile")
	if first.WorkspaceDir != workspace || first.StateDir != wantState || !strings.HasPrefix(firstEntry.Dir, filepath.Join(wantState, "cache", "git")+string(filepath.Separator)) {
		t.Fatalf("cache is not workspace-anchored: cache=%#v entry=%#v", first, firstEntry)
	}
	if _, err := os.Stat(filepath.Join(rootBundle, ".factile")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("cache appeared inside root bundle: %v", err)
	}
	if _, err := os.Stat(filepath.Join(secondary, ".factile")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("cache appeared inside secondary bundle: %v", err)
	}
}

func TestCacheDoesNotPersistCredentialEnvironment(t *testing.T) {
	fixture := newResolutionFixture(t)
	workspace := resolveGitSourceWorkspace(t, writeGitSourceRoot(t))
	canaries := []string{
		"hosted-token-canary-7d9f",
		"git-password-canary-42ac",
		"authorization-canary-c831",
	}
	t.Setenv("FACTILE_HOSTED_TOKEN", canaries[0])
	t.Setenv("GIT_PASSWORD", canaries[1])
	t.Setenv("HTTP_AUTHORIZATION", "Bearer "+canaries[2])

	cache, err := OpenCache(workspace, fixture.runner)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := cache.Resolve(context.Background(), Intent{MountPath: "/coding", Source: fixture.remote}); err != nil {
		t.Fatal(err)
	}
	if err := filepath.WalkDir(workspace.StateDir, func(filename string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		for _, canary := range canaries {
			if strings.Contains(filename, canary) {
				t.Fatalf("credential canary entered a cache path: %s", filename)
			}
		}
		if entry.IsDir() {
			return nil
		}
		data, err := os.ReadFile(filename)
		if err != nil {
			return err
		}
		for _, canary := range canaries {
			if strings.Contains(string(data), canary) {
				t.Fatalf("credential canary %q entered cache file %s", canary, filename)
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

func TestCacheIgnoresAmbientRepositoryEnvironment(t *testing.T) {
	ctx := context.Background()
	fixture := newResolutionFixture(t)
	root := writeGitSourceRoot(t)
	controlRepository := filepath.Join(t.TempDir(), "control.git")
	if _, err := fixture.runner.Run(ctx, "", "init", "--bare", "--", controlRepository); err != nil {
		t.Fatal(err)
	}
	controlConfigPath := filepath.Join(controlRepository, "config")
	controlConfig, err := os.ReadFile(controlConfigPath)
	if err != nil {
		t.Fatal(err)
	}

	hostileRoot := t.TempDir()
	hostileObjects := filepath.Join(hostileRoot, "objects")
	hostileAlternate := filepath.Join(hostileRoot, "alternate")
	hostileWorkTree := filepath.Join(hostileRoot, "worktree")
	hostileTemplate := filepath.Join(hostileRoot, "template")
	for _, path := range []string{hostileObjects, hostileAlternate, hostileWorkTree, hostileTemplate} {
		if err := os.Mkdir(path, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	hostileIndex := filepath.Join(hostileRoot, "index")
	if err := os.WriteFile(hostileIndex, []byte("outside index\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	hostileMarker := filepath.Join(hostileWorkTree, "outside.txt")
	if err := os.WriteFile(hostileMarker, []byte("outside worktree\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	hostileValues := map[string]string{
		"GIT_ALTERNATE_OBJECT_DIRECTORIES": hostileAlternate,
		"GIT_CEILING_DIRECTORIES":          hostileRoot,
		"GIT_COMMON_DIR":                   controlRepository,
		"GIT_DEFAULT_HASH":                 "sha256",
		"GIT_DEFAULT_REF_FORMAT":           "reftable",
		"GIT_DIR":                          controlRepository,
		"GIT_DISCOVERY_ACROSS_FILESYSTEM":  "1",
		"GIT_GRAFT_FILE":                   filepath.Join(hostileRoot, "grafts"),
		"GIT_IMPLICIT_WORK_TREE":           "0",
		"GIT_INDEX_FILE":                   hostileIndex,
		"GIT_INTERNAL_SUPER_PREFIX":        "outside/",
		"GIT_NAMESPACE":                    "outside",
		"GIT_NO_REPLACE_OBJECTS":           "1",
		"GIT_OBJECT_DIRECTORY":             hostileObjects,
		"GIT_PREFIX":                       "outside/",
		"GIT_QUARANTINE_PATH":              hostileObjects,
		"GIT_REPLACE_REF_BASE":             "refs/replace/outside",
		"GIT_SHALLOW_FILE":                 filepath.Join(hostileRoot, "shallow"),
		"GIT_TEMPLATE_DIR":                 hostileTemplate,
		"GIT_WORK_TREE":                    hostileWorkTree,
	}
	for key, value := range hostileValues {
		t.Setenv(key, value)
	}

	cache, err := OpenCache(resolveGitSourceWorkspace(t, root), fixture.runner)
	if err != nil {
		t.Fatal(err)
	}
	intent := Intent{MountPath: "/coding", Source: fixture.remote}
	resolved, err := cache.Resolve(ctx, intent)
	if err != nil {
		t.Fatal(err)
	}
	if resolved.Revision != fixture.mainRevision {
		t.Fatalf("resolved revision = %q, want %q", resolved.Revision, fixture.mainRevision)
	}
	entry, err := cache.Entry(intent.MountPath, intent.Source)
	if err != nil {
		t.Fatal(err)
	}
	info, err := cache.InspectRepository(ctx, entry)
	if err != nil {
		t.Fatal(err)
	}
	if !info.Initialized || info.Remote != fixture.remote {
		t.Fatalf("unexpected managed repository: %#v", info)
	}
	format, err := fixture.runner.Run(ctx, "", "-C", entry.RepositoryPath, "rev-parse", "--show-object-format")
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(string(format)) != "sha1" {
		t.Fatalf("managed object format = %q", format)
	}

	afterControlConfig, err := os.ReadFile(controlConfigPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(afterControlConfig) != string(controlConfig) {
		t.Fatalf("ambient repository config changed:\n%s", afterControlConfig)
	}
	indexData, err := os.ReadFile(hostileIndex)
	if err != nil || string(indexData) != "outside index\n" {
		t.Fatalf("ambient index changed: %q, %v", indexData, err)
	}
	markerData, err := os.ReadFile(hostileMarker)
	if err != nil || string(markerData) != "outside worktree\n" {
		t.Fatalf("ambient worktree changed: %q, %v", markerData, err)
	}
	objects, err := os.ReadDir(hostileObjects)
	if err != nil || len(objects) != 0 {
		t.Fatalf("ambient object directory changed: %v, %v", objects, err)
	}
}

func TestCacheStateReplacementAndValidation(t *testing.T) {
	cache, entry := testCacheEntry(t)
	state := State{Version: cacheStateVersion, Key: entry.Key, MountPath: entry.MountPath, Source: entry.Source}
	if err := cache.WriteState(entry, state); err != nil {
		t.Fatal(err)
	}
	state.Initialized = true
	if err := cache.WriteState(entry, state); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(entry.Dir, ".state.json-interrupted"), []byte("{"), 0o600); err != nil {
		t.Fatal(err)
	}
	loaded, err := cache.ReadState(entry)
	if err != nil || !loaded.Initialized {
		t.Fatalf("atomic state replacement failed: %#v %v", loaded, err)
	}

	mismatch := state
	mismatch.Source = "file:///different.git"
	if err := cache.WriteState(entry, mismatch); !errors.Is(err, ErrInvalidCache) {
		t.Fatalf("state identity mismatch error = %v", err)
	}
	mismatchData, err := json.Marshal(mismatch)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(entry.StatePath, mismatchData, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := cache.ReadState(entry); !errors.Is(err, ErrInvalidCache) {
		t.Fatalf("stored state identity mismatch error = %v", err)
	}
	if err := os.WriteFile(entry.StatePath, []byte("not-json"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := cache.ReadState(entry); !errors.Is(err, ErrInvalidCache) {
		t.Fatalf("malformed state error = %v", err)
	}
}

func TestStatusCacheOpenDoesNotCreateCacheState(t *testing.T) {
	root := writeGitSourceRoot(t)
	cache, err := OpenCacheForStatus(resolveGitSourceWorkspace(t, root), NewRunner())
	if err != nil {
		t.Fatal(err)
	}
	status, err := cache.Status(Intent{MountPath: "/coding", Source: "https://example.test/coding.git"})
	if err != nil {
		t.Fatal(err)
	}
	if status.SnapshotAvailable || !status.RefreshDue || status.SelectorMode != SelectorHead {
		t.Fatalf("unexpected empty-cache status: %#v", status)
	}
	if _, err := os.Stat(filepath.Join(root, ".factile", "cache")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("status-only open created cache state: %v", err)
	}
}

func TestCacheRejectsUnsafeRootsPathsAndSources(t *testing.T) {
	runner := NewRunner()
	missingRoot := t.TempDir()
	if _, err := OpenCache(vfs.WorkspaceContext{
		WorkspaceDir: missingRoot,
		StateDir:     filepath.Join(t.TempDir(), ".factile"),
	}, runner); err == nil {
		t.Fatal("cache opened an inconsistent workspace context")
	}

	root := writeGitSourceRoot(t)
	outside := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(root, ".factile", "cache")); err == nil {
		if _, err := OpenCache(resolveGitSourceWorkspace(t, root), runner); err == nil {
			t.Fatalf("symlinked cache error = %v", err)
		}
	} else {
		t.Logf("symlinked cache check unavailable: %v", err)
	}

	root = writeGitSourceRoot(t)
	cache, err := OpenCache(resolveGitSourceWorkspace(t, root), runner)
	if err != nil {
		t.Fatal(err)
	}
	for _, source := range []string{
		"https://alice:correct-horse@example.test/repo.git",
		"ssh://alice:correct-horse@example.test/repo.git",
		"https://example.test/repo.git?token=hunter2",
		"https://example.test/repo.git#private",
	} {
		_, err := cache.Entry("/coding", source)
		if !errors.Is(err, ErrInvalidCache) {
			t.Fatalf("unsafe source error = %v", err)
		}
		for _, secret := range []string{"alice", "correct-horse", "hunter2", source} {
			if strings.Contains(err.Error(), secret) {
				t.Fatalf("unsafe source error exposed %q: %v", secret, err)
			}
		}
	}
	if err := ValidateSource("ssh://alice@example.test/repo.git"); err != nil {
		t.Fatalf("SSH connection identity was rejected: %v", err)
	}

	entry, err := cache.Entry("/coding", "https://example.test/repo.git")
	if err != nil {
		t.Fatal(err)
	}
	forged := entry
	forged.Dir = outside
	if err := cache.WithUpdateLock(forged, func() error { return nil }); !errors.Is(err, ErrInvalidCache) {
		t.Fatalf("forged cache path error = %v", err)
	}
}

func TestValidateSourceRejectsURIQueryAndFragmentDelimiters(t *testing.T) {
	for _, source := range []string{
		"https://example.test/repository.git?",
		"https://example.test/repository.git?token=value",
		"https://example.test/repository.git#",
		"https://example.test/repository.git#private",
		"git+https://example.test/repository.git?",
		"git+https://example.test/repository.git?token=value",
		"git+https://example.test/repository.git#",
		"git+https://example.test/repository.git#private",
		"http://example.test/repository.git?",
		"ssh://git@example.test/repository.git#",
		"git://example.test/repository.git?token=value",
		"file:///srv/remotes/repository.git#private",
	} {
		t.Run(source, func(t *testing.T) {
			err := ValidateSource(source)
			if !errors.Is(err, ErrInvalidCache) {
				t.Fatalf("URI delimiter source %q error = %v, want ErrInvalidCache", source, err)
			}
			if strings.Contains(err.Error(), source) {
				t.Fatalf("URI delimiter error exposed source %q: %v", source, err)
			}
		})
	}

	for _, source := range []string{
		"https://example.test/team/repository%3Fcopy%23one.git",
		"git+https://example.test/team/repository%3Fcopy%23one.git",
		"git@example.test:team/repository.git",
		"git@example.test:team/repository?copy#one.git",
	} {
		if err := ValidateSource(source); err != nil {
			t.Errorf("valid encoded-path or SCP source %q was rejected: %v", source, err)
		}
	}
}

func TestCacheRejectsSymlinkedRepositoryAndState(t *testing.T) {
	cache, entry := testCacheEntry(t)
	outside := t.TempDir()
	if err := os.Symlink(outside, entry.RepositoryPath); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	if _, err := cache.InspectRepository(context.Background(), entry); !errors.Is(err, ErrInvalidCache) {
		t.Fatalf("symlinked repository error = %v", err)
	}
	if err := os.Remove(entry.RepositoryPath); err != nil {
		t.Fatal(err)
	}
	outsideState := filepath.Join(outside, "state.json")
	if err := os.WriteFile(outsideState, []byte("outside"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outsideState, entry.StatePath); err != nil {
		t.Skipf("state symlinks unavailable: %v", err)
	}
	state := State{Version: cacheStateVersion, Key: entry.Key, MountPath: entry.MountPath, Source: entry.Source}
	stateData, err := json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(outsideState, stateData, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := cache.ReadState(entry); !errors.Is(err, ErrInvalidCache) {
		t.Fatalf("symlinked state read error = %v", err)
	}
	if err := cache.WriteState(entry, state); !errors.Is(err, ErrInvalidCache) {
		t.Fatalf("symlinked state error = %v", err)
	}
}

func TestCacheRejectsSymlinkedEntryAndSnapshotDirectories(t *testing.T) {
	cache, entry := testCacheEntry(t)
	outside := t.TempDir()
	if err := os.Remove(entry.Dir); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, entry.Dir); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	intent := Intent{MountPath: entry.MountPath, Source: entry.Source}
	if status, err := cache.Status(intent); err != nil || status.LastErrorCode != "validation_failed" || status.SnapshotAvailable {
		t.Fatalf("symlinked entry status = %#v, %v", status, err)
	}
	if _, err := cache.Entry(entry.MountPath, entry.Source); !errors.Is(err, ErrInvalidCache) {
		t.Fatalf("symlinked entry open error = %v", err)
	}

	if err := os.Remove(entry.Dir); err != nil {
		t.Fatal(err)
	}
	entry, err := cache.Entry(entry.MountPath, entry.Source)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, entry.SnapshotsPath); err != nil {
		t.Skipf("snapshot symlinks unavailable: %v", err)
	}
	if _, _, err := cache.materializeSnapshot(context.Background(), entry, strings.Repeat("1", 40)); !errors.Is(err, ErrInvalidCache) {
		t.Fatalf("symlinked snapshots error = %v", err)
	}
}

func TestCacheRejectsManagedAncestorSymlinkSubstitution(t *testing.T) {
	for _, ancestor := range []string{".factile", "cache", "git"} {
		t.Run(ancestor, func(t *testing.T) {
			cache, entry := testCacheEntry(t)
			state := initialState(entry)
			if err := cache.WriteState(entry, state); err != nil {
				t.Fatal(err)
			}

			replaced := cache.StateDir
			switch ancestor {
			case "cache":
				replaced = filepath.Join(replaced, "cache")
			case "git":
				replaced = filepath.Dir(entry.Dir)
			}
			moved := filepath.Join(t.TempDir(), "moved")
			if err := os.Rename(replaced, moved); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(moved, replaced); err != nil {
				t.Skipf("symlinks unavailable: %v", err)
			}

			if _, err := cache.Entry(entry.MountPath, entry.Source); !errors.Is(err, ErrInvalidCache) {
				t.Fatalf("entry through substituted %s ancestor error = %v", ancestor, err)
			}
			if _, err := cache.ReadState(entry); !errors.Is(err, ErrInvalidCache) {
				t.Fatalf("state read through substituted %s ancestor error = %v", ancestor, err)
			}
			if _, err := cache.InspectRepository(context.Background(), entry); !errors.Is(err, ErrInvalidCache) {
				t.Fatalf("repository inspection through substituted %s ancestor error = %v", ancestor, err)
			}
			if err := cache.InitializeRepository(context.Background(), entry); !errors.Is(err, ErrInvalidCache) {
				t.Fatalf("repository initialization through substituted %s ancestor error = %v", ancestor, err)
			}
			state.Initialized = true
			if err := cache.WriteState(entry, state); !errors.Is(err, ErrInvalidCache) {
				t.Fatalf("state write through substituted %s ancestor error = %v", ancestor, err)
			}
			called := false
			if err := cache.WithUpdateLock(entry, func() error {
				called = true
				return nil
			}); !errors.Is(err, ErrInvalidCache) {
				t.Fatalf("lock through substituted %s ancestor error = %v", ancestor, err)
			}
			if called {
				t.Fatalf("lock callback ran through substituted %s ancestor", ancestor)
			}

			intent := Intent{MountPath: entry.MountPath, Source: entry.Source}
			if _, err := cache.Resolve(context.Background(), intent); !errors.Is(err, ErrInvalidCache) {
				t.Fatalf("resolve through substituted %s ancestor error = %v", ancestor, err)
			}
			if status, err := cache.Status(intent); err != nil || status.LastErrorCode != "validation_failed" || status.SnapshotAvailable {
				t.Fatalf("status through substituted %s ancestor = %#v, %v", ancestor, status, err)
			}
		})
	}
}

func TestCacheUpdateLockSerializesWriters(t *testing.T) {
	cache, entry := testCacheEntry(t)
	entered := make(chan struct{})
	release := make(chan struct{})
	firstDone := make(chan error, 1)
	go func() {
		firstDone <- cache.WithUpdateLock(entry, func() error {
			close(entered)
			<-release
			return nil
		})
	}()
	<-entered

	var concurrent atomic.Int32
	secondDone := make(chan error, 1)
	go func() {
		secondDone <- cache.WithUpdateLock(entry, func() error {
			concurrent.Add(1)
			return nil
		})
	}()
	select {
	case err := <-secondDone:
		t.Fatalf("contending update entered early: %v", err)
	case <-time.After(50 * time.Millisecond):
	}
	close(release)
	if err := <-firstDone; err != nil {
		t.Fatal(err)
	}
	if err := <-secondDone; err != nil {
		t.Fatal(err)
	}
	if concurrent.Load() != 1 {
		t.Fatalf("contending update ran %d times", concurrent.Load())
	}
}

func testCacheEntry(t *testing.T) (*Cache, Entry) {
	t.Helper()
	cache, err := OpenCache(resolveGitSourceWorkspace(t, writeGitSourceRoot(t)), NewRunner())
	if err != nil {
		t.Fatal(err)
	}
	entry, err := cache.Entry("/coding", "https://example.test/repo.git")
	if err != nil {
		t.Fatal(err)
	}
	return cache, entry
}

func writeGitSourceRoot(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "factile.toml"), []byte(`version = 2

[workspace]
root = "."

[bundle]
name = "git-source-test"
`), 0o644); err != nil {
		t.Fatal(err)
	}
	return root
}

func writeGitSourceTestFile(t *testing.T, filename string, data string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(filename), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filename, []byte(data), 0o644); err != nil {
		t.Fatal(err)
	}
}

func resolveGitSourceWorkspace(t *testing.T, workDir string) vfs.WorkspaceContext {
	t.Helper()
	workspace, err := vfs.ResolveWorkspace(vfs.ResolveWorkspaceOptions{WorkDir: workDir})
	if err != nil {
		t.Fatal(err)
	}
	return workspace
}

func fileRemote(t *testing.T, path string) string {
	t.Helper()
	absolute, err := filepath.Abs(path)
	if err != nil {
		t.Fatal(err)
	}
	return (&url.URL{Scheme: "file", Path: filepath.ToSlash(absolute)}).String()
}
