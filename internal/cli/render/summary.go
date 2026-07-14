package render

import (
	"fmt"
	"io"
	"strings"

	"github.com/factile/factile/pkg/factile"
)

func (r *Renderer) RenderSummary(w io.Writer, result factile.SummaryResult) error {
	if _, err := fmt.Fprintln(w, r.summaryHeading("Factile Workspace:")); err != nil {
		return err
	}
	workspace := "  " + r.summaryPath(result.Workspace.Path)
	if result.Workspace.Version != "" {
		workspace += " (" + result.Workspace.Version + ")"
	}
	if _, err := fmt.Fprintln(w, workspace); err != nil {
		return err
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
	if _, err := fmt.Fprintln(w, "\n"+r.summaryHeading("Knowledge:")); err != nil {
		return err
	}
	if len(cards) == 0 {
		_, err := fmt.Fprintln(w, "  No local knowledge configured.")
		return err
	}
	for _, card := range cards {
		if _, err := fmt.Fprintln(w, "  "+r.summaryLine(card.Path, card.Title, card.Description)); err != nil {
			return err
		}
	}
	return nil
}

func (r *Renderer) renderSummaryViews(w io.Writer, views []factile.View) error {
	if _, err := fmt.Fprintln(w, "\n"+r.summaryHeading("Views:")); err != nil {
		return err
	}
	if len(views) == 0 {
		_, err := fmt.Fprintln(w, "  No views configured.")
		return err
	}
	for _, view := range views {
		if _, err := fmt.Fprintln(w, "  "+summaryLine(view.ID, view.Title, view.Description)); err != nil {
			return err
		}
		if len(view.Paths) > 0 {
			if _, err := fmt.Fprintln(w, "    paths: "+strings.Join(r.summaryPaths(view.Paths), ", ")); err != nil {
				return err
			}
		}
	}
	return nil
}

func (r *Renderer) summaryHeading(value string) string {
	return r.helpTitle(value)
}

func (r *Renderer) summaryPath(value string) string {
	if !r.colorEnabled {
		return value
	}
	return r.styles.Path.Render(value)
}

func (r *Renderer) summaryPaths(paths []string) []string {
	styled := make([]string, len(paths))
	for i, path := range paths {
		styled[i] = r.summaryPath(path)
	}
	return styled
}

func (r *Renderer) summaryLine(path string, title string, description string) string {
	line := r.summaryPath(path)
	if title != "" {
		line += "  " + title
	}
	if description != "" {
		line += " - " + description
	}
	return line
}

func (r *Renderer) renderSummarySources(w io.Writer, sources []factile.Mount) error {
	if _, err := fmt.Fprintln(w, "\n"+r.summaryHeading("Sources:")); err != nil {
		return err
	}
	if len(sources) == 0 {
		_, err := fmt.Fprintln(w, "  No sources configured.")
		return err
	}
	for _, source := range sources {
		line := fmt.Sprintf("  %s -> %s", r.summaryPath(source.MountPath), source.Source)
		if source.Kind != "" {
			line += " (" + source.Kind + ")"
		}
		if source.Writable {
			line += " writable"
		} else {
			line += " read-only"
		}
		line += renderSourceStatus(source.SourceStatus)
		if _, err := fmt.Fprintln(w, line); err != nil {
			return err
		}
	}
	return nil
}

func (r *Renderer) renderSummaryHealth(w io.Writer, health []factile.HealthSummary) error {
	if _, err := fmt.Fprintln(w, "\n"+r.summaryHeading("Health:")); err != nil {
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
	if _, err := fmt.Fprintln(w, "\n"+r.summaryHeading("Next:")); err != nil {
		return err
	}
	for _, command := range commands {
		if _, err := fmt.Fprintln(w, "  "+r.summaryCommand(command)); err != nil {
			return err
		}
	}
	return nil
}

func (r *Renderer) summaryCommand(command string) string {
	if !r.colorEnabled {
		return command
	}
	parts := strings.Fields(command)
	for i, part := range parts {
		if strings.HasPrefix(part, "/") {
			parts[i] = r.summaryPath(part)
		}
	}
	return strings.Join(parts, " ")
}
