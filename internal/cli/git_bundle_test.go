package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/factile/factile/pkg/factile"
)

const cliSelectedManifest = "version = 2\n[bundle]\nname = \"coding\"\ntitle = \"Selected Docs\"\ndescription = \"Selected bundle description.\"\n"
const cliBoundaryConcept = "---\ntype: Guide\ntitle: Boundary Contracts\n---\n\n# Boundary Contracts\n\nDescribe inputs and guarantees.\n"

func TestCLIGitSelectedBundleLayouts(t *testing.T) {
	for _, layout := range []string{"bundle", "combined", "nested"} {
		t.Run(layout, func(t *testing.T) {
			root := t.TempDir()
			writeCLICombinedWorkspace(t, root)
			t.Chdir(root)
			files := map[string]string{"factile.toml": cliSelectedManifest}
			bundle := ""
			if layout == "combined" {
				files["factile.toml"] += "[workspace]\nroot = \".\"\n"
			}
			if layout == "nested" {
				bundle = "docs/"
				files["factile.toml"] = "version = 2\n[workspace]\nroot = \"docs\"\n"
				files["docs/factile.toml"] = cliSelectedManifest
				files["outside.md"] = "This invalid concept must stay outside the mounted bundle.\n"
			}
			files[bundle+"practices/boundary-contracts.md"] = cliBoundaryConcept
			files[bundle+"index.md"] = "# Coding\n\n[Boundary Contracts](practices/boundary-contracts.md)\n"
			files[bundle+"hidden.mount.toml"] = "source = \"file:///does-not-exist.git\"\nwritable = false\n"
			files["factile.views.toml"] = "[[views]]\nid = \"source-only\"\npaths = [\"/hidden\"]\n"
			remote, _, revision := cliSnapshotRemote(t, files, false)
			mounted := runCLIJSON[factile.MountResult](t, "mount", remote, "/coding", "--revision", revision, "--json")
			if mounted.Mount.Title != "Selected Docs" || mounted.Mount.Description != "Selected bundle description." || mounted.Mount.Writable {
				t.Fatalf("selected metadata/capability = %#v", mounted)
			}
			for _, phase := range []string{"cached", "cold"} {
				if phase == "cold" {
					if err := os.RemoveAll(filepath.Join(root, ".factile", "cache", "git")); err != nil {
						t.Fatal(err)
					}
				}
				read := runCLIJSON[factile.ConceptResult](t, "read", "/coding/practices/boundary-contracts", "--json")
				if read.Concept.Path != "/coding/practices/boundary-contracts" || !strings.Contains(read.Concept.Markdown, "Describe inputs") {
					t.Fatalf("%s read = %#v", phase, read)
				}
				listed := runCLIJSON[factile.ListResult](t, "list", "/coding/practices", "--json")
				if len(listed.Documents) != 1 || listed.Documents[0].Path != read.Concept.Path {
					t.Fatalf("list = %#v", listed)
				}
				search := runCLIJSON[factile.SearchResults](t, "search", "/coding", "guarantees", "--json")
				if len(search.Results) != 1 || search.Results[0].Concept.Path != read.Concept.Path {
					t.Fatalf("search = %#v", search)
				}
				validated := runCLIJSON[factile.ValidationResult](t, "validate", "/coding", "--json")
				if !validated.Valid {
					t.Fatalf("validation = %#v", validated)
				}
				assertCLIJSONError(t, 4, factile.ErrConceptNotFound, "", "read", "/coding/docs/practices/boundary-contracts", "--json")
				assertCLIJSONError(t, 4, factile.ErrConceptNotFound, "", "read", "/coding/outside", "--json")
				mounts := runCLIJSON[factile.MountListResult](t, "mounts", "--json")
				if len(mounts.Mounts) != 1 || mounts.Mounts[0].SourceStatus.SelectedRevision != revision {
					t.Fatalf("source mounts imported: %#v", mounts)
				}
				views := runCLIJSON[factile.ViewListResult](t, "view", "list", "--json")
				if len(views.Views) != 0 {
					t.Fatalf("source views imported: %#v", views)
				}
			}
		})
	}
}

