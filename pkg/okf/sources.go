package okf

import (
	"path"
	"regexp"
	"strings"
)

type SourceReference struct {
	Source               map[string]any `json:"source"`
	Kind                 string         `json:"kind"`
	Target               string         `json:"target,omitempty"`
	EffectiveUsageWindow map[string]any `json:"effective_usage_window,omitempty"`
}

type ClaimReference struct {
	ID       string `json:"id"`
	Resolved bool   `json:"resolved"`
}

var resourceScheme = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9+.-]*:`)
var resourceFilename = regexp.MustCompile(`^[^\s/]+\.[A-Za-z0-9_-]+$`)
var footnoteDefinition = regexp.MustCompile(`^ {0,3}\[\^([^\]\n]+)\]:`)
var footnoteMarker = regexp.MustCompile(`\[\^([^\]\n]+)\]`)

// SourceTarget resolves only within the owning physical bundle. An empty target
// for an internal reference means that it escapes the bundle or names no path.
func SourceTarget(conceptID, resource string) (kind, target string) {
	if resourceScheme.MatchString(resource) {
		return "external", ""
	}
	resource = strings.SplitN(resource, "#", 2)[0]
	explicit := strings.HasPrefix(resource, "/") || strings.HasPrefix(resource, "./") || strings.HasPrefix(resource, "../")
	if !explicit && !(strings.Contains(resource, "/") && !strings.ContainsAny(resource, " \t\r\n")) && !resourceFilename.MatchString(resource) {
		return "scope", ""
	}
	parts := []string{}
	if !strings.HasPrefix(resource, "/") {
		dir := path.Dir(strings.TrimPrefix(conceptID, "/"))
		if dir != "." {
			parts = strings.Split(dir, "/")
		}
	}
	for _, part := range strings.Split(resource, "/") {
		switch part {
		case "", ".":
			continue
		case "..":
			if len(parts) == 0 {
				return "internal", ""
			}
			parts = parts[:len(parts)-1]
		default:
			if strings.EqualFold(part, ".git") || strings.EqualFold(part, ".factile") {
				return "internal", ""
			}
			parts = append(parts, part)
		}
	}
	if len(parts) == 0 {
		return "internal", ""
	}
	return "internal", "/" + strings.TrimSuffix(strings.Join(parts, "/"), ".md")
}

// SourceReferences preserves declared evidence. visible receives bundle-relative
// paths and must enforce the caller's source, view, and access boundary.
func SourceReferences(conceptID string, fields map[string]any, markdown string, visible func(string) bool) ([]SourceReference, []ClaimReference, []Diagnostic) {
	refs := []SourceReference{}
	claims := []ClaimReference{}
	diagnostics := []Diagnostic{}
	counts := map[string]int{}
	sources, _ := metadataList(fields["sources"])
	for _, raw := range sources {
		source, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		resource, ok := source["resource"].(string)
		if !ok || strings.TrimSpace(resource) == "" {
			continue
		}
		kind, target := SourceTarget(conceptID, resource)
		ref := SourceReference{Source: source, Kind: kind}
		if id, ok := source["id"].(string); ok && sourceIDPattern.MatchString(id) {
			counts[id]++
		}
		if kind == "internal" {
			if target != "" && visible != nil && visible(target) {
				ref.Target = target
			} else {
				diagnostics = append(diagnostics, Diagnostic{Code: "broken_source", Field: "sources", Message: "Unresolved internal source: " + resource})
			}
		}
		window, present := source["usage_window"]
		if !present {
			window = fields["usage_window"]
		}
		if m, ok := window.(map[string]any); ok && len(MetadataDiagnostics(map[string]any{"usage_window": m})) == 0 {
			ref.EffectiveUsageWindow = m
		}
		refs = append(refs, ref)
	}
	seen := map[string]bool{}
	for _, ref := range refs {
		id, _ := ref.Source["id"].(string)
		if counts[id] > 1 && !seen[id] {
			diagnostics = append(diagnostics, Diagnostic{Code: "duplicate_source_id", Field: "sources", Message: "Duplicate source ID: " + id})
			seen[id] = true
		}
	}
	labels, duplicates := ClaimLabels(markdown)
	for _, label := range labels {
		resolved := counts[label] == 1
		claims = append(claims, ClaimReference{ID: label, Resolved: resolved})
		if !resolved {
			diagnostics = append(diagnostics, Diagnostic{Code: "unmatched_claim", Field: "markdown", Message: "No unique source for claim: " + label})
		}
	}
	for _, label := range duplicates {
		diagnostics = append(diagnostics, Diagnostic{Code: "duplicate_claim_id", Field: "markdown", Message: "Duplicate footnote definition: " + label})
	}
	return refs, claims, diagnostics
}

// ClaimLabels extracts distinct footnote uses, excluding definitions and Markdown
// code. Definition text never supplies an inferred source or reviewer.
func ClaimLabels(markdown string) (labels, duplicates []string) {
	labels = []string{}
	duplicates = []string{}
	seen := map[string]bool{}
	definitions := map[string]int{}
	fenceChar := byte(0)
	fenceSize := 0
	definition := false
	lines := []string{}
	for _, line := range strings.Split(strings.ReplaceAll(markdown, "\r\n", "\n"), "\n") {
		trimmed := strings.TrimLeft(line, " ")
		indent := len(line) - len(trimmed)
		if indent <= 3 && len(trimmed) > 0 && (trimmed[0] == '`' || trimmed[0] == '~') {
			count := 0
			for count < len(trimmed) && trimmed[count] == trimmed[0] {
				count++
			}
			if fenceSize == 0 && count >= 3 {
				fenceChar = trimmed[0]
				fenceSize = count
				lines = append(lines, "")
				continue
			}
			if trimmed[0] == fenceChar && count >= fenceSize && strings.TrimSpace(trimmed[count:]) == "" {
				fenceSize = 0
				lines = append(lines, "")
				continue
			}
		}
		if fenceSize > 0 {
			lines = append(lines, "")
			continue
		}
		if match := footnoteDefinition.FindStringSubmatch(line); match != nil {
			definition = true
			definitions[match[1]]++
			if definitions[match[1]] == 2 {
				duplicates = append(duplicates, match[1])
			}
			lines = append(lines, "")
			continue
		}
		if definition && (strings.TrimSpace(line) == "" || strings.HasPrefix(line, " ") || strings.HasPrefix(line, "\t")) {
			lines = append(lines, "")
			continue
		}
		definition = false
		if indent >= 4 || strings.HasPrefix(line, "\t") {
			lines = append(lines, "")
			continue
		}
		lines = append(lines, line)
	}
	text := []byte(strings.Join(lines, "\n"))
	// Inline code may span lines within a paragraph. An unmatched delimiter is text.
	for i := 0; i < len(text); {
		if text[i] != '`' {
			i++
			continue
		}
		escapes := 0
		for j := i - 1; j >= 0 && text[j] == '\\'; j-- {
			escapes++
		}
		if escapes%2 == 1 {
			i++
			continue
		}
		end := i
		for end < len(text) && text[end] == '`' {
			end++
		}
		run := end - i
		closeAt := -1
		for j := end; j < len(text); {
			if text[j] == '\n' {
				k := j + 1
				for k < len(text) && (text[k] == ' ' || text[k] == '\t') {
					k++
				}
				if k < len(text) && text[k] == '\n' {
					break
				}
			}
			if text[j] != '`' {
				j++
				continue
			}
			k := j
			for k < len(text) && text[k] == '`' {
				k++
			}
			if k-j == run {
				closeAt = k
				break
			}
			j = k
		}
		if closeAt < 0 {
			i = end
			continue
		}
		for j := i; j < closeAt; j++ {
			if text[j] != '\n' {
				text[j] = ' '
			}
		}
		i = closeAt
	}
	for _, match := range footnoteMarker.FindAllSubmatchIndex(text, -1) {
		escapes := 0
		for i := match[0] - 1; i >= 0 && text[i] == '\\'; i-- {
			escapes++
		}
		if escapes%2 == 1 {
			continue
		}
		label := string(text[match[2]:match[3]])
		if !seen[label] {
			labels = append(labels, label)
			seen[label] = true
		}
	}
	return labels, duplicates
}
