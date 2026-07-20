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
	title := "Factile workspace is ready"
	if initChanged(result) {
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
		{label: "Ignore", value: annotateAction(filePath(result.Files, ".gitignore"), fileAction(result.Files, ".gitignore"))},
		{label: "Workspace manifest", value: annotateAction(filePath(result.Files, "factile.toml"), fileAction(result.Files, "factile.toml"))},
	}
	if bundleManifest != "factile.toml" {
		rows = append(rows, row{label: "Bundle manifest", value: annotateAction(filePath(result.Files, bundleManifest), fileAction(result.Files, bundleManifest))})
	}
	rows = append(rows,
		row{label: "Index", value: annotateAction(filePath(result.Files, "index.md"), fileAction(result.Files, "index.md"))},
		row{label: "Overview", value: annotateAction(filePath(result.Files, "overview.md"), fileAction(result.Files, "overview.md"))},
		row{label: "Agent guidance", value: agentSummary(result.Agents)},
	)
	if err := r.renderRows(w, rows); err != nil {
		return err
	}
	_, err := fmt.Fprint(w, "\nNext:\n  factile list /\n  factile read /overview\n  factile context / \"what should I know?\"\n")
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
