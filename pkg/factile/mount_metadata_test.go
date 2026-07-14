package factile_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/factile/factile/pkg/factile"
)

func TestMountMetadataDefaultsPreferExplicitValuesThenRootConfig(t *testing.T) {
	ctx := context.Background()
	tmp := t.TempDir()
	root := filepath.Join(tmp, "root")
	source := filepath.Join(tmp, "source")
	writeRootConfig(t, root)
	mustWriteWorkspace(t, filepath.Join(source, ".factile", "config.toml"), `version = 1

name = "shared-engineering"
title = "Shared Engineering Practice"
description = "Portable engineering defaults."
`)
	mustWriteWorkspace(t, filepath.Join(source, "overview.md"), `---
type: Reference
title: Overview Title
description: Overview description.
---

# Overview
`)
	ws := factile.NewWorkspace(factile.WorkspaceOptions{WorkDir: root})

	inferred, err := ws.Mount(ctx, source, "/coding", factile.MountOptions{Writable: false})
	if err != nil {
		t.Fatal(err)
	}
	if inferred.Mount.Title != "Shared Engineering Practice" || inferred.Mount.Description != "Portable engineering defaults." {
		t.Fatalf("unexpected root config defaults: %#v", inferred.Mount)
	}

	overridden, err := ws.Mount(ctx, source, "/team-coding", factile.MountOptions{
		Writable: true,
		Title:    "Team Coding",
	})
	if err != nil {
		t.Fatal(err)
	}
	if overridden.Mount.Title != "Team Coding" || overridden.Mount.Description != "Portable engineering defaults." {
		t.Fatalf("explicit title should win independently: %#v", overridden.Mount)
	}

	relative, err := ws.Mount(ctx, "../../source", "/engineering/coding", factile.MountOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if relative.Mount.Title != "Shared Engineering Practice" || relative.Mount.Description != "Portable engineering defaults." {
		t.Fatalf("relative source should resolve from the descriptor directory: %#v", relative.Mount)
	}

	descriptor, err := os.ReadFile(filepath.Join(root, "coding.mount.toml"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`title = "Shared Engineering Practice"`, `description = "Portable engineering defaults."`} {
		if !strings.Contains(string(descriptor), want) {
			t.Fatalf("descriptor missing inferred metadata %q:\n%s", want, descriptor)
		}
	}
}

func TestMountMetadataDefaultsUseOverviewThenMountPath(t *testing.T) {
	ctx := context.Background()
	tmp := t.TempDir()
	root := filepath.Join(tmp, "root")
	writeRootConfig(t, root)
	ws := factile.NewWorkspace(factile.WorkspaceOptions{WorkDir: root})

	overviewSource := filepath.Join(tmp, "overview-source")
	mustWriteWorkspace(t, filepath.Join(overviewSource, "overview.md"), `---
type: Reference
title: Engineering Handbook
description: Shared engineering workflows and principles.
---

# Engineering Handbook
`)
	overview, err := ws.Mount(ctx, overviewSource, "/handbook", factile.MountOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if overview.Mount.Title != "Engineering Handbook" || overview.Mount.Description != "Shared engineering workflows and principles." {
		t.Fatalf("unexpected overview defaults: %#v", overview.Mount)
	}

	plainSource := filepath.Join(tmp, "plain-source")
	if err := os.MkdirAll(plainSource, 0o755); err != nil {
		t.Fatal(err)
	}
	plain, err := ws.Mount(ctx, plainSource, "/plain-notes", factile.MountOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if plain.Mount.Title != "Plain Notes" || plain.Mount.Description != "" {
		t.Fatalf("unexpected mount path fallback: %#v", plain.Mount)
	}

	malformedSource := filepath.Join(tmp, "malformed-source")
	mustWriteWorkspace(t, filepath.Join(malformedSource, ".factile", "config.toml"), "not valid root config\n")
	mustWriteWorkspace(t, filepath.Join(malformedSource, "overview.md"), "# Missing frontmatter\n")
	malformed, err := ws.Mount(ctx, malformedSource, "/malformed-source", factile.MountOptions{})
	if err != nil {
		t.Fatalf("malformed optional metadata should not block mounting: %v", err)
	}
	if malformed.Mount.Title != "Malformed Source" || malformed.Mount.Description != "" {
		t.Fatalf("unexpected malformed metadata fallback: %#v", malformed.Mount)
	}
}

func TestMountMetadataDefaultsPersistInLegacyRegistry(t *testing.T) {
	ctx := context.Background()
	tmp := t.TempDir()
	registry := filepath.Join(tmp, "mounts.toml")
	source := filepath.Join(tmp, "source")
	mustWriteWorkspace(t, filepath.Join(source, "overview.md"), `---
type: Reference
title: Shared Reference
description: Reusable reference material.
---

# Shared Reference
`)
	ws := factile.NewWorkspace(factile.WorkspaceOptions{MountFile: registry})
	if _, err := ws.Mount(ctx, source, "/reference", factile.MountOptions{Writable: true}); err != nil {
		t.Fatal(err)
	}

	mounts, err := ws.ListMounts(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(mounts.Mounts) != 1 || mounts.Mounts[0].Title != "Shared Reference" || mounts.Mounts[0].Description != "Reusable reference material." {
		t.Fatalf("registry did not preserve inferred metadata: %#v", mounts.Mounts)
	}
}
