package vfs

import (
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"

	"github.com/BurntSushi/toml"
)

const (
	ManifestFilename = "factile.toml"
	StateDirname     = ".factile"

	ErrNoActiveWorkspace = "no_active_workspace"
	ErrInvalidWorkspace  = "invalid_workspace"
	ErrInvalidBundle     = "invalid_bundle"
)

type Manifest struct {
	Version   int              `json:"version"`
	Workspace *WorkspaceConfig `json:"workspace,omitempty"`
	Bundle    *BundleConfig    `json:"bundle,omitempty"`
	Defaults  *BundleDefaults  `json:"defaults,omitempty"`
}

type WorkspaceConfig struct {
	Root string `json:"root"`
}

type BundleConfig struct {
	Name        string `json:"name"`
	Title       string `json:"title,omitempty"`
	Description string `json:"description,omitempty"`
	WhenToUse   string `json:"when_to_use,omitempty"`
}

type BundleDefaults struct {
	Format string `json:"format,omitempty"`
}

type WorkspaceContext struct {
	WorkspaceDir  string `json:"workspace_dir"`
	RootBundleDir string `json:"root_bundle_dir"`
	StateDir      string `json:"state_dir"`
}

type ResolveWorkspaceOptions struct {
	Workspace string
	WorkDir   string
}

func ManifestPath(dir string) string {
	return filepath.Join(dir, ManifestFilename)
}

func LoadManifest(dir string) (Manifest, error) {
	exists, err := regularManifestExists(dir)
	if err != nil {
		return Manifest{}, errors.New("factile.toml must be a regular file")
	}
	if !exists {
		return Manifest{}, &os.PathError{Op: "open", Path: ManifestPath(dir), Err: os.ErrNotExist}
	}
	raw, err := decodeManifest(ManifestPath(dir))
	if err != nil {
		return Manifest{}, err
	}
	return parseManifest(raw)
}

func ResolveWorkspace(opts ResolveWorkspaceOptions) (WorkspaceContext, error) {
	if opts.Workspace != "" {
		workspaceDir, err := canonicalDir(opts.Workspace)
		if err != nil {
			return WorkspaceContext{}, layoutError(
				ErrInvalidWorkspace,
				"Selected directory is not a Factile workspace.",
				map[string]string{"workspace": opts.Workspace},
			)
		}
		manifest, err := loadWorkspaceCandidate(workspaceDir)
		if err != nil {
			return WorkspaceContext{}, err
		}
		if manifest.Workspace == nil {
			return WorkspaceContext{}, layoutError(
				ErrInvalidWorkspace,
				"Selected directory is not a Factile workspace.",
				map[string]string{"manifest": ManifestPath(workspaceDir)},
			)
		}
		return resolveWorkspaceRoot(workspaceDir, manifest)
	}

	start, err := workspaceStartDir(opts.WorkDir)
	if err != nil {
		return WorkspaceContext{}, layoutError(
			ErrInvalidWorkspace,
			"Working directory is not available.",
			map[string]string{"workdir": opts.WorkDir},
		)
	}
	for dir := start; ; dir = filepath.Dir(dir) {
		exists, err := regularManifestExists(dir)
		if err != nil {
			return WorkspaceContext{}, layoutError(
				ErrInvalidWorkspace,
				"Workspace factile.toml must be a regular file.",
				map[string]string{"manifest": ManifestPath(dir)},
			)
		}
		if exists {
			raw, err := decodeManifest(ManifestPath(dir))
			if err != nil {
				return WorkspaceContext{}, layoutError(
					ErrInvalidWorkspace,
					"Workspace factile.toml is not valid TOML.",
					map[string]string{"manifest": ManifestPath(dir)},
				)
			}
			if _, ok := tableValue(raw, "workspace"); ok {
				manifest, err := parseManifest(raw)
				if err != nil {
					return WorkspaceContext{}, manifestLayoutError(ErrInvalidWorkspace, ManifestPath(dir), err)
				}
				return resolveWorkspaceRoot(dir, manifest)
			}
		}

		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
	}

	details := map[string]string{}
	if legacy := findLegacyRoot(start); legacy != nil {
		details["legacy_path"] = legacy.ConfigPath
		details["migration"] = legacy.Migration
	}
	return WorkspaceContext{}, layoutError(
		ErrNoActiveWorkspace,
		"No active Factile workspace.",
		details,
	)
}

