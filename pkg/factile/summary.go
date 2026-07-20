package factile

import (
	"context"
	"fmt"

	"github.com/factile/factile/pkg/version"
)

func (w *LocalWorkspace) Summary(ctx context.Context) (SummaryResult, error) {
	workspace, err := w.resolvedWorkspace()
	if err != nil {
		return SummaryResult{}, err
	}
	result := SummaryResult{
		Workspace: WorkspaceSummary{
			WorkspaceDir:  workspace.WorkspaceDir,
			RootBundleDir: workspace.RootBundleDir,
			StateDir:      workspace.StateDir,
			Version:       version.Current().Version,
		},
	}
	var health []HealthSummary
	if list, err := w.List(ctx, "/", ListOptions{Brief: true}); err != nil {
		health = append(health, HealthSummary{Status: "warning", Message: "Knowledge listing failed: " + err.Error()})
	} else {
		result.Knowledge = list.Cards
	}
	if views, err := w.ListViews(ctx); err != nil {
		health = append(health, HealthSummary{Status: "warning", Message: "View listing failed: " + err.Error()})
	} else {
		result.Views = views.Views
	}
	if sources, err := w.ListMounts(ctx); err != nil {
		health = append(health, HealthSummary{Status: "warning", Message: "Source listing failed: " + err.Error()})
	} else {
		result.Sources = sources.Mounts
	}
	if len(health) == 0 {
		health = append(health, HealthSummary{
			Status:  "ok",
			Message: fmt.Sprintf("%d knowledge entries, %d views, %d sources.", len(result.Knowledge), len(result.Views), len(result.Sources)),
		})
	}
	result.Health = health
	result.NextCommands = summaryNextCommands(result)
	return result, nil
}

func summaryNextCommands(result SummaryResult) []string {
	if len(result.Knowledge) == 0 && len(result.Sources) == 0 {
		return []string{
			"factile init",
			"factile mount <source> <path>",
			"factile --help",
		}
	}
	commands := []string{
		"factile list / --brief",
		"factile context / \"<task>\"",
	}
	if len(result.Views) > 0 {
		commands = append(commands,
			"factile view list",
			"factile context / \"<task>\" --view "+result.Views[0].ID,
		)
	}
	commands = append(commands, "factile --help")
	return commands
}
