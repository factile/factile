package catalog

import (
	"bufio"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

type Library struct {
	ID             string             `json:"id"`
	Title          string             `json:"title,omitempty"`
	Description    string             `json:"description,omitempty"`
	Scope          string             `json:"scope,omitempty"`
	Status         string             `json:"status,omitempty"`
	KnowledgeBases []KnowledgeBaseRef `json:"knowledge_bases,omitempty"`
	Views          []LibraryView      `json:"views,omitempty"`
}

type KnowledgeBaseRef struct {
	ID          string `json:"id"`
	Path        string `json:"path"`
	Catalog     string `json:"catalog"`
	Title       string `json:"title,omitempty"`
	Description string `json:"description,omitempty"`
	Status      string `json:"status,omitempty"`
}

type LibraryView struct {
	ID          string   `json:"id"`
	Title       string   `json:"title,omitempty"`
	Description string   `json:"description,omitempty"`
	Status      string   `json:"status,omitempty"`
	Paths       []string `json:"paths,omitempty"`
}

type KnowledgeBase struct {
	ID              string       `json:"id"`
	Path            string       `json:"path"`
	Title           string       `json:"title,omitempty"`
	Description     string       `json:"description,omitempty"`
	Purpose         string       `json:"purpose,omitempty"`
	Audience        string       `json:"audience,omitempty"`
	Profile         string       `json:"profile,omitempty"`
	DefaultTrust    string       `json:"default_trust,omitempty"`
	DefaultWritable bool         `json:"default_writable,omitempty"`
	Status          string       `json:"status,omitempty"`
	Bundles         []BundleLink `json:"bundles,omitempty"`
}

type BundleLink struct {
	ID           string `json:"id"`
	Path         string `json:"path"`
	Source       string `json:"source"`
	Kind         string `json:"kind"`
	Writable     bool   `json:"writable"`
	Version      string `json:"version,omitempty"`
	Revision     string `json:"revision,omitempty"`
	Title        string `json:"title,omitempty"`
	Description  string `json:"description,omitempty"`
	Trust        string `json:"trust,omitempty"`
	Status       string `json:"status,omitempty"`
	Profile      string `json:"profile,omitempty"`
	Priority     int    `json:"priority,omitempty"`
	WhenToUse    string `json:"when_to_use,omitempty"`
	WhenNotToUse string `json:"when_not_to_use,omitempty"`
}

func LoadLibraryFile(filename string) (Library, error) {
	records, err := parseFile(filename)
	if err != nil {
		return Library{}, err
	}
	var library Library
	for _, record := range records {
		switch record.table {
		case "":
			if err := assignLibrary(&library, record.key, record.value); err != nil {
				return Library{}, recordError(record, err)
			}
		case "knowledge_bases":
			if len(library.KnowledgeBases) == 0 || record.start {
				library.KnowledgeBases = append(library.KnowledgeBases, KnowledgeBaseRef{})
			}
			if err := assignKnowledgeBaseRef(&library.KnowledgeBases[len(library.KnowledgeBases)-1], record.key, record.value); err != nil {
				return Library{}, recordError(record, err)
			}
		case "views":
			if len(library.Views) == 0 || record.start {
				library.Views = append(library.Views, LibraryView{})
			}
			if err := assignLibraryView(&library.Views[len(library.Views)-1], record.key, record.value); err != nil {
				return Library{}, recordError(record, err)
			}
		default:
			return Library{}, fmt.Errorf("unsupported catalog table %q", record.table)
		}
	}
	return library, ValidateLibrary(library)
}

func WriteLibraryFile(filename string, library Library) error {
	if err := ValidateLibrary(library); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(filename), 0o755); err != nil {
		return err
	}
	return os.WriteFile(filename, []byte(formatLibrary(library)), 0o644)
}

