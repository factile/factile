package vfs

import (
	"regexp"
	"strings"

	"github.com/factile/factile/pkg/okf"
	"github.com/factile/factile/pkg/storage"
)

type BundleMetadata struct {
	Title       string `json:"title,omitempty"`
	Description string `json:"description,omitempty"`
	OKFVersion  string `json:"okf_version,omitempty"`
	License     string `json:"license,omitempty"`
	LicensePath string `json:"license_path,omitempty"`
	LicenseKind string `json:"license_kind,omitempty"`
	Origin      string `json:"origin"`
}

var spdxText = regexp.MustCompile(`^[A-Za-z0-9(][A-Za-z0-9.+(): -]*$`)

func readBundleMetadata(fields map[string]any, store storage.Local) *BundleMetadata {
	read := func(key string) string {
		value, _ := fields[key].(string)
		return strings.TrimSpace(value)
	}
	metadata := BundleMetadata{
		Title: read("title"), Description: read("description"), OKFVersion: read("okf_version"),
		License: read("license"), Origin: "index.md",
	}
	if metadata.License != "" {
		kind, rel := okf.ResolvePath("index", metadata.License, "license", "0.3", store.EntryKind)
		if kind == "file" {
			metadata.LicenseKind, metadata.LicensePath = "file", rel
		} else if spdxText.MatchString(metadata.License) {
			metadata.LicenseKind = "spdx"
		}
	}
	if metadata.Title == "" && metadata.Description == "" && metadata.License == "" && metadata.OKFVersion == "" {
		return nil
	}
	return &metadata
}

// SelectOKF determines the format at a materialized source's physical root.
func SelectOKF(mount Mount, standalone bool) Mount {
	mount.Portable = standalone
	if mount.SourcePath == "" {
		return mount
	}
	store := storage.Local{Root: mount.SourcePath, Portable: true}
	if store.EntryKind("index.md") != "file" {
		return mount
	}
	data, _, err := store.ReadConcept("index")
	if err != nil {
		return mount
	}
	doc, err := okf.ParseConcept("index", data)
	if err == nil {
		mount.OKFVersion, _ = doc.Frontmatter["okf_version"].(string)
		metadata := readBundleMetadata(doc.Frontmatter, store)
		if metadata != nil && (standalone || mount.OKFVersion == "0.3" || metadata.Title != "" || metadata.Description != "" || metadata.License != "") {
			mount.BundleMetadata = metadata
			if mount.Title == "" && mount.Description == "" && mount.MountPath == "/" && !standalone {
				if manifest, err := LoadManifest(mount.SourcePath); err == nil && manifest.Bundle != nil {
					mount.Title, mount.Description = manifest.Bundle.Title, manifest.Bundle.Description
				}
			}
			if mount.Title == "" {
				mount.Title = metadata.Title
			}
			if mount.Description == "" {
				mount.Description = metadata.Description
			}
		}
	}
	switch mount.OKFVersion {
	case "", "0.1", "0.2":
	default:
		mount.Portable = true
	}
	return mount
}

// portableConcept checks the document shape before entering the concept namespace.
func portableConcept(root, rel string) bool {
	store := storage.Local{Root: root, Portable: true}
	if store.EntryKind(rel+".md") != "file" {
		return false
	}
	if okf.IsReservedFile(rel + ".md") {
		return true
	}
	data, _, err := store.ReadConcept(rel)
	if err != nil {
		return false
	}
	doc, err := okf.ParseConcept(rel, data)
	value, _ := doc.Frontmatter["type"].(string)
	return err == nil && strings.TrimSpace(value) != ""
}
