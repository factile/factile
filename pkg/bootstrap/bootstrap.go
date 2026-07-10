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
	RootPath string        `json:"root_path"`
	Files    []FileChange  `json:"files"`
	Agents   []AgentResult `json:"agents,omitempty"`
	Message  string        `json:"message"`
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

	rootDir, err := initRootDir(workDir, opts.Here)
	if err != nil {
		return Result{}, err
	}
	rootConfig, err := ensureRootConfig(workDir, rootDir)
	if err != nil {
		return Result{}, err
	}
	files, err := ensureKnowledgeBundle(workDir, rootDir, now)
	if err != nil {
		return Result{}, err
	}
	files = append([]FileChange{rootConfig}, files...)

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
		RootPath: relPath(workDir, rootDir),
		Files:    files,
		Agents:   installed,
		Message:  message,
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

func initRootDir(workDir string, here bool) (string, error) {
	if here {
		return workDir, nil
	}
	if root, ok, err := vfs.FindRoot(vfs.LoadOptions{WorkDir: workDir}); err != nil {
		return "", err
	} else if ok {
		return root, nil
	}
	return filepath.Join(workDir, "docs"), nil
}

func ensureRootConfig(workDir, rootDir string) (FileChange, error) {
	name := filepath.Base(workDir)
	if name == "." || name == string(filepath.Separator) {
		name = "project"
	}
	data := fmt.Sprintf(`version = 1

name = "%s"
title = "%s"
description = "Project knowledge for %s."
when_to_use = "Use for questions about this project, its architecture, operations, and development standards."

[defaults]
format = "okf"
`, escapeTOMLString(name), escapeTOMLString(titleFromName(name)), escapeTOMLString(titleFromName(name)))
	return writeFileIfMissing(workDir, filepath.Join(rootDir, ".factile", "config.toml"), []byte(data))
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
	return "# Factile Knowledge\n\n- [Overview](overview.md)\n- [Log](log.md)\n"
}

func logMarkdown(now time.Time) string {
	return fmt.Sprintf("# Factile Log\n\n- %s: Initialized Factile root.\n", now.Format(time.RFC3339))
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
