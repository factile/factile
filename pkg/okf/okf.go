package okf

import (
	"errors"
	"fmt"
	"path"
	"sort"
	"strconv"
	"strings"
)

var (
	ErrMissingFrontmatter = errors.New("missing frontmatter")
	ErrInvalidFrontmatter = errors.New("invalid frontmatter")
)

type Document struct {
	ConceptID   string
	Frontmatter map[string]any
	Order       []string
	Markdown    string
}

func IsReservedFile(name string) bool {
	base := path.Base(strings.TrimSpace(name))
	return base == "index.md" || base == "log.md"
}

func ConceptIDFromRel(rel string) (string, bool) {
	rel = path.Clean(strings.ReplaceAll(rel, "\\", "/"))
	if rel == "." || strings.HasPrefix(rel, "../") || strings.Contains(rel, "/../") {
		return "", false
	}
	if !strings.HasSuffix(rel, ".md") || IsReservedFile(rel) {
		return "", false
	}
	id := strings.TrimSuffix(rel, ".md")
	if id == "" || id == "." {
		return "", false
	}
	return id, true
}

func RelFromConceptID(id string) (string, error) {
	id = NormalizeConceptID(id)
	if id == "" {
		return "", fmt.Errorf("empty concept id")
	}
	for _, part := range strings.Split(id, "/") {
		if part == "" || part == "." || part == ".." {
			return "", fmt.Errorf("unsafe concept id: %s", id)
		}
	}
	return id + ".md", nil
}

func NormalizeConceptID(id string) string {
	id = strings.TrimSpace(strings.ReplaceAll(id, "\\", "/"))
	id = strings.TrimPrefix(id, "/")
	id = path.Clean(id)
	if id == "." {
		return ""
	}
	id = strings.TrimSuffix(id, ".md")
	return id
}

func ParseConcept(conceptID string, data []byte) (Document, error) {
	text := strings.ReplaceAll(string(data), "\r\n", "\n")
	frontmatter, body, err := splitFrontmatter(text)
	if err != nil {
		return Document{}, err
	}
	values, order, err := ParseFrontmatter(frontmatter)
	if err != nil {
		return Document{}, err
	}
	return Document{
		ConceptID:   NormalizeConceptID(conceptID),
		Frontmatter: values,
		Order:       order,
		Markdown:    body,
	}, nil
}

func splitFrontmatter(text string) (string, string, error) {
	lines := strings.SplitAfter(text, "\n")
	if len(lines) == 0 || strings.TrimRight(lines[0], "\r\n") != "---" {
		return "", "", ErrMissingFrontmatter
	}
	offset := len(lines[0])
	for i := 1; i < len(lines); i++ {
		line := lines[i]
		if strings.TrimRight(line, "\r\n") == "---" {
			start := offset
			end := offset + len(line)
			return text[len(lines[0]):start], text[end:], nil
		}
		offset += len(line)
	}
	return "", "", ErrMissingFrontmatter
}

func ParseFrontmatter(text string) (map[string]any, []string, error) {
	values := map[string]any{}
	var order []string
	for i, raw := range strings.Split(text, "\n") {
		line := strings.TrimSpace(raw)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if strings.HasPrefix(raw, " ") || strings.HasPrefix(raw, "\t") {
			return nil, nil, fmt.Errorf("%w: nested YAML is not supported on line %d", ErrInvalidFrontmatter, i+1)
		}
		parts := strings.SplitN(line, ":", 2)
		if len(parts) != 2 {
			return nil, nil, fmt.Errorf("%w: expected key-value pair on line %d", ErrInvalidFrontmatter, i+1)
		}
		key := strings.TrimSpace(parts[0])
		if key == "" {
			return nil, nil, fmt.Errorf("%w: empty key on line %d", ErrInvalidFrontmatter, i+1)
		}
		if _, exists := values[key]; !exists {
			order = append(order, key)
		}
		value, err := ParseValue(strings.TrimSpace(parts[1]))
		if err != nil {
			return nil, nil, fmt.Errorf("%w: %v on line %d", ErrInvalidFrontmatter, err, i+1)
		}
		values[key] = value
	}
	return values, order, nil
}

