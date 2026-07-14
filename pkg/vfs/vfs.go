package vfs

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

type TargetKind string

const (
	TargetVirtualRoot TargetKind = "virtual_root"
	TargetBundle      TargetKind = "bundle"
	TargetPath        TargetKind = "path"
	TargetConcept     TargetKind = "concept"
)

type Error struct {
	Code    string
	Message string
}

func (e *Error) Error() string {
	return e.Message
}

type Mount struct {
	MountPath    string        `json:"mount_path"`
	Source       string        `json:"source"`
	Kind         string        `json:"kind"`
	Writable     bool          `json:"writable"`
	Title        string        `json:"title,omitempty"`
	Description  string        `json:"description,omitempty"`
	WhenToUse    string        `json:"when_to_use,omitempty"`
	WhenNotToUse string        `json:"when_not_to_use,omitempty"`
	Version      string        `json:"version,omitempty"`
	Ref          string        `json:"ref,omitempty"`
	Revision     string        `json:"revision,omitempty"`
	VersionSet   bool          `json:"-"`
	RefSet       bool          `json:"-"`
	RevisionSet  bool          `json:"-"`
	Trust        string        `json:"trust,omitempty"`
	SourceStatus *SourceStatus `json:"source_status,omitempty"`
	RegistryPath string        `json:"-"`
	SourcePath   string        `json:"-"`
}

type SourceStatus struct {
	MountPath         string         `json:"mount_path"`
	Source            string         `json:"source"`
	Kind              string         `json:"kind"`
	SelectorMode      string         `json:"selector_mode"`
	IntentRef         string         `json:"intent_ref,omitempty"`
	IntentRevision    string         `json:"intent_revision,omitempty"`
	SelectedRevision  string         `json:"selected_revision,omitempty"`
	SnapshotAvailable bool           `json:"snapshot_available"`
	RefreshDue        bool           `json:"refresh_due"`
	Stale             bool           `json:"stale"`
	LastAttemptAt     string         `json:"last_attempt_at,omitempty"`
	LastSuccessAt     string         `json:"last_success_at,omitempty"`
	LastErrorCode     string         `json:"last_error_code,omitempty"`
	Warning           *SourceWarning `json:"warning,omitempty"`
}

type SourceWarning struct {
	Code      string `json:"code"`
	Message   string `json:"message"`
	MountPath string `json:"mount_path"`
}

type LoadOptions struct {
	MountFile string
	Root      string
	WorkDir   string
}

type Target struct {
	Kind      TargetKind
	Path      string
	Mount     Mount
	ConceptID string
	RelPath   string
	Exists    bool
}

func NormalizePath(input string) (string, error) {
	if input == "" || !strings.HasPrefix(input, "/") {
		return "", &Error{Code: "invalid_path", Message: "Factile path must start with /"}
	}
	normalizedInput := strings.ReplaceAll(input, "\\", "/")
	for _, part := range strings.Split(normalizedInput, "/") {
		if part == "." || part == ".." {
			return "", &Error{Code: "invalid_path", Message: "Factile path must not contain . or .. segments"}
		}
	}
	clean := path.Clean(normalizedInput)
	if clean == "." {
		clean = "/"
	}
	if clean != "/" && strings.HasSuffix(clean, ".md") {
		clean = strings.TrimSuffix(clean, ".md")
	}
	for _, part := range strings.Split(strings.TrimPrefix(clean, "/"), "/") {
		if strings.EqualFold(part, ".factile") || strings.EqualFold(part, ".git") {
			return "", &Error{Code: "invalid_path", Message: "Factile path must not address internal .factile or .git directories"}
		}
	}
	return clean, nil
}

func ValidateMountPath(input string) (string, error) {
	p, err := NormalizePath(input)
	if err != nil {
		return "", err
	}
	if p == "/" {
		return "", &Error{Code: "invalid_path", Message: "Mount path must not be /"}
	}
	return p, nil
}

