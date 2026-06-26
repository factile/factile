package patch

import (
	"fmt"
	"regexp"
	"strings"
)

var headingRE = regexp.MustCompile(`^(#{1,6})\s+(.+?)\s*$`)

func ReplaceSection(markdown, heading, replacement string) (string, error) {
	section, ok := findSection(markdown, heading)
	if !ok {
		return "", fmt.Errorf("section not found: %s", heading)
	}
	replacement = strings.TrimRight(replacement, "\n")
	newSection := section.headingLine
	if replacement != "" {
		newSection += "\n\n" + replacement
	}
	if section.end < len(markdown) && !strings.HasSuffix(newSection, "\n") {
		newSection += "\n\n"
	} else if section.end == len(markdown) && !strings.HasSuffix(newSection, "\n") {
		newSection += "\n"
	}
	return markdown[:section.start] + newSection + markdown[section.end:], nil
}

func AppendSection(markdown, heading, addition string) string {
	addition = strings.TrimRight(addition, "\n")
	if section, ok := findSection(markdown, heading); ok {
		insert := section.end
		prefix := "\n\n"
		trailingNewlines := 0
		for i := insert - 1; i >= 0 && markdown[i] == '\n'; i-- {
			trailingNewlines++
		}
		if trailingNewlines >= 2 {
			prefix = ""
		} else if trailingNewlines == 1 {
			prefix = "\n"
		}
		suffix := ""
		if insert < len(markdown) && !strings.HasSuffix(addition, "\n") {
			suffix = "\n\n"
		}
		return markdown[:insert] + prefix + addition + suffix + markdown[insert:]
	}
	suffix := "\n\n"
	if strings.HasSuffix(markdown, "\n\n") {
		suffix = ""
	} else if strings.HasSuffix(markdown, "\n") {
		suffix = "\n"
	}
	return markdown + suffix + "## " + heading + "\n\n" + addition + "\n"
}

type sectionSpan struct {
	start       int
	end         int
	level       int
	headingLine string
}

func findSection(markdown, heading string) (sectionSpan, bool) {
	lines := strings.SplitAfter(markdown, "\n")
	offset := 0
	for i, line := range lines {
		trimmed := strings.TrimRight(line, "\r\n")
		match := headingRE.FindStringSubmatch(trimmed)
		if len(match) != 3 {
			offset += len(line)
			continue
		}
		if strings.EqualFold(strings.TrimSpace(match[2]), strings.TrimSpace(heading)) {
			level := len(match[1])
			end := len(markdown)
			nextOffset := offset + len(line)
			for _, later := range lines[i+1:] {
				laterTrimmed := strings.TrimRight(later, "\r\n")
				laterMatch := headingRE.FindStringSubmatch(laterTrimmed)
				if len(laterMatch) == 3 && len(laterMatch[1]) <= level {
					end = nextOffset
					break
				}
				nextOffset += len(later)
			}
			return sectionSpan{
				start:       offset,
				end:         end,
				level:       level,
				headingLine: trimmed,
			}, true
		}
		offset += len(line)
	}
	return sectionSpan{}, false
}
