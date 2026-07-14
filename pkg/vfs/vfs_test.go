package vfs

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestResolveTargetKindsAndMDNormalization(t *testing.T) {
	tmp := t.TempDir()
	bundle := filepath.Join(tmp, "bundle")
	mustWrite(t, filepath.Join(bundle, "workflows", "invoice-import.md"), "---\ntype: Workflow\n---\n")
	registry := filepath.Join(tmp, "mount-registry.toml")
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
	for _, input := range []string{
		"/.factile/cache",
		"/.factile.md",
		"/docs/.git/config",
		"/docs/.GIT/HEAD",
	} {
		if _, err := NormalizePath(input); err == nil {
			t.Fatalf("NormalizePath(%q) should reject internal paths", input)
		}
	}
	tmp := t.TempDir()
	registry := filepath.Join(tmp, "mount-registry.toml")
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

func TestResolveLongestPrefixWins(t *testing.T) {
	tmp := t.TempDir()
	docsBundle := filepath.Join(tmp, "docs")
	deepBundle := filepath.Join(tmp, "deep")
	mustWrite(t, filepath.Join(docsBundle, "a.md"), "---\ntype: Note\n---\n")
	mustWrite(t, filepath.Join(deepBundle, "b.md"), "---\ntype: Note\n---\n")

	mounts := []Mount{
		{MountPath: "/docs", Source: docsBundle, SourcePath: docsBundle, Kind: "local", Writable: true},
		{MountPath: "/docs/deep", Source: deepBundle, SourcePath: deepBundle, Kind: "local", Writable: true},
	}
	target, err := Resolve(mounts, "/docs/a")
	if err != nil {
		t.Fatal(err)
	}
	if target.Mount.SourcePath != docsBundle {
		t.Fatalf("docs mount did not resolve: %s", target.Mount.SourcePath)
	}
	deep, err := Resolve(mounts, "/docs/deep/b")
	if err != nil {
		t.Fatal(err)
	}
	if deep.Mount.SourcePath != deepBundle {
		t.Fatalf("longest prefix did not win: %s", deep.Mount.SourcePath)
	}
}

func TestResolveMaterializedGitMountUsesLocalSnapshot(t *testing.T) {
	snapshot := t.TempDir()
	mustWrite(t, filepath.Join(snapshot, "guides", "setup.md"), "---\ntype: Guide\n---\n")
	mounts := []Mount{{MountPath: "/git", Source: "https://example.test/docs.git", SourcePath: snapshot, Kind: SourceKindGit}}
	target, err := Resolve(mounts, "/git/guides/setup")
	if err != nil {
		t.Fatal(err)
	}
	if target.Kind != TargetConcept || !target.Exists || target.Mount.Kind != SourceKindGit || target.Mount.SourcePath != snapshot {
		t.Fatalf("materialized Git mount did not use the local snapshot: %#v", target)
	}
}

func TestFindRootAndLoadMountsUseNearestRootConfig(t *testing.T) {
	tmp := t.TempDir()
	outer := filepath.Join(tmp, "outer")
	outerBundle := filepath.Join(tmp, "outer-bundle")
	inner := filepath.Join(outer, "docs", "nested")
	innerBundle := filepath.Join(tmp, "inner-bundle")
	mustWrite(t, filepath.Join(outerBundle, "a.md"), "---\ntype: Note\n---\n")
	mustWrite(t, filepath.Join(innerBundle, "b.md"), "---\ntype: Note\n---\n")
	writeRootConfig(t, outer)
	mustWrite(t, filepath.Join(outer, "outer.mount.toml"), `source = "`+outerBundle+`"
writable = true
`)
	writeRootConfig(t, inner)
	mustWrite(t, filepath.Join(inner, "inner.mount.toml"), `source = "`+innerBundle+`"
writable = true
`)

	root, ok, err := FindRoot(LoadOptions{WorkDir: filepath.Join(inner, "child")})
	if err != nil {
		t.Fatal(err)
	}
	if !ok || root != inner {
		t.Fatalf("root = %q ok=%v, want %q", root, ok, inner)
	}
	mounts, err := LoadMounts(LoadOptions{WorkDir: filepath.Join(inner, "child")})
	if err != nil {
		t.Fatal(err)
	}
	if len(mounts) != 2 || mounts[0].MountPath != "/" || mounts[1].MountPath != "/inner" {
		t.Fatalf("expected nearest root descriptor mounts only: %#v", mounts)
	}
}

func TestFindRootUsesGitDocsFallback(t *testing.T) {
	tmp := t.TempDir()
	repo := filepath.Join(tmp, "repo")
	child := filepath.Join(repo, "src", "pkg")
	writeRootConfig(t, filepath.Join(repo, "docs"))
	mustWrite(t, filepath.Join(repo, ".git", "HEAD"), "ref: refs/heads/main\n")
	if err := os.MkdirAll(child, 0o755); err != nil {
		t.Fatal(err)
	}

	root, ok, err := FindRoot(LoadOptions{WorkDir: child})
	if err != nil {
		t.Fatal(err)
	}
	if !ok || root != filepath.Join(repo, "docs") {
		t.Fatalf("root = %q ok=%v, want docs root", root, ok)
	}
}

func TestFindRootUsesDocsFallbackWithoutGit(t *testing.T) {
	tmp := t.TempDir()
	project := filepath.Join(tmp, "project")
	child := filepath.Join(project, "src", "pkg")
	writeRootConfig(t, filepath.Join(project, "docs"))
	if err := os.MkdirAll(child, 0o755); err != nil {
		t.Fatal(err)
	}

	root, ok, err := FindRoot(LoadOptions{WorkDir: child})
	if err != nil {
		t.Fatal(err)
	}
	if !ok || root != filepath.Join(project, "docs") {
		t.Fatalf("root = %q ok=%v, want docs root", root, ok)
	}
}

func TestFindRootDocsFallbackStopsAtGitBoundary(t *testing.T) {
	tmp := t.TempDir()
	parent := filepath.Join(tmp, "parent")
	repo := filepath.Join(parent, "repo")
	child := filepath.Join(repo, "src")
	writeRootConfig(t, filepath.Join(parent, "docs"))
	mustWrite(t, filepath.Join(repo, ".git", "HEAD"), "ref: refs/heads/main\n")
	if err := os.MkdirAll(child, 0o755); err != nil {
		t.Fatal(err)
	}

	root, ok, err := FindRoot(LoadOptions{WorkDir: child})
	if err != nil {
		t.Fatal(err)
	}
	if ok {
		t.Fatalf("root = %q ok=true, want no active root inside nested repo", root)
	}
}

func TestLoadDescriptorMountsParsesLocalMetadata(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "sources", "django")
	mustWrite(t, filepath.Join(source, "overview.md"), "---\ntype: Guide\n---\n")
	mustWrite(t, filepath.Join(root, "engineering.mount.toml"), `source = "sources/django"
writable = true
title = "Engineering"
description = "Engineering docs"
when_to_use = "Use for engineering work."
when_not_to_use = "Do not use for legal work."
version = "1"
ref = "main"
revision = "abc123"
trust = "local"
`)

	mounts, err := LoadDescriptorMounts(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(mounts) != 1 {
		t.Fatalf("mount count = %d: %#v", len(mounts), mounts)
	}
	mount := mounts[0]
	if mount.MountPath != "/engineering" || mount.Source != "sources/django" || mount.Kind != "local" || !mount.Writable || mount.SourcePath != source {
		t.Fatalf("unexpected local descriptor mount: %#v", mount)
	}
	if mount.Title != "Engineering" || mount.Description != "Engineering docs" || mount.WhenToUse == "" || mount.WhenNotToUse == "" || mount.Version != "1" || mount.Ref != "main" || mount.Revision != "abc123" || mount.Trust != "local" {
		t.Fatalf("metadata not preserved: %#v", mount)
	}
	if !mount.VersionSet || !mount.RefSet || !mount.RevisionSet {
		t.Fatalf("metadata field presence not preserved: %#v", mount)
	}
}

func TestLoadDescriptorMountPreservesEmptySelectorPresence(t *testing.T) {
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, "coding.mount.toml"), `source = "https://example.test/coding.git"
writable = false
ref = ""
`)
	mounts, err := LoadDescriptorMounts(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(mounts) != 1 || !mounts[0].RefSet || mounts[0].Ref != "" {
		t.Fatalf("empty ref presence was lost: %#v", mounts)
	}
}

