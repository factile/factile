package catalog_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/factile/factile/pkg/catalog"
)

func TestLibraryCatalogRoundTrip(t *testing.T) {
	tmp := t.TempDir()
	filename := filepath.Join(tmp, "library.toml")
	library := catalog.Library{
		ID:          "local",
		Title:       "Local Library",
		Description: "Knowledge bases available in this workspace.",
		KnowledgeBases: []catalog.KnowledgeBaseRef{
			{ID: "project", Path: "/project", Catalog: "knowledge-bases/project.toml", Title: "Project"},
			{ID: "law", Path: "/law", Catalog: "knowledge-bases/law.toml", Status: "draft"},
		},
	}

	if err := catalog.WriteLibraryFile(filename, library); err != nil {
		t.Fatal(err)
	}
	loaded, err := catalog.LoadLibraryFile(filename)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.ID != "local" || len(loaded.KnowledgeBases) != 2 {
		t.Fatalf("unexpected library: %#v", loaded)
	}
	if loaded.KnowledgeBases[0].Path != "/law" || loaded.KnowledgeBases[1].Path != "/project" {
		t.Fatalf("expected stable path ordering: %#v", loaded.KnowledgeBases)
	}
}

func TestKnowledgeBaseCatalogRoundTrip(t *testing.T) {
	tmp := t.TempDir()
	filename := filepath.Join(tmp, "knowledge-bases", "project.toml")
	kb := catalog.KnowledgeBase{
		ID:              "project",
		Path:            "/project",
		Title:           "Project Knowledge Base",
		Description:     "Architecture and runbooks.",
		Purpose:         "Ground project work.",
		Audience:        "Coding agents",
		Profile:         "software-engineering",
		DefaultTrust:    "local",
		DefaultWritable: true,
		Bundles: []catalog.BundleLink{
			{
				ID:          "security",
				Path:        "/project/security",
				Source:      "factile://public/software-security-basics",
				Kind:        "remote",
				Version:     "2026-06",
				Trust:       "public",
				WhenToUse:   "Use for secure software design guidance.",
				Description: "Public software security basics.",
			},
			{
				ID:          "docs",
				Path:        "/project/docs",
				Source:      "./docs",
				Kind:        "local",
				Writable:    true,
				Title:       "Project Documentation",
				Description: "Local architecture references.",
				Profile:     "software-engineering",
				Priority:    100,
			},
		},
		Views: []catalog.View{
			{
				ID:          "reader",
				Title:       "Reader",
				Description: "Small reader-facing navigation.",
				Bundles:     []string{"docs"},
				WhenToUse:   "Use for ordinary project questions.",
			},
			{
				ID:           "engineering",
				Title:        "Engineering",
				Bundles:      []string{"docs", "security"},
				WhenNotToUse: "Do not use for sales workflows.",
				Status:       "active",
			},
		},
	}

	if err := catalog.WriteKnowledgeBaseFile(filename, kb); err != nil {
		t.Fatal(err)
	}
	loaded, err := catalog.LoadKnowledgeBaseFile(filename)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.ID != "project" || loaded.Profile != "software-engineering" || loaded.DefaultTrust != "local" || !loaded.DefaultWritable || len(loaded.Bundles) != 2 {
		t.Fatalf("unexpected knowledge base: %#v", loaded)
	}
	if loaded.Bundles[0].Path != "/project/docs" || !loaded.Bundles[0].Writable {
		t.Fatalf("expected local docs link first: %#v", loaded.Bundles)
	}
	if loaded.Bundles[1].Kind != "remote" || loaded.Bundles[1].Writable {
		t.Fatalf("expected read-only remote link: %#v", loaded.Bundles[1])
	}
	if len(loaded.Views) != 2 {
		t.Fatalf("expected views to round trip: %#v", loaded.Views)
	}
	if loaded.Views[0].ID != "engineering" || strings.Join(loaded.Views[0].Bundles, ",") != "docs,security" {
		t.Fatalf("expected stable engineering view first: %#v", loaded.Views)
	}
	if loaded.Views[1].WhenToUse != "Use for ordinary project questions." {
		t.Fatalf("expected view guidance to round trip: %#v", loaded.Views[1])
	}
}