func LoadKnowledgeBaseFile(filename string) (KnowledgeBase, error) {
	records, err := parseFile(filename)
	if err != nil {
		return KnowledgeBase{}, err
	}
	var kb KnowledgeBase
	for _, record := range records {
		switch record.table {
		case "":
			if err := assignKnowledgeBase(&kb, record.key, record.value); err != nil {
				return KnowledgeBase{}, recordError(record, err)
			}
		case "bundles":
			if len(kb.Bundles) == 0 || record.start {
				kb.Bundles = append(kb.Bundles, BundleLink{Kind: "local"})
			}
			if err := assignBundleLink(&kb.Bundles[len(kb.Bundles)-1], record.key, record.value); err != nil {
				return KnowledgeBase{}, recordError(record, err)
			}
		default:
			return KnowledgeBase{}, fmt.Errorf("unsupported catalog table %q", record.table)
		}
	}
	return kb, ValidateKnowledgeBase(kb)
}

func WriteKnowledgeBaseFile(filename string, kb KnowledgeBase) error {
	if err := ValidateKnowledgeBase(kb); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(filename), 0o755); err != nil {
		return err
	}
	return os.WriteFile(filename, []byte(formatKnowledgeBase(kb)), 0o644)
}

func ValidateLibrary(library Library) error {
	if strings.TrimSpace(library.ID) == "" {
		return fmt.Errorf("library id is required")
	}
	ids := map[string]bool{}
	paths := map[string]bool{}
	for _, ref := range library.KnowledgeBases {
		if strings.TrimSpace(ref.ID) == "" {
			return fmt.Errorf("knowledge base id is required")
		}
		if ids[ref.ID] {
			return fmt.Errorf("duplicate knowledge base id: %s", ref.ID)
		}
		ids[ref.ID] = true
		p, err := normalizeCatalogPath(ref.Path)
		if err != nil || p == "/" {
			return fmt.Errorf("invalid knowledge base path: %s", ref.Path)
		}
		if paths[p] {
			return fmt.Errorf("duplicate knowledge base path: %s", p)
		}
		paths[p] = true
		if strings.TrimSpace(ref.Catalog) == "" {
			return fmt.Errorf("knowledge base catalog is required: %s", ref.ID)
		}
	}
	viewIDs := map[string]bool{}
	for _, view := range library.Views {
		if strings.TrimSpace(view.ID) == "" {
			return fmt.Errorf("view id is required")
		}
		if viewIDs[view.ID] {
			return fmt.Errorf("duplicate view id: %s", view.ID)
		}
		viewIDs[view.ID] = true
		if len(view.Paths) == 0 {
			return fmt.Errorf("view paths are required: %s", view.ID)
		}
		viewPaths := map[string]bool{}
		for _, viewPath := range view.Paths {
			p, err := normalizeCatalogPath(viewPath)
			if err != nil || p == "/" {
				return fmt.Errorf("invalid view path: %s", viewPath)
			}
			if viewPaths[p] {
				return fmt.Errorf("duplicate view path: %s in %s", p, view.ID)
			}
			viewPaths[p] = true
		}
	}
	return nil
}

func ValidateKnowledgeBase(kb KnowledgeBase) error {
	if strings.TrimSpace(kb.ID) == "" {
		return fmt.Errorf("knowledge base id is required")
	}
	kbPath, err := normalizeCatalogPath(kb.Path)
	if err != nil || kbPath == "/" {
		return fmt.Errorf("invalid knowledge base path: %s", kb.Path)
	}
	ids := map[string]bool{}
	paths := map[string]bool{}
	for _, bundle := range kb.Bundles {
		if strings.TrimSpace(bundle.ID) == "" {
			return fmt.Errorf("bundle link id is required")
		}
		if ids[bundle.ID] {
			return fmt.Errorf("duplicate bundle link id: %s", bundle.ID)
		}
		ids[bundle.ID] = true
		linkPath, err := normalizeCatalogPath(bundle.Path)
		if err != nil || linkPath == "/" {
			return fmt.Errorf("invalid bundle link path: %s", bundle.Path)
		}
		if linkPath != kbPath && !strings.HasPrefix(linkPath, kbPath+"/") {
			return fmt.Errorf("bundle link path must sit under knowledge base path: %s", bundle.Path)
		}
		if paths[linkPath] {
			return fmt.Errorf("duplicate bundle link path: %s", linkPath)
		}
		paths[linkPath] = true
		if strings.TrimSpace(bundle.Source) == "" {
			return fmt.Errorf("bundle link source is required: %s", bundle.ID)
		}
		kind := bundle.Kind
		if kind == "" {
			kind = "local"
		}
		if kind != "local" && kind != "remote" {
			return fmt.Errorf("unsupported bundle link kind: %s", kind)
		}
		if kind == "remote" && bundle.Writable {
			return fmt.Errorf("remote bundle links must be read-only: %s", bundle.ID)
		}
	}
	return nil
}

