package search

import (
	"sort"
	"strings"
	"unicode"
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

const (
	maxTermOccurrences   = 3
	secondaryPhraseBonus = 5
	headingEvidenceBonus = 2
)

var questionScaffolding = map[string]bool{
	"how": true, "what": true, "where": true, "when": true, "why": true,
	"who": true, "which": true, "do": true, "does": true, "did": true,
	"is": true, "are": true, "was": true, "were": true, "can": true,
	"could": true, "should": true, "would": true,
}

var commonProseTerms = map[string]bool{
	"a": true, "an": true, "the": true, "and": true, "or": true,
	"as": true, "at": true, "by": true, "for": true, "from": true,
	"in": true, "into": true, "of": true, "on": true, "to": true,
	"with": true,
}

func Score(query string, fields []Fields) []Scored {
	terms := queryTerms(query)
	if len(terms) == 0 {
		return nil
	}
	phrase := strings.Join(terms, " ")
	boundedPhrase := allPlainTerms(terms)
	var scored []Scored
	for i, item := range fields {
		metadataMatches := make(map[string]bool, len(terms))
		metadataScore := 0.0
		metadataScore += scoreText(item.Title, terms, 12, metadataMatches)
		metadataScore += scoreText(strings.Join(item.Tags, " "), terms, 10, metadataMatches)
		metadataScore += scoreText(item.Description, terms, 6, metadataMatches)
		metadataScore += scoreText(item.ConceptID, terms, 5, metadataMatches)
		metadataScore += scoreText(item.Resource, terms, 4, metadataMatches)
		if newTextMatcher(item.Title).find(phrase, boundedPhrase).Count > 0 {
			metadataScore += 10
		}
		if len(terms) > 1 {
			if newTextMatcher(item.Description).find(phrase, boundedPhrase).Count > 0 {
				metadataScore += secondaryPhraseBonus
			}
		}

		bestScore := coveredScore(metadataScore, len(metadataMatches), len(terms))
		bestMatchedTerms := len(metadataMatches)
		bestPassage := -1
		passages := markdownPassages(item.Body)
		for passageIndex, passage := range passages {
			matchedTerms := cloneTermMatches(metadataMatches)
			passageText := passage.text(item.Body)
			passageScore := metadataScore + scoreText(passageText, terms, 1, matchedTerms)
			if passage.headingLevel >= 2 {
				passageScore += scoreHeadingEvidence(passage.heading, terms)
			}
			if len(terms) > 1 && newTextMatcher(passageText).find(phrase, boundedPhrase).Count > 0 {
				passageScore += secondaryPhraseBonus
			}
			passageScore = coveredScore(passageScore, len(matchedTerms), len(terms))
			if passageScore > bestScore ||
				passageScore == bestScore && len(matchedTerms) > bestMatchedTerms {
				bestScore = passageScore
				bestMatchedTerms = len(matchedTerms)
				bestPassage = passageIndex
			}
		}

		if bestScore > 0 {
			snippet := ""
			if bestPassage >= 0 {
				snippet = passageSnippet(item.Body, passages[bestPassage], terms)
			}
			scored = append(scored, Scored{
				Index:   i,
				Score:   bestScore,
				Snippet: snippet,
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

func coveredScore(score float64, matchedTerms, totalTerms int) float64 {
	return score * float64(matchedTerms) / float64(totalTerms)
}

func cloneTermMatches(matches map[string]bool) map[string]bool {
	cloned := make(map[string]bool, len(matches))
	for term := range matches {
		cloned[term] = true
	}
	return cloned
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
		if !questionScaffolding[term] && !commonProseTerms[term] {
			substantive = append(substantive, term)
		}
	}
	if len(substantive) == 0 {
		return cleaned
	}
	return substantive
}

func scoreText(text string, terms []string, weight float64, matchedTerms map[string]bool) float64 {
	matcher := newTextMatcher(text)
	score := 0.0
	for _, term := range terms {
		if term == "" {
			continue
		}
		count := matcher.find(term, isPlainTerm(term)).Count
		if count > 0 {
			matchedTerms[term] = true
			if count > maxTermOccurrences {
				count = maxTermOccurrences
			}
			score += weight + float64(count-1)
		}
	}
	return score
}

func scoreHeadingEvidence(heading string, terms []string) float64 {
	matcher := newTextMatcher(heading)
	score := 0.0
	for _, term := range terms {
		if term != "" && matcher.find(term, isPlainTerm(term)).Count > 0 {
			score += headingEvidenceBonus
		}
	}
	return score
}

func Snippet(body string, terms []string) string {
	if len(terms) == 0 {
		return ""
	}
	phrase := strings.Join(terms, " ")
	boundedPhrase := allPlainTerms(terms)
	passages := markdownPassages(body)
	bestScore := 0.0
	bestMatchedTerms := 0
	bestPassage := -1
	for passageIndex, passage := range passages {
		matchedTerms := make(map[string]bool, len(terms))
		passageText := passage.text(body)
		score := scoreText(passageText, terms, 1, matchedTerms)
		if passage.headingLevel >= 2 {
			score += scoreHeadingEvidence(passage.heading, terms)
		}
		if len(terms) > 1 && newTextMatcher(passageText).find(phrase, boundedPhrase).Count > 0 {
			score += secondaryPhraseBonus
		}
		score = coveredScore(score, len(matchedTerms), len(terms))
		if score > bestScore || score == bestScore && len(matchedTerms) > bestMatchedTerms {
			bestScore = score
			bestMatchedTerms = len(matchedTerms)
			bestPassage = passageIndex
		}
	}
	if bestPassage >= 0 {
		return passageSnippet(body, passages[bestPassage], terms)
	}
	return ""
}

type textMatch struct {
	Count int
	First int
}

type textMatcher struct {
	runes       []rune
	byteOffsets []int
}

func newTextMatcher(text string) textMatcher {
	matcher := textMatcher{
		runes:       make([]rune, 0, len(text)),
		byteOffsets: make([]int, 0, len(text)+1),
	}
	for offset, r := range text {
		matcher.runes = append(matcher.runes, r)
		matcher.byteOffsets = append(matcher.byteOffsets, offset)
	}
	matcher.byteOffsets = append(matcher.byteOffsets, len(text))
	return matcher
}

func (m textMatcher) find(term string, bounded bool) textMatch {
	match := textMatch{First: -1}
	m.eachMatch(term, bounded, func(_, _ int, byteStart, _ int) bool {
		if match.First < 0 {
			match.First = byteStart
		}
		match.Count++
		return true
	})
	return match
}

func (m textMatcher) eachMatch(term string, bounded bool, visit func(runeStart, runeEnd, byteStart, byteEnd int) bool) {
	pattern := []rune(term)
	if len(pattern) == 0 {
		return
	}
	for i := 0; i+len(pattern) <= len(m.runes); {
		end := i + len(pattern)
		if bounded && (i > 0 && isWordRune(m.runes[i-1]) || end < len(m.runes) && isWordRune(m.runes[end])) {
			i++
			continue
		}
		equal := true
		for j, want := range pattern {
			if !equalFoldRune(m.runes[i+j], want) {
				equal = false
				break
			}
		}
		if !equal {
			i++
			continue
		}
		if !visit(i, end, m.byteOffsets[i], m.byteOffsets[end]) {
			return
		}
		i = end
	}
}

func equalFoldRune(a, b rune) bool {
	if a == b {
		return true
	}
	for folded := unicode.SimpleFold(a); folded != a; folded = unicode.SimpleFold(folded) {
		if folded == b {
			return true
		}
	}
	return false
}

func isPlainTerm(term string) bool {
	if term == "" {
		return false
	}
	for _, r := range term {
		if !isWordRune(r) {
			return false
		}
	}
	return true
}

func allPlainTerms(terms []string) bool {
	for _, term := range terms {
		if !isPlainTerm(term) {
			return false
		}
	}
	return len(terms) > 0
}

func isWordRune(r rune) bool {
	return unicode.IsLetter(r) || unicode.IsDigit(r)
}
