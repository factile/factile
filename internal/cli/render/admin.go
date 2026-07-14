package render

import (
	"fmt"
	"io"
	"strings"

	"github.com/factile/factile/pkg/factile"
)

func (r *Renderer) RenderConceptConfirmation(w io.Writer, verb string, result factile.ConceptResult) error {
	if _, err := fmt.Fprintf(w, "%s %s\n", verb, result.Concept.Path); err != nil {
		return err
	}
	if result.Concept.Revision != "" {
		_, err := fmt.Fprintln(w, "Rev: "+result.Concept.Revision)
		return err
	}
	return nil
}

func (r *Renderer) RenderMkdir(w io.Writer, result factile.DirectoryResult) error {
	if _, err := fmt.Fprintln(w, "Created directory "+result.Directory.Path); err != nil {
		return err
	}
	if len(result.Directory.Files) == 0 {
		return nil
	}
	if _, err := fmt.Fprintln(w, "Files:"); err != nil {
		return err
	}
	for _, file := range result.Directory.Files {
		if _, err := fmt.Fprintln(w, "  "+file); err != nil {
			return err
		}
	}
	return nil
}

func (r *Renderer) RenderRename(w io.Writer, result factile.RenameResult) error {
	if _, err := fmt.Fprintln(w, "Renamed "+result.Concept.Path); err != nil {
		return err
	}
	if result.Concept.Revision != "" {
		if _, err := fmt.Fprintln(w, "Rev: "+result.Concept.Revision); err != nil {
			return err
		}
	}
	if len(result.Warnings) > 0 {
		return r.renderIssues(w, result.Warnings)
	}
	return nil
}

func (r *Renderer) RenderDelete(w io.Writer, result factile.DeleteResult) error {
	if result.Deleted {
		_, err := fmt.Fprintln(w, "Deleted "+result.Path)
		return err
	}
	_, err := fmt.Fprintln(w, "Not deleted "+result.Path)
	return err
}

func (r *Renderer) RenderViewList(w io.Writer, result factile.ViewListResult) error {
	if len(result.Views) == 0 {
		_, err := fmt.Fprintln(w, "No Views.")
		return err
	}
	if _, err := fmt.Fprintln(w, "Views:"); err != nil {
		return err
	}
	for _, view := range result.Views {
		if _, err := fmt.Fprintln(w, "  "+summaryLine(view.ID, view.Title, view.Description)); err != nil {
			return err
		}
	}
	return nil
}

func (r *Renderer) RenderView(w io.Writer, result factile.ViewResult) error {
	view := result.View
	if result.Action != "" {
		if _, err := fmt.Fprintln(w, actionTitle(result.Action)+" View "+view.ID); err != nil {
			return err
		}
	} else if _, err := fmt.Fprintln(w, "View "+view.ID); err != nil {
		return err
	}
	metadata := map[string]any{
		"title":       view.Title,
		"description": view.Description,
		"status":      view.Status,
	}
	if rendered := r.RenderMetadata(metadata); rendered != "" {
		if _, err := fmt.Fprintln(w, rendered); err != nil {
			return err
		}
	}
	if len(view.Paths) == 0 {
		return nil
	}
	if _, err := fmt.Fprintln(w, "\nPaths:"); err != nil {
		return err
	}
	for _, path := range view.Paths {
		if _, err := fmt.Fprintln(w, "  "+path); err != nil {
			return err
		}
	}
	return nil
}

func (r *Renderer) RenderViewDelete(w io.Writer, result factile.ViewDeleteResult) error {
	if result.Deleted {
		_, err := fmt.Fprintln(w, "Deleted View "+result.ID)
		return err
	}
	_, err := fmt.Fprintln(w, "View not deleted "+result.ID)
	return err
}

func (r *Renderer) RenderMount(w io.Writer, result factile.MountResult) error {
	capability := "read-only"
	if result.Mount.Writable {
		capability = "writable"
	}
	_, err := fmt.Fprintf(w, "Mounted %s at %s (%s)\n", result.Mount.Source, result.Mount.MountPath, capability)
	return err
}

