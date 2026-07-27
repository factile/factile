package search

import (
	"sort"
	"strings"
	"unicode/utf8"
)

const snippetWindowBytes = 180

type markdownPassage struct {
	start        int
	end          int
	headingStart int
	headingLevel int
	heading      string
}

func (p markdownPassage) text(body string) string {
	return body[p.start:p.end]
}

type markdownLine struct {
	start           int
	end             int
	headingLevel    int
	heading         string
	structuralBlank bool
}

func markdownPassages(body string) []markdownPassage {
	lines, hasHeadings := scanMarkdownLines(body)
	if len(lines) == 0 {
		return nil
	}

	var passages []markdownPassage
	if hasHeadings {
		start := 0
		var heading markdownLine
		for _, line := range lines {
			if line.headingLevel == 0 {
				continue
			}
			passages = appendPassage(passages, body, start, line.start, heading)
			start = line.start
			heading = line
		}
		return appendPassage(passages, body, start, len(body), heading)
	}

	start := 0
	for _, line := range lines {
		if !line.structuralBlank {
			continue
		}
		passages = appendPassage(passages, body, start, line.start, markdownLine{})
		start = line.end
	}
	return appendPassage(passages, body, start, len(body), markdownLine{})
}

func appendPassage(passages []markdownPassage, body string, start, end int, heading markdownLine) []markdownPassage {
	if start >= end || strings.TrimSpace(body[start:end]) == "" {
		return passages
	}
	passage := markdownPassage{
		start:        start,
		end:          end,
		headingLevel: heading.headingLevel,
		heading:      heading.heading,
	}
	if heading.headingLevel > 0 {
		passage.headingStart = heading.start
	}
	return append(passages, passage)
}

func scanMarkdownLines(body string) ([]markdownLine, bool) {
	var lines []markdownLine
	var fence byte
	var fenceWidth int
	hasHeadings := false

	for start := 0; start < len(body); {
		contentEnd := strings.IndexByte(body[start:], '\n')
		end := len(body)
		if contentEnd >= 0 {
			contentEnd += start
			end = contentEnd + 1
		} else {
			contentEnd = len(body)
		}
		lineText := strings.TrimSuffix(body[start:contentEnd], "\r")
		line := markdownLine{start: start, end: end}

		marker, width, isFence := markdownFence(lineText)
		switch {
		case fence != 0:
			if isFence && marker == fence && width >= fenceWidth && fenceRemainderIsBlank(lineText, width) {
				fence = 0
				fenceWidth = 0
			}
		case isFence:
			fence = marker
			fenceWidth = width
		default:
			line.structuralBlank = strings.TrimSpace(lineText) == ""
			line.headingLevel, line.heading = markdownHeading(lineText)
			hasHeadings = hasHeadings || line.headingLevel > 0
		}

		lines = append(lines, line)
		start = end
	}
	return lines, hasHeadings
}

func markdownFence(line string) (byte, int, bool) {
	trimmed := strings.TrimLeft(line, " \t")
	if len(trimmed) < 3 || trimmed[0] != '`' && trimmed[0] != '~' {
		return 0, 0, false
	}
	marker := trimmed[0]
	width := 1
	for width < len(trimmed) && trimmed[width] == marker {
		width++
	}
	return marker, width, width >= 3
}

func fenceRemainderIsBlank(line string, width int) bool {
	trimmed := strings.TrimLeft(line, " \t")
	return width <= len(trimmed) && strings.TrimSpace(trimmed[width:]) == ""
}

func markdownHeading(line string) (int, string) {
	trimmed := strings.TrimLeft(line, " \t")
	level := 0
	for level < len(trimmed) && trimmed[level] == '#' {
		level++
	}
	if level == 0 || level > 6 {
		return 0, ""
	}
	if level < len(trimmed) && trimmed[level] != ' ' && trimmed[level] != '\t' {
		return 0, ""
	}
	return level, strings.TrimSpace(trimmed[level:])
}

type snippetMatch struct {
	term      int
	runeStart int
	runeEnd   int
	byteStart int
	byteEnd   int
}

