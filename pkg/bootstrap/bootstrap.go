package bootstrap

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/factile/factile/pkg/skill"
	"github.com/factile/factile/pkg/vfs"
)

type Options struct {
	WorkDir string
	Here    bool
	Agents  []string
	Now     time.Time
}

type FileChange struct {
	Path   string `json:"path"`
	Action string `json:"action"`
}

type AgentResult struct {
	Agent    string             `json:"agent"`
	Detected bool               `json:"detected"`
	Files    []skill.FileChange `json:"files,omitempty"`
	Message  string             `json:"message,omitempty"`
}

type Result struct {
	WorkspacePath  string        `json:"workspace_path"`
	RootBundlePath string        `json:"root_bundle_path"`
	Files          []FileChange  `json:"files"`
	Agents         []AgentResult `json:"agents,omitempty"`
	Message        string        `json:"message"`
}

func Init(ctx context.Context, opts Options) (Result, error) {
	workDir, err := initWorkDir(opts.WorkDir)
	if err != nil {
		return Result{}, err
	}
	now := opts.Now
	if now.IsZero() {
		now = time.Now().UTC()
	}
	agentsExplicit := len(opts.Agents) > 0
	agents := normalizeAgents(opts.Agents)
	if !agentsExplicit {
		agents = DetectAgents(workDir)
	}

	layout, err := inspectInitLayout(workDir, opts.Here)
	if err != nil {
		return Result{}, err
	}
	files, err := ensureInitLayout(workDir, layout, now)
	if err != nil {
		return Result{}, err
	}

	var installed []AgentResult
	for _, agent := range agents {
		result, err := skill.Install(agent, skill.InstallOptions{Scope: "repo", WorkDir: workDir})
		if err != nil {
			return Result{}, err
		}
		installed = append(installed, AgentResult{
			Agent:    agent,
			Detected: !agentsExplicit,
			Files:    result.Files,
			Message:  result.Message,
		})
	}

	message := "Initialized local Factile workspace and root bundle."
	if len(installed) == 0 {
		message += " No supported repo agents were detected."
	}
	_ = ctx
	return Result{
		WorkspacePath:  ".",
		RootBundlePath: relPath(workDir, layout.rootBundleDir),
		Files:          files,
		Agents:         installed,
		Message:        message,
	}, nil
}

func DetectAgents(workDir string) []string {
	var agents []string
	for _, detector := range []struct {
		name  string
		paths []string
	}{
		{name: skill.TargetCodex, paths: []string{".codex", ".agents/skills", "AGENTS.md"}},
	} {
		for _, p := range detector.paths {
			if pathExists(filepath.Join(workDir, p)) {
				agents = append(agents, detector.name)
				break
			}
		}
	}
	sort.Strings(agents)
	return agents
}

func normalizeAgents(input []string) []string {
	seen := map[string]bool{}
	var agents []string
	for _, value := range input {
		for _, item := range strings.Split(value, ",") {
			agent := strings.TrimSpace(item)
			if agent == "" || seen[agent] {
				continue
			}
			seen[agent] = true
			agents = append(agents, agent)
		}
	}
	sort.Strings(agents)
	return agents
}

type initLayout struct {
	rootBundleDir string
	combined      bool
}

func inspectInitLayout(workDir string, here bool) (initLayout, error) {
	layout := initLayout{rootBundleDir: filepath.Join(workDir, "docs")}
	if here {
		layout.rootBundleDir = workDir
		layout.combined = true
	}

	for _, filename := range legacyInitPaths(workDir, layout.rootBundleDir) {
		if _, err := os.Lstat(filename); err == nil {
			return initLayout{}, initError(
				"Cannot initialize over a legacy Factile layout. Migrate or remove the legacy files first.",
				map[string]string{"legacy_path": filename},
			)
		} else if !os.IsNotExist(err) {
			return initLayout{}, err
		}
	}

	workspaceManifest := vfs.ManifestPath(workDir)
	bundleManifest := vfs.ManifestPath(layout.rootBundleDir)
	workspaceExists, err := regularFileExists(workspaceManifest)
	if err != nil {
		return initLayout{}, initError("Workspace factile.toml must be a regular file.", map[string]string{"manifest": workspaceManifest})
	}
	if layout.combined {
		if !workspaceExists {
			return validateInitOutputPaths(workDir, layout)
		}
		manifest, err := vfs.LoadManifest(workDir)
		if err != nil || manifest.Workspace == nil || manifest.Bundle == nil || manifest.Workspace.Root != "." {
			return initLayout{}, initError("Existing factile.toml is not a compatible combined workspace and bundle manifest.", map[string]string{"manifest": workspaceManifest})
		}
		return validateInitOutputPaths(workDir, layout)
	}

	bundleExists, err := regularFileExists(bundleManifest)
	if err != nil {
		return initLayout{}, initError("Root bundle factile.toml must be a regular file.", map[string]string{"manifest": bundleManifest})
	}
	if workspaceExists != bundleExists {
		return initLayout{}, initError(
			"Cannot initialize a partial Factile v2 layout. Preserve or remove the existing manifests before retrying.",
			map[string]string{"workspace_manifest": workspaceManifest, "root_bundle_manifest": bundleManifest},
		)
	}
	if !workspaceExists {
		return validateInitOutputPaths(workDir, layout)
	}

	workspace, workspaceErr := vfs.LoadManifest(workDir)
	bundle, bundleErr := vfs.LoadManifest(layout.rootBundleDir)
	if workspaceErr != nil || bundleErr != nil || workspace.Workspace == nil || workspace.Workspace.Root != "docs" || workspace.Bundle != nil || bundle.Bundle == nil || bundle.Workspace != nil {
		return initLayout{}, initError(
			"Existing Factile manifests are not compatible with the default docs workspace layout.",
			map[string]string{"workspace_manifest": workspaceManifest, "root_bundle_manifest": bundleManifest},
		)
	}
	return validateInitOutputPaths(workDir, layout)
}

