package factile

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

func (w *LocalWorkspace) viewsPath() (string, error) {
	workspace, err := w.resolvedWorkspace()
	if err != nil {
		return "", NormalizeError(err)
	}
	return filepath.Join(workspace.WorkspaceDir, "factile.views.toml"), nil
}

func loadViewsAllowMissing(filename string) ([]View, error) {
	views, err := loadViewsFile(filename)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	return views, nil
}

func loadViewsFile(filename string) ([]View, error) {
	info, err := os.Lstat(filename)
	if err != nil {
		return nil, err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return nil, fmt.Errorf("factile.views.toml must be a regular file")
	}
	file, err := os.Open(filename)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	var views []View
	current := -1
	scanner := bufio.NewScanner(file)
	for lineNo := 1; scanner.Scan(); lineNo++ {
		line := stripViewsComment(strings.TrimSpace(scanner.Text()))
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "[[") && strings.HasSuffix(line, "]]") {
			table := strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(line, "[["), "]]"))
			if table != "views" {
				return nil, fmt.Errorf("unsupported views table %q on line %d", table, lineNo)
			}
			views = append(views, View{})
			current = len(views) - 1
			continue
		}
		if strings.HasPrefix(line, "[") {
			return nil, fmt.Errorf("unsupported views table on line %d", lineNo)
		}
		if current < 0 {
			return nil, fmt.Errorf("view property before table on line %d", lineNo)
		}
		parts := strings.SplitN(line, "=", 2)
		if len(parts) != 2 {
			return nil, fmt.Errorf("invalid views assignment on line %d", lineNo)
		}
		if err := assignViewFileValue(&views[current], strings.TrimSpace(parts[0]), strings.TrimSpace(parts[1]), lineNo); err != nil {
			return nil, err
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return normalizeLoadedViews(views)
}

func assignViewFileValue(view *View, key string, rawValue string, lineNo int) error {
	switch key {
	case "id":
		return assignViewString(&view.ID, rawValue, key, lineNo)
	case "title":
		return assignViewString(&view.Title, rawValue, key, lineNo)
	case "description":
		return assignViewString(&view.Description, rawValue, key, lineNo)
	case "status":
		return assignViewString(&view.Status, rawValue, key, lineNo)
	case "paths":
		paths, err := parseViewStringArray(rawValue)
		if err != nil {
			return fmt.Errorf("invalid paths on line %d: %w", lineNo, err)
		}
		view.Paths = paths
	default:
		return fmt.Errorf("unsupported views key %q on line %d", key, lineNo)
	}
	return nil
}

func assignViewString(target *string, rawValue string, key string, lineNo int) error {
	if !strings.HasPrefix(rawValue, `"`) {
		return fmt.Errorf("views key %q on line %d expects quoted string", key, lineNo)
	}
	value, err := strconv.Unquote(rawValue)
	if err != nil {
		return fmt.Errorf("invalid string for views key %q on line %d: %w", key, lineNo, err)
	}
	*target = value
	return nil
}

func parseViewStringArray(raw string) ([]string, error) {
	if !strings.HasPrefix(raw, "[") || !strings.HasSuffix(raw, "]") {
		return nil, fmt.Errorf("expected string array")
	}
	body := strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(raw, "["), "]"))
	if body == "" {
		return nil, nil
	}
	var values []string
	for body != "" {
		body = strings.TrimSpace(body)
		if !strings.HasPrefix(body, `"`) {
			return nil, fmt.Errorf("array values must be strings")
		}
		end := 1
		escaped := false
		for end < len(body) {
			if escaped {
				escaped = false
				end++
				continue
			}
			switch body[end] {
			case '\\':
				escaped = true
			case '"':
				text, err := strconv.Unquote(body[:end+1])
				if err != nil {
					return nil, err
				}
				values = append(values, text)
				body = strings.TrimSpace(body[end+1:])
				if body == "" {
					return values, nil
				}
				if !strings.HasPrefix(body, ",") {
					return nil, fmt.Errorf("expected comma after array value")
				}
				body = strings.TrimSpace(strings.TrimPrefix(body, ","))
				if body == "" {
					return nil, fmt.Errorf("trailing comma in array")
				}
				end = 0
			}
			end++
		}
		if end >= len(body) {
			return nil, fmt.Errorf("unterminated string in array")
		}
	}
	return values, nil
}

func normalizeLoadedViews(views []View) ([]View, error) {
	ids := map[string]bool{}
	out := make([]View, 0, len(views))
	for _, view := range views {
		view.ID = strings.TrimSpace(view.ID)
		if view.ID == "" {
			return nil, errorf(ErrValidationFailed, "View id is required")
		}
		if ids[view.ID] {
			return nil, errorf(ErrValidationFailed, "Duplicate view id: %s", view.ID)
		}
		ids[view.ID] = true
		paths, err := normalizeViewPaths(view.Paths)
		if err != nil {
			return nil, err
		}
		view.Paths = paths
		out = append(out, view)
	}
	return out, nil
}

func writeViewsFile(filename string, views []View) error {
	normalized, err := normalizeLoadedViews(views)
	if err != nil {
		return err
	}
	return atomicWriteViewsFile(filename, []byte(formatViewsFile(normalized)), os.Rename)
}

func atomicWriteViewsFile(filename string, data []byte, replace func(string, string) error) error {
	if info, err := os.Lstat(filename); err == nil {
		if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
			return fmt.Errorf("factile.views.toml must be a regular file")
		}
	} else if !os.IsNotExist(err) {
		return err
	}
	temporary, err := os.CreateTemp(filepath.Dir(filename), ".factile.views.toml.tmp-*")
	if err != nil {
		return err
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	if err := temporary.Chmod(0o644); err != nil {
		temporary.Close()
		return err
	}
	if _, err := temporary.Write(data); err != nil {
		temporary.Close()
		return err
	}
	if err := temporary.Sync(); err != nil {
		temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	return replace(temporaryPath, filename)
}

func formatViewsFile(views []View) string {
	views = sortedViews(views)
	var b strings.Builder
	for _, view := range views {
		b.WriteString("[[views]]\n")
		writeViewString(&b, "id", view.ID)
		writeOptionalViewString(&b, "title", view.Title)
		writeOptionalViewString(&b, "description", view.Description)
		writeOptionalViewString(&b, "status", view.Status)
		writeViewStringArray(&b, "paths", view.Paths)
		b.WriteString("\n")
	}
	return b.String()
}

func writeViewString(b *strings.Builder, key string, value string) {
	b.WriteString(key)
	b.WriteString(" = ")
	b.WriteString(strconv.Quote(value))
	b.WriteString("\n")
}

func writeOptionalViewString(b *strings.Builder, key string, value string) {
	if strings.TrimSpace(value) == "" {
		return
	}
	writeViewString(b, key, value)
}

func writeViewStringArray(b *strings.Builder, key string, values []string) {
	b.WriteString(key)
	b.WriteString(" = [")
	for i, value := range values {
		if i > 0 {
			b.WriteString(", ")
		}
		b.WriteString(strconv.Quote(value))
	}
	b.WriteString("]\n")
}

func stripViewsComment(line string) string {
	inString := false
	escaped := false
	for i, r := range line {
		if escaped {
			escaped = false
			continue
		}
		if r == '\\' && inString {
			escaped = true
			continue
		}
		if r == '"' {
			inString = !inString
			continue
		}
		if r == '#' && !inString {
			return strings.TrimSpace(line[:i])
		}
	}
	return line
}
