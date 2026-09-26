package gitsource

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

const selectedBundleManifest = "version = 2\n[bundle]\nname = \"selected\"\ntitle = \"Selected Docs\"\ndescription = \"Nested selected bundle.\"\n"

func TestSnapshotBundleSelection(t *testing.T) {
	for _, tc := range []struct {
		name, manifest, root, bundle, reason string
		extra                                map[string]string
	}{
		{name: "bundle", manifest: selectedBundleManifest, root: "."},
		{name: "combined", manifest: selectedBundleManifest + "[workspace]\nroot = \".\"\n", root: "."},
		{name: "nested", manifest: "version = 2\n[workspace]\nroot = \"docs\"\n", root: "docs", bundle: selectedBundleManifest},
		{name: "deep", manifest: "version = 2\n[workspace]\nroot = \"knowledge/docs\"\n", root: "knowledge/docs", bundle: selectedBundleManifest},
		{name: "missing manifest", reason: "missing_manifest"},
		{name: "malformed manifest", manifest: "[broken", reason: "invalid_manifest"},
		{name: "unsupported version", manifest: "version = 1\n[bundle]\nname = \"old\"", reason: "invalid_manifest"},
		{name: "unknown manifest", manifest: selectedBundleManifest + "unknown = true\n", reason: "invalid_manifest"},
		{name: "unknown sections", manifest: "version = 2\n[unknown]\nname = \"x\"", reason: "invalid_manifest"},
		{name: "invalid combined", manifest: selectedBundleManifest + "[workspace]\nroot = \"docs\"\n", reason: "invalid_manifest"},
		{name: "absolute", manifest: "version = 2\n[workspace]\nroot = \"/tmp\"\n", reason: "invalid_root"},
		{name: "traversal", manifest: "version = 2\n[workspace]\nroot = \"../docs\"\n", reason: "invalid_root"},
		{name: "unnormalized", manifest: "version = 2\n[workspace]\nroot = \"docs/../docs\"\n", reason: "invalid_root"},
		{name: "private state", manifest: "version = 2\n[workspace]\nroot = \".factile/docs\"\n", reason: "invalid_root"},
		{name: "private git", manifest: "version = 2\n[workspace]\nroot = \".git/docs\"\n", reason: "invalid_root"},
		{name: "missing root", manifest: "version = 2\n[workspace]\nroot = \"docs\"\n", reason: "missing_bundle"},
		{name: "missing bundle", manifest: "version = 2\n[workspace]\nroot = \"docs\"\n", extra: map[string]string{"docs/overview.md": "# Docs"}, reason: "missing_bundle"},
		{name: "malformed bundle", manifest: "version = 2\n[workspace]\nroot = \"docs\"\n", root: "docs", bundle: "[broken", reason: "invalid_bundle"},
		{name: "non bundle", manifest: "version = 2\n[workspace]\nroot = \"docs\"\n", root: "docs", bundle: "version = 2\n[workspace]\nroot = \".\"\n", reason: "invalid_bundle"},
		{name: "cross workspace", manifest: "version = 2\n[workspace]\nroot = \"nested/docs\"\n", root: "nested/docs", bundle: selectedBundleManifest, extra: map[string]string{"nested/factile.toml": "version = 2\n[workspace]\nroot = \"docs\"\n"}, reason: "invalid_root"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			// An outer consumer must never supply a missing source manifest.
			parent := writeGitSourceRoot(t)
			snapshot := filepath.Join(parent, "snapshot")
			if err := os.Mkdir(snapshot, 0o700); err != nil {
				t.Fatal(err)
			}
			files := map[string]string{}
			if tc.manifest != "" {
				files["factile.toml"] = tc.manifest
			}
			if tc.bundle != "" {
				files[filepath.Join(tc.root, "factile.toml")] = tc.bundle
			}
			for name, contents := range tc.extra {
				files[name] = contents
			}
			writeSnapshotFiles(t, snapshot, files)
			bundle, err := snapshotBundle(parent, snapshot)
			if tc.reason != "" {
				assertSelectionReason(t, err, tc.reason)
			} else if err != nil || bundle != filepath.Join(snapshot, tc.root) {
				t.Fatalf("bundle = %q, err = %v", bundle, err)
			}
		})
	}
}

