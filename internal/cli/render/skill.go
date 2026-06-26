package render

import (
	"fmt"
	"io"
	"strings"

	"github.com/factile/factile/pkg/skill"
)

func (r *Renderer) RenderSkillList(w io.Writer, result skill.ListResult) error {
	if len(result.Skills) == 0 {
		_, err := fmt.Fprintln(w, "No skills.")
		return err
	}
	if _, err := fmt.Fprintln(w, "Agent skills:"); err != nil {
		return err
	}
	for _, item := range result.Skills {
		line := summaryLine(item.Target+"/"+item.Name, item.Summary, "")
		if _, err := fmt.Fprintln(w, "  "+line); err != nil {
			return err
		}
	}
	return nil
}

func (r *Renderer) RenderSkillInspect(w io.Writer, result skill.InspectResult) error {
	if _, err := fmt.Fprintln(w, r.path("Skill "+result.Target+"/"+result.Name)); err != nil {
		return err
	}
	metadata := map[string]any{
		"summary":     result.Summary,
		"description": result.Description,
	}
	if rendered := r.RenderMetadata(metadata); rendered != "" {
		if _, err := fmt.Fprintln(w, rendered); err != nil {
			return err
		}
	}
	if len(result.Files) > 0 {
		if _, err := fmt.Fprintln(w, "\nFiles:"); err != nil {
			return err
		}
		for _, filename := range result.Files {
			if _, err := fmt.Fprintln(w, "  "+filename); err != nil {
				return err
			}
		}
	}
	if _, err := fmt.Fprintln(w, "\nGenerated content:"); err != nil {
		return err
	}
	for _, line := range []string{
		"  SKILL.md",
		"  AGENTS.md managed block",
		"  .codex/config.toml MCP block",
	} {
		if _, err := fmt.Fprintln(w, line); err != nil {
			return err
		}
	}
	return nil
}

func (r *Renderer) RenderSkillInstall(w io.Writer, result skill.InstallResult) error {
	if _, err := fmt.Fprintf(w, "Installed %s skill (%s)\n", result.Target, result.Scope); err != nil {
		return err
	}
	metadata := map[string]any{
		"mode":    result.Mode,
		"profile": result.Profile,
		"message": result.Message,
	}
	if rendered := r.RenderMetadata(metadata); rendered != "" {
		if _, err := fmt.Fprintln(w, rendered); err != nil {
			return err
		}
	}
	return renderFileChanges(w, result.Files)
}

func (r *Renderer) RenderSkillUninstall(w io.Writer, result skill.UninstallResult) error {
	if _, err := fmt.Fprintf(w, "Uninstalled %s skill (%s)\n", result.Target, result.Scope); err != nil {
		return err
	}
	if result.Message != "" {
		if _, err := fmt.Fprintln(w, "Message: "+result.Message); err != nil {
			return err
		}
	}
	return renderFileChanges(w, result.Files)
}

func (r *Renderer) RenderSkillDoctor(w io.Writer, result skill.DoctorResult) error {
	status := "OK"
	if !result.OK {
		status = "Needs attention"
	}
	if _, err := fmt.Fprintf(w, "Skill doctor %s: %s\n", result.Target, status); err != nil {
		return err
	}
	if len(result.Checks) == 0 {
		return nil
	}
	if _, err := fmt.Fprintln(w, "\nChecks:"); err != nil {
		return err
	}
	for _, check := range result.Checks {
		status := strings.ToUpper(check.Status)
		line := fmt.Sprintf("  %s %s", status, check.Name)
		if check.Message != "" {
			line += " - " + check.Message
		}
		if _, err := fmt.Fprintln(w, line); err != nil {
			return err
		}
	}
	return nil
}

func renderFileChanges(w io.Writer, files []skill.FileChange) error {
	if len(files) == 0 {
		return nil
	}
	if _, err := fmt.Fprintln(w, "\nFiles:"); err != nil {
		return err
	}
	for _, file := range files {
		line := "  " + file.Path
		if file.Action != "" {
			line += " (" + file.Action + ")"
		}
		if _, err := fmt.Fprintln(w, line); err != nil {
			return err
		}
	}
	return nil
}
