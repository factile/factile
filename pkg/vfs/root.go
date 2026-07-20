package vfs

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

const rootConfigRelativePath = ".factile/config.toml"

type RootConfig struct {
	Version     int          `json:"version"`
	Name        string       `json:"name,omitempty"`
	Title       string       `json:"title,omitempty"`
	Description string       `json:"description,omitempty"`
	WhenToUse   string       `json:"when_to_use,omitempty"`
	Defaults    RootDefaults `json:"defaults,omitempty"`
}

type RootDefaults struct {
	Format string `json:"format,omitempty"`
}

// RootConfigPath returns the retained Root Layout v1 config path.
// Deprecated: use ManifestPath for Root Layout v2.
func RootConfigPath(root string) string {
	return filepath.Join(root, filepath.FromSlash(rootConfigRelativePath))
}

// LoadRootConfig loads retained Root Layout v1 metadata.
// Deprecated: use LoadManifest for Root Layout v2.
func LoadRootConfig(root string) (RootConfig, error) {
	filename := RootConfigPath(root)
	file, err := os.Open(filename)
	if err != nil {
		return RootConfig{}, err
	}
	defer file.Close()

	var cfg RootConfig
	table := ""
	scanner := bufio.NewScanner(file)
	for lineNo := 1; scanner.Scan(); lineNo++ {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if strings.HasPrefix(line, "[") {
			if line != "[defaults]" {
				return RootConfig{}, fmt.Errorf("unsupported root config table %q on line %d", line, lineNo)
			}
			table = "defaults"
			continue
		}
		parts := strings.SplitN(line, "=", 2)
		if len(parts) != 2 {
			return RootConfig{}, fmt.Errorf("invalid root config assignment on line %d", lineNo)
		}
		key := strings.TrimSpace(parts[0])
		rawValue := strings.TrimSpace(parts[1])
		switch table {
		case "":
			if err := assignRootConfig(&cfg, key, rawValue, lineNo); err != nil {
				return RootConfig{}, err
			}
		case "defaults":
			if err := assignRootDefaults(&cfg.Defaults, key, rawValue, lineNo); err != nil {
				return RootConfig{}, err
			}
		default:
			return RootConfig{}, fmt.Errorf("unsupported root config table %q on line %d", table, lineNo)
		}
	}
	if err := scanner.Err(); err != nil {
		return RootConfig{}, err
	}
	if cfg.Version != 1 {
		return RootConfig{}, fmt.Errorf("unsupported root config version %d", cfg.Version)
	}
	return cfg, nil
}

func assignRootConfig(cfg *RootConfig, key string, rawValue string, lineNo int) error {
	switch key {
	case "version":
		value, err := strconv.Atoi(rawValue)
		if err != nil {
			return fmt.Errorf("root config version on line %d expects an integer", lineNo)
		}
		cfg.Version = value
	case "name":
		value, err := parseRootString(rawValue, key, lineNo)
		if err != nil {
			return err
		}
		cfg.Name = value
	case "title":
		value, err := parseRootString(rawValue, key, lineNo)
		if err != nil {
			return err
		}
		cfg.Title = value
	case "description":
		value, err := parseRootString(rawValue, key, lineNo)
		if err != nil {
			return err
		}
		cfg.Description = value
	case "when_to_use":
		value, err := parseRootString(rawValue, key, lineNo)
		if err != nil {
			return err
		}
		cfg.WhenToUse = value
	default:
		return fmt.Errorf("unsupported root config key %q on line %d", key, lineNo)
	}
	return nil
}

func assignRootDefaults(defaults *RootDefaults, key string, rawValue string, lineNo int) error {
	switch key {
	case "format":
		value, err := parseRootString(rawValue, key, lineNo)
		if err != nil {
			return err
		}
		defaults.Format = value
	default:
		return fmt.Errorf("unsupported root defaults key %q on line %d", key, lineNo)
	}
	return nil
}

func parseRootString(raw string, key string, lineNo int) (string, error) {
	if !strings.HasPrefix(raw, `"`) {
		return "", fmt.Errorf("root config key %q on line %d expects quoted string", key, lineNo)
	}
	value, err := strconv.Unquote(raw)
	if err != nil {
		return "", fmt.Errorf("invalid string for root config key %q on line %d: %w", key, lineNo, err)
	}
	return value, nil
}

// FindRoot implements retained Root Layout v1 discovery for callers awaiting
// the ft-qhg.3 runtime cutover.
// Deprecated: use ResolveWorkspace for Root Layout v2.
func FindRoot(opts LoadOptions) (string, bool, error) {
	if opts.Root != "" {
		root, err := filepath.Abs(opts.Root)
		if err != nil {
			return "", false, err
		}
		if err := requireRootConfig(root); err != nil {
			return "", false, err
		}
		return root, true, nil
	}

	start, err := defaultWorkDir(opts.WorkDir)
	if err != nil {
		return "", false, err
	}
	info, err := os.Stat(start)
	if err == nil && !info.IsDir() {
		start = filepath.Dir(start)
	}
	for dir := start; ; dir = filepath.Dir(dir) {
		configPath := RootConfigPath(dir)
		if fileExists(configPath) {
			if _, err := LoadRootConfig(dir); err != nil {
				return "", false, err
			}
			return dir, true, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
	}

	for dir := start; ; dir = filepath.Dir(dir) {
		docsRoot := filepath.Join(dir, "docs")
		if fileExists(RootConfigPath(docsRoot)) {
			if _, err := LoadRootConfig(docsRoot); err != nil {
				return "", false, err
			}
			return docsRoot, true, nil
		}
		if hasGitMarker(dir) {
			break
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
	}

	if gitRoot, ok := findGitRoot(start); ok {
		docsRoot := filepath.Join(gitRoot, "docs")
		if fileExists(RootConfigPath(docsRoot)) {
			if _, err := LoadRootConfig(docsRoot); err != nil {
				return "", false, err
			}
			return docsRoot, true, nil
		}
	}
	return start, false, nil
}

func requireRootConfig(root string) error {
	configPath := RootConfigPath(root)
	if !fileExists(configPath) {
		return &Error{Code: "no_active_root", Message: "Factile root config not found at " + configPath + ". Run factile init to create one."}
	}
	_, err := LoadRootConfig(root)
	return err
}

func findGitRoot(start string) (string, bool) {
	for dir := start; ; dir = filepath.Dir(dir) {
		if hasGitMarker(dir) {
			return dir, true
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", false
		}
	}
}

func hasGitMarker(dir string) bool {
	return fileExists(filepath.Join(dir, ".git")) || dirExists(filepath.Join(dir, ".git"))
}