func TestCLIGitSelectionErrorsAndDescriptorAtomicity(t *testing.T) {
	for _, tc := range []struct {
		name, manifest, reason, message string
		files                           map[string]string
		symlink                         bool
	}{
		{name: "manifestless", reason: "missing_manifest", message: "Git snapshot has no repository-root factile.toml."},
		{name: "malformed", manifest: "[broken", reason: "invalid_manifest", message: "Git snapshot repository-root factile.toml is invalid."},
		{name: "traversing", manifest: "version = 2\n[workspace]\nroot = \"../secret\"\n", reason: "invalid_root", message: "Git snapshot workspace root is invalid."},
		{name: "missing", manifest: "version = 2\n[workspace]\nroot = \"docs\"\n", reason: "missing_bundle", message: "Git snapshot selected bundle is missing."},
		{name: "invalid", manifest: "version = 2\n[workspace]\nroot = \"docs\"\n", files: map[string]string{"docs/factile.toml": "[broken"}, reason: "invalid_bundle", message: "Git snapshot selected bundle manifest is invalid."},
		{name: "symlink", manifest: cliSelectedManifest, symlink: true, reason: "symlink", message: "Git snapshot contains a symlink."},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			writeCLICombinedWorkspace(t, root)
			t.Chdir(root)
			files := map[string]string{"overview.md": cliBoundaryConcept}
			if tc.manifest != "" {
				files["factile.toml"] = tc.manifest
			}
			for name, contents := range tc.files {
				files[name] = contents
			}
			remote, _, _ := cliSnapshotRemote(t, files, tc.symlink)
			assertCLISelectionError(t, tc.reason, tc.message, "mount", remote, "/coding", "--json")
			descriptor := filepath.Join(root, "coding.mount.toml")
			if _, err := os.Stat(descriptor); !os.IsNotExist(err) {
				t.Fatalf("failed mount wrote descriptor: %v", err)
			}
			// A failed replacement preserves an existing descriptor byte for byte.
			local := t.TempDir()
			writeCLIBundleManifest(t, local, "prior", "Prior Bundle")
			runCLIJSON[factile.MountResult](t, "mount", local, "/coding", "--json")
			before, err := os.ReadFile(descriptor)
			if err != nil {
				t.Fatal(err)
			}
			assertCLISelectionError(t, tc.reason, tc.message, "mount", remote, "/coding", "--json")
			after, err := os.ReadFile(descriptor)
			if err != nil || !bytes.Equal(before, after) {
				t.Fatalf("failed replacement changed descriptor: %v", err)
			}
			// Hand-authored intent remains visible, with mount-scoped validation errors.
			writeCLITestFile(t, descriptor, fmt.Sprintf("source = %q\nwritable = false\n", remote))
			mounts := runCLIJSON[factile.MountListResult](t, "mounts", "--json")
			if len(mounts.Mounts) != 1 || mounts.Mounts[0].SourceStatus.SnapshotAvailable {
				t.Fatalf("invalid source status = %#v", mounts)
			}
			assertCLISelectionError(t, tc.reason, tc.message, "read", "/coding/overview", "--json")
			validated := runCLIJSONWithCode[factile.ValidationResult](t, 3, "validate", "/coding", "--json")
			if validated.Valid || len(validated.Issues) != 1 || validated.Issues[0].Path != "/coding" || validated.Issues[0].Details["reason"] != tc.reason || validated.Issues[0].Message != tc.message {
				t.Fatalf("validation lost selection diagnostic: %#v", validated)
			}
		})
	}
}

