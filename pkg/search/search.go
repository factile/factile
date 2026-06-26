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

func Score(query string, fields []Fields) []Scored {
	terms := strings.Fields(strings.ToLower(query))
	if len(terms) == 0 {
		return nil
	}
	var scored []Scored
	for i, item := range fields {
		score := 0.0
		score += scoreText(strings.ToLower(item.Title), terms, 12)
		score += scoreText(strings.ToLower(strings.Join(item.Tags, " ")), terms, 10)
		score += scoreText(strings.ToLower(item.Description), terms, 6)
		score += scoreText(strings.ToLower(item.ConceptID), terms, 5)
		score += scoreText(strings.ToLower(item.Resource), terms, 4)
		score += scoreText(strings.ToLower(item.Body), terms, 1)
		if strings.Contains(strings.ToLower(item.Title), strings.ToLower(query)) {
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
