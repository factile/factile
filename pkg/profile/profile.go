package profile

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/factile/factile/profiles"
)

type Summary struct {
	ID          string `json:"id"`
	Title       string `json:"title"`
	Description string `json:"description,omitempty"`
}

type DocumentType struct {
	ID      string `json:"id"`
	Type    string `json:"type"`
	Purpose string `json:"purpose,omitempty"`
}

type Template struct {
	ID           string `json:"id"`
	DocumentType string `json:"document_type"`
	Path         string `json:"path"`
}

type Profile struct {
	ID            string         `json:"id"`
	Title         string         `json:"title"`
	Description   string         `json:"description,omitempty"`
	DocumentTypes []DocumentType `json:"document_types"`
	Templates     []Template     `json:"templates"`
	Recipes       []string       `json:"recipes"`
}

type Step struct {
	Title   string `json:"title"`
	Command string `json:"command"`
}

type Recipe struct {
	ID      string `json:"id"`
	Title   string `json:"title"`
	Profile string `json:"profile"`
	Mode    string `json:"mode"`
	Purpose string `json:"purpose,omitempty"`
	Steps   []Step `json:"steps"`
}

func List(root string) ([]Summary, error) {
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil, err
	}
	var summaries []Summary
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		profile, err := Load(root, entry.Name())
		if err != nil {
			return nil, err
		}
		summaries = append(summaries, Summary{ID: profile.ID, Title: profile.Title, Description: profile.Description})
	}
	sort.Slice(summaries, func(i, j int) bool { return summaries[i].ID < summaries[j].ID })
	return summaries, nil
}

func Load(root string, id string) (Profile, error) {
	var profile Profile
	if err := readJSON(filepath.Join(root, id, "profile.json"), &profile); err != nil {
		return Profile{}, err
	}
	return profile, nil
}

func BundledTemplate(id string) ([]byte, error) {
	manifest, err := profiles.Software.ReadFile("software/profile.json")
	if err != nil {
		return nil, err
	}
	var software Profile
	if err := json.Unmarshal(manifest, &software); err != nil {
		return nil, err
	}
	var ids []string
	for _, template := range software.Templates {
		if template.ID == id {
			return profiles.Software.ReadFile("software/" + template.Path)
		}
		ids = append(ids, template.ID)
	}
	return nil, fmt.Errorf("unknown template %q. Available templates: %s", id, strings.Join(ids, ", "))
}

func LoadRecipe(root string, profileID string, recipeID string) (Recipe, error) {
	filename := strings.TrimPrefix(recipeID, profileID+".")
	var recipe Recipe
	if err := readJSON(filepath.Join(root, profileID, "recipes", filename+".json"), &recipe); err != nil {
		return Recipe{}, err
	}
	return recipe, nil
}

func LoadProfileRecipes(root string, profileID string) ([]Recipe, error) {
	profile, err := Load(root, profileID)
	if err != nil {
		return nil, err
	}
	recipes := make([]Recipe, 0, len(profile.Recipes))
	for _, id := range profile.Recipes {
		recipe, err := LoadRecipe(root, profileID, id)
		if err != nil {
			return nil, err
		}
		recipes = append(recipes, recipe)
	}
	return recipes, nil
}

func readJSON(filename string, value any) error {
	data, err := os.ReadFile(filename)
	if err != nil {
		return err
	}
	return json.Unmarshal(data, value)
}
