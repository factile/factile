package graph

import (
	"path"
	"regexp"
	"strings"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/text"
)

var markdownLinkRE = regexp.MustCompile(`(?m)\[[^\]\n]+\]\([ \t]*(?:<([^>\n]+)>|((?:\\.|[^\s()\\]|\([^()\s]*\))+))(?:[ \t]+(?:"[^"\n]*"|'[^'\n]*'|\([^()\n]*\)))?[ \t]*\)|^ {0,3}\[[^\]\n]+\]:[ \t]*(?:<([^>\n]+)>|([^\s]+))`)

type Link struct {
	Raw    string
	Target string
	Kind   string
}

type MarkdownTarget struct {
	Link
	Start, End int
}

func ExtractMarkdownLinks(markdown string) []Link {
	targets := MarkdownTargets(markdown)
	links := make([]Link, 0, len(targets))
	for _, target := range targets {
		links = append(links, target.Link)
	}
	return links
}

// MarkdownTargets finds the editable part of local Markdown link destinations.
// Offsets exclude fragments, query values, titles and angle brackets.
func MarkdownTargets(markdown string) []MarkdownTarget {
	matches := markdownLinkRE.FindAllStringSubmatchIndex(markdown, -1)
	if len(matches) == 0 {
		return nil
	}
	inCode := codeRanges([]byte(markdown))
	var targets []MarkdownTarget
	for _, match := range matches {
		if containsOffset(inCode, match[0]) || escapedAt(markdown, match[0]) {
			continue
		}
		start, end := -1, -1
		for i := 2; i < len(match); i += 2 {
			if match[i] != -1 {
				start, end = match[i], match[i+1]
				break
			}
		}
		if start < 0 {
			continue
		}
		raw := markdown[start:end]
		if strings.HasPrefix(raw, "//") || strings.HasPrefix(raw, "http://") || strings.HasPrefix(raw, "https://") || strings.HasPrefix(raw, "mailto:") {
			continue
		}
		if suffix := strings.IndexAny(raw, "#?"); suffix >= 0 {
			end = start + suffix
		}
		if end > start {
			targets = append(targets, MarkdownTarget{Link: Link{Raw: raw, Target: markdown[start:end]}, Start: start, End: end})
		}
	}
	return targets
}

func codeRanges(markdown []byte) []text.Segment {
	root := goldmark.DefaultParser().Parse(text.NewReader(markdown))
	var ranges []text.Segment
	_ = ast.Walk(root, func(node ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering {
			return ast.WalkContinue, nil
		}
		switch node := node.(type) {
		case *ast.CodeSpan:
			for child := node.FirstChild(); child != nil; child = child.NextSibling() {
				if value, ok := child.(*ast.Text); ok {
					ranges = append(ranges, value.Segment)
				}
			}
		case *ast.CodeBlock, *ast.FencedCodeBlock:
			lines := node.Lines()
			for i := 0; i < lines.Len(); i++ {
				ranges = append(ranges, lines.At(i))
			}
		}
		return ast.WalkContinue, nil
	})
	return ranges
}

func containsOffset(ranges []text.Segment, offset int) bool {
	for _, segment := range ranges {
		if segment.Start <= offset && offset < segment.Stop {
			return true
		}
	}
	return false
}

func escapedAt(markdown string, offset int) bool {
	count := 0
	for i := offset - 1; i >= 0 && markdown[i] == '\\'; i-- {
		count++
	}
	return count%2 != 0
}

func ResolveLink(fromConceptPath, target string) (string, bool) {
	if target == "" || strings.HasPrefix(target, "factile://") {
		return "", false
	}
	if strings.HasPrefix(target, "/") {
		clean := path.Clean(target)
		return strings.TrimSuffix(clean, ".md"), true
	}
	base := path.Dir(fromConceptPath)
	resolved := path.Clean(path.Join(base, target))
	if !strings.HasPrefix(resolved, "/") {
		resolved = "/" + resolved
	}
	return strings.TrimSuffix(resolved, ".md"), true
}
