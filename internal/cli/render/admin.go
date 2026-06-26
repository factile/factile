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

func (r *Renderer) RenderKnowledgeBaseList(w io.Writer, result factile.KnowledgeBaseListResult) error {
	if len(result.KnowledgeBases) == 0 {
		_, err := fmt.Fprintln(w, "No Knowledge Bases.")
		return err
	}
	if _, err := fmt.Fprintln(w, "Knowledge Bases:"); err != nil {
		return err
	}
	for _, kb := range result.KnowledgeBases {
		if _, err := fmt.Fprintln(w, "  "+summaryLine(kb.Path, kb.Title, kb.Description)); err != nil {
			return err
		}
	}
	return nil
}

func (r *Renderer) RenderKnowledgeBase(w io.Writer, result factile.KnowledgeBaseResult) error {
	kb := result.KnowledgeBase
	if result.Action != "" {
		if _, err := fmt.Fprintln(w, actionTitle(result.Action)+" Knowledge Base "+kb.Path); err != nil {
			return err
		}
	} else if _, err := fmt.Fprintln(w, "Knowledge Base "+kb.Path); err != nil {
		return err
	}
	metadata := map[string]any{
		"title":            kb.Title,
		"description":      kb.Description,
		"catalog":          result.Catalog,
		"audience":         kb.Audience,
		"profile":          kb.Profile,
		"default_trust":    kb.DefaultTrust,
		"default_writable": kb.DefaultWritable,
		"status":           kb.Status,
	}
	if rendered := r.RenderMetadata(metadata); rendered != "" {
		if _, err := fmt.Fprintln(w, rendered); err != nil {
			return err
		}
	}
	if len(kb.Bundles) > 0 {
		if _, err := fmt.Fprintln(w, "\nBundles:"); err != nil {
			return err
		}
		for _, bundle := range kb.Bundles {
			line := fmt.Sprintf("  %s -> %s", bundle.Path, bundle.Source)
			if bundle.Kind != "" {
				line += " (" + bundle.Kind + ")"
			}
			if bundle.Writable {
				line += " writable"
			} else {
				line += " read-only"
			}
			if bundle.Title != "" {
				line += " - " + bundle.Title
			}
			if _, err := fmt.Fprintln(w, line); err != nil {
				return err
			}
		}
	}
	if len(kb.Views) > 0 {
		if _, err := fmt.Fprintln(w, "\nViews:"); err != nil {
			return err
		}
		for _, view := range kb.Views {
			line := summaryLine(view.ID, view.Title, view.Description)
			if len(view.Bundles) > 0 {
				line += " [" + strings.Join(view.Bundles, ", ") + "]"
			}
			if _, err := fmt.Fprintln(w, "  "+line); err != nil {
				return err
			}
		}
	}
	return nil
}

func (r *Renderer) RenderBundleLink(w io.Writer, result factile.BundleLinkResult) error {
	action := actionTitle(defaultText(result.Action, "linked"))
	if _, err := fmt.Fprintf(w, "%s %s -> %s\n", action, result.Bundle.Path, result.Bundle.Source); err != nil {
		return err
	}
	_, err := fmt.Fprintln(w, "Knowledge Base: "+result.KnowledgeBase.Path)
	return err
}

func (r *Renderer) RenderBundleUnlink(w io.Writer, result factile.BundleUnlinkResult) error {
	if result.Removed {
		if _, err := fmt.Fprintln(w, "Unlinked "+result.BundlePath); err != nil {
			return err
		}
	} else if _, err := fmt.Fprintln(w, "Bundle not linked "+result.BundlePath); err != nil {
		return err
	}
	_, err := fmt.Fprintln(w, "Knowledge Base: "+result.KnowledgeBase.Path)
	return err
}

func (r *Renderer) RenderView(w io.Writer, result factile.ViewResult) error {
	action := actionTitle(defaultText(result.Action, "set"))
	if _, err := fmt.Fprintf(w, "%s View %s\n", action, result.View.ID); err != nil {
		return err
	}
	if _, err := fmt.Fprintln(w, "Knowledge Base: "+result.KnowledgeBase.Path); err != nil {
		return err
	}
	if len(result.View.Bundles) > 0 {
		_, err := fmt.Fprintln(w, "Bundles: "+strings.Join(result.View.Bundles, ", "))
		return err
	}
	return nil
}

func (r *Renderer) RenderViewDelete(w io.Writer, result factile.ViewDeleteResult) error {
	if result.Deleted {
		if _, err := fmt.Fprintln(w, "Deleted View "+result.ViewID); err != nil {
			return err
		}
	} else if _, err := fmt.Fprintln(w, "View not found "+result.ViewID); err != nil {
		return err
	}
	_, err := fmt.Fprintln(w, "Knowledge Base: "+result.KnowledgeBase.Path)
	return err
}

func (r *Renderer) RenderMount(w io.Writer, result factile.MountResult) error {
	_, err := fmt.Fprintf(w, "Mounted %s at %s\n", result.Mount.Source, result.Mount.MountPath)
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
		_, err := fmt.Fprintln(w, "No bundles mounted.")
		return err
	}
	if _, err := fmt.Fprintln(w, "Mounted bundles:"); err != nil {
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
		if _, err := fmt.Fprintln(w, line); err != nil {
			return err
		}
	}
	return nil
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
