package patch

import (
	"fmt"
	"regexp"
	"strings"
)

// Operation describes one ordered edit. Text replacements match exactly once.
type Operation struct {
	Op       string `json:"op"`
	Old      string `json:"old,omitempty"`
	New      string `json:"new,omitempty"`
	Heading  string `json:"heading,omitempty"`
	Markdown string `json:"markdown,omitempty"`
	Key      string `json:"key,omitempty"`
	Value    any    `json:"value,omitempty"`
}

type Error struct {
	Code    string
	Message string
}

func (e *Error) Error() string { return e.Message }

func ReplaceText(markdown, old, replacement string) (string, error) {
	if old == "" {
		return "", &Error{"invalid_patch", "Replacement old text must not be empty"}
	}
	// Count overlapping matches too: "aa" is ambiguous in "aaa".
	first := strings.Index(markdown, old)
	if first < 0 {
		return "", &Error{"text_not_found", "Old text does not match; read the document and use exact text"}
	}
	if strings.Contains(markdown[first+1:], old) {
		return "", &Error{"text_ambiguous", "Old text matches more than once; include more surrounding text"}
	}
	return markdown[:first] + replacement + markdown[first+len(old):], nil
}

var headingRE = regexp.MustCompile(`^ {0,3}(#{1,6})[\t ]+(.+?)[\t ]*$`)
var closingHashesRE = regexp.MustCompile(`[\t ]+#+[\t ]*$`)

func ReplaceSection(markdown, heading, replacement string) (string, error) {
	section, ok, err := findSection(markdown, heading)
	if err != nil {
		return "", err
	}
	if !ok {
		return "", &Error{"section_not_found", fmt.Sprintf("Section not found: %s", heading)}
	}
	nl := newline(markdown)
	replacement = strings.TrimRight(withNewline(replacement, nl), "\r\n")
	body := nl
	if replacement != "" {
		body += replacement + nl
	}
	if section.end < len(markdown) {
		body += nl
	}
	// Keep the heading itself byte-for-byte, including its spacing and newline.
	prefix := markdown[:section.body]
	if !strings.HasSuffix(prefix, "\n") {
		prefix += nl
	}
	return prefix + body + markdown[section.end:], nil
}

func AppendSection(markdown, heading, addition string) (string, error) {
	section, ok, err := findSection(markdown, heading)
	if err != nil {
		return "", err
	}
	nl := newline(markdown)
	addition = strings.TrimRight(withNewline(addition, nl), "\r\n")
	if ok {
		if addition == "" {
			return markdown, nil
		}
		prefix := markdown[:section.end]
		suffix := ""
		if section.end < len(markdown) {
			suffix = nl + nl
		}
		return prefix + separation(prefix, nl) + addition + suffix + markdown[section.end:], nil
	}
	if strings.TrimSpace(heading) == "" || strings.ContainsAny(heading, "\r\n") {
		return "", &Error{"invalid_patch", "Section heading must be a non-empty single line"}
	}
	return markdown + separation(markdown, nl) + "## " + heading + nl + nl + addition + nl, nil
}

func separation(text, nl string) string {
	if strings.HasSuffix(text, nl+nl) {
		return ""
	}
	if strings.HasSuffix(text, nl) {
		return nl
	}
	return nl + nl
}

func newline(text string) string {
	if i := strings.IndexByte(text, '\n'); i > 0 && text[i-1] == '\r' {
		return "\r\n"
	}
	return "\n"
}

func withNewline(text, nl string) string {
	return strings.ReplaceAll(strings.ReplaceAll(text, "\r\n", "\n"), "\n", nl)
}

type sectionSpan struct {
	start, body, end, level int
	title                   string
}

func findSection(markdown, heading string) (sectionSpan, bool, error) {
	var headings []sectionSpan
	offset := 0
	var fence byte
	fenceLength := 0
	for _, line := range strings.SplitAfter(markdown, "\n") {
		trimmed := strings.TrimRight(line, "\r\n")
		indent := len(trimmed) - len(strings.TrimLeft(trimmed, " "))
		content := strings.TrimLeft(trimmed, " ")
		if indent <= 3 && len(content) > 0 && (content[0] == '`' || content[0] == '~') {
			n := 0
			for n < len(content) && content[n] == content[0] {
				n++
			}
			if fence != 0 {
				if content[0] == fence && n >= fenceLength && strings.TrimSpace(content[n:]) == "" {
					fence = 0
				}
			} else if n >= 3 && (content[0] != '`' || !strings.Contains(content[n:], "`")) {
				fence, fenceLength = content[0], n
			}
		} else if fence == 0 {
			if match := headingRE.FindStringSubmatch(trimmed); len(match) == 3 {
				title := strings.TrimSpace(closingHashesRE.ReplaceAllString(match[2], ""))
				headings = append(headings, sectionSpan{offset, offset + len(line), len(markdown), len(match[1]), title})
			}
		}
		offset += len(line)
	}
	found := -1
	for i, h := range headings {
		if strings.EqualFold(h.title, strings.TrimSpace(heading)) {
			if found >= 0 {
				return sectionSpan{}, false, &Error{"section_ambiguous", "Section heading matches more than once; use an exact text replacement with surrounding text"}
			}
			found = i
		}
	}
	if found < 0 {
		return sectionSpan{}, false, nil
	}
	section := headings[found]
	for _, h := range headings[found+1:] {
		if h.level <= section.level {
			section.end = h.start
			break
		}
	}
	return section, true, nil
}

// Diff returns one unified hunk around the changed lines, without a diff dependency.
func Diff(path, before, after string) string {
	if before == after {
		return ""
	}
	split := func(s string) []string {
		x := strings.SplitAfter(s, "\n")
		if x[len(x)-1] == "" {
			x = x[:len(x)-1]
		}
		return x
	}
	a, b := split(before), split(after)
	start := 0
	for start < len(a) && start < len(b) && a[start] == b[start] {
		start++
	}
	endA, endB := len(a), len(b)
	for endA > start && endB > start && a[endA-1] == b[endB-1] {
		endA--
		endB--
	}
	from := max(0, start-3)
	toA, toB := min(len(a), endA+3), min(len(b), endB+3)
	var out strings.Builder
	lineStart := func(from, count int) int {
		if count == 0 {
			return from
		}
		return from + 1
	}
	fmt.Fprintf(&out, "--- a%s\n+++ b%s\n@@ -%d,%d +%d,%d @@\n", path, path, lineStart(from, toA-from), toA-from, lineStart(from, toB-from), toB-from)
	emit := func(prefix string, line string) {
		out.WriteString(prefix + line)
		if !strings.HasSuffix(line, "\n") {
			out.WriteString("\n\\ No newline at end of file\n")
		}
	}
	for _, s := range a[from:start] {
		emit(" ", s)
	}
	for _, s := range a[start:endA] {
		emit("-", s)
	}
	for _, s := range b[start:endB] {
		emit("+", s)
	}
	for _, s := range a[endA:toA] {
		emit(" ", s)
	}
	return out.String()
}