func TestCLIGitInvalidRefreshPreservesBundleAndDescriptor(t *testing.T) {
	root := t.TempDir()
	writeCLICombinedWorkspace(t, root)
	t.Chdir(root)
	remote, source, revision := cliSnapshotRemote(t, map[string]string{
		"factile.toml":                         "version = 2\n[workspace]\nroot = \"docs\"\n",
		"docs/factile.toml":                    cliSelectedManifest,
		"docs/practices/boundary-contracts.md": cliBoundaryConcept,
	}, false)
	runCLIJSON[factile.MountResult](t, "mount", remote, "/coding", "--json")
	before, err := os.ReadFile(filepath.Join(root, "coding.mount.toml"))
	if err != nil {
		t.Fatal(err)
	}
	writeCLITestFile(t, filepath.Join(source, "factile.toml"), "version = 2\n[workspace]\nroot = \"missing\"\n")
	cliGitRun(t, source, "add", "--", ".")
	cliGitRun(t, source, "commit", "-m", "invalid selected root")
	cliGitRun(t, source, "push", "--", remote, "main:main")
	refreshed := runCLIJSON[factile.RefreshResult](t, "refresh", "/coding", "--json")
	if refreshed.Outcome != "stale" || refreshed.Status.SelectedRevision != revision || refreshed.Status.LastErrorCode != "validation_failed" || refreshed.Status.LastErrorReason != "missing_bundle" {
		t.Fatalf("refresh = %#v", refreshed)
	}
	runCLIJSON[factile.ConceptResult](t, "read", "/coding/practices/boundary-contracts", "--json")
	// Remounting the same intent must not hide the failed candidate with stale data.
	assertCLISelectionError(t, "missing_bundle", "Git snapshot selected bundle is missing.", "mount", remote, "/coding", "--title", "Replacement", "--json")
	after, err := os.ReadFile(filepath.Join(root, "coding.mount.toml"))
	if err != nil || !bytes.Equal(before, after) {
		t.Fatalf("invalid remount replaced descriptor: %v", err)
	}
	mounts := runCLIJSON[factile.MountListResult](t, "mounts", "--json")
	if mounts.Mounts[0].Title != "Selected Docs" || mounts.Mounts[0].Description != "Selected bundle description." {
		t.Fatalf("stale metadata changed: %#v", mounts)
	}
}

func TestCLIGitCommittedConsumerReconstructionAndPinnedRoot(t *testing.T) {
	root := t.TempDir()
	writeCLITestFile(t, filepath.Join(root, "factile.toml"), "version = 2\n[workspace]\nroot = \"docs\"\n")
	writeCLIBundleManifest(t, filepath.Join(root, "docs"), "consumer", "Consumer")
	remote, source, revision := cliSnapshotRemote(t, map[string]string{
		"factile.toml":                         "version = 2\n[workspace]\nroot = \"docs\"\n",
		"docs/factile.toml":                    cliSelectedManifest,
		"docs/practices/boundary-contracts.md": cliBoundaryConcept,
	}, false)
	t.Chdir(root)
	runCLIJSON[factile.MountResult](t, "mount", remote, "/coding", "--revision", revision, "--json")
	descriptor := filepath.Join("docs", "coding.mount.toml")
	before, err := os.ReadFile(filepath.Join(root, descriptor))
	if err != nil || !strings.Contains(string(before), revision) {
		t.Fatalf("descriptor must contain full pin: %s, %v", before, err)
	}
	cliGitRun(t, root, "init")
	cliGitRun(t, root, "config", "user.name", "Factile Test")
	cliGitRun(t, root, "config", "user.email", "factile@example.test")
	cliGitRun(t, root, "add", "--", "factile.toml", "docs/factile.toml", descriptor)
	cliGitRun(t, root, "commit", "-m", "consumer intent only")
	if tracked := cliGitOutput(t, root, "ls-files"); tracked != "docs/coding.mount.toml\ndocs/factile.toml\nfactile.toml" {
		t.Fatalf("consumer committed more than manifests and descriptor: %s", tracked)
	}
	assertPin := func(workspace string) {
		t.Helper()
		for _, phase := range []string{"before refresh", "after refresh"} {
			if phase == "after refresh" {
				refreshed := runCLIJSON[factile.RefreshResult](t, "--workspace", workspace, "refresh", "/coding", "--json")
				if refreshed.Outcome != "pinned" || refreshed.Status.SelectedRevision != revision {
					t.Fatalf("pin moved: %#v", refreshed)
				}
			}
			read := runCLIJSON[factile.ConceptResult](t, "--workspace", workspace, "read", "/coding/practices/boundary-contracts", "--json")
			if !strings.Contains(read.Concept.Markdown, "Describe inputs and guarantees.") || strings.Contains(read.Concept.Markdown, "Changed root") {
				t.Fatalf("%s read changed pinned content: %#v", phase, read)
			}
			validated := runCLIJSON[factile.ValidationResult](t, "--workspace", workspace, "validate", "/coding", "--json")
			if !validated.Valid || len(validated.Issues) != 0 {
				t.Fatalf("validation: %#v", validated)
			}
			mounts := runCLIJSON[factile.MountListResult](t, "--workspace", workspace, "mounts", "--json")
			if len(mounts.Mounts) != 1 || mounts.Mounts[0].SourceStatus.SelectedRevision != revision || mounts.Mounts[0].Writable {
				t.Fatalf("pin status: %#v", mounts)
			}
		}
		after, err := os.ReadFile(filepath.Join(workspace, descriptor))
		if err != nil || !bytes.Equal(before, after) {
			t.Fatalf("intent changed: %v", err)
		}
	}
	cloneConsumer := func() string {
		t.Helper()
		cold := filepath.Join(t.TempDir(), "consumer")
		cliGitRun(t, "", "clone", "--no-local", "--", root, cold)
		if _, err := os.Stat(filepath.Join(cold, ".factile")); !os.IsNotExist(err) {
			t.Fatalf("consumer inherited generated state: %v", err)
		}
		return cold
	}
	assertPin(cloneConsumer())
	// Keep both valid bundles to detect selection from remote HEAD instead of the pin.
	writeCLITestFile(t, filepath.Join(source, "factile.toml"), "version = 2\n[workspace]\nroot = \"knowledge\"\n")
	writeCLITestFile(t, filepath.Join(source, "knowledge", "factile.toml"), cliSelectedManifest)
	writeCLITestFile(t, filepath.Join(source, "knowledge", "practices", "boundary-contracts.md"), strings.ReplaceAll(cliBoundaryConcept, "Describe inputs and guarantees.", "Changed root content."))
	cliGitRun(t, source, "add", "--all", "--", ".")
	cliGitRun(t, source, "commit", "-m", "move workspace root")
	cliGitRun(t, source, "push", "--", remote, "main:main")
	assertPin(root)
	assertPin(cloneConsumer())
	// A new floating consumer proves that HEAD really selects the changed root.
	floating := t.TempDir()
	writeCLICombinedWorkspace(t, floating)
	runCLIJSON[factile.MountResult](t, "--workspace", floating, "mount", remote, "/coding", "--json")
	read := runCLIJSON[factile.ConceptResult](t, "--workspace", floating, "read", "/coding/practices/boundary-contracts", "--json")
	if !strings.Contains(read.Concept.Markdown, "Changed root content.") {
		t.Fatalf("HEAD did not move: %#v", read)
	}
}

