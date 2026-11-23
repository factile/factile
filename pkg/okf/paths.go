package okf

import (
	"net/url"
	"path"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/text"
)

type PathReference struct {
	Field string
	Value string
}

var pathSuffix = regexp.MustCompile(`\.[A-Za-z0-9]+$`)

func PathReferences(fields map[string]any, markdown string) []PathReference {
	var references []PathReference
	add := func(field string, value any) {
		if text, ok := value.(string); ok && strings.TrimSpace(text) != "" {
			references = append(references, PathReference{Field: field, Value: text})
		}
	}
	for _, key := range []string{"resource", "computation", "superseded_by"} {
		add(key, fields[key])
	}
	for _, key := range []string{"executor", "attester"} {
		if object, ok := fields[key].(map[string]any); ok {
			add(key+".resource", object["resource"])
		}
	}
	for _, entry := range MetadataItems(fields["sources"]) {
		if object, ok := entry.(map[string]any); ok {
			add("sources[].resource", object["resource"])
		}
	}
	doc := goldmark.DefaultParser().Parse(text.NewReader([]byte(markdown)))
	_ = ast.Walk(doc, func(node ast.Node, entering bool) (ast.WalkStatus, error) {
		if link, ok := node.(*ast.Link); ok && entering {
			add("markdown", string(link.Destination))
		}
		return ast.WalkContinue, nil
	})
	return references
}

// ResolvePath resolves actual file paths. lookup classifies entries within one physical root.
func ResolvePath(id, value, field, version string, lookup func(string) string) (kind, rel string) {
	if value == "" {
		return "invalid", ""
	}
	if resourceScheme.MatchString(value) || strings.HasPrefix(value, "//") {
		return "external", ""
	}
	if field == "sources[].resource" && strings.ContainsFunc(value, unicode.IsSpace) &&
		!strings.ContainsAny(value, "/\\#?%") && !pathSuffix.MatchString(value) {
		return "scope", ""
	}
	if strings.ContainsAny(value, "\\") || strings.ContainsFunc(value, func(r rune) bool {
		return unicode.IsSpace(r) || unicode.IsControl(r)
	}) {
		return "invalid", ""
	}
	if _, err := url.PathUnescape(value); err != nil {
		return "invalid", ""
	}
	raw := value
	if end := strings.IndexAny(raw, "?#"); end >= 0 {
		raw = raw[:end]
	}
	if raw == "" {
		if field == "markdown" {
			return "local-fragment", ""
		}
		return "invalid", ""
	}
	parts := strings.Split(raw, "/")
	for i, part := range parts {
		decoded, err := url.PathUnescape(part)
		if err != nil || !utf8.ValidString(decoded) || strings.ContainsAny(decoded, "/\\") ||
			strings.ContainsFunc(decoded, unicode.IsControl) {
			return "invalid", ""
		}
		parts[i] = decoded
	}
	resolve := func(base string) (string, string) {
		var relative []string
		if base != "." && base != "" {
			relative = strings.Split(base, "/")
		}
		for _, part := range parts {
			switch part {
			case "", ".":
				continue
			case "..":
				if len(relative) == 0 {
					return "outside", ""
				}
				relative = relative[:len(relative)-1]
			default:
				relative = append(relative, part)
				if strings.HasPrefix(part, ".") || lookup(strings.Join(relative, "/")) == "outside" {
					return "outside", ""
				}
			}
		}
		rel := strings.Join(relative, "/")
		kind := lookup(rel)
		if strings.HasSuffix(raw, "/") && kind != "directory" {
			return "missing", rel
		}
		return kind, rel
	}
	if strings.HasPrefix(raw, "/") {
		return resolve("")
	}
	kind, rel = resolve(path.Dir(id))
	if kind == "missing" && field != "markdown" && (version == "" || version == "0.1" || version == "0.2") {
		return resolve("")
	}
	return kind, rel
}

type ComputationDeclaration struct {
	Parameters []map[string]any `json:"parameters"`
	BodyKind   string           `json:"body_kind"`
	Attestable bool             `json:"attestable"`
}

func DescribeComputation(fields map[string]any, body string, selected string) ComputationDeclaration {
	result := ComputationDeclaration{Parameters: []map[string]any{}, BodyKind: "missing"}
	for _, raw := range MetadataItems(fields["parameters"]) {
		entry, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		name, _ := entry["name"].(string)
		kind, _ := entry["type"].(string)
		required, hasRequired := entry["required"]
		_, boolean := required.(bool)
		if strings.TrimSpace(name) == "" || strings.TrimSpace(kind) == "" || hasRequired && !boolean {
			continue
		}
		safe := map[string]any{"required": false}
		for key, value := range entry {
			safe[key] = value
		}
		result.Parameters = append(result.Parameters, safe)
	}
	if !EmptyMetadata(fields["computation"]) {
		if selected == "asset" || selected == "concept" || selected == "file" {
			result.BodyKind = "file"
		}
	} else {
		input := []byte(body)
		doc := goldmark.DefaultParser().Parse(text.NewReader(input))
		var level int
		_ = ast.Walk(doc, func(node ast.Node, entering bool) (ast.WalkStatus, error) {
			if !entering {
				return ast.WalkContinue, nil
			}
			switch n := node.(type) {
			case *ast.Heading:
				if level != 0 && n.Level <= level {
					return ast.WalkStop, nil
				}
				if level == 0 && string(n.Text(input)) == "Computation" && n.Lines().Len() > 0 {
					line := n.Lines().At(0)
					start := line.Start
					for start > 0 && input[start-1] != '\n' {
						start--
					}
					if strings.HasPrefix(strings.TrimSpace(string(input[start:line.Start])), "#") {
						level = n.Level
					}
				}
			case *ast.FencedCodeBlock:
				if level != 0 {
					result.BodyKind = "inline"
					return ast.WalkStop, nil
				}
			}
			return ast.WalkContinue, nil
		})
	}
	attester, _ := fields["attester"].(map[string]any)
	runtime, _ := fields["runtime"].(string)
	resource, _ := attester["resource"].(string)
	result.Attestable = result.BodyKind != "missing" && strings.TrimSpace(runtime) != "" && strings.TrimSpace(resource) != ""
	return result
}

// CanonicalReference compares explicit paths without changing their authored spelling.
func CanonicalReference(id, value, field string) string {
	kind, rel := ResolvePath(id, value, field, "0.3", func(string) string { return "file" })
	if kind != "file" && kind != "missing" {
		return value
	}
	raw, suffix := value, ""
	if index := strings.IndexAny(value, "?#"); index >= 0 {
		raw, suffix = value[:index], value[index:]
	}
	if strings.HasSuffix(raw, "/") && rel != "" {
		rel += "/"
	}
	return "/" + rel + suffix
}
