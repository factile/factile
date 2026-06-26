package render

import (
	"fmt"
	"io"
	"strings"

	"github.com/factile/factile/pkg/factile"
)

func (r *Renderer) RenderSearch(w io.Writer, result factile.SearchResults) error {
	if _, err := fmt.Fprintln(w, r.path("Search "+result.Path)); err != nil {
		return err
	}
	if _, err := fmt.Fprintln(w, "Query: "+result.Query); err != nil {
		return err
	}
	if len(result.Results) == 0 {
		_, err := fmt.Fprintln(w, "\nNo results.")
		return err
	}
	for i, item := range result.Results {
		if _, err := fmt.Fprintln(w); err != nil {
			return err
		}
		line := fmt.Sprintf("%d. %.2f %s", i+1, item.Score, item.Concept.Path)
		if item.Concept.Title != "" {
			line += "  " + item.Concept.Title
		}
		if _, err := fmt.Fprintln(w, line); err != nil {
			return err
		}
		if item.Snippet != "" {
			if _, err := fmt.Fprintln(w, "   "+oneLine(item.Snippet)); err != nil {
				return err
			}
		}
	}
	return nil
}

func (r *Renderer) RenderContext(w io.Writer, result factile.ContextPack) error {
	if _, err := fmt.Fprintln(w, r.path("Context "+result.Path)); err != nil {
		return err
	}
	if _, err := fmt.Fprintln(w, "Query: "+result.Query); err != nil {
		return err
	}
	if len(result.Concepts) == 0 {
		if _, err := fmt.Fprintln(w, "\nNo context documents selected."); err != nil {
			return err
		}
	} else {
		for _, concept := range result.Concepts {
			if _, err := fmt.Fprintln(w, "\n---"); err != nil {
				return err
			}
			if err := r.RenderRead(w, factile.ConceptResult{Concept: concept}); err != nil {
				return err
			}
		}
	}
	if len(result.Omitted) > 0 {
		if err := r.renderOmitted(w, result.Omitted); err != nil {
			return err
		}
	}
	return nil
}

func (r *Renderer) RenderGraph(w io.Writer, result factile.GraphResult) error {
	if _, err := fmt.Fprintln(w, r.path("Graph "+result.Path)); err != nil {
		return err
	}
	if len(result.Nodes) > 0 {
		if _, err := fmt.Fprintln(w, "\nNodes:"); err != nil {
			return err
		}
		for _, node := range result.Nodes {
			if _, err := fmt.Fprintln(w, "  "+summaryLine(node.Concept.Path, node.Concept.Title, node.Concept.Description)); err != nil {
				return err
			}
		}
	}
	if len(result.Edges) > 0 {
		if _, err := fmt.Fprintln(w, "\nEdges:"); err != nil {
			return err
		}
		for _, edge := range result.Edges {
			line := fmt.Sprintf("  %s -> %s", edge.From, edge.To)
			if edge.Kind != "" {
				line += " (" + edge.Kind + ")"
			}
			if _, err := fmt.Fprintln(w, line); err != nil {
				return err
			}
		}
	}
	if len(result.Nodes) == 0 && len(result.Edges) == 0 {
		if _, err := fmt.Fprintln(w, "\nNo graph entries."); err != nil {
			return err
		}
	}
	if len(result.Issues) > 0 {
		if err := r.renderIssues(w, result.Issues); err != nil {
			return err
		}
	}
	if len(result.Omitted) > 0 {
		if err := r.renderOmitted(w, result.Omitted); err != nil {
			return err
		}
	}
	return nil
}

func (r *Renderer) RenderValidation(w io.Writer, result factile.ValidationResult) error {
	status := "valid"
	if !result.Valid {
		status = "invalid"
	}
	if _, err := fmt.Fprintln(w, status+" "+result.Path); err != nil {
		return err
	}
	if len(result.Issues) == 0 {
		return nil
	}
	return r.renderIssues(w, result.Issues)
}

func (r *Renderer) renderIssues(w io.Writer, issues []factile.ValidationIssue) error {
	groups := map[string][]factile.ValidationIssue{}
	var order []string
	for _, issue := range issues {
		severity := strings.ToLower(strings.TrimSpace(issue.Severity))
		if severity == "" {
			severity = "issue"
		}
		if _, ok := groups[severity]; !ok {
			order = append(order, severity)
		}
		groups[severity] = append(groups[severity], issue)
	}
	for _, severity := range issueSeverityOrder(order) {
		if _, err := fmt.Fprintln(w); err != nil {
			return err
		}
		if _, err := fmt.Fprintln(w, r.issueHeading(severity)); err != nil {
			return err
		}
		for _, issue := range groups[severity] {
			if _, err := fmt.Fprintln(w, "  "+issueLine(issue)); err != nil {
				return err
			}
		}
	}
	return nil
}

func (r *Renderer) renderOmitted(w io.Writer, omitted []factile.OmittedItem) error {
	if _, err := fmt.Fprintln(w, "\nOmitted:"); err != nil {
		return err
	}
	for _, item := range omitted {
		line := "  "
		if item.Path != "" {
			line += item.Path + " - "
		}
		line += item.Reason
		if _, err := fmt.Fprintln(w, line); err != nil {
			return err
		}
	}
	return nil
}

func (r *Renderer) issueHeading(severity string) string {
	heading := strings.ToUpper(severity[:1]) + severity[1:] + "s:"
	if !r.colorEnabled {
		return heading
	}
	switch severity {
	case "error":
		return r.styles.Error.Render(heading)
	case "warning", "warn":
		return r.styles.Warning.Render(heading)
	default:
		return r.styles.Label.Render(heading)
	}
}

func issueSeverityOrder(seen []string) []string {
	preferred := []string{"error", "warning", "warn", "issue"}
	known := map[string]bool{}
	var ordered []string
	for _, severity := range preferred {
		for _, item := range seen {
			if item == severity && !known[item] {
				ordered = append(ordered, item)
				known[item] = true
			}
		}
	}
	for _, item := range seen {
		if !known[item] {
			ordered = append(ordered, item)
			known[item] = true
		}
	}
	return ordered
}

func issueLine(issue factile.ValidationIssue) string {
	var parts []string
	if issue.Path != "" {
		parts = append(parts, issue.Path)
	}
	if issue.ConceptID != "" {
		parts = append(parts, issue.ConceptID)
	}
	if issue.Code != "" {
		parts = append(parts, "["+issue.Code+"]")
	}
	if issue.Message != "" {
		parts = append(parts, issue.Message)
	}
	return strings.Join(parts, " ")
}

func oneLine(value string) string {
	return strings.Join(strings.Fields(value), " ")
}