func cliSnapshotRemote(t *testing.T, files map[string]string, symlink bool) (string, string, string) {
	t.Helper()
	source := t.TempDir()
	for name, contents := range files {
		writeCLITestFile(t, filepath.Join(source, name), contents)
	}
	if symlink {
		if err := os.Symlink("overview.md", filepath.Join(source, "linked.md")); err != nil {
			t.Skip(err)
		}
	}
	cliGitRun(t, "", "init", "--", source)
	cliGitRun(t, source, "config", "--local", "--", "user.name", "Factile Test")
	cliGitRun(t, source, "config", "--local", "--", "user.email", "factile@example.test")
	cliGitRun(t, source, "add", "--", ".")
	cliGitRun(t, source, "commit", "-m", "snapshot fixture")
	cliGitRun(t, source, "branch", "-M", "main")
	remote := filepath.Join(t.TempDir(), "remote.git")
	cliGitRun(t, "", "clone", "--bare", "--", source, remote)
	return (&url.URL{Scheme: "file", Path: filepath.ToSlash(remote)}).String(), source, cliGitOutput(t, source, "rev-parse", "HEAD")
}

func assertCLISelectionError(t *testing.T, reason, message string, args ...string) {
	t.Helper()
	var stdout, stderr bytes.Buffer
	code := Run(context.Background(), args, nil, &stdout, &stderr)
	var payload struct {
		Error factile.AppError `json:"error"`
	}
	if err := json.Unmarshal(stderr.Bytes(), &payload); err != nil {
		t.Fatalf("error JSON: %v, %s", err, stderr.String())
	}
	if code != 3 || stdout.Len() != 0 || payload.Error.Code != "validation_failed" || payload.Error.Message != message || payload.Error.Details["reason"] != reason || len(payload.Error.Details) != 1 {
		t.Fatalf("%v: code=%d stdout=%s error=%#v", args, code, stdout.String(), payload.Error)
	}
}
