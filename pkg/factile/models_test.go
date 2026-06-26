package factile_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/factile/factile/pkg/factile"
)

func TestListResultReaderNavigationJSON(t *testing.T) {
	result := factile.ListResult{
		Path: "/project",
		Folders: []factile.FolderSummary{
			{
				Path:        "/project/docs",
				Title:       "Project Documentation",
				Description: "Local architecture and development references.",
			},
		},
		Documents: []factile.DocumentSummary{
			{
				Path:        "/project/overview",
				Type:        "Guide",
				Title:       "Overview",
				Description: "Project overview.",
				Tags:        []string{"project"},
				Revision:    "sha256:abc",
			},
		},
	}

	data, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	for _, want := range []string{`"path":"/project"`, `"folders"`, `"documents"`, `"/project/docs"`, `"/project/overview"`} {
		if !strings.Contains(text, want) {
			t.Fatalf("reader JSON missing %s: %s", want, text)
		}
	}
	for _, forbidden := range []string{`"kind"`, `"target_kind"`, `"concept_id"`, `"mounts"`} {
		if strings.Contains(text, forbidden) {
			t.Fatalf("reader JSON exposed %s: %s", forbidden, text)
		}
	}
}
