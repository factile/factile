package okf

import (
	"encoding/json"
	"fmt"
	"math/big"
	"path"
	"regexp"
	"sort"
	"strings"
	"time"
)

// Diagnostic describes optional metadata without changing authored values.
type Diagnostic struct {
	Code    string `json:"code"`
	Field   string `json:"field"`
	Message string `json:"message"`
}

var datetimePattern = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(?:\.\d+)?(?:Z|[+-]\d{2}:\d{2})$`)
var portableActorPattern = regexp.MustCompile(`^(?:[^\s/:]+/\S+|[A-Za-z][A-Za-z0-9+.-]*:\S+)$`)

func PortableDatetime(value any) (time.Time, bool) {
	text, ok := value.(string)
	if !ok {
		return time.Time{}, false
	}
	if len(text) == 10 {
		date, err := time.Parse(time.DateOnly, text)
		return date, err == nil && date.Year() > 0
	}
	return Datetime(strings.ToUpper(text))
}

func PortableActor(value any) bool {
	text, ok := value.(string)
	return ok && portableActorPattern.MatchString(text)
}

func EmptyMetadata(value any) bool {
	switch v := value.(type) {
	case nil:
		return true
	case string:
		return v == ""
	case []any:
		return len(v) == 0
	case []string:
		return len(v) == 0
	default:
		return false
	}
}

func MetadataItems(value any) []any {
	if EmptyMetadata(value) {
		return nil
	}
	if items, ok := metadataList(value); ok {
		return items
	}
	return []any{value}
}

var formatVersionPattern = regexp.MustCompile(`^\d+\.\d+$`)
var sourceIDPattern = regexp.MustCompile(`^[^\s\[\]]+$`)

func Datetime(value any) (time.Time, bool) {
	text, ok := value.(string)
	if !ok || !datetimePattern.MatchString(text) {
		return time.Time{}, false
	}
	if text[len(text)-1] != 'Z' {
		offset := text[len(text)-5:]
		if offset[:2] > "23" || offset[3:] > "59" {
			return time.Time{}, false
		}
	}
	parsed, err := time.Parse(time.RFC3339Nano, text)
	return parsed, err == nil && parsed.Year() > 0
}

// IndexDiagnostic uses the physical bundle-relative ID, including index.
func IndexDiagnostic(doc Document) *Diagnostic {
	if path.Base(doc.ConceptID) != "index" || (!doc.HasFrontmatter && len(doc.Frontmatter) == 0) {
		return nil
	}
	if doc.ConceptID == "index" && doc.Frontmatter["okf_version"] == "0.3" {
		return nil
	}
	invalid := doc.ConceptID != "index"
	for key, value := range doc.Frontmatter {
		version, ok := value.(string)
		if key != "okf_version" || !ok || !formatVersionPattern.MatchString(version) {
			invalid = true
		}
	}
	if invalid {
		return &Diagnostic{Code: "invalid_reserved_file", Message: "Index frontmatter is only permitted at the bundle root with an okf_version key"}
	}
	if version, ok := doc.Frontmatter["okf_version"].(string); ok && version != "0.2" {
		return &Diagnostic{Code: "unsupported_okf_version", Message: "Unknown OKF version: " + version}
	}
	return nil
}

// MetadataDiagnostics validates standard optional families; type and reserved
// file validity are handled by the caller. Unknown fields remain untouched.
func MetadataDiagnostics(fm map[string]any) []Diagnostic {
	return metadataDiagnostics(fm, false)
}

func PortableMetadataDiagnostics(fm map[string]any) []Diagnostic {
	return metadataDiagnostics(fm, true)
}

func metadataDiagnostics(fm map[string]any, portable bool) []Diagnostic {
	datetime := Datetime
	list := metadataList
	if portable {
		datetime = PortableDatetime
		list = func(value any) ([]any, bool) { return MetadataItems(value), true }
		// Optional empty values have the same projection as absent keys.
		present := make(map[string]any, len(fm))
		for key, value := range fm {
			if !EmptyMetadata(value) {
				present[key] = value
			}
		}
		fm = present
	}
	result := []Diagnostic{}
	warn := func(field string) {
		result = append(result, Diagnostic{Code: "invalid_metadata", Field: field, Message: "Invalid OKF metadata: " + field})
	}
	stringValue := func(value any) bool { s, ok := value.(string); return ok && strings.TrimSpace(s) != "" }
	object := func(value any, field string) map[string]any {
		m, ok := value.(map[string]any)
		if !ok {
			warn(field)
		}
		return m
	}
	requiredText := func(m map[string]any, key, field string) {
		if !stringValue(m[key]) {
			warn(field + "." + key)
		}
	}
	date := func(m map[string]any, key, field string, required bool) {
		value, exists := m[key]
		if !exists && !required {
			return
		}
		if _, ok := datetime(value); !ok {
			warn(field + "." + key)
		}
	}
	window := func(value any, field string) {
		m := object(value, field)
		if m == nil {
			return
		}
		date(m, "from", field, true)
		date(m, "to", field, true)
		_, a := datetime(m["from"])
		_, b := datetime(m["to"])
		if a && b && CompareDatetimes(m["from"].(string), m["to"].(string)) > 0 {
			warn(field)
		}
	}
	for _, key := range []string{"title", "description", "resource", "runtime", "computation"} {
		if v, ok := fm[key]; ok {
			s, text := v.(string)
			if !text || ((key == "runtime" || key == "computation") && strings.TrimSpace(s) == "") {
				warn(key)
			}
		}
	}
	if v, ok := fm["tags"]; ok {
		if values, yes := list(v); !yes {
			warn("tags")
		} else {
			for i, v := range values {
				if _, yes := v.(string); !yes {
					warn(fmt.Sprintf("tags[%d]", i))
				}
			}
		}
	}
	if v, ok := fm["status"]; ok && v != "draft" && v != "stable" && v != "deprecated" {
		warn("status")
	}
	if v, ok := fm["stale_after"]; ok {
		if _, yes := datetime(v); !yes {
			warn("stale_after")
		}
	}
	if v, ok := fm["generated"]; ok {
		if m := object(v, "generated"); m != nil {
			requiredText(m, "by", "generated")
			if portable && !PortableActor(m["by"]) {
				warn("generated.by")
			}
			date(m, "at", "generated", false)
		}
	}
	if v, ok := fm["verified"]; ok {
		values, isList := list(v)
		if m, yes := v.(map[string]any); yes {
			values = []any{m}
			isList = true
		}
		if !isList {
			warn("verified")
		} else {
			for i, v := range values {
				field := fmt.Sprintf("verified[%d]", i)
				if m := object(v, field); m != nil {
					requiredText(m, "by", field)
					if portable && !PortableActor(m["by"]) {
						warn(field + ".by")
					}
					date(m, "at", field, true)
				}
			}
		}
	}
	if v, ok := fm["usage_window"]; ok {
		window(v, "usage_window")
	}
	if v, ok := fm["sources"]; ok {
		values, isList := list(v)
		if !isList {
			warn("sources")
		} else {
			for i, v := range values {
				field := fmt.Sprintf("sources[%d]", i)
				m := object(v, field)
				if m == nil {
					continue
				}
				requiredText(m, "resource", field)
				for _, key := range []string{"id", "author", "title"} {
					if v, exists := m[key]; exists {
						s, yes := v.(string)
						if (portable && key == "author" && !PortableActor(v)) || !yes || (key != "title" && strings.TrimSpace(s) == "") || (key == "id" && !sourceIDPattern.MatchString(s)) {
							warn(field + "." + key)
						}
					}
				}
				date(m, "last_modified", field, false)
				effective, hasWindow := m["usage_window"]
				if hasWindow {
					window(effective, field+".usage_window")
				} else {
					_, hasWindow = fm["usage_window"]
				}
				if count, exists := m["usage_count"]; exists {
					number, yes := new(big.Rat).SetString(fmt.Sprint(count))
					if _, boolean := count.(bool); boolean || !yes || !number.IsInt() || number.Sign() < 0 {
						warn(field + ".usage_count")
					}
					switch count.(type) {
					case json.Number, int, int64, float64:
					default:
						warn(field + ".usage_count")
					}
					if !hasWindow {
						warn(field + ".usage_window")
					}
				}
			}
		}
	}
	if fm["type"] == "Attested Computation" {
		if _, present := fm["runtime"]; !present {
			warn("runtime")
		}
	}
	if v, ok := fm["parameters"]; ok {
		values, isList := list(v)
		if !isList {
			warn("parameters")
		} else {
			names := map[string]bool{}
			for i, v := range values {
				field := fmt.Sprintf("parameters[%d]", i)
				m := object(v, field)
				if m == nil {
					continue
				}
				requiredText(m, "name", field)
				requiredText(m, "type", field)
				if value, exists := m["required"]; (!portable || exists) && !isBool(value) {
					warn(field + ".required")
				}
				if name, yes := m["name"].(string); yes {
					if names[name] {
						warn(field + ".name")
					}
					names[name] = true
				}
			}
		}
	}
	for _, key := range []string{"executor", "attester"} {
		if v, ok := fm[key]; ok {
			m := object(v, key)
			if m == nil {
				continue
			}
			requiredText(m, "resource", key)
			if key == "executor" {
				values, isList := metadataList(m["receipt"])
				if !isList {
					warn(key + ".receipt")
				} else {
					seen := map[string]bool{}
					for i, v := range values {
						s, yes := v.(string)
						if !yes || !stringValue(v) || seen[s] {
							warn(fmt.Sprintf("executor.receipt[%d]", i))
						}
						seen[s] = true
					}
				}
			}
		}
	}
	sort.SliceStable(result, func(i, j int) bool { return result[i].Field < result[j].Field })
	unique := result[:0]
	for _, item := range result {
		if len(unique) == 0 || unique[len(unique)-1].Field != item.Field {
			unique = append(unique, item)
		}
	}
	return unique
}

func metadataList(value any) ([]any, bool) {
	switch v := value.(type) {
	case []any:
		return v, true
	case []string:
		result := make([]any, len(v))
		for i, s := range v {
			result[i] = s
		}
		return result, true
	default:
		return nil, false
	}
}

func isBool(value any) bool {
	_, ok := value.(bool)
	return ok
}
