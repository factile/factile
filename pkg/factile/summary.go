package factile

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/factile/factile/pkg/gitsource"
	"github.com/factile/factile/pkg/version"
	"github.com/factile/factile/pkg/vfs"
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
		for index := range result.Sources {
			source := &result.Sources[index]
			if source.Kind != vfs.SourceKindLocal || source.Revision == "" {
				continue
			}
			checkout := localCheckoutStatus(ctx, *source)
			source.LocalCheckout = &checkout
			if checkout.PinMatches != nil && !*checkout.PinMatches {
				health = append(health, HealthSummary{Status: "warning", Message: source.MountPath + " checkout differs from the declared revision."})
			}
		}
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

func localCheckoutStatus(ctx context.Context, source vfs.Mount) vfs.LocalCheckoutStatus {
	git := gitsource.NewRunner()
	git.Timeout = 3 * time.Second
	if source.SourcePath == "" {
		return vfs.LocalCheckoutStatus{Reason: "Local source unavailable"}
	}
	head, err := git.Run(ctx, source.SourcePath, "rev-parse", "--verify", "HEAD")
	if err != nil {
		return vfs.LocalCheckoutStatus{Reason: "Git checkout revision unavailable"}
	}
	revision := strings.TrimSpace(string(head))
	matches := strings.EqualFold(revision, source.Revision)
	result := vfs.LocalCheckoutStatus{ObservedRevision: revision, PinMatches: &matches}
	status, err := git.Run(ctx, source.SourcePath, "--no-optional-locks", "status", "--porcelain=v1", "-z",
		"--untracked-files=all", "--", ".")
	if err != nil {
		result.Reason = "Mounted directory status unavailable"
		return result
	}
	dirty := len(status) > 0
	result.Dirty = &dirty
	return result
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
