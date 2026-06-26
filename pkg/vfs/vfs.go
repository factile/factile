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

	"github.com/factile/factile/pkg/catalog"
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
	MountPath    string `json:"mount_path"`
	Source       string `json:"source"`
	Kind         string `json:"kind"`
	Writable     bool   `json:"writable"`
	RegistryPath string `json:"-"`
	SourcePath   string `json:"-"`
}

type LoadOptions struct {
	MountFile string
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
	workDir := opts.WorkDir
	if workDir == "" {
		var err error
		workDir, err = os.Getwd()
		if err != nil {
			return nil, err
		}
	}
	if opts.MountFile != "" {
		return LoadRegistryFile(opts.MountFile)
	}
	var merged []Mount
	userPath := UserRegistryPath()
	if userPath != "" {
		if fileExists(userPath) {
			mounts, err := LoadRegistryFile(userPath)
			if err != nil {
				return nil, err
			}
			merged = append(merged, mounts...)
		}
	}
	catalogMounts, err := LoadCatalogMounts(LoadOptions{WorkDir: workDir})
	if err != nil {
		return nil, err
	}
	if len(catalogMounts) > 0 {
		merged = overlayMounts(merged, catalogMounts)
	}
	projectPath := filepath.Join(workDir, ".factile", "mounts.toml")
	if fileExists(projectPath) {
		mounts, err := LoadRegistryFile(projectPath)
		if err != nil {
			return nil, err
		}
		merged = overlayMounts(merged, mounts)
	}
	sortMounts(merged)
	return merged, nil
}

func LoadCatalogMounts(opts LoadOptions) ([]Mount, error) {
	workDir := opts.WorkDir
	if workDir == "" {
		var err error
		workDir, err = os.Getwd()
		if err != nil {
			return nil, err
		}
	}
	libraryPath := filepath.Join(workDir, ".factile", "library.toml")
	if !fileExists(libraryPath) {
		return nil, nil
	}
	library, err := catalog.LoadLibraryFile(libraryPath)
	if err != nil {
		return nil, err
	}
	libraryDir := filepath.Dir(libraryPath)
	var mounts []Mount
	seen := map[string]bool{}
	for _, ref := range library.KnowledgeBases {
		kbPath := ref.Catalog
		if !filepath.IsAbs(kbPath) {
			kbPath = filepath.Join(libraryDir, kbPath)
		}
		kb, err := catalog.LoadKnowledgeBaseFile(kbPath)
		if err != nil {
			return nil, err
		}
		kbDir := filepath.Dir(kbPath)
		for _, link := range kb.Bundles {
			mountPath, err := ValidateMountPath(link.Path)
			if err != nil {
				return nil, err
			}
			if seen[mountPath] {
				return nil, &Error{Code: "validation_failed", Message: "Duplicate catalog mount path: " + mountPath}
			}
			seen[mountPath] = true
			kind := link.Kind
			if kind == "" {
				kind = "local"
			}
			mount := Mount{
				MountPath:    mountPath,
				Source:       link.Source,
				Kind:         kind,
				Writable:     link.Writable,
				RegistryPath: kbPath,
			}
			if kind == "local" {
				mount.SourcePath = resolveLocalSource(link.Source, kbDir, workDir)
			}
			mounts = append(mounts, mount)
		}
	}
	sortMounts(mounts)
	return mounts, nil
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

func UserRegistryPath() string {
	dir, err := os.UserConfigDir()
	if err != nil || dir == "" {
		return ""
	}
	return filepath.Join(dir, "factile", "mounts.toml")
}

func ProjectRegistryPath(workDir string) string {
	if workDir == "" {
		workDir, _ = os.Getwd()
	}
	return filepath.Join(workDir, ".factile", "mounts.toml")
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
			mounts = append(mounts, Mount{MountPath: normalized, Kind: "local", Writable: true, RegistryPath: registryPath})
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
		if normalized == mount.MountPath || strings.HasPrefix(normalized, mount.MountPath+"/") {
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
	rel := strings.TrimPrefix(normalized, selected.MountPath+"/")
	target.RelPath = rel
	target.ConceptID = rel
	if selected.Kind != "local" {
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

func RegistryPathForWrite(opts LoadOptions) string {
	if opts.MountFile != "" {
		return opts.MountFile
	}
	return ProjectRegistryPath(opts.WorkDir)
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
			b.WriteString("writable = true\n\n")
		} else {
			b.WriteString("writable = false\n\n")
		}
	}
	return os.WriteFile(filename, []byte(b.String()), 0o644)
}
