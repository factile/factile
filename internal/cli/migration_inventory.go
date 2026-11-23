package cli

import (
	"crypto/sha256"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/factile/factile/pkg/okf"
	"github.com/factile/factile/pkg/storage"
	"github.com/factile/factile/pkg/vfs"
)

type migrationEntry struct {
	Path     string `json:"path"`
	Kind     string `json:"kind"`
	Revision string `json:"revision,omitempty"`
}

type migrationInventory struct {
	Entries []migrationEntry
	Inputs  map[string][]byte
	IDs     []string
	Version string
}

func inspectMigrationInventory(store storage.Local) (migrationInventory, error) {
	inventory := migrationInventory{Entries: []migrationEntry{}, Inputs: map[string][]byte{}}
	root, err := os.OpenRoot(store.Root)
	if err != nil {
		return inventory, err
	}
	defer root.Close()
	err = filepath.WalkDir(store.Root, func(filename string, entry fs.DirEntry, err error) error {
		if err != nil || filename == store.Root {
			return err
		}
		relative, err := filepath.Rel(store.Root, filename)
		if err != nil {
			return err
		}
		rel := filepath.ToSlash(relative)
		if storage.HiddenPath(rel) || isMigrationLock(rel) {
			if entry.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if entry.Type()&os.ModeSymlink != 0 {
			inventory.Entries = append(inventory.Entries, migrationEntry{Path: rel, Kind: "symlink"})
			return nil
		}
		if entry.IsDir() {
			mount := vfs.SelectOKF(vfs.Mount{SourcePath: filename}, true)
			manifest, _ := vfs.LoadManifest(filename)
			if mount.OKFVersion != "" || manifest.Bundle != nil {
				inventory.Entries = append(inventory.Entries, migrationEntry{Path: rel, Kind: "nested_bundle"})
				return filepath.SkipDir
			}
			inventory.Entries = append(inventory.Entries, migrationEntry{Path: rel, Kind: "directory"})
			return nil
		}
		if !entry.Type().IsRegular() {
			inventory.Entries = append(inventory.Entries, migrationEntry{Path: rel, Kind: "special_file"})
			return nil
		}
		kind := "asset"
		revision := ""
		if strings.HasSuffix(rel, ".md") {
			id := strings.TrimSuffix(rel, ".md")
			data, _, err := store.ReadConcept(id)
			if err != nil {
				return err
			}
			revision = fmt.Sprintf("sha256:%x", sha256.Sum256(data))
			doc, err := okf.ParseConcept(id, data)
			kind = "ordinary_markdown"
			value, _ := doc.Frontmatter["type"].(string)
			switch {
			case okf.IsReservedFile(rel):
				kind = "reserved"
			case okf.FrontmatterStarts(string(data)) && err != nil:
				kind = "invalid_document"
			case err == nil && strings.TrimSpace(value) != "":
				kind = "concept"
			}
			if kind != "ordinary_markdown" {
				inventory.IDs = append(inventory.IDs, id)
				inventory.Inputs[id] = data
			}
			if id == "index" {
				if value, exists := doc.Frontmatter["okf_version"]; exists {
					version, ok := value.(string)
					if ok && version != "" {
						inventory.Version = version
					} else if !okf.EmptyMetadata(value) {
						inventory.Version = "invalid"
					}
				}
			}
		} else {
			file, err := root.Open(relative)
			if err != nil {
				return err
			}
			hash := sha256.New()
			_, err = io.Copy(hash, file)
			closeErr := file.Close()
			if err != nil {
				return err
			}
			if closeErr != nil {
				return closeErr
			}
			revision = fmt.Sprintf("sha256:%x", hash.Sum(nil))
		}
		inventory.Entries = append(inventory.Entries, migrationEntry{Path: rel, Kind: kind, Revision: revision})
		return nil
	})
	return inventory, err
}

func isMigrationLock(rel string) bool {
	return strings.HasSuffix(rel, ".md.lock")
}

func copyMigrationAssets(store storage.Local, destination string, entries []migrationEntry) error {
	root, err := os.OpenRoot(store.Root)
	if err != nil {
		return err
	}
	defer root.Close()
	for _, entry := range entries {
		switch entry.Kind {
		case "directory":
			if err := os.MkdirAll(filepath.Join(destination, filepath.FromSlash(entry.Path)), 0700); err != nil {
				return err
			}
		case "asset", "ordinary_markdown":
			if store.EntryKind(entry.Path) != "file" {
				return fmt.Errorf("migration input changed: %s", entry.Path)
			}
			input, err := root.Open(filepath.FromSlash(entry.Path))
			if err != nil {
				return err
			}
			output, err := os.Create(filepath.Join(destination, filepath.FromSlash(entry.Path)))
			if err != nil {
				input.Close()
				return err
			}
			_, copyErr := io.Copy(output, input)
			input.Close()
			closeErr := output.Close()
			if copyErr != nil {
				return copyErr
			}
			if closeErr != nil {
				return closeErr
			}
		}
	}
	return nil
}
