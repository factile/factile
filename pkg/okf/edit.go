package okf

import (
	"fmt"
	"reflect"
	"sort"
	"strings"

	"go.yaml.in/yaml/v4"
)

// ReplaceMarkdown preserves the original frontmatter and replaces only the body.
func ReplaceMarkdown(conceptID string, data []byte, markdown string) ([]byte, error) {
	doc, err := ParseConcept(conceptID, data)
	if err != nil {
		return nil, err
	}
	prefix := string(data[:len(data)-len(doc.Markdown)])
	if prefix != "" && markdown != "" && !strings.HasSuffix(prefix, "\n") {
		if strings.Contains(prefix, "\r\n") {
			prefix += "\r\n"
		} else {
			prefix += "\n"
		}
	}
	return []byte(prefix + markdown), nil
}

// PatchFrontmatter changes only named entries; unrelated lines retain their bytes.
func PatchFrontmatter(conceptID string, data []byte, set map[string]any, deleteKeys []string) ([]byte, error) {
	doc, err := ParseConcept(conceptID, data)
	if err != nil {
		return nil, err
	}
	if len(set) == 0 && len(deleteKeys) == 0 {
		return data, nil
	}
	for key := range set {
		if strings.TrimSpace(key) != key || key == "" || strings.ContainsAny(key, ":\r\n\t#") {
			return nil, fmt.Errorf("%w: invalid metadata key %q", ErrInvalidFrontmatter, key)
		}
	}
	deleted := map[string]bool{}
	for _, key := range deleteKeys {
		deleted[key] = true
	}
	prefix := string(data[:len(data)-len(doc.Markdown)])
	nl := "\n"
	if strings.Contains(prefix, "\r\n") || prefix == "" && strings.Contains(doc.Markdown, "\r\n") {
		nl = "\r\n"
	}
	entry := func(key string, value any) string {
		var b strings.Builder
		writeFrontmatterEntry(&b, key, value, 0)
		return strings.ReplaceAll(b.String(), "\n", nl)
	}
	if prefix == "" {
		var b strings.Builder
		for _, key := range orderedKeys(set, nil) {
			if !deleted[key] {
				b.WriteString(entry(key, set[key]))
			}
		}
		if b.Len() == 0 {
			return data, nil
		}
		return []byte("---" + nl + b.String() + "---" + nl + doc.Markdown), nil
	}
	lines := strings.SplitAfter(prefix, "\n")
	frontmatter, _, err := splitFrontmatter(string(data))
	if err != nil {
		return nil, err
	}
	node, err := frontmatterNode(frontmatter)
	if err != nil {
		return nil, err
	}
	if node != nil && node.Style&yaml.FlowStyle != 0 {
		return nil, fmt.Errorf("%w: metadata patches require a block mapping", ErrInvalidFrontmatter)
	}
	entryKeys := map[int]string{}
	blockScalars := map[int]bool{}
	if node != nil {
		remaining := maxProjectionNodes
		for i := 0; i < len(node.Content); i += 2 {
			key, err := projectYAML(node.Content[i], map[*yaml.Node]bool{}, &remaining)
			if err != nil {
				return nil, err
			}
			entryKeys[node.Content[i].Line] = key.(string)
			blockScalars[node.Content[i].Line] = node.Content[i+1].Style&(yaml.LiteralStyle|yaml.FoldedStyle) != 0
		}
	}
	// Leave opening/closing delimiters and all comment and blank lines in place.
	closing := len(lines) - 1
	if lines[closing] == "" {
		closing--
	}
	var b strings.Builder
	b.WriteString(lines[0])
	seen := map[string]bool{}
	for i := 1; i < closing; {
		line := lines[i]
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			b.WriteString(line)
			i++
			continue
		}
		key, exists := entryKeys[i]
		if !exists {
			return nil, fmt.Errorf("%w: unsupported metadata entry on line %d", ErrInvalidFrontmatter, i+1)
		}
		end := i + 1
		for end < closing {
			if _, exists := entryKeys[end]; exists {
				break
			}
			end++
		}
		value, changed := set[key]
		// Semantically identical updates do not reformat an entry.
		changed = changed && !reflect.DeepEqual(value, doc.Frontmatter[key])
		if deleted[key] || changed {
			if changed && !deleted[key] && !seen[key] {
				b.WriteString(entry(key, value))
			}
			for _, extra := range lines[i+1 : end] {
				if strings.TrimSpace(extra) == "" || strings.HasPrefix(strings.TrimSpace(extra), "#") && (indentation(extra) == 0 || !blockScalars[i]) {
					b.WriteString(extra)
				}
			}
		} else {
			for _, unchanged := range lines[i:end] {
				b.WriteString(unchanged)
			}
		}
		seen[key] = true
		i = end
	}
	keys := make([]string, 0, len(set))
	for key := range set {
		if !seen[key] && !deleted[key] {
			keys = append(keys, key)
		}
	}
	sort.Strings(keys)
	for _, key := range keys {
		b.WriteString(entry(key, set[key]))
	}
	b.WriteString(lines[closing])
	b.WriteString(doc.Markdown)
	result := []byte(b.String())
	if _, err := ParseConcept(conceptID, result); err != nil {
		return nil, err
	}
	return result, nil
}
