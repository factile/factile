package cli

import (
	"context"
	"encoding/json"
	"path/filepath"
	"reflect"
	"strings"

	"github.com/factile/factile/pkg/factile"
	"github.com/factile/factile/pkg/revision"
	"github.com/factile/factile/pkg/storage"
)

type migrationChange struct {
	Path    string `json:"path"`
	Before  string `json:"before_revision"`
	After   string `json:"after_revision"`
	Diff    string `json:"diff"`
	Applied bool   `json:"applied"`
}

func migrationPlanRevision(root string, inventory migrationInventory, changes []migrationChange) string {
	value, _ := json.Marshal(struct {
		Root    string
		Target  string
		Entries []migrationEntry
		Changes []migrationChange
	}{root, "0.3", inventory.Entries, changes})
	return revision.DigestBytes(value)
}

func applyMigration(
	ctx context.Context,
	store storage.Local,
	inventory migrationInventory,
	changes []migrationChange,
	after map[string][]byte,
	replace func(string, []byte) error,
) error {
	if len(changes) == 0 {
		return nil
	}
	lock := filepath.Join(store.Root, ".factile-migration")
	return storage.WithFileLock(lock, func() error {
		expected := append([]migrationEntry{}, inventory.Entries...)
		check := func() error {
			current, err := inspectMigrationInventory(store)
			if err != nil {
				return err
			}
			if !reflect.DeepEqual(current.Entries, expected) {
				return factile.NewError(factile.ErrRevisionMismatch, "Bundle inventory changed; preview the current bundle before retrying")
			}
			return nil
		}
		if err := check(); err != nil {
			return err
		}
		write := func(item *migrationChange) error {
			if err := ctx.Err(); err != nil {
				return err
			}
			id := strings.TrimSuffix(item.Path, ".md")
			if item.Before == "" && store.EntryKind(item.Path) != "missing" {
				return factile.NewError(factile.ErrRevisionMismatch, "A new file appeared; preview again: "+item.Path)
			}
			if item.Before != "" {
				data, _, err := store.ReadConcept(id)
				if err != nil || revision.DigestBytes(data) != item.Before {
					return factile.NewError(factile.ErrRevisionMismatch, "Document changed; preview again: "+item.Path)
				}
			}
			if err := replace(id, after[id]); err != nil {
				return err
			}
			item.Applied = true
			for index := range expected {
				if expected[index].Path == item.Path {
					expected[index].Revision = item.After
					break
				}
			}
			return nil
		}
		var root *migrationChange
		for index := range changes {
			item := &changes[index]
			if item.Path == "index.md" {
				root = item
				continue
			}
			if err := write(item); err != nil {
				return err
			}
		}
		if err := check(); err != nil {
			return err
		}
		if root != nil {
			return write(root)
		}
		return nil
	})
}