func decodeManifest(filename string) (map[string]any, error) {
	var raw map[string]any
	if _, err := toml.DecodeFile(filename, &raw); err != nil {
		return nil, fmt.Errorf("factile.toml is not valid TOML: %w", err)
	}
	if raw == nil {
		raw = map[string]any{}
	}
	return raw, nil
}

func parseManifest(raw map[string]any) (Manifest, error) {
	if err := rejectUnknownKeys(raw, "", "version", "workspace", "bundle", "defaults"); err != nil {
		return Manifest{}, err
	}

	version, ok := raw["version"].(int64)
	if !ok || version != 2 {
		return Manifest{}, errors.New("Factile manifest version must be the integer 2.")
	}
	manifest := Manifest{Version: int(version)}

	if table, ok := tableValue(raw, "workspace"); ok {
		if err := rejectUnknownKeys(table, "workspace", "root"); err != nil {
			return Manifest{}, err
		}
		root, ok := table["root"].(string)
		if !ok || root == "" {
			return Manifest{}, errors.New("Workspace root must be a relative directory string.")
		}
		manifest.Workspace = &WorkspaceConfig{Root: root}
	} else if _, exists := raw["workspace"]; exists {
		return Manifest{}, errors.New("Workspace must be a table.")
	}

	if table, ok := tableValue(raw, "bundle"); ok {
		if err := rejectUnknownKeys(table, "bundle", "name", "title", "description", "when_to_use"); err != nil {
			return Manifest{}, err
		}
		name, ok := table["name"].(string)
		if !ok || strings.TrimSpace(name) == "" {
			return Manifest{}, errors.New("Bundle name must be a non-empty string.")
		}
		bundle := &BundleConfig{Name: name}
		var err error
		if bundle.Title, err = optionalString(table, "title", "Bundle title"); err != nil {
			return Manifest{}, err
		}
		if bundle.Description, err = optionalString(table, "description", "Bundle description"); err != nil {
			return Manifest{}, err
		}
		if bundle.WhenToUse, err = optionalString(table, "when_to_use", "Bundle when_to_use"); err != nil {
			return Manifest{}, err
		}
		manifest.Bundle = bundle
	} else if _, exists := raw["bundle"]; exists {
		return Manifest{}, errors.New("Bundle must be a table.")
	}

	if table, ok := tableValue(raw, "defaults"); ok {
		if err := rejectUnknownKeys(table, "defaults", "format"); err != nil {
			return Manifest{}, err
		}
		format, err := optionalString(table, "format", "Bundle default format")
		if err != nil {
			return Manifest{}, err
		}
		manifest.Defaults = &BundleDefaults{Format: format}
	} else if _, exists := raw["defaults"]; exists {
		return Manifest{}, errors.New("Defaults must be a table.")
	}

	if manifest.Workspace == nil && manifest.Bundle == nil {
		return Manifest{}, errors.New("Factile manifest must contain a workspace or bundle table.")
	}
	if manifest.Defaults != nil && manifest.Bundle == nil {
		return Manifest{}, errors.New("Bundle defaults require a bundle table.")
	}
	if manifest.Workspace != nil && manifest.Bundle != nil && manifest.Workspace.Root != "." {
		return Manifest{}, errors.New("A combined workspace and bundle manifest requires workspace.root = \".\".")
	}
	return manifest, nil
}