func LoadMounts(opts LoadOptions) ([]Mount, error) {
	if opts.MountFile != "" {
		return LoadRegistryFile(opts.MountFile)
	}
	workDir, ok, err := FindRoot(opts)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, NoActiveRootError()
	}
	var merged []Mount
	descriptorMounts, err := LoadDescriptorMounts(workDir)
	if err != nil {
		return nil, err
	}
	if len(descriptorMounts) > 0 {
		merged = overlayMounts(merged, descriptorMounts)
	}
	if err := validateRootMountConflicts(workDir, merged); err != nil {
		return nil, err
	}
	merged = append(merged, rootMount(workDir))
	sortMounts(merged)
	return merged, nil
}

func overlayMounts(base []Mount, overlay []Mount) []Mount {
	byPath := map[string]int{}
	for i, mount := range base {
		byPath[mount.MountPath] = i
	}
	for _, mount := range overlay {
		if i, ok := byPath[mount.MountPath]; ok {
			base[i] = mount
		} else {
			byPath[mount.MountPath] = len(base)
			base = append(base, mount)
		}
	}
	return base
}

func resolveLocalSource(source string, baseDir string, workDir string) string {
	candidate := source
	if !filepath.IsAbs(candidate) {
		candidate = filepath.Join(baseDir, candidate)
	}
	abs, err := filepath.Abs(candidate)
	if err == nil && fileExists(abs) {
		return filepath.Clean(abs)
	}
	if !filepath.IsAbs(source) {
		fallback := filepath.Join(workDir, source)
		abs, err := filepath.Abs(fallback)
		if err == nil && fileExists(abs) {
			return filepath.Clean(abs)
		}
	}
	if err == nil {
		return filepath.Clean(abs)
	}
	return filepath.Clean(candidate)
}

func defaultWorkDir(workDir string) (string, error) {
	if workDir == "" {
		return os.Getwd()
	}
	return filepath.Abs(workDir)
}

func RequireRoot(opts LoadOptions) (string, error) {
	root, ok, err := FindRoot(opts)
	if err != nil {
		return "", err
	}
	if !ok {
		return "", NoActiveRootError()
	}
	return root, nil
}

func NoActiveRootError() error {
	return &Error{Code: "no_active_root", Message: "No active Factile root found. Run factile init to create one."}
}