func TestWriteMountDescriptorFile(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "sources", "django")
	written, err := WriteMountDescriptorFile(root, Mount{
		MountPath:   "/engineering/django",
		Source:      source,
		Writable:    false,
		Title:       "Django",
		Description: "Framework docs",
		Trust:       "team",
	})
	if err != nil {
		t.Fatal(err)
	}
	filename := filepath.Join(root, "engineering", "django.mount.toml")
	if written.RegistryPath != filename || written.Kind != "local" || written.SourcePath != source {
		t.Fatalf("unexpected written mount: %#v", written)
	}
	data, err := os.ReadFile(filename)
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	for _, want := range []string{`source = "` + source + `"`, "writable = false", `title = "Django"`, `description = "Framework docs"`, `trust = "team"`} {
		if !strings.Contains(text, want) {
			t.Fatalf("descriptor missing %q:\n%s", want, text)
		}
	}
	mounts, err := LoadDescriptorMounts(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(mounts) != 1 || mounts[0].MountPath != "/engineering/django" || mounts[0].Title != "Django" {
		t.Fatalf("unexpected reloaded descriptor: %#v", mounts)
	}
}

func TestLoadMountsDiscoversDescriptorMounts(t *testing.T) {
	root := t.TempDir()
	writeRootConfig(t, root)
	source := filepath.Join(root, "content", "docs")
	mustWrite(t, filepath.Join(source, "a.md"), "---\ntype: Note\n---\n")
	mustWrite(t, filepath.Join(root, "docs.mount.toml"), `source = "content/docs"
writable = true
title = "Docs"
`)

	mounts, err := LoadMounts(LoadOptions{WorkDir: root})
	if err != nil {
		t.Fatal(err)
	}
	target, err := Resolve(mounts, "/docs/a")
	if err != nil {
		t.Fatal(err)
	}
	if target.Kind != TargetConcept || target.Mount.SourcePath != source || target.Mount.Title != "Docs" {
		t.Fatalf("unexpected descriptor target: %#v", target)
	}
}