func validateInitOutputPaths(workDir string, layout initLayout) (initLayout, error) {
	if !layout.combined {
		if info, err := os.Lstat(layout.rootBundleDir); err == nil {
			if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
				return initLayout{}, initError("Workspace docs must be a real directory.", map[string]string{"path": layout.rootBundleDir})
			}
		} else if !os.IsNotExist(err) {
			return initLayout{}, err
		}
	}
	for _, filename := range []string{
		filepath.Join(workDir, ".gitignore"),
		vfs.ManifestPath(workDir),
		vfs.ManifestPath(layout.rootBundleDir),
		filepath.Join(layout.rootBundleDir, "index.md"),
		filepath.Join(layout.rootBundleDir, "overview.md"),
	} {
		info, err := os.Lstat(filename)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return initLayout{}, err
		}
		if !info.Mode().IsRegular() {
			return initLayout{}, initError("Initialization output paths must be regular files.", map[string]string{"path": filename})
		}
	}
	return layout, nil
}

func ensureInitLayout(workDir string, layout initLayout, now time.Time) ([]FileChange, error) {
	name := filepath.Base(workDir)
	if name == "." || name == string(filepath.Separator) {
		name = "project"
	}

	var changes []FileChange
	ignore, err := ensureStateIgnored(workDir)
	if err != nil {
		return nil, err
	}
	changes = append(changes, ignore)

	if layout.combined {
		manifest, err := writeFileIfMissing(workDir, vfs.ManifestPath(workDir), combinedManifest(name))
		if err != nil {
			return nil, err
		}
		changes = append(changes, manifest)
	} else {
		workspace, err := writeFileIfMissing(workDir, vfs.ManifestPath(workDir), workspaceManifest())
		if err != nil {
			return nil, err
		}
		changes = append(changes, workspace)
		bundle, err := writeFileIfMissing(workDir, vfs.ManifestPath(layout.rootBundleDir), bundleManifest(name))
		if err != nil {
			return nil, err
		}
		changes = append(changes, bundle)
	}

	knowledge, err := ensureKnowledgeBundle(workDir, layout.rootBundleDir, now)
	if err != nil {
		return nil, err
	}
	return append(changes, knowledge...), nil
}

func workspaceManifest() []byte {
	return []byte("version = 2\n\n[workspace]\nroot = \"docs\"\n")
}

func bundleManifest(name string) []byte {
	title := titleFromName(name)
	return []byte(fmt.Sprintf(`version = 2

[bundle]
name = "%s"
title = "%s"
`, escapeTOMLString(name), escapeTOMLString(title)))
}

func combinedManifest(name string) []byte {
	title := titleFromName(name)
	return []byte(fmt.Sprintf(`version = 2

[workspace]
root = "."

[bundle]
name = "%s"
title = "%s"
`, escapeTOMLString(name), escapeTOMLString(title)))
}

func ensureKnowledgeBundle(workDir, bundlePath string, now time.Time) ([]FileChange, error) {
	var changes []FileChange
	for _, file := range []struct {
		path string
		data string
	}{
		{path: filepath.Join(bundlePath, "index.md"), data: indexMarkdown()},
		{path: filepath.Join(bundlePath, "overview.md"), data: overviewMarkdown(now)},
	} {
		change, err := writeFileIfMissing(workDir, file.path, []byte(file.data))
		if err != nil {
			return nil, err
		}
		changes = append(changes, change)
	}
	return changes, nil
}

