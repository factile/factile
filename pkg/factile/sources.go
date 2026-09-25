package factile

import (
	"strings"

	graphpkg "github.com/factile/factile/pkg/graph"
	"github.com/factile/factile/pkg/okf"
	"github.com/factile/factile/pkg/storage"
	"github.com/factile/factile/pkg/vfs"
)

func conceptMount(concept Concept) string {
	return strings.TrimSuffix(strings.TrimSuffix(concept.Path, concept.ConceptID), "/")
}

func conceptSources(concept Concept, mount vfs.Mount) Concept {
	if okf.IsReservedFile(concept.ConceptID + ".md") {
		return concept
	}
	store, err := storage.NewLocal(mount.SourcePath)
	refs, claims, diagnostics := okf.SourceReferences(concept.ConceptID, concept.Frontmatter, concept.Markdown, func(target string) bool {
		if err != nil {
			return false
		}
		rel := strings.TrimPrefix(target, "/")
		return store.ResourceExists(rel+".md") || store.ResourceExists(rel)
	})
	for i := range refs {
		if refs[i].Target != "" {
			refs[i].Target = cleanVirtualJoin(mount.MountPath, refs[i].Target)
		}
	}
	concept.SourceReferences = refs
	concept.ClaimReferences = claims
	concept.MetadataDiagnostics = append(okf.MetadataDiagnostics(concept.Frontmatter), diagnostics...)
	return concept
}

func conceptLinks(item scopedConcept) []graphpkg.Link {
	links := graphpkg.ExtractMarkdownLinks(item.Concept.Markdown)
	sources, _ := item.Concept.Frontmatter["sources"].([]any)
	for _, raw := range sources {
		source, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		resource, ok := source["resource"].(string)
		if !ok {
			continue
		}
		kind, target := okf.SourceTarget(item.Concept.ConceptID, resource)
		if kind == "internal" && target != "" {
			links = append(links, graphpkg.Link{Raw: resource, Target: cleanVirtualJoin(conceptMount(item.Concept), target), Kind: "source_reference"})
		}
	}
	return links
}

func linkKind(link graphpkg.Link) string {
	if link.Kind == "source_reference" {
		return link.Kind
	}
	return "markdown_link"
}

func linkIssue(item scopedConcept, link graphpkg.Link) ValidationIssue {
	code, message := "broken_link", "Broken Markdown link: "+link.Target
	if link.Kind == "source_reference" {
		code, message = "broken_source", "Unresolved internal source: "+link.Raw
	}
	issue := ValidationIssue{Severity: "warning", Code: code, Message: message, Path: item.Concept.Path, ConceptID: item.Concept.ConceptID}
	if link.Kind == "source_reference" {
		issue.Details = map[string]any{"field": "sources"}
	}
	return issue
}

func missingLink(item scopedConcept, link graphpkg.Link, target string, known map[string]bool) bool {
	if link.Kind == "source_reference" {
		for _, reference := range item.Concept.SourceReferences {
			if reference.Target == target && reference.Target != "" {
				return false
			}
		}
		return true
	}
	return !known[target]
}

func sourceTargetVisible(item, target scopedConcept, link graphpkg.Link) bool {
	return link.Kind != "source_reference" || conceptMount(item.Concept) == conceptMount(target.Concept)
}

// Authored resource strings stay intact; derived target paths respect the view.
func restrictSourceTargets(concept Concept, paths []string) Concept {
	concept.SourceReferences = append([]okf.SourceReference(nil), concept.SourceReferences...)
	for i := range concept.SourceReferences {
		target := concept.SourceReferences[i].Target
		if target != "" && !pathInAnyScope(target, paths) {
			concept.SourceReferences[i].Target = ""
		}
	}
	diagnostics := []okf.Diagnostic{}
	for _, diagnostic := range concept.MetadataDiagnostics {
		if diagnostic.Code != "broken_source" {
			diagnostics = append(diagnostics, diagnostic)
		}
	}
	concept.MetadataDiagnostics = diagnostics
	return concept
}

func contextOrigin(mount vfs.Mount) map[string]string {
	bundlePath := mount.MountPath
	if bundlePath == "" {
		bundlePath = "/"
	}
	origin := map[string]string{"bundle_path": bundlePath, "kind": mount.Kind}
	if mount.Kind == vfs.SourceKindGit {
		origin["source_uri"] = safeGitSource(mount.Source)
	}
	if mount.Ref != "" {
		origin["ref"] = mount.Ref
	}
	if mount.Revision != "" {
		origin["revision"] = mount.Revision
	}
	if mount.SourceStatus != nil && mount.SourceStatus.SelectedRevision != "" {
		origin["revision"] = mount.SourceStatus.SelectedRevision
	}
	return origin
}
