package render

import (
	"fmt"
	"io"
	"strings"

	"github.com/factile/factile/pkg/factile"
)

func (r *Renderer) RenderSummary(w io.Writer, result factile.SummaryResult) error {
	if _, err := fmt.Fprintln(w, r.helpTitle("Factile workspace")); err != nil {
		return err
	}
	if _, err := fmt.Fprintln(w, "Path: "+result.Workspace.Path); err != nil {
		return err
	}
	if result.Workspace.Version != "" {
		if _, err := fmt.Fprintln(w, "Version: "+result.Workspace.Version); err != nil {
			return err
		}
	}
	if err := r.renderSummaryKnowledge(w, result.Knowledge); err != nil {
		return err
	}
	if err := r.renderSummaryViews(w, result.Views); err != nil {
		return err
	}
	if err := r.renderSummarySources(w, result.Sources); err != nil {
		return err
	}
	if err := r.renderSummaryHealth(w, result.Health); err != nil {
		return err
	}
	return r.renderSummaryNext(w, result.NextCommands)
}

func (r *Renderer) renderSummaryKnowledge(w io.Writer, cards []factile.CardSummary) error {
	if _, err := fmt.Fprintln(w, "\nKnowledge:"); err != nil {
		return err
	}
	if len(cards) == 0 {
		_, err := fmt.Fprintln(w, "  No local knowledge configured.")
		return err
	}
	for _, card := range cards {
		if _, err := fmt.Fprintln(w, "  "+summaryLine(card.Path, card.Title, card.Description)); err != nil {
			return err
		}
	}
	return nil
}

func (r *Renderer) renderSummaryViews(w io.Writer, views []factile.LibraryView) error {
	if _, err := fmt.Fprintln(w, "\nKnowledge Views:"); err != nil {
		return err
	}
	if len(views) == 0 {
		_, err := fmt.Fprintln(w, "  No library views configured.")
		return err
	}
	for _, view := range views {
		if _, err := fmt.Fprintln(w, "  "+summaryLine(view.ID, view.Title, view.Description)); err != nil {
			return err
		}
		if len(view.Paths) > 0 {
			if _, err := fmt.Fprintln(w, "    paths: "+strings.Join(view.Paths, ", ")); err != nil {
				return err
			}
		}
	}
	return nil
}

func (r *Renderer) renderSummarySources(w io.Writer, sources []factile.Mount) error {
	if _, err := fmt.Fprintln(w, "\nSources:"); err != nil {
		return err
	}
	if len(sources) == 0 {
		_, err := fmt.Fprintln(w, "  No sources configured.")
		return err
	}
	for _, source := range sources {
		line := fmt.Sprintf("  %s -> %s", source.MountPath, source.Source)
		if source.Kind != "" {
			line += " (" + source.Kind + ")"
		}
		if source.Writable {
			line += " writable"
		} else {
			line += " read-only"
		}
		if _, err := fmt.Fprintln(w, line); err != nil {
			return err
		}
	}
	return nil
}

func (r *Renderer) renderSummaryHealth(w io.Writer, health []factile.HealthSummary) error {
	if _, err := fmt.Fprintln(w, "\nHealth:"); err != nil {
		return err
	}
	if len(health) == 0 {
		_, err := fmt.Fprintln(w, "  ok")
		return err
	}
	for _, item := range health {
		line := "  " + item.Status
		if item.Message != "" {
			line += " - " + item.Message
		}
		if _, err := fmt.Fprintln(w, line); err != nil {
			return err
		}
	}
	return nil
}

func (r *Renderer) renderSummaryNext(w io.Writer, commands []string) error {
	if _, err := fmt.Fprintln(w, "\nNext:"); err != nil {
		return err
	}
	for _, command := range commands {
		if _, err := fmt.Fprintln(w, "  "+command); err != nil {
			return err
		}
	}
	return nil
}
