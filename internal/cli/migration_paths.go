package cli

import (
	"maps"
	"net/url"
	"strings"

	"github.com/factile/factile/pkg/okf"
	"github.com/factile/factile/pkg/storage"
)

func migrateMetadataPaths(store storage.Local, doc okf.Document, set map[string]any, finding func(string, string, string, bool)) {
	value := func(key string) any {
		if updated, ok := set[key]; ok {
			return updated
		}
		return doc.Frontmatter[key]
	}
	rewrite := func(field string, input any) (string, bool) {
		ref, ok := input.(string)
		if !ok || strings.HasPrefix(ref, "/") {
			return "", false
		}
		localKind, local := okf.ResolvePath(doc.ConceptID, ref, field, "0.3", store.EntryKind)
		rootKind, rooted := okf.ResolvePath("index", ref, field, "0.3", store.EntryKind)
		rootExists := rootKind == "file" || rootKind == "directory"
		if !rootExists || local == rooted {
			return "", false
		}
		switch localKind {
		case "file", "directory":
			if !strings.HasPrefix(ref, "./") && !strings.HasPrefix(ref, "../") {
				finding(doc.ConceptID, "ambiguous_path", field+" "+ref+" resolves both to "+local+" and "+rooted+"; use an explicit relative or root path", true)
			}
		case "missing":
			result := (&url.URL{Path: "/" + rooted}).EscapedPath()
			raw := ref
			if index := strings.IndexAny(ref, "?#"); index >= 0 {
				raw = ref[:index]
				if strings.HasSuffix(raw, "/") && !strings.HasSuffix(result, "/") {
					result += "/"
				}
				result += ref[index:]
			} else if strings.HasSuffix(raw, "/") && !strings.HasSuffix(result, "/") {
				result += "/"
			}
			finding(doc.ConceptID, "root_path_preserved", field+": "+ref+" → "+result, false)
			return result, true
		}
		return "", false
	}
	for _, key := range []string{"resource", "computation", "superseded_by"} {
		if migrated, changed := rewrite(key, value(key)); changed {
			set[key] = migrated
		}
	}
	for _, key := range []string{"executor", "attester"} {
		if section, ok := value(key).(map[string]any); ok {
			if migrated, changed := rewrite(key+".resource", section["resource"]); changed {
				section = maps.Clone(section)
				section["resource"] = migrated
				set[key] = section
			}
		}
	}
	original := value("sources")
	sources := okf.MetadataItems(original)
	changed := false
	sources = append([]any{}, sources...)
	for index, raw := range sources {
		if source, ok := raw.(map[string]any); ok {
			if migrated, edited := rewrite("sources[].resource", source["resource"]); edited {
				source = maps.Clone(source)
				source["resource"] = migrated
				sources[index] = source
				changed = true
			}
		}
	}
	if changed {
		if _, singleton := original.(map[string]any); singleton {
			set["sources"] = sources[0]
		} else {
			set["sources"] = sources
		}
	}
}