func resolveWorkspaceRoot(workspaceDir string, manifest Manifest) (WorkspaceContext, error) {
	root := manifest.Workspace.Root
	if !validWorkspaceRoot(root) {
		return WorkspaceContext{}, layoutError(
			ErrInvalidWorkspace,
			"Workspace root must remain inside the workspace.",
			map[string]string{"root": root},
		)
	}

	rootBundleDir, err := canonicalDir(filepath.Join(workspaceDir, filepath.FromSlash(root)))
	if err != nil {
		return WorkspaceContext{}, invalidRootBundle(filepath.Join(workspaceDir, filepath.FromSlash(root)))
	}
	contained, err := pathContainedBy(workspaceDir, rootBundleDir)
	if err != nil || !contained {
		return WorkspaceContext{}, layoutError(
			ErrInvalidWorkspace,
			"Workspace root must remain inside the workspace.",
			map[string]string{"root": root},
		)
	}
	rootRel, err := filepath.Rel(workspaceDir, rootBundleDir)
	if err != nil || hasPrivatePathSegment(rootRel) {
		return WorkspaceContext{}, layoutError(
			ErrInvalidWorkspace,
			"Workspace root must not use private .factile or .git directories.",
			map[string]string{"root": root},
		)
	}

	bundleManifest, err := LoadManifest(rootBundleDir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return WorkspaceContext{}, invalidRootBundle(rootBundleDir)
		}
		return WorkspaceContext{}, manifestLayoutError(ErrInvalidBundle, ManifestPath(rootBundleDir), err)
	}
	if bundleManifest.Bundle == nil {
		return WorkspaceContext{}, invalidRootBundle(rootBundleDir)
	}

	return WorkspaceContext{
		WorkspaceDir:  workspaceDir,
		RootBundleDir: rootBundleDir,
		StateDir:      filepath.Join(workspaceDir, StateDirname),
	}, nil
}

func loadWorkspaceCandidate(dir string) (Manifest, error) {
	exists, err := regularManifestExists(dir)
	if err != nil || !exists {
		return Manifest{}, layoutError(
			ErrInvalidWorkspace,
			"Selected directory is not a Factile workspace.",
			map[string]string{"manifest": ManifestPath(dir)},
		)
	}
	manifest, err := LoadManifest(dir)
	if err != nil {
		return Manifest{}, manifestLayoutError(ErrInvalidWorkspace, ManifestPath(dir), err)
	}
	return manifest, nil
}

func workspaceStartDir(workDir string) (string, error) {
	if workDir == "" {
		var err error
		workDir, err = os.Getwd()
		if err != nil {
			return "", err
		}
	}
	return canonicalDir(workDir)
}

func canonicalDir(input string) (string, error) {
	abs, err := filepath.Abs(input)
	if err != nil {
		return "", err
	}
	canonical, err := filepath.EvalSymlinks(abs)
	if err != nil {
		return "", err
	}
	info, err := os.Stat(canonical)
	if err != nil {
		return "", err
	}
	if !info.IsDir() {
		return "", fmt.Errorf("not a directory: %s", input)
	}
	return filepath.Clean(canonical), nil
}

func regularManifestExists(dir string) (bool, error) {
	filename := ManifestPath(dir)
	info, err := os.Lstat(filename)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if !info.Mode().IsRegular() {
		return false, errors.New("factile.toml is not a regular file")
	}
	return true, nil
}

