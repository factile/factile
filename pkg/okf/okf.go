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
	text := string(data)
	frontmatter, body, err := splitFrontmatter(text)
	if err != nil && IsReservedFile(NormalizeConceptID(conceptID)+".md") && strings.TrimRight(strings.SplitN(text, "\n", 2)[0], "\r") != "---" {
		return Document{ConceptID: NormalizeConceptID(conceptID), Frontmatter: map[string]any{}, Markdown: text}, nil
	}
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

func Serialize(doc Document) []byte {
	body := strings.ReplaceAll(doc.Markdown, "\r\n", "\n")
	var b strings.Builder
	b.WriteString("---\n")
	for _, key := range orderedKeys(doc.Frontmatter, doc.Order) {
		writeFrontmatterEntry(&b, key, doc.Frontmatter[key], 0)
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
	case nil:
		return "null"
	case []string:
		items := make([]string, len(v))
		for i, item := range v {
			items[i] = FormatValue(item)
		}
		return "[" + strings.Join(items, ", ") + "]"
	case []any:
		items := make([]string, 0, len(v))
		for _, item := range v {
			items = append(items, FormatValue(item))
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
		scalar, err := coreScalar(v)
		if _, isString := scalar.(string); err == nil && isString && isPlainScalar(v) {
			return v
		}
		return strconv.Quote(v)
	default:
		return fmt.Sprint(v)
	}
}

func indentation(line string) int {
	count := 0
	for _, r := range line {
		if r == ' ' {
			count++
			continue
		}
		if r == '\t' {
			count += 2
			continue
		}
		break
	}
	return count
}

func writeFrontmatterEntry(b *strings.Builder, key string, value any, indent int) {
	prefix := strings.Repeat(" ", indent)
	switch v := value.(type) {
	case map[string]any:
		if len(v) == 0 {
			b.WriteString(prefix + key + ": {}\n")
			return
		}
		b.WriteString(prefix + FormatValue(key) + ":\n")
		for _, child := range orderedKeys(v, nil) {
			writeFrontmatterEntry(b, child, v[child], indent+2)
		}
	case []any:
		if scalarList(v) {
			b.WriteString(prefix + FormatValue(key) + ": " + FormatValue(v) + "\n")
			return
		}
		b.WriteString(prefix + FormatValue(key) + ":\n")
		writeList(b, v, indent+2)
	default:
		b.WriteString(prefix + FormatValue(key) + ": " + FormatValue(value) + "\n")
	}
}

func writeList(b *strings.Builder, values []any, indent int) {
	prefix := strings.Repeat(" ", indent)
	for _, value := range values {
		switch v := value.(type) {
		case map[string]any:
			if len(v) == 0 {
				b.WriteString(prefix + "- {}\n")
				continue
			}
			b.WriteString(prefix + "-\n")
			for _, key := range orderedKeys(v, nil) {
				writeFrontmatterEntry(b, key, v[key], indent+2)
			}
		case []any:
			if len(v) == 0 {
				b.WriteString(prefix + "- []\n")
				continue
			}
			b.WriteString(prefix + "-\n")
			writeList(b, v, indent+2)
		default:
			b.WriteString(prefix + "- " + FormatValue(value) + "\n")
		}
	}
}

func scalarList(values []any) bool {
	for _, value := range values {
		switch value.(type) {
		case map[string]any, []any:
			return false
		}
	}
	return true
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
	if strings.ContainsAny(value, "\n#{}[],&*?|-<>=!%@`\"'") {
		return false
	}
	if strings.Contains(value, ": ") || strings.HasSuffix(value, ":") {
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
