package vfs

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestResolveTargetKindsAndMDNormalization(t *testing.T) {
	tmp := t.TempDir()
	bundle := filepath.Join(tmp, "bundle")
	mustWrite(t, filepath.Join(bundle, "workflows", "invoice-import.md"), "---\ntype: Workflow\n---\n")
	registry := filepath.Join(tmp, "mounts.toml")
	mustWrite(t, registry, `[mounts."/product-docs"]
source = "./bundle"
kind = "local"
writable = true
`)
	mounts, err := LoadRegistryFile(registry)
	if err != nil {
		t.Fatal(err)
	}
	cases := map[string]TargetKind{
		"/":                                      TargetVirtualRoot,
		"/product-docs":                          TargetBundle,
		"/product-docs/workflows":                TargetPath,
		"/product-docs/workflows/invoice-import": TargetConcept,
		"/product-docs/workflows/invoice-import.md": TargetConcept,
	}
	for input, want := range cases {
		target, err := Resolve(mounts, input)
		if err != nil {
			t.Fatalf("%s: %v", input, err)
		}
		if target.Kind != want {
			t.Fatalf("%s kind = %s, want %s", input, target.Kind, want)
		}
	}
}

func TestInvalidPathsAndDuplicateMounts(t *testing.T) {
	if _, err := NormalizePath("relative"); err == nil {
		t.Fatal("expected invalid relative path")
	}
	if _, err := NormalizePath("/a/../b"); err == nil {
		t.Fatal("expected traversal rejection")
	}
	if _, err := NormalizePath(`/a\..\b`); err == nil {
		t.Fatal("expected backslash traversal rejection")
	}
	tmp := t.TempDir()
	registry := filepath.Join(tmp, "mounts.toml")
	mustWrite(t, registry, `[mounts."/docs"]
source = "./a"
kind = "local"
writable = true

[mounts."/docs"]
source = "./b"
kind = "local"
writable = true
`)
	if _, err := LoadRegistryFile(registry); err == nil {
		t.Fatal("expected duplicate mount error")
	}
}

func TestLongestPrefixAndProjectOverride(t *testing.T) {
	tmp := t.TempDir()
	globalBundle := filepath.Join(tmp, "global")
	projectBundle := filepath.Join(tmp, "project")
	deepBundle := filepath.Join(tmp, "deep")
	mustWrite(t, filepath.Join(globalBundle, "a.md"), "---\ntype: Note\n---\n")
	mustWrite(t, filepath.Join(projectBundle, "a.md"), "---\ntype: Note\n---\n")
	mustWrite(t, filepath.Join(deepBundle, "b.md"), "---\ntype: Note\n---\n")

	configHome := filepath.Join(tmp, "config")
	t.Setenv("XDG_CONFIG_HOME", configHome)
	userRegistry := filepath.Join(configHome, "factile", "mounts.toml")
	mustWrite(t, userRegistry, `[mounts."/docs"]
source = "`+globalBundle+`"
kind = "local"
writable = true
`)
	projectDir := filepath.Join(tmp, "projectdir")
	projectRegistry := filepath.Join(projectDir, ".factile", "mounts.toml")
	mustWrite(t, projectRegistry, `[mounts."/docs"]
source = "`+projectBundle+`"
kind = "local"
writable = true

[mounts."/docs/deep"]
source = "`+deepBundle+`"
kind = "local"
writable = true
`)
	mounts, err := LoadMounts(LoadOptions{WorkDir: projectDir})
	if err != nil {
		t.Fatal(err)
	}
	target, err := Resolve(mounts, "/docs/a")
	if err != nil {
		t.Fatal(err)
	}
	if target.Mount.SourcePath != projectBundle {
		t.Fatalf("project mount did not override user mount: %s", target.Mount.SourcePath)
	}
	deep, err := Resolve(mounts, "/docs/deep/b")
	if err != nil {
		t.Fatal(err)
	}
	if deep.Mount.SourcePath != deepBundle {
		t.Fatalf("longest prefix did not win: %s", deep.Mount.SourcePath)
	}
}

func TestLoadMountsCompilesCatalogBundleLinks(t *testing.T) {
	tmp := t.TempDir()
	projectDir := filepath.Join(tmp, "project")
	docs := filepath.Join(projectDir, "docs")
	mustWrite(t, filepath.Join(docs, "architecture", "overview.md"), "---\ntype: Guide\n---\n")
	mustWrite(t, filepath.Join(projectDir, ".factile", "library.toml"), `id = "local"
title = "Local Library"

[[knowledge_bases]]
id = "project"
path = "/project"
catalog = "knowledge-bases/project.toml"
`)
	mustWrite(t, filepath.Join(projectDir, ".factile", "knowledge-bases", "project.toml"), `id = "project"
path = "/project"
title = "Project Knowledge Base"
default_trust = "local"

[[bundles]]
id = "docs"
path = "/project/docs"
source = "docs"
kind = "local"
writable = true

[[bundles]]
id = "security"
path = "/project/security"
source = "factile://public/software-security-basics"
kind = "remote"
writable = false
`)

	mounts, err := LoadMounts(LoadOptions{WorkDir: projectDir})
	if err != nil {
		t.Fatal(err)
	}
	if len(mounts) != 2 {
		t.Fatalf("mount count = %d: %#v", len(mounts), mounts)
	}
	target, err := Resolve(mounts, "/project/docs/architecture/overview")
	if err != nil {
		t.Fatal(err)
	}
	if target.Kind != TargetConcept || target.Mount.SourcePath != docs || !target.Mount.Writable {
		t.Fatalf("unexpected catalog-backed target: %#v", target)
	}
	remote, err := Resolve(mounts, "/project/security")
	if err != nil {
		t.Fatal(err)
	}
	if remote.Mount.Kind != "remote" || remote.Mount.Writable || remote.Mount.SourcePath != "" {
		t.Fatalf("unexpected remote catalog mount: %#v", remote.Mount)
	}
}