func validWorkspaceRoot(root string) bool {
	if root == "." {
		return true
	}
	if root == "" || filepath.IsAbs(root) || isWindowsAbsolute(root) || strings.Contains(root, `\`) {
		return false
	}
	if path.Clean(root) != root {
		return false
	}
	for _, segment := range strings.Split(root, "/") {
		if segment == "" || segment == "." || segment == ".." || strings.EqualFold(segment, StateDirname) || strings.EqualFold(segment, ".git") {
			return false
		}
	}
	return true
}

func hasPrivatePathSegment(input string) bool {
	for _, segment := range strings.Split(filepath.ToSlash(input), "/") {
		if strings.EqualFold(segment, StateDirname) || strings.EqualFold(segment, ".git") {
			return true
		}
	}
	return false
}

func isWindowsAbsolute(input string) bool {
	return len(input) >= 3 && ((input[0] >= 'A' && input[0] <= 'Z') || (input[0] >= 'a' && input[0] <= 'z')) && input[1] == ':' && input[2] == '/'
}

func pathContainedBy(parent string, child string) (bool, error) {
	rel, err := filepath.Rel(parent, child)
	if err != nil {
		return false, err
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && !filepath.IsAbs(rel), nil
}

func tableValue(raw map[string]any, key string) (map[string]any, bool) {
	value, ok := raw[key]
	if !ok {
		return nil, false
	}
	table, ok := value.(map[string]any)
	return table, ok
}

func rejectUnknownKeys(values map[string]any, prefix string, allowed ...string) error {
	known := make(map[string]bool, len(allowed))
	for _, key := range allowed {
		known[key] = true
	}
	var unknown []string
	for key := range values {
		if !known[key] {
			if prefix != "" {
				key = prefix + "." + key
			}
			unknown = append(unknown, key)
		}
	}
	if len(unknown) == 0 {
		return nil
	}
	sort.Strings(unknown)
	return fmt.Errorf("Unsupported factile.toml field %q.", unknown[0])
}

func optionalString(values map[string]any, key string, label string) (string, error) {
	value, exists := values[key]
	if !exists {
		return "", nil
	}
	text, ok := value.(string)
	if !ok {
		return "", fmt.Errorf("%s must be a string.", label)
	}
	return text, nil
}

func manifestLayoutError(code string, manifestPath string, err error) error {
	message := err.Error()
	if strings.HasPrefix(message, "factile.toml is not valid TOML:") {
		if code == ErrInvalidWorkspace {
			message = "Workspace factile.toml is not valid TOML."
		} else {
			message = "Root bundle factile.toml is not valid TOML."
		}
	}
	return layoutError(code, message, map[string]string{"manifest": manifestPath})
}

func invalidRootBundle(rootBundleDir string) error {
	return layoutError(
		ErrInvalidBundle,
		"Workspace root bundle has no valid factile.toml.",
		map[string]string{"root_bundle": rootBundleDir},
	)
}

func layoutError(code string, message string, details map[string]string) error {
	if len(details) == 0 {
		details = nil
	}
	return &Error{Code: code, Message: message, Details: details}
}

type legacyRoot struct {
	ConfigPath string
	Migration  string
}

func findLegacyRoot(start string) *legacyRoot {
	for dir := start; ; dir = filepath.Dir(dir) {
		if marker := regularLegacyConfig(filepath.Join(dir, StateDirname, "config.toml")); marker != "" {
			workspaceDir := dir
			if filepath.Base(dir) == "docs" {
				workspaceDir = filepath.Dir(dir)
			}
			return &legacyRoot{
				ConfigPath: marker,
				Migration:  migrationMessage(workspaceDir, dir),
			}
		}
		docsDir := filepath.Join(dir, "docs")
		if marker := regularLegacyConfig(filepath.Join(docsDir, StateDirname, "config.toml")); marker != "" {
			return &legacyRoot{
				ConfigPath: marker,
				Migration:  migrationMessage(dir, docsDir),
			}
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return nil
		}
	}
}

func regularLegacyConfig(filename string) string {
	info, err := os.Lstat(filename)
	if err != nil || !info.Mode().IsRegular() {
		return ""
	}
	return filename
}

func migrationMessage(workspaceDir string, bundleDir string) string {
	if workspaceDir == bundleDir {
		return fmt.Sprintf("Create %s with workspace and bundle tables.", ManifestPath(workspaceDir))
	}
	return fmt.Sprintf("Create %s and %s.", ManifestPath(workspaceDir), ManifestPath(bundleDir))
}
