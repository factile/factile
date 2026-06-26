package render

import (
	"fmt"
	"io"
	"strings"

	"github.com/factile/factile/pkg/factile"
)

func (r *Renderer) RenderList(w io.Writer, result factile.ListResult) error {
	if _, err := fmt.Fprintln(w, r.path(result.Path)); err != nil {
		return err
	}
	if len(result.Cards) > 0 {
		return r.renderCards(w, result.Cards)
	}
	wrote := false
	if len(result.Folders) > 0 {
		if err := r.renderFolderSummaries(w, result.Folders); err != nil {
			return err
		}
		wrote = true
	}
	if len(result.Documents) > 0 {
		if wrote {
			if _, err := fmt.Fprintln(w); err != nil {
				return err
			}
		}
		if err := r.renderDocumentSummaries(w, result.Documents); err != nil {
			return err
		}
		wrote = true
	}
	if len(result.Concepts) > 0 {
		if wrote {
			if _, err := fmt.Fprintln(w); err != nil {
				return err
			}
		}
		if err := r.renderConceptSummaries(w, result.Concepts); err != nil {
			return err
		}
		wrote = true
	}
	if len(result.Mounts) > 0 {
		if wrote {
			if _, err := fmt.Fprintln(w); err != nil {
				return err
			}
		}
		if err := r.renderMountSummaries(w, result.Mounts); err != nil {
			return err
		}
		wrote = true
	}
	if len(result.Paths) > 0 {
		if wrote {
			if _, err := fmt.Fprintln(w); err != nil {
				return err
			}
		}
		if err := r.renderPaths(w, result.Paths); err != nil {
			return err
		}
		wrote = true
	}
	if !wrote {
		_, err := fmt.Fprintln(w, "No entries.")
		return err
	}
	return nil
}

func (r *Renderer) RenderStat(w io.Writer, result factile.StatResult) error {
	return r.renderCard(w, result.Card)
}

func (r *Renderer) RenderRead(w io.Writer, result factile.ConceptResult) error {
	concept := result.Concept
	if _, err := fmt.Fprintln(w, r.path(concept.Path)); err != nil {
		return err
	}
	metadata := cloneMetadata(concept.Frontmatter)
	metadata["revision"] = concept.Revision
	if rendered := r.RenderMetadata(metadata); rendered != "" {
		if _, err := fmt.Fprintln(w); err != nil {
			return err
		}
		if _, err := fmt.Fprintln(w, rendered); err != nil {
			return err
		}
	}
	body := strings.TrimSpace(concept.Markdown)
	if body == "" {
		return nil
	}
	rendered, err := r.RenderMarkdown(body)
	if err != nil {
		return err
	}
	if _, err := fmt.Fprintln(w); err != nil {
		return err
	}
	_, err = fmt.Fprintln(w, strings.TrimRight(rendered, "\n"))
	return err
}

func (r *Renderer) renderFolderSummaries(w io.Writer, folders []factile.FolderSummary) error {
	if _, err := fmt.Fprintln(w, "Folders:"); err != nil {
		return err
	}
	for _, folder := range folders {
		if _, err := fmt.Fprintln(w, "  "+summaryLine(folder.Path, folder.Title, folder.Description)); err != nil {
			return err
		}
	}
	return nil
}

func (r *Renderer) renderDocumentSummaries(w io.Writer, documents []factile.DocumentSummary) error {
	if _, err := fmt.Fprintln(w, "Documents:"); err != nil {
		return err
	}
	for _, document := range documents {
		if _, err := fmt.Fprintln(w, "  "+summaryLine(document.Path, document.Title, document.Description)); err != nil {
			return err
		}
	}
	return nil
}

func (r *Renderer) renderConceptSummaries(w io.Writer, concepts []factile.ConceptSummary) error {
	if _, err := fmt.Fprintln(w, "Documents:"); err != nil {
		return err
	}
	for _, concept := range concepts {
		if _, err := fmt.Fprintln(w, "  "+summaryLine(concept.Path, concept.Title, concept.Description)); err != nil {
			return err
		}
	}
	return nil
}

func (r *Renderer) renderMountSummaries(w io.Writer, mounts []factile.Mount) error {
	if _, err := fmt.Fprintln(w, "Bundles:"); err != nil {
		return err
	}
	for _, mount := range mounts {
		if _, err := fmt.Fprintln(w, "  "+summaryLine(mount.MountPath, mount.Source, mount.Kind)); err != nil {
			return err
		}
	}
	return nil
}

func (r *Renderer) renderPaths(w io.Writer, paths []string) error {
	if _, err := fmt.Fprintln(w, "Paths:"); err != nil {
		return err
	}
	for _, path := range paths {
		if _, err := fmt.Fprintln(w, "  "+path); err != nil {
			return err
		}
	}
	return nil
}

func (r *Renderer) renderCards(w io.Writer, cards []factile.CardSummary) error {
	for _, card := range cards {
		if _, err := fmt.Fprintln(w); err != nil {
			return err
		}
		if err := r.renderCard(w, card); err != nil {
			return err
		}
	}
	return nil
}

func (r *Renderer) renderCard(w io.Writer, card factile.CardSummary) error {
	if _, err := fmt.Fprintln(w, r.path(card.Path)); err != nil {
		return err
	}
	metadata := map[string]any{}
	if card.Title != "" {
		metadata["title"] = card.Title
	}
	if card.Description != "" {
		metadata["description"] = card.Description
	}
	if len(card.Tags) > 0 {
		metadata["tags"] = card.Tags
	}
	if card.WhenToUse != "" {
		metadata["when_to_use"] = card.WhenToUse
	}
	if card.Writable != nil {
		metadata["writable"] = *card.Writable
	}
	if card.Revision != "" {
		metadata["revision"] = card.Revision
	}
	if rendered := r.RenderMetadata(metadata); rendered != "" {
		_, err := fmt.Fprintln(w, rendered)
		return err
	}
	return nil
}

func (r *Renderer) path(value string) string {
	if !r.colorEnabled {
		return value
	}
	return r.styles.Heading.Render(value)
}

func summaryLine(path string, title string, description string) string {
	line := path
	if title != "" {
		line += "  " + title
	}
	if description != "" {
		line += " - " + description
	}
	return line
}

func cloneMetadata(values map[string]any) map[string]any {
	cloned := make(map[string]any, len(values)+1)
	for key, value := range values {
		cloned[key] = value
	}
	return cloned
}