type record struct {
	table string
	key   string
	value value
	start bool
	line  int
}

type value struct {
	text    string
	strings []string
	kind    string
}

func parseFile(filename string) ([]record, error) {
	file, err := os.Open(filename)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	var records []record
	table := ""
	nextStart := false
	scanner := bufio.NewScanner(file)
	for lineNo := 1; scanner.Scan(); lineNo++ {
		line := stripComment(strings.TrimSpace(scanner.Text()))
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "[[") && strings.HasSuffix(line, "]]") {
			table = strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(line, "[["), "]]"))
			nextStart = true
			continue
		}
		if strings.HasPrefix(line, "[") {
			return nil, fmt.Errorf("unsupported table on line %d", lineNo)
		}
		parts := strings.SplitN(line, "=", 2)
		if len(parts) != 2 {
			return nil, fmt.Errorf("invalid assignment on line %d", lineNo)
		}
		parsed, err := parseValue(strings.TrimSpace(parts[1]))
		if err != nil {
			return nil, fmt.Errorf("invalid value on line %d: %w", lineNo, err)
		}
		records = append(records, record{
			table: table,
			key:   strings.TrimSpace(parts[0]),
			value: parsed,
			start: nextStart,
			line:  lineNo,
		})
		nextStart = false
	}
	return records, scanner.Err()
}