func writeFileIfMissing(workDir, filename string, data []byte) (FileChange, error) {
	info, err := os.Lstat(filename)
	if err == nil {
		if !info.Mode().IsRegular() {
			return FileChange{}, fmt.Errorf("cannot preserve non-regular file %s", filename)
		}
		return FileChange{Path: relPath(workDir, filename), Action: "unchanged"}, nil
	}
	if !os.IsNotExist(err) {
		return FileChange{}, err
	}
	if err := os.MkdirAll(filepath.Dir(filename), 0o755); err != nil {
		return FileChange{}, err
	}
	if err := os.WriteFile(filename, data, 0o644); err != nil {
		return FileChange{}, err
	}
	return FileChange{Path: relPath(workDir, filename), Action: "created"}, nil
}

func ensureStateIgnored(workDir string) (FileChange, error) {
	filename := filepath.Join(workDir, ".gitignore")
	info, statErr := os.Lstat(filename)
	if statErr == nil && !info.Mode().IsRegular() {
		return FileChange{}, initError("Workspace .gitignore must be a regular file.", map[string]string{"path": filename})
	}
	if statErr != nil && !os.IsNotExist(statErr) {
		return FileChange{}, statErr
	}
	data, err := os.ReadFile(filename)
	if err == nil {
		for _, line := range strings.Split(strings.ReplaceAll(string(data), "\r\n", "\n"), "\n") {
			if strings.TrimSpace(line) == "/.factile/" {
				return FileChange{Path: ".gitignore", Action: "unchanged"}, nil
			}
		}
		updated := append([]byte(nil), data...)
		if len(updated) > 0 && updated[len(updated)-1] != '\n' {
			updated = append(updated, '\n')
		}
		updated = append(updated, []byte("/.factile/\n")...)
		if err := os.WriteFile(filename, updated, 0o644); err != nil {
			return FileChange{}, err
		}
		return FileChange{Path: ".gitignore", Action: "updated"}, nil
	}
	if !os.IsNotExist(err) {
		return FileChange{}, err
	}
	if err := os.WriteFile(filename, []byte("/.factile/\n"), 0o644); err != nil {
		return FileChange{}, err
	}
	return FileChange{Path: ".gitignore", Action: "created"}, nil
}

func legacyInitPaths(workDir, rootBundleDir string) []string {
	seen := map[string]bool{}
	var paths []string
	for _, dir := range []string{workDir, rootBundleDir} {
		for _, name := range []string{"config.toml", "views.toml"} {
			filename := filepath.Join(dir, vfs.StateDirname, name)
			if !seen[filename] {
				seen[filename] = true
				paths = append(paths, filename)
			}
		}
	}
	return paths
}

func regularFileExists(filename string) (bool, error) {
	info, err := os.Lstat(filename)
	if os.IsNotExist(err) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if !info.Mode().IsRegular() {
		return false, fmt.Errorf("not a regular file")
	}
	return true, nil
}

func initError(message string, details map[string]string) error {
	return &vfs.Error{Code: vfs.ErrInvalidWorkspace, Message: message, Details: details}
}

func titleFromName(name string) string {
	name = strings.TrimSpace(strings.ReplaceAll(name, "-", " "))
	name = strings.TrimSpace(strings.ReplaceAll(name, "_", " "))
	if name == "" {
		return "Project"
	}
	parts := strings.Fields(name)
	for i, part := range parts {
		parts[i] = strings.ToUpper(part[:1]) + part[1:]
	}
	return strings.Join(parts, " ")
}

func escapeTOMLString(value string) string {
	value = strings.ReplaceAll(value, `\`, `\\`)
	return strings.ReplaceAll(value, `"`, `\"`)
}

func indexMarkdown() string {
	return `---
type: Index
title: Factile Knowledge
description: Starting point for repository-specific Factile knowledge.
tags: [factile, project]
---

# Factile Knowledge

- [Overview](overview.md)
`
}

func overviewMarkdown(now time.Time) string {
	return fmt.Sprintf(`---
type: Reference
title: Project Knowledge Overview
description: Starting point for repository-specific Factile knowledge.
tags: [factile, project]
timestamp: %s
---

# Project Knowledge Overview

Capture repository-specific architecture, workflows, decisions, runbooks, and conventions here.
`, now.Format(time.RFC3339))
}

func initWorkDir(workDir string) (string, error) {
	if workDir == "" {
		var err error
		workDir, err = os.Getwd()
		if err != nil {
			return "", err
		}
	}
	abs, err := filepath.Abs(workDir)
	if err != nil {
		return "", err
	}
	canonical, err := filepath.EvalSymlinks(abs)
	if err != nil {
		return "", initError("Initialization target is not an existing directory.", map[string]string{"workspace": abs})
	}
	info, err := os.Stat(canonical)
	if err != nil || !info.IsDir() {
		return "", initError("Initialization target is not an existing directory.", map[string]string{"workspace": abs})
	}
	return filepath.Clean(canonical), nil
}

func relPath(root, filename string) string {
	rel, err := filepath.Rel(root, filename)
	if err != nil {
		return filepath.ToSlash(filename)
	}
	return filepath.ToSlash(rel)
}

func pathExists(filename string) bool {
	_, err := os.Stat(filename)
	return err == nil
}