func TestLoadMountsIncludesRootSourceAndResolvesRootDocuments(t *testing.T) {
	root := t.TempDir()
	writeRootConfig(t, root)
	mustWrite(t, filepath.Join(root, "overview.md"), "---\ntype: Guide\n---\n")
	mustWrite(t, filepath.Join(root, "guides", "setup.md"), "---\ntype: Guide\n---\n")

	mounts, err := LoadMounts(LoadOptions{WorkDir: root})
	if err != nil {
		t.Fatal(err)
	}
	if len(mounts) != 1 || mounts[0].MountPath != "/" || mounts[0].SourcePath != root || !mounts[0].Writable {
		t.Fatalf("expected root mount only, got %#v", mounts)
	}
	overview, err := Resolve(mounts, "/overview")
	if err != nil {
		t.Fatal(err)
	}
	if overview.Kind != TargetConcept || overview.Mount.MountPath != "/" || overview.ConceptID != "overview" {
		t.Fatalf("unexpected root document target: %#v", overview)
	}
	folder, err := Resolve(mounts, "/guides")
	if err != nil {
		t.Fatal(err)
	}
	if folder.Kind != TargetPath || !folder.Exists || folder.ConceptID != "guides" {
		t.Fatalf("unexpected root directory target: %#v", folder)
	}
	nested, err := Resolve(mounts, "/guides/setup.md")
	if err != nil {
		t.Fatal(err)
	}
	if nested.Kind != TargetConcept || nested.Path != "/guides/setup" || nested.ConceptID != "guides/setup" {
		t.Fatalf("unexpected normalized nested target: %#v", nested)
	}
}

