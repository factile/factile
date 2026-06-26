package bootstrap

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/factile/factile/pkg/catalog"
	"github.com/factile/factile/pkg/factile"
	"github.com/factile/factile/pkg/skill"
	"github.com/factile/factile/pkg/storage"
	"github.com/factile/factile/pkg/vfs"
)

const (
	defaultMountPath = "/project"
	defaultSource    = ".factile/knowledge"
)

type Options struct {
	WorkDir       string
	KnowledgePath string
	Agents        []string
	Now           time.Time
}

type FileChange struct {
	Path   string `json:"path"`
	Action string `json:"action"`
}

type MountChange struct {
	Mount  vfs.Mount `json:"mount"`
	Action string    `json:"action"`
}

type AgentResult struct {
	Agent    string             `json:"agent"`
	Detected bool               `json:"detected"`
	Files    []skill.FileChange `json:"files,omitempty"`
	Message  string             `json:"message,omitempty"`
}

type Result struct {
	BundlePath string        `json:"bundle_path"`
	MountPath  string        `json:"mount_path"`
	Files      []FileChange  `json:"files"`
	Mount      MountChange   `json:"mount"`
	Agents     []AgentResult `json:"agents,omitempty"`
	Message    string        `json:"message"`
}

func Init(ctx context.Context, opts Options) (Result, error) {
	workDir, err := defaultWorkDir(opts.WorkDir)
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

	bundlePath, bundleDisplay, err := knowledgePath(workDir, opts.KnowledgePath)
	if err != nil {
		return Result{}, err
	}
	registryPath := vfs.ProjectRegistryPath(workDir)
	if err := ensureProjectMountAvailable(registryPath, bundlePath); err != nil {
		return Result{}, err
	}

	files, err := ensureKnowledgeBundle(workDir, bundlePath, now)
	if err != nil {
		return Result{}, err
	}
	catalogFiles, err := ensureCatalogFiles(workDir, bundlePath)
	if err != nil {
		return Result{}, err
	}
	files = append(files, catalogFiles...)
	mount, err := ensureProjectMount(registryPath, bundlePath)
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

	message := "Initialized local Factile project knowledge."
	if len(installed) == 0 {
		message += " No supported repo agents were detected."
	}
	_ = ctx
	return Result{
		BundlePath: bundleDisplay,
		MountPath:  defaultMountPath,
		Files:      files,
		Mount:      mount,
		Agents:     installed,
		Message:    message,
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

func ensureProjectMountAvailable(registryPath, bundlePath string) error {
	mounts, err := loadMounts(registryPath)
	if err != nil {
		return err
	}
	for _, mount := range mounts {
		if mount.MountPath == defaultMountPath && !sameLocalSource(mount, bundlePath) {
			return factile.NewError(factile.ErrValidationFailed, fmt.Sprintf("%s is already mounted to %s", defaultMountPath, mount.Source))
		}
	}
	return nil
}

func ensureKnowledgeBundle(workDir, bundlePath string, now time.Time) ([]FileChange, error) {
	var changes []FileChange
	for _, file := range []struct {
		path string
		data string
	}{
		{path: filepath.Join(bundlePath, "index.md"), data: indexMarkdown()},
		{path: filepath.Join(bundlePath, "log.md"), data: logMarkdown(now)},
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

func ensureCatalogFiles(workDir, bundlePath string) ([]FileChange, error) {
	libraryPath := filepath.Join(workDir, ".factile", "library.toml")
	kbPath := filepath.Join(workDir, ".factile", "knowledge-bases", "project.toml")
	source, err := sourceForRegistry(kbPath, bundlePath)
	if err != nil {
		return nil, err
	}
	var changes []FileChange
	libraryChange, err := ensureLibraryCatalog(workDir, libraryPath)
	if err != nil {
		return nil, err
	}
	changes = append(changes, libraryChange)
	kbChange, err := ensureProjectKBCatalog(workDir, kbPath, source)
	if err != nil {
		return nil, err
	}
	changes = append(changes, kbChange)
	return changes, nil
}

func ensureLibraryCatalog(workDir, libraryPath string) (FileChange, error) {
	ref := catalog.KnowledgeBaseRef{
		ID:          "project",
		Path:        defaultMountPath,
		Catalog:     "knowledge-bases/project.toml",
		Title:       "Project Knowledge Base",
		Description: "Repository-specific architecture, workflows, decisions, runbooks, and conventions.",
	}
	if !pathExists(libraryPath) {
		library := catalog.Library{
			ID:             "local",
			Title:          "Local Library",
			Description:    "Knowledge bases available in this workspace.",
			KnowledgeBases: []catalog.KnowledgeBaseRef{ref},
		}
		if err := catalog.WriteLibraryFile(libraryPath, library); err != nil {
			return FileChange{}, err
		}
		return FileChange{Path: relPath(workDir, libraryPath), Action: "created"}, nil
	}
	library, err := catalog.LoadLibraryFile(libraryPath)
	if err != nil {
		return FileChange{}, err
	}
	for _, existing := range library.KnowledgeBases {
		if existing.ID == ref.ID || existing.Path == ref.Path {
			return FileChange{Path: relPath(workDir, libraryPath), Action: "unchanged"}, nil
		}
	}
	library.KnowledgeBases = append(library.KnowledgeBases, ref)
	if err := catalog.WriteLibraryFile(libraryPath, library); err != nil {
		return FileChange{}, err
	}
	return FileChange{Path: relPath(workDir, libraryPath), Action: "updated"}, nil
}

func ensureProjectKBCatalog(workDir, kbPath string, source string) (FileChange, error) {
	link := catalog.BundleLink{
		ID:          "project",
		Path:        defaultMountPath,
		Source:      source,
		Kind:        "local",
		Writable:    true,
		Title:       "Project Knowledge",
		Description: "Local repository knowledge bundle.",
		Trust:       "local",
		Profile:     "software-engineering",
		Priority:    100,
		WhenToUse:   "Use when designing, changing, debugging, or documenting this repository.",
	}
	if !pathExists(kbPath) {
		kb := catalog.KnowledgeBase{
			ID:              "project",
			Path:            defaultMountPath,
			Title:           "Project Knowledge Base",
			Description:     "Repository-specific architecture, workflows, decisions, runbooks, and conventions.",
			Audience:        "Coding agents and developers",
			Profile:         "software-engineering",
			DefaultTrust:    "local",
			DefaultWritable: true,
			Bundles:         []catalog.BundleLink{link},
		}
		if err := catalog.WriteKnowledgeBaseFile(kbPath, kb); err != nil {
			return FileChange{}, err
		}
		return FileChange{Path: relPath(workDir, kbPath), Action: "created"}, nil
	}
	kb, err := catalog.LoadKnowledgeBaseFile(kbPath)
	if err != nil {
		return FileChange{}, err
	}
	for _, existing := range kb.Bundles {
		if existing.ID == link.ID || existing.Path == link.Path {
			return FileChange{Path: relPath(workDir, kbPath), Action: "unchanged"}, nil
		}
	}
	kb.Bundles = append(kb.Bundles, link)
	if err := catalog.WriteKnowledgeBaseFile(kbPath, kb); err != nil {
		return FileChange{}, err
	}
	return FileChange{Path: relPath(workDir, kbPath), Action: "updated"}, nil
}

func ensureProjectMount(registryPath, bundlePath string) (MountChange, error) {
	var change MountChange
	registryExists := pathExists(registryPath)
	err := storage.WithFileLock(registryPath, func() error {
		mounts, err := loadMounts(registryPath)
		if err != nil {
			return err
		}
		for _, mount := range mounts {
			if mount.MountPath == defaultMountPath {
				if sameLocalSource(mount, bundlePath) {
					change = MountChange{Mount: scrubMount(mount), Action: "unchanged"}
					return nil
				}
				return factile.NewError(factile.ErrValidationFailed, fmt.Sprintf("%s is already mounted to %s", defaultMountPath, mount.Source))
			}
		}
		source, err := sourceForRegistry(registryPath, bundlePath)
		if err != nil {
			return err
		}
		mount := vfs.Mount{MountPath: defaultMountPath, Source: source, Kind: "local", Writable: true}
		mounts = append(mounts, mount)
		if err := vfs.WriteRegistryFile(registryPath, mounts); err != nil {
			return err
		}
		action := "created"
		if registryExists {
			action = "updated"
		}
		change = MountChange{Mount: mount, Action: action}
		return nil
	})
	if err != nil {
		return MountChange{}, err
	}
	return change, nil
}

func loadMounts(registryPath string) ([]vfs.Mount, error) {
	mounts, err := vfs.LoadRegistryFile(registryPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	return mounts, nil
}

func sameLocalSource(mount vfs.Mount, bundlePath string) bool {
	if mount.Kind != "" && mount.Kind != "local" {
		return false
	}
	if mount.SourcePath == "" {
		return false
	}
	sourcePath, err := filepath.Abs(mount.SourcePath)
	if err != nil {
		return false
	}
	bundleAbs, err := filepath.Abs(bundlePath)
	if err != nil {
		return false
	}
	return filepath.Clean(sourcePath) == filepath.Clean(bundleAbs)
}

func knowledgePath(workDir, input string) (string, string, error) {
	if input == "" {
		input = defaultSource
	}
	cleaned := filepath.Clean(input)
	var abs string
	if filepath.IsAbs(cleaned) {
		abs = cleaned
	} else {
		abs = filepath.Join(workDir, cleaned)
	}
	abs, err := filepath.Abs(abs)
	if err != nil {
		return "", "", err
	}
	display := relPath(workDir, abs)
	if filepath.IsAbs(cleaned) {
		display = filepath.ToSlash(cleaned)
	}
	return abs, display, nil
}

func sourceForRegistry(registryPath, bundlePath string) (string, error) {
	rel, err := filepath.Rel(filepath.Dir(registryPath), bundlePath)
	if err != nil {
		return filepath.ToSlash(bundlePath), nil
	}
	return filepath.ToSlash(rel), nil
}

func scrubMount(mount vfs.Mount) vfs.Mount {
	mount.RegistryPath = ""
	mount.SourcePath = ""
	return mount
}

func writeFileIfMissing(workDir, filename string, data []byte) (FileChange, error) {
	if pathExists(filename) {
		return FileChange{Path: relPath(workDir, filename), Action: "unchanged"}, nil
	}
	if err := os.MkdirAll(filepath.Dir(filename), 0o755); err != nil {
		return FileChange{}, err
	}
	if err := os.WriteFile(filename, data, 0o644); err != nil {
		return FileChange{}, err
	}
	return FileChange{Path: relPath(workDir, filename), Action: "created"}, nil
}

func indexMarkdown() string {
	return "# Project Knowledge\n\n- [Overview](overview.md)\n"
}

func logMarkdown(now time.Time) string {
	return fmt.Sprintf("# Knowledge Log\n\n- %s: Initialized local Factile project knowledge.\n", now.Format(time.RFC3339))
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

func defaultWorkDir(workDir string) (string, error) {
	if workDir != "" {
		return filepath.Abs(workDir)
	}
	return os.Getwd()
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