func stripComment(line string) string {
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

func parseValue(raw string) (value, error) {
	if strings.HasPrefix(raw, `"`) {
		text, err := strconv.Unquote(raw)
		if err != nil {
			return value{}, err
		}
		return value{text: text, kind: "string"}, nil
	}
	if strings.HasPrefix(raw, "[") {
		strings, err := parseStringArray(raw)
		if err != nil {
			return value{}, err
		}
		return value{strings: strings, kind: "strings"}, nil
	}
	switch raw {
	case "true", "false":
		return value{text: raw, kind: "bool"}, nil
	default:
		if _, err := strconv.Atoi(raw); err == nil {
			return value{text: raw, kind: "int"}, nil
		}
		return value{}, fmt.Errorf("unsupported scalar %q", raw)
	}
}

func parseStringArray(raw string) ([]string, error) {
	if !strings.HasSuffix(raw, "]") {
		return nil, fmt.Errorf("unterminated array")
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

func recordError(record record, err error) error {
	table := "root"
	if record.table != "" {
		table = record.table
	}
	return fmt.Errorf("%s table key %q on line %d: %w", table, record.key, record.line, err)
}

func requireKind(v value, want string) error {
	if v.kind != want {
		return fmt.Errorf("expected %s, got %s", want, v.kind)
	}
	return nil
}

func assignLibrary(library *Library, key string, v value) error {
	if err := requireKind(v, "string"); err != nil {
		return err
	}
	switch key {
	case "id":
		library.ID = v.text
	case "title":
		library.Title = v.text
	case "description":
		library.Description = v.text
	case "scope":
		library.Scope = v.text
	case "status":
		library.Status = v.text
	default:
		return fmt.Errorf("unsupported catalog key")
	}
	return nil
}

func assignKnowledgeBaseRef(ref *KnowledgeBaseRef, key string, v value) error {
	if err := requireKind(v, "string"); err != nil {
		return err
	}
	switch key {
	case "id":
		ref.ID = v.text
	case "path":
		ref.Path = v.text
	case "catalog":
		ref.Catalog = v.text
	case "title":
		ref.Title = v.text
	case "description":
		ref.Description = v.text
	case "status":
		ref.Status = v.text
	default:
		return fmt.Errorf("unsupported catalog key")
	}
	return nil
}

func assignLibraryView(view *LibraryView, key string, v value) error {
	switch key {
	case "id", "title", "description", "status":
		if err := requireKind(v, "string"); err != nil {
			return err
		}
	case "paths":
		if err := requireKind(v, "strings"); err != nil {
			return err
		}
	}
	switch key {
	case "id":
		view.ID = v.text
	case "title":
		view.Title = v.text
	case "description":
		view.Description = v.text
	case "status":
		view.Status = v.text
	case "paths":
		view.Paths = append([]string(nil), v.strings...)
	default:
		return fmt.Errorf("unsupported catalog key")
	}
	return nil
}

func assignKnowledgeBase(kb *KnowledgeBase, key string, v value) error {
	switch key {
	case "id", "path", "title", "description", "purpose", "audience", "profile", "default_trust", "status":
		if err := requireKind(v, "string"); err != nil {
			return err
		}
	}
	switch key {
	case "id":
		kb.ID = v.text
	case "path":
		kb.Path = v.text
	case "title":
		kb.Title = v.text
	case "description":
		kb.Description = v.text
	case "purpose":
		kb.Purpose = v.text
	case "audience":
		kb.Audience = v.text
	case "profile":
		kb.Profile = v.text
	case "default_trust":
		kb.DefaultTrust = v.text
	case "default_writable":
		if err := requireKind(v, "bool"); err != nil {
			return err
		}
		kb.DefaultWritable = v.text == "true"
	case "status":
		kb.Status = v.text
	default:
		return fmt.Errorf("unsupported catalog key")
	}
	return nil
}

func assignBundleLink(link *BundleLink, key string, v value) error {
	switch key {
	case "id", "path", "source", "kind", "version", "revision", "title", "description", "trust", "status", "profile", "when_to_use", "when_not_to_use":
		if err := requireKind(v, "string"); err != nil {
			return err
		}
	case "writable":
		if err := requireKind(v, "bool"); err != nil {
			return err
		}
	case "priority":
		if err := requireKind(v, "int"); err != nil {
			return err
		}
	}
	switch key {
	case "id":
		link.ID = v.text
	case "path":
		link.Path = v.text
	case "source":
		link.Source = v.text
	case "kind":
		link.Kind = v.text
	case "writable":
		link.Writable = v.text == "true"
	case "version":
		link.Version = v.text
	case "revision":
		link.Revision = v.text
	case "title":
		link.Title = v.text
	case "description":
		link.Description = v.text
	case "trust":
		link.Trust = v.text
	case "status":
		link.Status = v.text
	case "profile":
		link.Profile = v.text
	case "priority":
		link.Priority, _ = strconv.Atoi(v.text)
	case "when_to_use":
		link.WhenToUse = v.text
	case "when_not_to_use":
		link.WhenNotToUse = v.text
	default:
		return fmt.Errorf("unsupported catalog key")
	}
	return nil
}

func formatLibrary(library Library) string {
	var b strings.Builder
	writeString(&b, "id", library.ID)
	writeOptionalString(&b, "title", library.Title)
	writeOptionalString(&b, "description", library.Description)
	writeOptionalString(&b, "scope", library.Scope)
	writeOptionalString(&b, "status", library.Status)
	refs := append([]KnowledgeBaseRef(nil), library.KnowledgeBases...)
	sort.Slice(refs, func(i, j int) bool { return refs[i].Path < refs[j].Path })
	for _, ref := range refs {
		b.WriteString("\n[[knowledge_bases]]\n")
		writeString(&b, "id", ref.ID)
		writeString(&b, "path", ref.Path)
		writeString(&b, "catalog", ref.Catalog)
		writeOptionalString(&b, "title", ref.Title)
		writeOptionalString(&b, "description", ref.Description)
		writeOptionalString(&b, "status", ref.Status)
	}
	views := append([]LibraryView(nil), library.Views...)
	sort.Slice(views, func(i, j int) bool { return views[i].ID < views[j].ID })
	for _, view := range views {
		b.WriteString("\n[[views]]\n")
		writeString(&b, "id", view.ID)
		writeOptionalString(&b, "title", view.Title)
		writeOptionalString(&b, "description", view.Description)
		writeOptionalString(&b, "status", view.Status)
		writeStringArray(&b, "paths", view.Paths)
	}
	return b.String()
}

func formatKnowledgeBase(kb KnowledgeBase) string {
	var b strings.Builder
	writeString(&b, "id", kb.ID)
	writeString(&b, "path", kb.Path)
	writeOptionalString(&b, "title", kb.Title)
	writeOptionalString(&b, "description", kb.Description)
	writeOptionalString(&b, "purpose", kb.Purpose)
	writeOptionalString(&b, "audience", kb.Audience)
	writeOptionalString(&b, "profile", kb.Profile)
	writeOptionalString(&b, "default_trust", kb.DefaultTrust)
	if kb.DefaultWritable {
		writeBool(&b, "default_writable", kb.DefaultWritable)
	}
	writeOptionalString(&b, "status", kb.Status)
	links := append([]BundleLink(nil), kb.Bundles...)
	sort.Slice(links, func(i, j int) bool { return links[i].Path < links[j].Path })
	for _, link := range links {
		b.WriteString("\n[[bundles]]\n")
		writeString(&b, "id", link.ID)
		writeString(&b, "path", link.Path)
		writeString(&b, "source", link.Source)
		writeString(&b, "kind", defaultString(link.Kind, "local"))
		writeBool(&b, "writable", link.Writable)
		writeOptionalString(&b, "version", link.Version)
		writeOptionalString(&b, "revision", link.Revision)
		writeOptionalString(&b, "title", link.Title)
		writeOptionalString(&b, "description", link.Description)
		writeOptionalString(&b, "trust", link.Trust)
		writeOptionalString(&b, "status", link.Status)
		writeOptionalString(&b, "profile", link.Profile)
		if link.Priority != 0 {
			writeInt(&b, "priority", link.Priority)
		}
		writeOptionalString(&b, "when_to_use", link.WhenToUse)
		writeOptionalString(&b, "when_not_to_use", link.WhenNotToUse)
	}
	return b.String()
}

func writeOptionalString(b *strings.Builder, key string, value string) {
	if value != "" {
		writeString(b, key, value)
	}
}

func writeString(b *strings.Builder, key string, value string) {
	b.WriteString(key)
	b.WriteString(" = ")
	b.WriteString(strconv.Quote(value))
	b.WriteByte('\n')
}

func writeBool(b *strings.Builder, key string, value bool) {
	b.WriteString(key)
	b.WriteString(" = ")
	if value {
		b.WriteString("true\n")
	} else {
		b.WriteString("false\n")
	}
}

func writeInt(b *strings.Builder, key string, value int) {
	b.WriteString(key)
	b.WriteString(" = ")
	b.WriteString(strconv.Itoa(value))
	b.WriteByte('\n')
}

func writeStringArray(b *strings.Builder, key string, values []string) {
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

func normalizeCatalogPath(input string) (string, error) {
	if input == "" || !strings.HasPrefix(input, "/") {
		return "", fmt.Errorf("path must start with /")
	}
	for _, part := range strings.Split(input, "/") {
		if part == "." || part == ".." {
			return "", fmt.Errorf("path must not contain . or ..")
		}
	}
	return path.Clean(input), nil
}

func defaultString(value string, fallback string) string {
	if value == "" {
		return fallback
	}
	return value
}