func TestLoadDescriptorMountsParsesRemoteSourceKinds(t *testing.T) {
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, "shared.mount.toml"), `source = "factile://public/shared"
writable = false
`)
	mustWrite(t, filepath.Join(root, "vendors", "tools.mount.toml"), `source = "git+https://example.test/tools.git"
writable = false
ref = "main"
revision = "abc123"
`)

	mounts, err := LoadDescriptorMounts(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(mounts) != 2 {
		t.Fatalf("mount count = %d: %#v", len(mounts), mounts)
	}
	if mounts[0].MountPath != "/shared" || mounts[0].Kind != "factile" || mounts[0].SourcePath != "" {
		t.Fatalf("unexpected factile mount: %#v", mounts[0])
	}
	if mounts[1].MountPath != "/vendors/tools" || mounts[1].Kind != "git" || mounts[1].Ref != "main" || mounts[1].Revision != "abc123" || mounts[1].SourcePath != "" {
		t.Fatalf("unexpected git mount: %#v", mounts[1])
	}
}

func TestMountDescriptorClassifiesNativeGitSources(t *testing.T) {
	root := t.TempDir()
	tests := []struct {
		mountPath string
		source    string
	}{
		{mountPath: "/native", source: "https://github.com/senseware/coding-practice.git"},
		{mountPath: "/scp", source: "git@github.com:senseware/coding-practice.git"},
		{mountPath: "/compat", source: "git+https://github.com/senseware/coding-practice.git"},
	}
	for _, tc := range tests {
		written, err := WriteMountDescriptorFile(root, Mount{MountPath: tc.mountPath, Source: tc.source})
		if err != nil {
			t.Fatal(err)
		}
		if written.Kind != SourceKindGit || written.Source != tc.source || written.SourcePath != "" {
			t.Fatalf("unexpected written Git descriptor: %#v", written)
		}
		loaded, err := LoadMountDescriptorFile(root, written.RegistryPath)
		if err != nil {
			t.Fatal(err)
		}
		if loaded.Kind != SourceKindGit || loaded.Source != tc.source || loaded.SourcePath != "" {
			t.Fatalf("unexpected loaded Git descriptor: %#v", loaded)
		}
	}
}

func TestLoadDescriptorMountsSkipsInternalAndMountedSources(t *testing.T) {
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, "external.mount.toml"), `source = "vendor/docs"
writable = true
`)
	mustWrite(t, filepath.Join(root, "vendor", "docs", "inner.mount.toml"), `source = "../inner"
writable = "bad"
`)
	mustWrite(t, filepath.Join(root, ".factile", "hidden.mount.toml"), `source = "../hidden"
writable = true
`)
	for _, internal := range []string{".FACTILE", ".git", ".GIT"} {
		mustWrite(t, filepath.Join(root, internal, "hidden.mount.toml"), `source = "../hidden"
writable = "invalid"
`)
	}

	mounts, err := LoadDescriptorMounts(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(mounts) != 1 || mounts[0].MountPath != "/external" {
		t.Fatalf("expected only parent descriptor mount, got %#v", mounts)
	}
}

func TestLoadDescriptorMountsRejectsDuplicateDescriptorPath(t *testing.T) {
	if os.PathSeparator == '\\' {
		t.Skip("backslash filename duplicate case is Unix-specific")
	}
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, "a\\b.mount.toml"), `source = "./one"
writable = true
`)
	mustWrite(t, filepath.Join(root, "a", "b.mount.toml"), `source = "../two"
writable = true
`)

	if _, err := LoadDescriptorMounts(root); err == nil || !strings.Contains(err.Error(), "Duplicate mount descriptor path: /a/b") {
		t.Fatalf("expected duplicate descriptor path error, got %v", err)
	}
}