func ParseValue(text string) (any, error) {
	text = strings.TrimSpace(text)
	if text == "" {
		return "", nil
	}
	if strings.HasPrefix(text, "[") || strings.HasSuffix(text, "]") {
		if !strings.HasPrefix(text, "[") || !strings.HasSuffix(text, "]") {
			return nil, fmt.Errorf("malformed list")
		}
		inside := strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(text, "["), "]"))
		if inside == "" {
			return []string{}, nil
		}
		parts := strings.Split(inside, ",")
		values := make([]string, 0, len(parts))
		for _, part := range parts {
			item := strings.TrimSpace(part)
			item = strings.Trim(item, `"'`)
			if item != "" {
				values = append(values, item)
			}
		}
		return values, nil
	}
	if (strings.HasPrefix(text, `"`) && strings.HasSuffix(text, `"`)) || (strings.HasPrefix(text, `'`) && strings.HasSuffix(text, `'`)) {
		unquoted, err := strconv.Unquote(text)
		if err == nil {
			return unquoted, nil
		}
		return strings.Trim(text, `"'`), nil
	}
	if text == "true" {
		return true, nil
	}
	if text == "false" {
		return false, nil
	}
	return text, nil
}

func Serialize(doc Document) []byte {
	body := strings.ReplaceAll(doc.Markdown, "\r\n", "\n")
	var b strings.Builder
	b.WriteString("---\n")
	for _, key := range orderedKeys(doc.Frontmatter, doc.Order) {
		b.WriteString(key)
		b.WriteString(": ")
		b.WriteString(FormatValue(doc.Frontmatter[key]))
		b.WriteString("\n")
	}
	b.WriteString("---\n")
	if body != "" && !strings.HasPrefix(body, "\n") {
		b.WriteString("\n")
	}
	b.WriteString(body)
	return []byte(b.String())
}

func FormatValue(value any) string {
	switch v := value.(type) {
	case []string:
		return "[" + strings.Join(v, ", ") + "]"
	case []any:
		items := make([]string, 0, len(v))
		for _, item := range v {
			items = append(items, strings.Trim(FormatValue(item), `"`))
		}
		return "[" + strings.Join(items, ", ") + "]"
	case bool:
		if v {
			return "true"
		}
		return "false"
	case string:
		if v == "" {
			return `""`
		}
		if isPlainScalar(v) {
			return v
		}
		return strconv.Quote(v)
	default:
		return fmt.Sprint(v)
	}
}

func orderedKeys(values map[string]any, order []string) []string {
	seen := map[string]bool{}
	keys := make([]string, 0, len(values))
	for _, key := range order {
		if _, ok := values[key]; ok && !seen[key] {
			keys = append(keys, key)
			seen[key] = true
		}
	}
	var rest []string
	for key := range values {
		if !seen[key] {
			rest = append(rest, key)
		}
	}
	sort.Strings(rest)
	return append(keys, rest...)
}

func isPlainScalar(value string) bool {
	if strings.TrimSpace(value) != value {
		return false
	}
	if strings.ContainsAny(value, "\n#{}[],&*?|-<>=!%@`") {
		return false
	}
	if strings.Contains(value, ": ") {
		return false
	}
	return true
}

func StringField(values map[string]any, key string) string {
	v, ok := values[key]
	if !ok {
		return ""
	}
	switch typed := v.(type) {
	case string:
		return typed
	default:
		return fmt.Sprint(typed)
	}
}

func StringSliceField(values map[string]any, key string) []string {
	v, ok := values[key]
	if !ok {
		return nil
	}
	switch typed := v.(type) {
	case []string:
		return append([]string(nil), typed...)
	case []any:
		out := make([]string, 0, len(typed))
		for _, item := range typed {
			out = append(out, fmt.Sprint(item))
		}
		return out
	case string:
		if typed == "" {
			return nil
		}
		return []string{typed}
	default:
		return []string{fmt.Sprint(typed)}
	}
}
