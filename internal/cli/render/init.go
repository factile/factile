package render

import (
	"fmt"
	"io"
	"strings"

	"github.com/factile/factile/pkg/bootstrap"
)

type row struct {
	label string
	value string
}

func (r *Renderer) RenderInit(w io.Writer, result bootstrap.Result) error {
	title := "Factile knowledge is ready"
	if initChanged(result) {
		title = "Initialized Factile knowledge"
	}
	if r.colorEnabled {
		title = r.styles.Heading.Render(title)
	}
	if _, err := fmt.Fprintln(w, title); err != nil {
		return err
	}
	if _, err := fmt.Fprintln(w); err != nil {
		return err
	}
	rows := []row{
		{label: "Knowledge Base", value: defaultText(result.MountPath, "/project")},
		{label: "Bundle", value: annotateAction(defaultText(result.BundlePath, ".factile/knowledge"), bundleAction(result))},
		{label: "Library", value: annotateAction(filePath(result.Files, ".factile/library.toml"), fileAction(result.Files, ".factile/library.toml"))},
		{label: "Catalog", value: annotateAction(filePath(result.Files, ".factile/knowledge-bases/project.toml"), fileAction(result.Files, ".factile/knowledge-bases/project.toml"))},
		{label: "Mounts", value: annotateAction(".factile/mounts.toml", result.Mount.Action)},
		{label: "Agent guidance", value: agentSummary(result.Agents)},
	}
	if err := r.renderRows(w, rows); err != nil {
		return err
	}
	_, err := fmt.Fprintf(w, "\nNext:\n  factile list /\n  factile read %s/overview\n  factile context %s \"what should I know?\"\n", defaultText(result.MountPath, "/project"), defaultText(result.MountPath, "/project"))
	return err
}

func (r *Renderer) renderRows(w io.Writer, rows []row) error {
	width := 0
	for _, row := range rows {
		if len(row.label) > width {
			width = len(row.label)
		}
	}
	for _, row := range rows {
		label := row.label + ":" + strings.Repeat(" ", width-len(row.label)+1)
		value := row.value
		if r.colorEnabled {
			label = r.styles.Label.Render(label)
			value = r.styles.Value.Render(value)
		}
		if _, err := fmt.Fprintln(w, label+value); err != nil {
			return err
		}
	}
	return nil
}

func initChanged(result bootstrap.Result) bool {
	if isChangingAction(result.Mount.Action) {
		return true
	}
	for _, file := range result.Files {
		if isChangingAction(file.Action) {
			return true
		}
	}
	for _, agent := range result.Agents {
		for _, file := range agent.Files {
			if isChangingAction(file.Action) {
				return true
			}
		}
	}
	return false
}

func bundleAction(result bootstrap.Result) string {
	prefix := strings.TrimSuffix(defaultText(result.BundlePath, ".factile/knowledge"), "/") + "/"
	changed := "unchanged"
	for _, file := range result.Files {
		if !strings.HasPrefix(file.Path, prefix) {
			continue
		}
		if file.Action == "created" {
			return "created"
		}
		if isChangingAction(file.Action) {
			changed = file.Action
		}
	}
	return changed
}

func fileAction(files []bootstrap.FileChange, suffix string) string {
	for _, file := range files {
		if file.Path == suffix || strings.HasSuffix(file.Path, "/"+suffix) {
			return file.Action
		}
	}
	return ""
}

func filePath(files []bootstrap.FileChange, fallback string) string {
	for _, file := range files {
		if file.Path == fallback || strings.HasSuffix(file.Path, "/"+fallback) {
			return file.Path
		}
	}
	return fallback
}

func annotateAction(value string, action string) string {
	switch action {
	case "created":
		return value + " (created)"
	case "updated":
		return value + " (updated)"
	case "unchanged":
		return value + " (reused)"
	default:
		if action == "" {
			return value
		}
		return value + " (" + action + ")"
	}
}

func agentSummary(agents []bootstrap.AgentResult) string {
	if len(agents) == 0 {
		return "none detected"
	}
	parts := make([]string, 0, len(agents))
	for _, agent := range agents {
		name := displayAgent(agent.Agent)
		status := agentInstallStatus(agent)
		if agent.Detected {
			status = "detected, " + status
		}
		parts = append(parts, fmt.Sprintf("%s reader mode (%s)", name, status))
	}
	return strings.Join(parts, "; ")
}

func agentInstallStatus(agent bootstrap.AgentResult) string {
	if len(agent.Files) == 0 {
		return "ready"
	}
	changed := false
	updated := false
	for _, file := range agent.Files {
		if file.Action == "created" {
			changed = true
		}
		if file.Action == "updated" {
			changed = true
			updated = true
		}
	}
	if !changed {
		return "already installed"
	}
	if updated {
		return "updated"
	}
	return "installed"
}

func displayAgent(agent string) string {
	if agent == "" {
		return "Agent"
	}
	return strings.ToUpper(agent[:1]) + agent[1:]
}

func defaultText(value string, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}

func isChangingAction(action string) bool {
	return action == "created" || action == "updated" || action == "removed"
}