func TestLoadMountsRejectsRootPathMountConflicts(t *testing.T) {
	tests := []struct {
		name  string
		setup func(t *testing.T, root string)
	}{
		{
			name: "file and mount",
			setup: func(t *testing.T, root string) {
				mustWrite(t, filepath.Join(root, "docs.md"), "---\ntype: Guide\n---\n")
			},
		},
		{
			name: "directory and mount",
			setup: func(t *testing.T, root string) {
				mustWrite(t, filepath.Join(root, "docs", "index.md"), "---\ntype: Guide\n---\n")
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			writeRootConfig(t, root)
			tc.setup(t, root)
			mustWrite(t, filepath.Join(root, "docs.mount.toml"), `source = "external"
writable = true
`)
			if _, err := LoadMounts(LoadOptions{WorkDir: root}); err == nil || !strings.Contains(err.Error(), "Path is both root path and mount: /docs") {
				t.Fatalf("expected root path mount conflict, got %v", err)
			}
		})
	}
}

func TestResolveRejectsRootFileDirectoryConflict(t *testing.T) {
	root := t.TempDir()
	writeRootConfig(t, root)
	mustWrite(t, filepath.Join(root, "topic.md"), "---\ntype: Guide\n---\n")
	mustWrite(t, filepath.Join(root, "topic", "index.md"), "---\ntype: Guide\n---\n")
	mounts, err := LoadMounts(LoadOptions{WorkDir: root})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Resolve(mounts, "/topic"); err == nil || !strings.Contains(err.Error(), "Path is both concept and directory: /topic") {
		t.Fatalf("expected file/directory ambiguity, got %v", err)
	}
}

func TestLoadMountDescriptorFileRejectsInvalidDescriptors(t *testing.T) {
	tests := []struct {
		name    string
		content string
		want    string
	}{
		{
			name: "missing source",
			content: `writable = true
`,
			want: "has no source",
		},
		{
			name: "missing writable",
			content: `source = "./docs"
`,
			want: "has no writable value",
		},
		{
			name: "unquoted source",
			content: `source = ./docs
writable = true
`,
			want: `mount key "source" on line 1 expects quoted string`,
		},
		{
			name: "quoted writable",
			content: `source = "./docs"
writable = "true"
`,
			want: `mount key "writable" on line 2 expects true or false`,
		},
		{
			name: "unsupported key",
			content: `source = "./docs"
writable = true
kind = "local"
`,
			want: `unsupported mount descriptor key "kind" on line 3`,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			filename := filepath.Join(root, "docs.mount.toml")
			mustWrite(t, filename, tc.content)
			if _, err := LoadMountDescriptorFile(root, filename); err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("expected %q error, got %v", tc.want, err)
			}
		})
	}
}

func TestExplicitRootRequiresConfig(t *testing.T) {
	tmp := t.TempDir()
	root := filepath.Join(tmp, "root")
	writeRootConfig(t, root)
	found, ok, err := FindRoot(LoadOptions{Root: root, WorkDir: filepath.Join(tmp, "elsewhere")})
	if err != nil {
		t.Fatal(err)
	}
	if !ok || found != root {
		t.Fatalf("root = %q ok=%v, want %q", found, ok, root)
	}
	if _, _, err := FindRoot(LoadOptions{Root: filepath.Join(tmp, "missing")}); err == nil || !strings.Contains(err.Error(), "config.toml") {
		t.Fatalf("expected explicit root config error, got %v", err)
	}
}