func TestCatalogValidation(t *testing.T) {
	if err := catalog.ValidateLibrary(catalog.Library{
		ID: "local",
		KnowledgeBases: []catalog.KnowledgeBaseRef{
			{ID: "bad", Path: "relative", Catalog: "bad.toml"},
		},
	}); err == nil || !strings.Contains(err.Error(), "invalid knowledge base path") {
		t.Fatalf("expected invalid library path error, got %v", err)
	}

	if err := catalog.ValidateLibrary(catalog.Library{
		ID: "local",
		KnowledgeBases: []catalog.KnowledgeBaseRef{
			{ID: "project", Path: "/project", Catalog: "project.toml"},
			{ID: "project-copy", Path: "/project", Catalog: "copy.toml"},
		},
	}); err == nil || !strings.Contains(err.Error(), "duplicate knowledge base path") {
		t.Fatalf("expected duplicate library path error, got %v", err)
	}

	if err := catalog.ValidateKnowledgeBase(catalog.KnowledgeBase{
		ID:   "project",
		Path: "/project",
		Bundles: []catalog.BundleLink{
			{ID: "docs", Path: "/other/docs", Source: "./docs", Kind: "local"},
		},
	}); err == nil || !strings.Contains(err.Error(), "under knowledge base path") {
		t.Fatalf("expected bundle path scope error, got %v", err)
	}

	if err := catalog.ValidateKnowledgeBase(catalog.KnowledgeBase{
		ID:   "project",
		Path: "/project",
		Bundles: []catalog.BundleLink{
			{ID: "docs", Path: "/project/docs", Source: "./docs", Kind: "local"},
			{ID: "docs-copy", Path: "/project/docs", Source: "./other-docs", Kind: "local"},
		},
	}); err == nil || !strings.Contains(err.Error(), "duplicate bundle link path") {
		t.Fatalf("expected duplicate bundle path error, got %v", err)
	}

	if err := catalog.ValidateKnowledgeBase(catalog.KnowledgeBase{
		ID:   "project",
		Path: "/project",
		Bundles: []catalog.BundleLink{
			{ID: "remote", Path: "/project/remote", Source: "factile://public/demo", Kind: "remote", Writable: true},
		},
	}); err == nil || !strings.Contains(err.Error(), "read-only") {
		t.Fatalf("expected remote read-only error, got %v", err)
	}
}

func TestKnowledgeBaseViewValidation(t *testing.T) {
	base := func(view catalog.View) catalog.KnowledgeBase {
		return catalog.KnowledgeBase{
			ID:   "project",
			Path: "/project",
			Bundles: []catalog.BundleLink{
				{ID: "docs", Path: "/project/docs", Source: "./docs", Kind: "local"},
				{ID: "security", Path: "/project/security", Source: "./security", Kind: "local"},
			},
			Views: []catalog.View{view},
		}
	}

	tests := []struct {
		name string
		kb   catalog.KnowledgeBase
		want string
	}{
		{
			name: "empty view id",
			kb:   base(catalog.View{Bundles: []string{"docs"}}),
			want: "view id is required",
		},
		{
			name: "duplicate view id",
			kb: catalog.KnowledgeBase{
				ID:   "project",
				Path: "/project",
				Bundles: []catalog.BundleLink{
					{ID: "docs", Path: "/project/docs", Source: "./docs", Kind: "local"},
				},
				Views: []catalog.View{
					{ID: "reader", Bundles: []string{"docs"}},
					{ID: "reader", Bundles: []string{"docs"}},
				},
			},
			want: "duplicate view id",
		},
		{
			name: "duplicate bundle reference",
			kb:   base(catalog.View{ID: "reader", Bundles: []string{"docs", "docs"}}),
			want: "duplicate view bundle reference",
		},
		{
			name: "unknown bundle reference",
			kb:   base(catalog.View{ID: "reader", Bundles: []string{"missing"}}),
			want: "view references unknown bundle",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if err := catalog.ValidateKnowledgeBase(tc.kb); err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("expected %q error, got %v", tc.want, err)
			}
		})
	}
}

