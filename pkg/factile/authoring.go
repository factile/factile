package factile

import (
	"bytes"
	"maps"
	"reflect"
	"regexp"
	"strings"
	"time"

	"github.com/factile/factile/pkg/okf"
	"github.com/factile/factile/pkg/version"
	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/extension"
	render "github.com/yuin/goldmark/renderer/html"
	"github.com/yuin/goldmark/text"
	"golang.org/x/net/html"
)

// GeneratedMetadata identifies the tool performing the current content change.
// It never asserts a human author or a review.
func GeneratedMetadata(at time.Time) map[string]any {
	return map[string]any{"by": "factile/" + version.Current().Version, "at": at.UTC().Format(time.RFC3339Nano)}
}

func meaningfulContentChanged(before, after okf.Document, portable bool) bool {
	content := func(fields map[string]any) map[string]any {
		result := make(map[string]any, len(fields))
		for key, value := range fields {
			switch key {
			case "generated", "verified", "status", "stale_after", "superseded_by":
				continue
			}
			result[key] = value
		}
		return result
	}
	left, right := content(before.Frontmatter), content(after.Frontmatter)
	if !portable {
		return before.Markdown != after.Markdown || !reflect.DeepEqual(left, right)
	}
	canonicalizeFields(before.ConceptID, left)
	canonicalizeFields(after.ConceptID, right)
	return !reflect.DeepEqual(left, right) ||
		(before.Markdown != after.Markdown && markdownMeaning(before) != markdownMeaning(after))
}

func canonicalizeFields(id string, fields map[string]any) {
	normalize := func(object map[string]any, key, name string) {
		if value, ok := object[key].(string); ok {
			object[key] = okf.CanonicalReference(id, value, name)
		}
	}
	for _, key := range []string{"resource", "computation"} {
		normalize(fields, key, key)
	}
	for _, key := range []string{"executor", "attester"} {
		if section, ok := fields[key].(map[string]any); ok {
			section = maps.Clone(section)
			normalize(section, "resource", key+".resource")
			fields[key] = section
		}
	}
	if original, ok := fields["sources"]; ok {
		sources := okf.MetadataItems(original)
		normalized := make([]any, 0, len(sources))
		for _, raw := range sources {
			if source, ok := raw.(map[string]any); ok {
				source = maps.Clone(source)
				normalize(source, "resource", "sources[].resource")
				raw = source
			}
			normalized = append(normalized, raw)
		}
		fields["sources"] = normalized
	}
}

var markupWhitespace = regexp.MustCompile(`[ \t\r\n]+`)

func markdownMeaning(doc okf.Document) string {
	parser := goldmark.New(goldmark.WithExtensions(extension.GFM), goldmark.WithRendererOptions(render.WithUnsafe()))
	body := []byte(doc.Markdown)
	tree := parser.Parser().Parse(text.NewReader(body))
	_ = ast.Walk(tree, func(node ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering {
			return ast.WalkContinue, nil
		}
		switch value := node.(type) {
		case *ast.Link:
			value.Destination = []byte(okf.CanonicalReference(doc.ConceptID, string(value.Destination), "markdown"))
		case *ast.Image:
			value.Destination = []byte(okf.CanonicalReference(doc.ConceptID, string(value.Destination), "markdown"))
		}
		return ast.WalkContinue, nil
	})
	var rendered bytes.Buffer
	if err := parser.Renderer().Render(&rendered, body, tree); err != nil {
		return doc.Markdown
	}
	tokens := html.NewTokenizer(&rendered)
	var normalized strings.Builder
	literal := 0
	for {
		kind := tokens.Next()
		if kind == html.ErrorToken {
			return normalized.String()
		}
		if kind == html.StartTagToken || kind == html.EndTagToken {
			tag, _ := tokens.TagName()
			switch string(tag) {
			case "code", "pre", "script", "style":
				if kind == html.StartTagToken {
					literal++
				} else if literal > 0 {
					literal--
				}
			}
		}
		if kind == html.TextToken && literal == 0 {
			plain := tokens.Text()
			normalized.Write(markupWhitespace.ReplaceAll(plain, []byte(" ")))
		} else {
			normalized.Write(tokens.Raw())
		}
	}
}
