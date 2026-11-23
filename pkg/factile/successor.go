package factile

import searchpkg "github.com/factile/factile/pkg/search"

func currentScopeIndex(concepts []scopedConcept) map[string]int {
	byPath := make(map[string]int, len(concepts))
	for index, item := range concepts {
		path := item.Concept.Path
		if _, exists := byPath[path]; exists {
			byPath[path] = -1
		} else {
			byPath[path] = index
		}
	}
	return byPath
}

func visibleCurrent(index int, at string, concepts []scopedConcept, byPath map[string]int) (int, bool) {
	visited := map[int]bool{}
	for !visited[index] {
		visited[index] = true
		concept := concepts[index].Concept
		switch conceptReview(concept, at).Status {
		case "stable":
			return index, true
		case "deprecated":
			target, exists := byPath[concept.Successor]
			if !exists || target < 0 || conceptMount(concept) != conceptMount(concepts[target].Concept) {
				return 0, false
			}
			index = target
		default:
			return 0, false
		}
	}
	return 0, false
}

func currentScores(scored []searchpkg.Scored, concepts []scopedConcept, at string) ([]searchpkg.Scored, []OmittedItem) {
	byPath := currentScopeIndex(concepts)
	seen := map[int]bool{}
	out := make([]searchpkg.Scored, 0, len(scored))
	var omitted []OmittedItem
	for _, item := range scored {
		current, ok := visibleCurrent(item.Index, at, concepts, byPath)
		if !ok {
			original := concepts[item.Index].Concept
			if original.portable && conceptReview(original, at).Status == "deprecated" {
				omitted = append(omitted, OmittedItem{Path: original.Path, Reason: "replacement_unavailable"})
			}
			continue
		}
		if seen[current] {
			continue
		}
		seen[current] = true
		if current != item.Index {
			item.Index = current
			item.Path = concepts[current].Concept.Path
			item.Snippet = ""
			item.Passage = concepts[current].Concept.Markdown
		}
		out = append(out, item)
	}
	return out, omitted
}