func TestKnowledgeBaseCatalogParsesViews(t *testing.T) {
	tmp := t.TempDir()
	filename := filepath.Join(tmp, "project.toml")
	data := `id = "project"
path = "/project"

[[bundles]]
id = "docs"
path = "/project/docs"
source = "./docs"
kind = "local"
writable = true

[[views]]
id = "reader"
title = "Reader"
bundles = ["docs"]
`
	if err := os.WriteFile(filename, []byte(data), 0o644); err != nil {
		t.Fatal(err)
	}
	kb, err := catalog.LoadKnowledgeBaseFile(filename)
	if err != nil {
		t.Fatal(err)
	}
	if len(kb.Views) != 1 || kb.Views[0].ID != "reader" || strings.Join(kb.Views[0].Bundles, ",") != "docs" {
		t.Fatalf("unexpected parsed views: %#v", kb.Views)
	}
}

func TestKnowledgeBaseCatalogParsesBundleOnlyCatalog(t *testing.T) {
	tmp := t.TempDir()
	filename := filepath.Join(tmp, "project.toml")
	data := `id = "project"
path = "/project"

[[bundles]]
id = "docs"
path = "/project/docs"
source = "./docs"
kind = "local"
writable = true
`
	if err := os.WriteFile(filename, []byte(data), 0o644); err != nil {
		t.Fatal(err)
	}
	kb, err := catalog.LoadKnowledgeBaseFile(filename)
	if err != nil {
		t.Fatal(err)
	}
	if len(kb.Bundles) != 1 || len(kb.Views) != 0 {
		t.Fatalf("unexpected bundle-only catalog: %#v", kb)
	}
}

func TestCatalogRejectsUnsupportedTables(t *testing.T) {
	tmp := t.TempDir()
	filename := filepath.Join(tmp, "library.toml")
	if err := os.WriteFile(filename, []byte("id = \"local\"\n[unexpected]\nkey = \"value\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := catalog.LoadLibraryFile(filename); err == nil {
		t.Fatal("expected unsupported table error")
	}
}

func TestCatalogRejectsUnsupportedKeysAndTypes(t *testing.T) {
	tests := []struct {
		name    string
		load    func(string) error
		content string
		want    string
	}{
		{
			name: "library unknown key",
			load: func(filename string) error {
				_, err := catalog.LoadLibraryFile(filename)
				return err
			},
			content: "id = \"local\"\nowner = \"team\"\n",
			want:    `root table key "owner" on line 2: unsupported catalog key`,
		},
		{
			name: "knowledge base bool key rejects string",
			load: func(filename string) error {
				_, err := catalog.LoadKnowledgeBaseFile(filename)
				return err
			},
			content: "id = \"project\"\npath = \"/project\"\ndefault_writable = \"true\"\n",
			want:    `root table key "default_writable" on line 3: expected bool, got string`,
		},
		{
			name: "bundle unknown key",
			load: func(filename string) error {
				_, err := catalog.LoadKnowledgeBaseFile(filename)
				return err
			},
			content: `id = "project"
path = "/project"

[[bundles]]
id = "docs"
path = "/project/docs"
source = "./docs"
kind = "local"
writable = true
extra = "ignored before"
`,
			want: `bundles table key "extra" on line 10: unsupported catalog key`,
		},
		{
			name: "view bundles reject scalar",
			load: func(filename string) error {
				_, err := catalog.LoadKnowledgeBaseFile(filename)
				return err
			},
			content: `id = "project"
path = "/project"

[[bundles]]
id = "docs"
path = "/project/docs"
source = "./docs"
kind = "local"
writable = true

[[views]]
id = "reader"
bundles = "docs"
`,
			want: `views table key "bundles" on line 13: expected strings, got string`,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			tmp := t.TempDir()
			filename := filepath.Join(tmp, "catalog.toml")
			if err := os.WriteFile(filename, []byte(tc.content), 0o644); err != nil {
				t.Fatal(err)
			}
			if err := tc.load(filename); err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("expected %q error, got %v", tc.want, err)
			}
		})
	}
}
