package vfs

import (
	"strings"

	"github.com/factile/factile/pkg/okf"
	"github.com/factile/factile/pkg/storage"
)

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