func TestResolveNestedBundleLifecycle(t *testing.T) {
	ctx := context.Background()
	fixture := newResolutionFixture(t)
	writeSnapshotFiles(t, fixture.workPath, map[string]string{
		"factile.toml":      "version = 2\n[workspace]\nroot = \"docs\"\n",
		"docs/factile.toml": selectedBundleManifest,
		"docs/overview.md":  "# Selected bundle\n",
	})
	revision := commitSnapshotFiles(t, fixture)
	var fetches atomic.Int32
	runner := fixture.runner
	runner.command = countingGitFactory(&fetches, nil, nil)
	workspace := resolveGitSourceWorkspace(t, writeGitSourceRoot(t))
	cache, err := OpenCache(workspace, runner)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 9, 26, 0, 0, 0, 0, time.UTC)
	cache.now = func() time.Time { return now }
	intent := Intent{MountPath: "/coding", Source: fixture.remote}
	first, err := cache.Resolve(ctx, intent)
	if err != nil {
		t.Fatal(err)
	}
	assertResolutionContent(t, first, revision, "Selected bundle")
	entry, err := cache.Entry(intent.MountPath, intent.Source)
	if err != nil {
		t.Fatal(err)
	}
	if first.SourcePath != filepath.Join(entry.SnapshotsPath, revision, "docs") {
		t.Fatalf("selected root = %s", first.SourcePath)
	}
	pin := Intent{MountPath: "/pin", Source: fixture.remote, Revision: revision}
	pinned, err := cache.Resolve(ctx, pin)
	if err != nil {
		t.Fatal(err)
	}
	before := fetches.Load()
	for _, current := range []Intent{intent, pin} {
		cached, err := cache.Resolve(ctx, current)
		if err != nil {
			t.Fatal(err)
		}
		assertResolutionContent(t, cached, revision, "Selected bundle")
		if _, err := cache.Status(current); err != nil {
			t.Fatal(err)
		}
	}
	if fetches.Load() != before {
		t.Fatal("cached reads or status fetched")
	}

	// Reconstruct the full snapshot from the bare repository without fetching.
	if err := makeTreeWritable(filepath.Dir(first.SourcePath)); err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(filepath.Dir(first.SourcePath)); err != nil {
		t.Fatal(err)
	}
	rebuilt, err := cache.Resolve(ctx, intent)
	if err != nil {
		t.Fatal(err)
	}
	assertResolutionContent(t, rebuilt, revision, "Selected bundle")
	if fetches.Load() != before {
		t.Fatal("snapshot reconstruction fetched")
	}

	// A bad floating update does not activate a candidate or move an exact pin.
	writeSnapshotFiles(t, fixture.workPath, map[string]string{"factile.toml": "version = 2\n[workspace]\nroot = \"missing\"\n"})
	badRevision := commitSnapshotFiles(t, fixture)
	now = now.Add(refreshInterval)
	stale, err := cache.Resolve(ctx, intent)
	if err != nil {
		t.Fatal(err)
	}
	if stale.SourcePath != first.SourcePath || stale.Updated || !stale.Status.Stale || stale.Status.LastErrorReason != "missing_bundle" || stale.Status.LastErrorCode != "validation_failed" {
		t.Fatalf("invalid automatic refresh = %#v", stale)
	}
	explicit, err := cache.Refresh(ctx, intent)
	if err != nil || explicit.Outcome != "stale" || explicit.Status.LastErrorReason != "missing_bundle" {
		t.Fatalf("invalid explicit refresh = %#v, %v", explicit, err)
	}
	if _, err := os.Stat(filepath.Join(entry.SnapshotsPath, badRevision)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("bad candidate activated: %v", err)
	}
	refreshedPin, err := cache.Refresh(ctx, pin)
	if err != nil || refreshedPin.Outcome != "pinned" || refreshedPin.Status.SelectedRevision != pinned.Revision {
		t.Fatalf("pin moved: %#v, %v", refreshedPin, err)
	}
	// A new cache reconstructs the pinned commit even after the remote advances.
	cold, err := OpenCache(resolveGitSourceWorkspace(t, writeGitSourceRoot(t)), runner)
	if err != nil {
		t.Fatal(err)
	}
	coldPin, err := cold.Resolve(ctx, pin)
	if err != nil {
		t.Fatal(err)
	}
	assertResolutionContent(t, coldPin, revision, "Selected bundle")

	// Selection can move to another contained root on a healthy floating update.
	writeSnapshotFiles(t, fixture.workPath, map[string]string{
		"factile.toml":           "version = 2\n[workspace]\nroot = \"knowledge\"\n",
		"knowledge/factile.toml": selectedBundleManifest,
		"knowledge/overview.md":  "# New bundle\n",
	})
	next := commitSnapshotFiles(t, fixture)
	refreshed, err := cache.Refresh(ctx, intent)
	if err != nil || refreshed.Outcome != "updated" || refreshed.Status.Stale || refreshed.Status.LastErrorReason != "" {
		t.Fatalf("healthy refresh = %#v, %v", refreshed, err)
	}
	updated, err := cache.Resolve(ctx, intent)
	if err != nil {
		t.Fatal(err)
	}
	assertResolutionContent(t, updated, next, "New bundle")
}