func TestMountPrecedenceIncludesCatalog(t *testing.T) {
	tmp := t.TempDir()
	configHome := filepath.Join(tmp, "config")
	t.Setenv("XDG_CONFIG_HOME", configHome)
	userBundle := filepath.Join(tmp, "user")
	catalogBundle := filepath.Join(tmp, "catalog")
	projectBundle := filepath.Join(tmp, "project-mount")
	for _, bundle := range []string{userBundle, catalogBundle, projectBundle} {
		mustWrite(t, filepath.Join(bundle, "a.md"), "---\ntype: Note\n---\n")
	}
	userRegistry := filepath.Join(configHome, "factile", "mounts.toml")
	mustWrite(t, userRegistry, `[mounts."/project/docs"]
source = "`+userBundle+`"
kind = "local"
writable = true
`)

	projectDir := filepath.Join(tmp, "project")
	writeCatalogMount(t, projectDir, "/project/docs", catalogBundle)
	projectRegistry := filepath.Join(projectDir, ".factile", "mounts.toml")
	mustWrite(t, projectRegistry, `[mounts."/project/docs"]
source = "`+projectBundle+`"
kind = "local"
writable = true
`)
	mounts, err := LoadMounts(LoadOptions{WorkDir: projectDir})
	if err != nil {
		t.Fatal(err)
	}
	target, err := Resolve(mounts, "/project/docs/a")
	if err != nil {
		t.Fatal(err)
	}
	if target.Mount.SourcePath != projectBundle {
		t.Fatalf("project mount should override catalog and user: %s", target.Mount.SourcePath)
	}

	catalogOnlyDir := filepath.Join(tmp, "catalog-only")
	writeCatalogMount(t, catalogOnlyDir, "/project/docs", catalogBundle)
	mounts, err = LoadMounts(LoadOptions{WorkDir: catalogOnlyDir})
	if err != nil {
		t.Fatal(err)
	}
	target, err = Resolve(mounts, "/project/docs/a")
	if err != nil {
		t.Fatal(err)
	}
	if target.Mount.SourcePath != catalogBundle {
		t.Fatalf("catalog mount should override user registry: %s", target.Mount.SourcePath)
	}
}

func TestExplicitMountFileBypassesCatalog(t *testing.T) {
	tmp := t.TempDir()
	projectDir := filepath.Join(tmp, "project")
	mustWrite(t, filepath.Join(projectDir, ".factile", "library.toml"), `id = "local"

[[knowledge_bases]]
id = "project"
path = "/project"
catalog = "knowledge-bases/project.toml"
`)
	mustWrite(t, filepath.Join(projectDir, ".factile", "knowledge-bases", "project.toml"), `id = "project"
path = "/project"

[[bundles]]
id = "docs"
path = "/project/docs"
source = "docs"
kind = "local"
writable = true
`)
	explicitBundle := filepath.Join(tmp, "explicit")
	mustWrite(t, filepath.Join(explicitBundle, "a.md"), "---\ntype: Note\n---\n")
	explicit := filepath.Join(tmp, "mounts.toml")
	mustWrite(t, explicit, `[mounts."/explicit"]
source = "`+explicitBundle+`"
kind = "local"
writable = true
`)

	mounts, err := LoadMounts(LoadOptions{WorkDir: projectDir, MountFile: explicit})
	if err != nil {
		t.Fatal(err)
	}
	if len(mounts) != 1 || mounts[0].MountPath != "/explicit" {
		t.Fatalf("explicit mount file should replace catalog discovery: %#v", mounts)
	}
}

func TestLoadRegistryFileRejectsUnsupportedMountValueForms(t *testing.T) {
	tests := []struct {
		name    string
		content string
		want    string
	}{
		{
			name: "unquoted source",
			content: `[mounts."/docs"]
source = ./docs
kind = "local"
writable = true
`,
			want: `mount key "source" on line 2 expects quoted string`,
		},
		{
			name: "quoted bool",
			content: `[mounts."/docs"]
source = "./docs"
writable = "true"
`,
			want: `mount key "writable" on line 3 expects true or false`,
		},
		{
			name: "invalid string",
			content: `[mounts."/docs"]
source = "unterminated
`,
			want: `invalid string for mount key "source" on line 2`,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			tmp := t.TempDir()
			registry := filepath.Join(tmp, "mounts.toml")
			mustWrite(t, registry, tc.content)
			if _, err := LoadRegistryFile(registry); err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("expected %q error, got %v", tc.want, err)
			}
		})
	}
}

func writeCatalogMount(t *testing.T, projectDir string, mountPath string, source string) {
	t.Helper()
	mustWrite(t, filepath.Join(projectDir, ".factile", "library.toml"), `id = "local"

[[knowledge_bases]]
id = "project"
path = "/project"
catalog = "knowledge-bases/project.toml"
`)
	mustWrite(t, filepath.Join(projectDir, ".factile", "knowledge-bases", "project.toml"), `id = "project"
path = "/project"

[[bundles]]
id = "docs"
path = "`+mountPath+`"
source = "`+source+`"
kind = "local"
writable = true
`)
}

func mustWrite(t *testing.T, name string, data string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(name), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(name, []byte(data), 0o644); err != nil {
		t.Fatal(err)
	}
}
