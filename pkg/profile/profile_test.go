package profile_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/factile/factile/pkg/profile"
)

func TestSoftwareProfileIsDiscoveredAsData(t *testing.T) {
	root := filepath.Join("..", "..", "profiles")
	summaries, err := profile.List(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(summaries) != 1 || summaries[0].ID != "software" {
		t.Fatalf("unexpected profile summaries: %#v", summaries)
	}

	loaded, err := profile.Load(root, "software")
	if err != nil {
		t.Fatal(err)
	}
	requiredTypes := []string{
		"Architecture Decision Record",
		"Runbook",
		"Workflow",
		"Domain Concept",
		"API",
		"Module or Service",
		"Data Model",
		"Deployment",
		"Testing Strategy",
		"Security Note",
	}
	for _, required := range requiredTypes {
		if !hasDocumentType(loaded, required) {
			t.Fatalf("profile missing document type %q: %#v", required, loaded.DocumentTypes)
		}
	}
	for _, tmpl := range loaded.Templates {
		if _, err := os.Stat(filepath.Join(root, "software", tmpl.Path)); err != nil {
			t.Fatalf("template %s missing: %v", tmpl.Path, err)
		}
	}
}

func TestSoftwareRecipesAreDiscoveredAsData(t *testing.T) {
	root := filepath.Join("..", "..", "profiles")
	recipes, err := profile.LoadProfileRecipes(root, "software")
	if err != nil {
		t.Fatal(err)
	}
	wantIDs := []string{
		"software.answer-question",
		"software.review-code",
		"software.design-feature",
		"software.document-feature",
		"software.write-runbook",
		"software.capture-decision",
		"software.validate-bundle",
	}
	if len(recipes) != len(wantIDs) {
		t.Fatalf("recipe count = %d, want %d: %#v", len(recipes), len(wantIDs), recipes)
	}
	for i, want := range wantIDs {
		if recipes[i].ID != want {
			t.Fatalf("recipe[%d] = %s, want %s", i, recipes[i].ID, want)
		}
		if recipes[i].Profile != "software" || len(recipes[i].Steps) == 0 {
			t.Fatalf("invalid recipe metadata: %#v", recipes[i])
		}
		if !recipeUsesDataOrFactile(recipes[i]) {
			t.Fatalf("recipe has no Factile command or profile data reference: %#v", recipes[i])
		}
	}
}

func hasDocumentType(p profile.Profile, value string) bool {
	for _, item := range p.DocumentTypes {
		if item.Type == value {
			return true
		}
	}
	return false
}

func recipeUsesDataOrFactile(recipe profile.Recipe) bool {
	for _, step := range recipe.Steps {
		if strings.Contains(step.Command, "factile ") || strings.Contains(step.Command, "profiles/software/") {
			return true
		}
	}
	return false
}