func TestInvalidSnapshotSelectionFreshCachedAndReconstructed(t *testing.T) {
	for _, mode := range []string{"fresh", "cached", "reconstructed"} {
		t.Run(mode, func(t *testing.T) {
			fixture := newResolutionFixture(t)
			writeSnapshotFiles(t, fixture.workPath, map[string]string{"factile.toml": "version = 2\n[workspace]\nroot = \"missing\"\n"})
			revision := commitSnapshotFiles(t, fixture)
			cache, err := OpenCache(resolveGitSourceWorkspace(t, writeGitSourceRoot(t)), fixture.runner)
			if err != nil {
				t.Fatal(err)
			}
			intent := Intent{MountPath: "/coding", Source: fixture.remote, Revision: revision}
			entry, err := cache.Entry(intent.MountPath, intent.Source)
			if err != nil {
				t.Fatal(err)
			}
			if mode != "fresh" {
				// Model state left by a resolver that accepted a raw repository.
				if err := cache.InitializeRepository(context.Background(), entry); err != nil {
					t.Fatal(err)
				}
				selected, err := validateIntent(intent)
				if err != nil {
					t.Fatal(err)
				}
				if _, err := cache.resolveRevision(context.Background(), entry, selected); err != nil {
					t.Fatal(err)
				}
				state := initialState(entry)
				setStateSelector(&state, selected)
				state.ResolvedRevision, state.SelectedSnapshot = revision, revision
				state.SelectedMode, state.SelectedRequest = selected.mode, revision
				state.LastAttemptAt = time.Now().UTC().Format(time.RFC3339Nano)
				if err := cache.WriteState(entry, state); err != nil {
					t.Fatal(err)
				}
				if mode == "cached" {
					writeSnapshotFiles(t, filepath.Join(entry.SnapshotsPath, revision), map[string]string{"factile.toml": "version = 2\n[workspace]\nroot = \"missing\"\n"})
				}
				if err := os.Rename(fixture.remotePath, fixture.remotePath+".offline"); err != nil {
					t.Fatal(err)
				}
			}
			for attempt := 0; attempt < 2; attempt++ {
				_, err := cache.Resolve(context.Background(), intent)
				assertSelectionReason(t, err, "missing_bundle")
			}
			status, err := cache.Status(intent)
			if err != nil || status.SnapshotAvailable || status.Stale || status.LastErrorReason != "missing_bundle" {
				t.Fatalf("invalid candidate status = %#v, %v", status, err)
			}
		})
	}
}

func TestSnapshotSymlinkOutsideSelectedBundle(t *testing.T) {
	fixture := newResolutionFixture(t)
	writeSnapshotFiles(t, fixture.workPath, map[string]string{
		"factile.toml":      "version = 2\n[workspace]\nroot = \"docs\"\n",
		"docs/factile.toml": selectedBundleManifest,
	})
	if err := os.Symlink("docs/factile.toml", filepath.Join(fixture.workPath, "linked")); err != nil {
		t.Skip(err)
	}
	commitSnapshotFiles(t, fixture)
	cache, err := OpenCache(resolveGitSourceWorkspace(t, writeGitSourceRoot(t)), fixture.runner)
	if err != nil {
		t.Fatal(err)
	}
	for attempt := 0; attempt < 2; attempt++ {
		_, err = cache.Resolve(context.Background(), Intent{MountPath: "/coding", Source: fixture.remote})
		assertSelectionReason(t, err, "symlink")
		if !errors.Is(err, ErrSnapshotSymlink) {
			t.Fatalf("symlink identity lost: %v", err)
		}
	}
}

func writeSnapshotFiles(t *testing.T, root string, files map[string]string) {
	t.Helper()
	for name, contents := range files {
		filename := filepath.Join(root, name)
		if err := os.MkdirAll(filepath.Dir(filename), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filename, []byte(contents), 0o600); err != nil {
			t.Fatal(err)
		}
	}
}

func commitSnapshotFiles(t *testing.T, fixture *resolutionFixture) string {
	t.Helper()
	gitRun(t, fixture.runner, fixture.workPath, "add", "--", ".")
	gitRun(t, fixture.runner, fixture.workPath, "commit", "-m", "snapshot layout")
	gitRun(t, fixture.runner, fixture.workPath, "push", "--", fixture.remote, "main:main")
	return gitOutput(t, fixture.runner, fixture.workPath, "rev-parse", "HEAD")
}

func assertSelectionReason(t *testing.T, err error, reason string) {
	t.Helper()
	if selectionReason(err) != reason {
		t.Fatalf("error = %v, want reason %s", err, reason)
	}
	if strings.Contains(err.Error(), string(filepath.Separator)+"tmp") {
		t.Fatalf("error leaks a path: %v", err)
	}
}