func passageSnippet(body string, passage markdownPassage, terms []string) string {
	text := passage.text(body)
	focus, ok := strongestSnippetMatch(text, terms)
	if !ok {
		return ""
	}
	focus.byteStart += passage.start
	focus.byteEnd += passage.start

	start := focus.byteStart - 60
	if start < passage.start {
		start = passage.start
	}
	end := focus.byteStart + 120
	if end > passage.end {
		end = passage.end
	}
	if passage.headingLevel > 0 &&
		passage.headingStart < start &&
		focus.byteEnd-passage.headingStart <= snippetWindowBytes {
		start = passage.headingStart
		end = start + snippetWindowBytes
		if end > passage.end {
			end = passage.end
		}
	}
	start = nextRuneBoundary(body, start, end)
	end = previousRuneBoundary(body, start, end)

	snippet := body[start:end]
	snippet = strings.ReplaceAll(snippet, "\r\n", " ")
	snippet = strings.ReplaceAll(snippet, "\n", " ")
	snippet = strings.ReplaceAll(snippet, "\r", " ")
	return strings.TrimSpace(snippet)
}

func strongestSnippetMatch(text string, terms []string) (snippetMatch, bool) {
	matcher := newTextMatcher(text)
	if len(terms) > 1 {
		phrase := strings.Join(terms, " ")
		var exact snippetMatch
		found := false
		matcher.eachMatch(phrase, allPlainTerms(terms), func(runeStart, runeEnd, byteStart, byteEnd int) bool {
			exact = snippetMatch{
				runeStart: runeStart,
				runeEnd:   runeEnd,
				byteStart: byteStart,
				byteEnd:   byteEnd,
			}
			found = true
			return false
		})
		if found {
			return exact, true
		}
	}

	var matches []snippetMatch
	availableTerms := map[int]bool{}
	for termIndex, term := range terms {
		if term == "" {
			continue
		}
		matcher.eachMatch(term, isPlainTerm(term), func(runeStart, runeEnd, byteStart, byteEnd int) bool {
			matches = append(matches, snippetMatch{
				term:      termIndex,
				runeStart: runeStart,
				runeEnd:   runeEnd,
				byteStart: byteStart,
				byteEnd:   byteEnd,
			})
			availableTerms[termIndex] = true
			return true
		})
	}
	if len(matches) == 0 {
		return snippetMatch{}, false
	}
	sort.SliceStable(matches, func(i, j int) bool {
		if matches[i].runeStart == matches[j].runeStart {
			return matches[i].runeEnd < matches[j].runeEnd
		}
		return matches[i].runeStart < matches[j].runeStart
	})

	wantTerms := len(availableTerms)
	var best snippetMatch
	bestSet := false
	for left := range matches {
		seen := map[int]bool{}
		endRune := matches[left].runeEnd
		endByte := matches[left].byteEnd
		for right := left; right < len(matches); right++ {
			match := matches[right]
			seen[match.term] = true
			if match.runeEnd > endRune {
				endRune = match.runeEnd
				endByte = match.byteEnd
			}
			if len(seen) != wantTerms {
				continue
			}
			candidate := snippetMatch{
				runeStart: matches[left].runeStart,
				runeEnd:   endRune,
				byteStart: matches[left].byteStart,
				byteEnd:   endByte,
			}
			if !bestSet ||
				candidate.runeEnd-candidate.runeStart < best.runeEnd-best.runeStart ||
				candidate.runeEnd-candidate.runeStart == best.runeEnd-best.runeStart && candidate.runeStart < best.runeStart {
				best = candidate
				bestSet = true
			}
			break
		}
	}
	return best, bestSet
}

func nextRuneBoundary(text string, start, end int) int {
	for start < end && !utf8.RuneStart(text[start]) {
		start++
	}
	return start
}

func previousRuneBoundary(text string, start, end int) int {
	for end > start && end < len(text) && !utf8.RuneStart(text[end]) {
		end--
	}
	return end
}