func LoadRegistryFile(filename string) ([]Mount, error) {
	f, err := os.Open(filename)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	registryPath, err := filepath.Abs(filename)
	if err != nil {
		return nil, err
	}
	baseDir := filepath.Dir(registryPath)
	var mounts []Mount
	seen := map[string]bool{}
	var current *Mount
	scanner := bufio.NewScanner(f)
	for lineNo := 1; scanner.Scan(); lineNo++ {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if strings.HasPrefix(line, "[") {
			if !strings.HasPrefix(line, `[mounts."`) || !strings.HasSuffix(line, `"]`) {
				return nil, fmt.Errorf("unsupported table on line %d", lineNo)
			}
			mountPath := strings.TrimSuffix(strings.TrimPrefix(line, `[mounts."`), `"]`)
			normalized, err := ValidateMountPath(mountPath)
			if err != nil {
				return nil, err
			}
			if seen[normalized] {
				return nil, &Error{Code: "validation_failed", Message: "Duplicate mount path in registry: " + normalized}
			}
			seen[normalized] = true
			mounts = append(mounts, Mount{MountPath: normalized, Kind: "local", RegistryPath: registryPath})
			current = &mounts[len(mounts)-1]
			continue
		}
		if current == nil {
			return nil, fmt.Errorf("mount property before table on line %d", lineNo)
		}
		parts := strings.SplitN(line, "=", 2)
		if len(parts) != 2 {
			return nil, fmt.Errorf("invalid TOML assignment on line %d", lineNo)
		}
		key := strings.TrimSpace(parts[0])
		rawValue := strings.TrimSpace(parts[1])
		switch key {
		case "source":
			value, err := parseMountString(rawValue, key, lineNo)
			if err != nil {
				return nil, err
			}
			current.Source = value
		case "kind":
			value, err := parseMountString(rawValue, key, lineNo)
			if err != nil {
				return nil, err
			}
			current.Kind = value
		case "writable":
			value, err := parseMountBool(rawValue, key, lineNo)
			if err != nil {
				return nil, err
			}
			current.Writable = value
		case "title":
			value, err := parseMountString(rawValue, key, lineNo)
			if err != nil {
				return nil, err
			}
			current.Title = value
		case "description":
			value, err := parseMountString(rawValue, key, lineNo)
			if err != nil {
				return nil, err
			}
			current.Description = value
		case "revision":
			if _, err := parseMountString(rawValue, key, lineNo); err != nil {
				return nil, err
			}
		default:
			return nil, fmt.Errorf("unsupported mount key %q on line %d", key, lineNo)
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	for i := range mounts {
		if mounts[i].Source == "" {
			return nil, fmt.Errorf("mount %s has no source", mounts[i].MountPath)
		}
		if mounts[i].Kind == "" {
			mounts[i].Kind = "local"
		}
		classification, err := ClassifySource(mounts[i].Source)
		if err != nil {
			return nil, err
		}
		if mounts[i].Kind == SourceKindGit || classification.Kind == SourceKindGit {
			return nil, &Error{Code: "unsupported_source", Message: "Git sources are not supported with --mount-file; use an active Factile root."}
		}
		if mounts[i].Kind == "local" {
			source := mounts[i].Source
			if !filepath.IsAbs(source) {
				source = filepath.Join(baseDir, source)
			}
			abs, err := filepath.Abs(source)
			if err != nil {
				return nil, err
			}
			if !fileExists(abs) && !filepath.IsAbs(mounts[i].Source) {
				if cwdAbs, cwdErr := filepath.Abs(mounts[i].Source); cwdErr == nil && fileExists(cwdAbs) {
					abs = cwdAbs
				}
			}
			mounts[i].SourcePath = filepath.Clean(abs)
		}
	}
	sortMounts(mounts)
	return mounts, nil
}

func parseMountString(raw string, key string, lineNo int) (string, error) {
	if !strings.HasPrefix(raw, `"`) {
		return "", fmt.Errorf("mount key %q on line %d expects quoted string", key, lineNo)
	}
	value, err := strconv.Unquote(raw)
	if err != nil {
		return "", fmt.Errorf("invalid string for mount key %q on line %d: %w", key, lineNo, err)
	}
	return value, nil
}

func parseMountBool(raw string, key string, lineNo int) (bool, error) {
	switch raw {
	case "true":
		return true, nil
	case "false":
		return false, nil
	default:
		return false, fmt.Errorf("mount key %q on line %d expects true or false", key, lineNo)
	}
}

func Resolve(mounts []Mount, input string) (Target, error) {
	normalized, err := NormalizePath(input)
	if err != nil {
		return Target{}, err
	}
	if normalized == "/" {
		return Target{Kind: TargetVirtualRoot, Path: "/"}, nil
	}
	var selected *Mount
	for i := range mounts {
		mount := mounts[i]
		if mountMatchesPath(mount.MountPath, normalized) {
			if selected == nil || len(mount.MountPath) > len(selected.MountPath) {
				selected = &mounts[i]
			}
		}
	}
	if selected == nil {
		return Target{}, &Error{Code: "mount_not_found", Message: "Mount not found for path: " + normalized}
	}
	target := Target{Kind: TargetBundle, Path: normalized, Mount: *selected, Exists: true}
	if normalized == selected.MountPath {
		return target, nil
	}
	rel := mountRelativePath(*selected, normalized)
	target.RelPath = rel
	target.ConceptID = rel
	if selected.SourcePath == "" {
		target.Kind = TargetPath
		return target, nil
	}
	file := filepath.Join(selected.SourcePath, filepath.FromSlash(rel)+".md")
	dir := filepath.Join(selected.SourcePath, filepath.FromSlash(rel))
	fileExists := regularFileExists(file)
	dirExists := dirExists(dir)
	if fileExists && dirExists {
		return Target{}, &Error{Code: "ambiguous_target", Message: "Path is both concept and directory: " + normalized}
	}
	if fileExists {
		target.Kind = TargetConcept
		target.Exists = true
		return target, nil
	}
	if dirExists {
		target.Kind = TargetPath
		target.Exists = true
		return target, nil
	}
	if selected.MountPath == "/" && hasDescendantMount(mounts, normalized) {
		return Target{}, &Error{Code: "mount_not_found", Message: "Mount not found for path: " + normalized}
	}
	target.Kind = TargetPath
	target.Exists = false
	return target, nil
}

func MountsForKnowledge(mounts []Mount) []Mount {
	out := make([]Mount, len(mounts))
	for i, mount := range mounts {
		out[i] = mount
		if filepath.IsAbs(out[i].Source) {
			out[i].Source = out[i].Kind
		}
		out[i].SourcePath = ""
		out[i].RegistryPath = ""
	}
	return out
}

func sortMounts(mounts []Mount) {
	sort.Slice(mounts, func(i, j int) bool {
		return mounts[i].MountPath < mounts[j].MountPath
	})
}

func rootMount(root string) Mount {
	return Mount{MountPath: "/", Source: ".", Kind: "local", Writable: true, SourcePath: root}
}

func validateRootMountConflicts(root string, mounts []Mount) error {
	for _, mount := range mounts {
		if mount.MountPath == "/" {
			continue
		}
		rel := strings.TrimPrefix(mount.MountPath, "/")
		file := filepath.Join(root, filepath.FromSlash(rel)+".md")
		dir := filepath.Join(root, filepath.FromSlash(rel))
		if regularFileExists(file) || dirExists(dir) {
			return &Error{Code: "ambiguous_target", Message: "Path is both root path and mount: " + mount.MountPath}
		}
	}
	return nil
}

func mountMatchesPath(mountPath string, normalized string) bool {
	if mountPath == "/" {
		return normalized != "/"
	}
	return normalized == mountPath || strings.HasPrefix(normalized, mountPath+"/")
}

func mountRelativePath(mount Mount, normalized string) string {
	if mount.MountPath == "/" {
		return strings.TrimPrefix(normalized, "/")
	}
	return strings.TrimPrefix(normalized, mount.MountPath+"/")
}

func hasDescendantMount(mounts []Mount, normalized string) bool {
	for _, mount := range mounts {
		if mount.MountPath == "/" {
			continue
		}
		if strings.HasPrefix(mount.MountPath, normalized+"/") {
			return true
		}
	}
	return false
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func regularFileExists(path string) bool {
	info, err := os.Lstat(path)
	return err == nil && info.Mode()&os.ModeSymlink == 0 && info.Mode().IsRegular()
}

func dirExists(path string) bool {
	info, err := os.Lstat(path)
	return err == nil && info.Mode()&os.ModeSymlink == 0 && info.IsDir()
}

func RegistryPathForWrite(opts LoadOptions) (string, error) {
	if opts.MountFile != "" {
		return opts.MountFile, nil
	}
	return "", &Error{Code: "unsupported_command", Message: "Mount registry writes require --mount-file"}
}

func WriteRegistryFile(filename string, mounts []Mount) error {
	if err := os.MkdirAll(filepath.Dir(filename), 0o755); err != nil {
		return err
	}
	sortMounts(mounts)
	var b strings.Builder
	for _, mount := range mounts {
		b.WriteString(`[mounts."`)
		b.WriteString(mount.MountPath)
		b.WriteString("\"]\n")
		b.WriteString(`source = "`)
		b.WriteString(strings.ReplaceAll(mount.Source, `"`, `\"`))
		b.WriteString("\"\n")
		b.WriteString(`kind = "`)
		if mount.Kind == "" {
			b.WriteString("local")
		} else {
			b.WriteString(mount.Kind)
		}
		b.WriteString("\"\n")
		if mount.Writable {
			b.WriteString("writable = true\n")
		} else {
			b.WriteString("writable = false\n")
		}
		writeOptionalMountDescriptorString(&b, "title", mount.Title)
		writeOptionalMountDescriptorString(&b, "description", mount.Description)
		b.WriteString("\n")
	}
	return os.WriteFile(filename, []byte(b.String()), 0o644)
}
