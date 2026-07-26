package search

import (
	"sort"
	"strings"
)

type Fields struct {
	Path        string
	ConceptID   string
	Title       string
	Description string
	Tags        []string
	Resource    string
	Body        string
}

type Scored struct {
	Index   int
	Score   float64
	Snippet string
	Path    string
}

var questionScaffolding = map[string]bool{
	"how": true, "what": true, "where": true, "when": true, "why": true,
	"who": true, "which": true, "do": true, "does": true, "did": true,
	"is": true, "are": true, "was": true, "were": true, "can": true,
	"could": true, "should": true, "would": true,
}

func Score(query string, fields []Fields) []Scored {
	terms := queryTerms(query)
	if len(terms) == 0 {
		return nil
	}
	phrase := strings.Join(terms, " ")
	var scored []Scored
	for i, item := range fields {
		score := 0.0
		score += scoreText(strings.ToLower(item.Title), terms, 12)
		score += scoreText(strings.ToLower(strings.Join(item.Tags, " ")), terms, 10)
		score += scoreText(strings.ToLower(item.Description), terms, 6)
		score += scoreText(strings.ToLower(item.ConceptID), terms, 5)
		score += scoreText(strings.ToLower(item.Resource), terms, 4)
		score += scoreText(strings.ToLower(item.Body), terms, 1)
		if strings.Contains(strings.ToLower(item.Title), phrase) {
			score += 10
		}
		if score > 0 {
			scored = append(scored, Scored{
				Index:   i,
				Score:   score,
				Snippet: Snippet(item.Body, terms),
				Path:    item.Path,
			})
		}
	}
	sort.Slice(scored, func(i, j int) bool {
		if scored[i].Score == scored[j].Score {
			return scored[i].Path < scored[j].Path
		}
		return scored[i].Score > scored[j].Score
	})
	return scored
}

func queryTerms(query string) []string {
	cleaned := make([]string, 0)
	seen := map[string]bool{}
	for _, term := range strings.Fields(strings.ToLower(query)) {
		term = strings.Trim(term, ",;!?\"'`()[]{}<>")
		if term == "" || seen[term] {
			continue
		}
		seen[term] = true
		cleaned = append(cleaned, term)
	}
	substantive := make([]string, 0, len(cleaned))
	for _, term := range cleaned {
		if !questionScaffolding[term] {
			substantive = append(substantive, term)
		}
	}
	if len(substantive) == 0 {
		return cleaned
	}
	return substantive
}

func scoreText(text string, terms []string, weight float64) float64 {
	score := 0.0
	for _, term := range terms {
		if term == "" {
			continue
		}
		count := strings.Count(text, term)
		if count > 0 {
			score += weight + float64(count-1)
		}
	}
	return score
}

func Snippet(body string, terms []string) string {
	lower := strings.ToLower(body)
	for _, term := range terms {
		if term == "" {
			continue
		}
		idx := strings.Index(lower, term)
		if idx < 0 {
			continue
		}
		start := idx - 60
		if start < 0 {
			start = 0
		}
		end := idx + 120
		if end > len(body) {
			end = len(body)
		}
		return strings.TrimSpace(strings.ReplaceAll(body[start:end], "\n", " "))
	}
	return ""
}