func TestLoadRootConfigParsesMinimalConfig(t *testing.T) {
	tmp := t.TempDir()
	writeRootConfig(t, tmp)
	cfg, err := LoadRootConfig(tmp)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Version != 1 || cfg.Name != "test" || cfg.Defaults.Format != "okf" {
		t.Fatalf("unexpected root config: %#v", cfg)
	}
	mustWrite(t, filepath.Join(tmp, ".factile", "config.toml"), `version = 2
`)
	if _, err := LoadRootConfig(tmp); err == nil || !strings.Contains(err.Error(), "unsupported root config version") {
		t.Fatalf("expected version validation error, got %v", err)
	}
}

func TestLoadMountsRequiresActiveRoot(t *testing.T) {
	tmp := t.TempDir()
	if _, ok, err := FindRoot(LoadOptions{WorkDir: tmp}); err != nil || ok {
		t.Fatalf("unexpected root ok=%v err=%v", ok, err)
	}
	if _, err := LoadMounts(LoadOptions{WorkDir: tmp}); err == nil || !strings.Contains(err.Error(), "No active Factile root") {
		t.Fatalf("expected no active root error, got %v", err)
	}
}

func TestExplicitMountFileBypassesRootDiscovery(t *testing.T) {
	tmp := t.TempDir()
	projectDir := filepath.Join(tmp, "project")
	writeRootConfig(t, projectDir)
	rootDocs := filepath.Join(projectDir, "docs")
	mustWrite(t, filepath.Join(rootDocs, "a.md"), "---\ntype: Note\n---\n")
	explicitBundle := filepath.Join(tmp, "explicit")
	mustWrite(t, filepath.Join(explicitBundle, "a.md"), "---\ntype: Note\n---\n")
	explicit := filepath.Join(tmp, "mount-registry.toml")
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
		t.Fatalf("explicit mount file should replace root discovery: %#v", mounts)
	}
}

func TestLoadRegistryFileDefaultsOmittedWritableToReadOnly(t *testing.T) {
	registry := filepath.Join(t.TempDir(), "mount-registry.toml")
	mustWrite(t, registry, `[mounts."/default"]
source = "./default"
kind = "local"

[mounts."/writable"]
source = "./writable"
kind = "local"
writable = true
`)

	mounts, err := LoadRegistryFile(registry)
	if err != nil {
		t.Fatal(err)
	}
	if len(mounts) != 2 || mounts[0].MountPath != "/default" || mounts[0].Writable || mounts[1].MountPath != "/writable" || !mounts[1].Writable {
		t.Fatalf("unexpected legacy registry capabilities: %#v", mounts)
	}
}

func TestLoadRegistryFileRejectsGitSources(t *testing.T) {
	tests := []struct {
		name    string
		content string
	}{
		{
			name: "declared Git kind",
			content: `[mounts."/coding"]
source = "file:///fixture/repository.git"
kind = "git"
writable = false
revision = "1111111111111111111111111111111111111111"
`,
		},
		{
			name: "native Git source with omitted kind",
			content: `[mounts."/coding"]
source = "https://example.test/coding.git"
writable = false
`,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			registry := filepath.Join(t.TempDir(), "mount-registry.toml")
			mustWrite(t, registry, tc.content)
			_, err := LoadRegistryFile(registry)
			var vfsErr *Error
			if !errors.As(err, &vfsErr) || vfsErr.Code != "unsupported_source" || vfsErr.Message != "Git sources are not supported with --mount-file; use an active Factile root." {
				t.Fatalf("unexpected legacy Git registry error: %v", err)
			}
		})
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
			registry := filepath.Join(tmp, "mount-registry.toml")
			mustWrite(t, registry, tc.content)
			if _, err := LoadRegistryFile(registry); err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("expected %q error, got %v", tc.want, err)
			}
		})
	}
}

func writeRootConfig(t *testing.T, root string) {
	t.Helper()
	mustWrite(t, filepath.Join(root, ".factile", "config.toml"), `version = 1

name = "test"
title = "Test"

[defaults]
format = "okf"
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