func (r *Renderer) RenderUnmount(w io.Writer, result factile.UnmountResult) error {
	if result.Removed {
		_, err := fmt.Fprintln(w, "Unmounted "+result.MountPath)
		return err
	}
	_, err := fmt.Fprintln(w, "Mount not found "+result.MountPath)
	return err
}

func (r *Renderer) RenderMountList(w io.Writer, result factile.MountListResult) error {
	if len(result.Mounts) == 0 {
		_, err := fmt.Fprintln(w, "No mounts configured.")
		return err
	}
	if _, err := fmt.Fprintln(w, "Mounts:"); err != nil {
		return err
	}
	for _, mount := range result.Mounts {
		line := fmt.Sprintf("  %s -> %s", mount.MountPath, mount.Source)
		if mount.Kind != "" {
			line += " (" + mount.Kind + ")"
		}
		if mount.Writable {
			line += " writable"
		} else {
			line += " read-only"
		}
		line += renderSourceStatus(mount.SourceStatus)
		if _, err := fmt.Fprintln(w, line); err != nil {
			return err
		}
	}
	return nil
}

func (r *Renderer) RenderRefresh(w io.Writer, result factile.RefreshResult) error {
	line := fmt.Sprintf("Refresh %s: %s", result.MountPath, result.Outcome)
	if result.Status.SelectedRevision != "" {
		line += " (" + result.Status.SelectedRevision + ")"
	}
	if _, err := fmt.Fprintln(w, line); err != nil {
		return err
	}
	if result.Warning != nil {
		_, err := fmt.Fprintln(w, "Warning: "+result.Warning.Message)
		return err
	}
	return nil
}

func renderSourceStatus(status *factile.SourceStatus) string {
	if status == nil {
		return ""
	}
	state := " unavailable"
	if status.SnapshotAvailable {
		state = " fresh"
	}
	if status.RefreshDue {
		state = " refresh-due"
	}
	if status.Stale {
		state = " stale"
	}
	if status.SelectedRevision != "" {
		state += " revision=" + status.SelectedRevision
	}
	if status.LastErrorCode != "" {
		state += " error=" + status.LastErrorCode
	}
	return state
}

func (r *Renderer) RenderBundleInspect(w io.Writer, result factile.BundleInspectResult) error {
	if _, err := fmt.Fprintln(w, "Bundle "+result.Source); err != nil {
		return err
	}
	metadata := map[string]any{
		"kind":          result.Kind,
		"plausible_okf": result.PlausibleOKF,
	}
	if rendered := r.RenderMetadata(metadata); rendered != "" {
		if _, err := fmt.Fprintln(w, rendered); err != nil {
			return err
		}
	}
	if len(result.Concepts) > 0 {
		if err := r.renderConceptSummaries(w, result.Concepts); err != nil {
			return err
		}
	}
	if len(result.Issues) > 0 {
		return r.renderIssues(w, result.Issues)
	}
	return nil
}

func (r *Renderer) RenderBundleFind(w io.Writer, result factile.BundleFindResult) error {
	if len(result.Sources) == 0 {
		_, err := fmt.Fprintln(w, "No OKF bundles found under "+result.StartPath)
		return err
	}
	if _, err := fmt.Fprintln(w, "OKF bundles under "+result.StartPath+":"); err != nil {
		return err
	}
	for _, source := range result.Sources {
		if _, err := fmt.Fprintln(w, "  "+source); err != nil {
			return err
		}
	}
	return nil
}

func actionTitle(action string) string {
	switch action {
	case "created":
		return "Created"
	case "updated":
		return "Updated"
	case "unchanged":
		return "Ready"
	case "linked":
		return "Linked"
	default:
		if action == "" {
			return "Updated"
		}
		return strings.ToUpper(action[:1]) + action[1:]
	}
}
