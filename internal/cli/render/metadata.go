package render

import (
	"bytes"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

var knownMetadataKeys = []string{
	"title",
	"type",
	"description",
	"tags",
	"resource",
	"timestamp",
	"revision",
	"writable",
}

var metadataLabels = map[string]string{
	"title":         "Title",
	"type":          "Type",
	"description":   "Description",
	"tags":          "Tags",
	"resource":      "Resource",
	"timestamp":     "Timestamp",
	"revision":      "Rev",
	"writable":      "Writable",
	"plausible_okf": "Plausible OKF",
}

type metadataField struct {
	label string
	value string
}

func (r *Renderer) RenderMetadata(values map[string]any) string {
	fields := metadataFields(values)
	if len(fields) == 0 {
		return ""
	}
	width := maxLabelWidth(fields)
	var b strings.Builder
	for _, field := range fields {
		label := field.label + ":" + strings.Repeat(" ", width-len(field.label)+1)
		value := field.value
		if r.colorEnabled {
			label = r.styles.Label.Render(label)
			value = r.styles.Value.Render(value)
		}
		b.WriteString(label)
		b.WriteString(value)
		b.WriteString("\n")
	}
	return strings.TrimRight(b.String(), "\n")
}

func metadataFields(values map[string]any) []metadataField {
	if len(values) == 0 {
		return nil
	}
	seen := map[string]bool{}
	var fields []metadataField
	for _, key := range knownMetadataKeys {
		if value, ok := values[key]; ok {
			if formatted, ok := formatMetadataValue(value); ok {
				fields = append(fields, metadataField{label: metadataLabel(key), value: formatted})
			}
			seen[key] = true
		}
	}
	var unknown []string
	for key := range values {
		if !seen[key] {
			unknown = append(unknown, key)
		}
	}
	sort.Strings(unknown)
	for _, key := range unknown {
		if formatted, ok := formatMetadataValue(values[key]); ok {
			fields = append(fields, metadataField{label: metadataLabel(key), value: formatted})
		}
	}
	return fields
}

func metadataLabel(key string) string {
	if label, ok := metadataLabels[key]; ok {
		return label
	}
	parts := strings.FieldsFunc(key, func(r rune) bool {
		return r == '_' || r == '-'
	})
	for i := range parts {
		if parts[i] == "" {
			continue
		}
		parts[i] = strings.ToUpper(parts[i][:1]) + parts[i][1:]
	}
	if len(parts) == 0 {
		return key
	}
	return strings.Join(parts, " ")
}

func maxLabelWidth(fields []metadataField) int {
	width := 0
	for _, field := range fields {
		if len(field.label) > width {
			width = len(field.label)
		}
	}
	return width
}

func formatMetadataValue(value any) (string, bool) {
	switch v := value.(type) {
	case nil:
		return "", false
	case string:
		if strings.TrimSpace(v) == "" {
			return "", false
		}
		return v, true
	case bool:
		if v {
			return "true", true
		}
		return "false", true
	case []string:
		if len(v) == 0 {
			return "", false
		}
		return strings.Join(v, ", "), true
	case []any:
		if len(v) == 0 {
			return "", false
		}
		if items, ok := simpleStringList(v); ok {
			return strings.Join(items, ", "), true
		}
		return compactJSON(v), true
	case map[string]any:
		if len(v) == 0 {
			return "", false
		}
		return compactJSON(v), true
	default:
		return fmt.Sprint(v), true
	}
}

func simpleStringList(values []any) ([]string, bool) {
	items := make([]string, 0, len(values))
	for _, item := range values {
		text, ok := item.(string)
		if !ok {
			return nil, false
		}
		if strings.TrimSpace(text) != "" {
			items = append(items, text)
		}
	}
	return items, len(items) > 0
}

func compactJSON(value any) string {
	data, err := json.Marshal(value)
	if err != nil {
		return fmt.Sprint(value)
	}
	var b bytes.Buffer
	if err := json.Compact(&b, data); err != nil {
		return string(data)
	}
	return b.String()
}
