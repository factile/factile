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

func (r *Renderer) RenderInit(w io.Writer, result bootstrap.Result, workspace string) error {
	title := "Factile workspace is ready"
	if !result.Health.OK {
		title = "Factile workspace needs attention"
	} else if initChanged(result) {
		title = "Initialized Factile workspace"
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
	rootBundle := defaultText(result.RootBundlePath, ".")
	bundleManifest := "factile.toml"
	if rootBundle != "." {
		bundleManifest = strings.TrimSuffix(rootBundle, "/") + "/factile.toml"
	}
	rows := []row{
		{label: "Workspace", value: defaultText(result.WorkspacePath, ".")},
		{label: "Root bundle", value: rootBundle},
		{label: "Bundle name", value: result.Bundle.Name},
		{label: "Title", value: result.Bundle.Title},
		{label: "Description", value: result.Bundle.Description},
		{label: "Ignore", value: annotateAction(filePath(result.Files, ".gitignore"), fileAction(result.Files, ".gitignore"))},
		{label: "Workspace manifest", value: annotateAction(filePath(result.Files, "factile.toml"), fileAction(result.Files, "factile.toml"))},
	}
	if bundleManifest != "factile.toml" {
		rows = append(rows, row{label: "Bundle manifest", value: annotateAction(filePath(result.Files, bundleManifest), fileAction(result.Files, bundleManifest))})
	}
	rows = append(rows,
		row{label: "Index", value: annotateAction(filePath(result.Files, "index.md"), fileAction(result.Files, "index.md"))},
		row{label: "Overview", value: annotateAction(filePath(result.Files, "overview.md"), fileAction(result.Files, "overview.md"))},
		row{label: "Agent guidance", value: agentSummary(result.AgentSelection, result.Agents)},
		row{label: "Health", value: healthSummary(result.Health)},
	)
	if err := r.renderRows(w, rows); err != nil {
		return err
	}
	if len(result.Health.Checks) > 0 {
		if _, err := fmt.Fprintln(w, "\nHealth checks:"); err != nil {
			return err
		}
		for _, check := range result.Health.Checks {
			if _, err := fmt.Fprintf(w, "  [%s] %s: %s\n", check.Status, check.Name, check.Message); err != nil {
				return err
			}
		}
	}
	command := "factile"
	if workspace != "" {
		command += " --workspace " + shellQuote(workspace)
	}
	_, err := fmt.Fprintf(w, "\nNext:\n  %s list /\n  %s read /overview\n  %s context / \"what should I know?\"\n", command, command, command)
	return err
}

func shellQuote(value string) string {
	if value != "" && strings.IndexFunc(value, func(r rune) bool {
		return !strings.ContainsRune("abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789_@%+=:,./-", r)
	}) == -1 {
		return value
	}
	return "'" + strings.ReplaceAll(value, "'", "'\"'\"'") + "'"
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

func agentSummary(selection string, agents []bootstrap.AgentResult) string {
	if len(agents) == 0 {
		if selection == bootstrap.AgentNone {
			return "skipped"
		}
		return "none detected"
	}
	parts := make([]string, 0, len(agents))
	for _, agent := range agents {
		name := displayAgent(agent.Agent)
		mode := defaultText(agent.Mode, "reader")
		name += " " + mode + " mode"
		if agent.Profile != "" {
			name += ", " + agent.Profile + " profile"
		}
		status := agentInstallStatus(agent)
		if agent.Detected {
			status = "detected, " + status
		}
		parts = append(parts, fmt.Sprintf("%s (%s)", name, status))
	}
	return strings.Join(parts, "; ")
}

func agentInstallStatus(agent bootstrap.AgentResult) string {
	if len(agent.Files) == 0 {
		return "ready"
	}
	changed := false
	upgraded := false
	for _, file := range agent.Files {
		switch file.Action {
		case "created":
			changed = true
		case "updated", "removed":
			changed = true
			upgraded = true
		}
	}
	if !changed {
		return "already installed"
	}
	if upgraded {
		return "upgraded"
	}
	return "installed"
}

func healthSummary(health bootstrap.HealthResult) string {
	switch health.Status {
	case "warning":
		return "healthy with warnings"
	case "failed":
		return "failed"
	case "healthy":
		return "healthy"
	default:
		if health.OK {
			return "healthy"
		}
		return "failed"
	}
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
