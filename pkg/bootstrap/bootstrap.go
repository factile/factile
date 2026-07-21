package bootstrap

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/factile/factile/internal/atomicfile"
	"github.com/factile/factile/pkg/factile"
	"github.com/factile/factile/pkg/okf"
	"github.com/factile/factile/pkg/skill"
	"github.com/factile/factile/pkg/vfs"
)

const (
	AgentAuto  = "auto"
	AgentCodex = "codex"
	AgentNone  = "none"

	CheckPass    = "pass"
	CheckWarning = "warning"
	CheckFail    = "fail"
)

var replaceInitFile = atomicfile.Write
var createInitFile = atomicfile.Create

type Options struct {
	WorkDir             string
	Workspace           string
	Root                string
	RootExplicit        bool
	Name                string
	NameExplicit        bool
	Title               string
	TitleExplicit       bool
	Description         string
	DescriptionExplicit bool
	Agent               string
	Now                 time.Time
}

type FileChange struct {
	Path   string `json:"path"`
	Action string `json:"action"`
}

type AgentResult struct {
	Agent    string             `json:"agent"`
	Detected bool               `json:"detected"`
	Mode     string             `json:"mode"`
	Profile  string             `json:"profile,omitempty"`
	Files    []skill.FileChange `json:"files,omitempty"`
	Message  string             `json:"message,omitempty"`
}

type BundlePlan struct {
	Name        string `json:"name"`
	Title       string `json:"title"`
	Description string `json:"description"`
}

type AgentPlan struct {
	Agent    string `json:"agent"`
	Detected bool   `json:"detected"`
	Mode     string `json:"mode"`
	Profile  string `json:"profile,omitempty"`
}

type HealthCheck struct {
	Name    string `json:"name"`
	Status  string `json:"status"`
	Message string `json:"message"`
}

type HealthResult struct {
	Status string        `json:"status"`
	OK     bool          `json:"ok"`
	Checks []HealthCheck `json:"checks"`
}

// InitPlan is a fully validated, read-only init plan. Its unexported state
// keeps Apply tied to a plan produced by Prepare.
type InitPlan struct {
	WorkspacePath  string     `json:"workspace_path"`
	RootBundlePath string     `json:"root_bundle_path"`
	PreviousRoot   string     `json:"previous_root,omitempty"`
	NewWorkspace   bool       `json:"new_workspace"`
	RootChanged    bool       `json:"root_changed"`
	Bundle         BundlePlan `json:"bundle"`
	AgentSelection string     `json:"agent_selection"`
	Agent          *AgentPlan `json:"agent,omitempty"`
	layout         initLayout
	agentPlan      *agentInstallPlan
	options        Options
	now            time.Time
}

type Result struct {
	WorkspacePath  string        `json:"workspace_path"`
	RootBundlePath string        `json:"root_bundle_path"`
	AgentSelection string        `json:"agent_selection"`
	Bundle         BundlePlan    `json:"bundle"`
	Files          []FileChange  `json:"files"`
	Agents         []AgentResult `json:"agents,omitempty"`
	Health         HealthResult  `json:"health"`
	Message        string        `json:"message"`
}

func Init(ctx context.Context, opts Options) (Result, error) {
	plan, err := Prepare(opts)
	if err != nil {
		return Result{}, err
	}
	return Apply(ctx, plan)
}

// Prepare resolves and validates init without changing the filesystem.
func Prepare(opts Options) (InitPlan, error) {
	layout, err := inspectInitLayout(opts)
	if err != nil {
		return InitPlan{}, err
	}
	agent, err := normalizeAgent(opts.Agent)
	if err != nil {
		return InitPlan{}, err
	}
	agentPlan := planAgent(layout.workspaceDir, agent)
	if err := prepareAgentInstall(layout.workspaceDir, agentPlan); err != nil {
		return InitPlan{}, err
	}
	now := opts.Now
	if now.IsZero() {
		now = time.Now().UTC()
	}
	opts.Now = now

	plan := InitPlan{
		WorkspacePath:  relPath(layout.invocationDir, layout.workspaceDir),
		RootBundlePath: layout.root,
		NewWorkspace:   layout.workspaceManifest == nil || layout.workspaceManifest.Workspace == nil,
		RootChanged:    layout.rootChanged,
		Bundle: BundlePlan{
			Name:        layout.bundle.Name,
			Title:       layout.bundle.Title,
			Description: layout.bundle.Description,
		},
		AgentSelection: agent,
		layout:         layout,
		agentPlan:      agentPlan,
		options:        opts,
		now:            now,
	}
	if layout.workspaceManifest != nil && layout.workspaceManifest.Workspace != nil {
		plan.PreviousRoot = layout.workspaceManifest.Workspace.Root
	}
	if agentPlan != nil {
		plan.Agent = &AgentPlan{
			Agent:    agentPlan.target,
			Detected: agentPlan.detected,
			Mode:     agentPlan.mode,
			Profile:  agentPlan.profile,
		}
	}
	return plan, nil
}

// Apply reconciles a plan previously returned by Prepare.
func Apply(ctx context.Context, plan InitPlan) (Result, error) {
	if plan.layout.workspaceDir == "" {
		return Result{}, factile.NewError(factile.ErrInvalidPath, "Invalid empty init plan")
	}
	if err := revalidateInitPlan(plan); err != nil {
		return Result{}, err
	}
	files, err := ensureInitLayout(plan.layout, plan.now)
	if err != nil {
		return Result{}, err
	}

	var installed []AgentResult
	if plan.agentPlan != nil {
		result, err := skill.ApplyRepoInstall(plan.agentPlan.prepared)
		if err != nil {
			return Result{}, err
		}
		installed = append(installed, AgentResult{
			Agent:    plan.agentPlan.target,
			Detected: plan.agentPlan.detected,
			Mode:     result.Mode,
			Profile:  result.Profile,
			Files:    result.Files,
			Message:  result.Message,
		})
	}

	message := "Initialized local Factile workspace and root bundle."
	if len(installed) == 0 {
		if plan.AgentSelection == AgentNone {
			message += " Agent guidance was skipped."
		} else {
			message += " No supported repo agents were detected."
		}
	}
	result := Result{
		WorkspacePath:  plan.WorkspacePath,
		RootBundlePath: plan.RootBundlePath,
		AgentSelection: plan.AgentSelection,
		Bundle:         plan.Bundle,
		Files:          files,
		Agents:         installed,
		Message:        message,
	}
	result.Health = verifyInit(ctx, plan)
	return result, nil
}

func revalidateInitPlan(plan InitPlan) error {
	fresh, err := Prepare(plan.options)
	if err != nil {
		return err
	}
	if plan.WorkspacePath != fresh.WorkspacePath ||
		plan.RootBundlePath != fresh.RootBundlePath ||
		plan.PreviousRoot != fresh.PreviousRoot ||
		plan.NewWorkspace != fresh.NewWorkspace ||
		plan.RootChanged != fresh.RootChanged ||
		plan.Bundle != fresh.Bundle ||
		plan.AgentSelection != fresh.AgentSelection ||
		!reflect.DeepEqual(plan.Agent, fresh.Agent) ||
		!reflect.DeepEqual(plan.layout, fresh.layout) ||
		!reflect.DeepEqual(plan.agentPlan, fresh.agentPlan) {
		return initError(
			"Initialization target changed after planning. Review the workspace and run init again.",
			nil,
		)
	}
	return nil
}

func verifyInit(ctx context.Context, plan InitPlan) HealthResult {
	health := HealthResult{Status: "healthy", OK: true, Checks: []HealthCheck{}}
	add := func(name, status, message string) {
		health.Checks = append(health.Checks, HealthCheck{Name: name, Status: status, Message: message})
		switch status {
		case CheckFail:
			health.OK = false
			health.Status = "failed"
		case CheckWarning:
			if health.Status == "healthy" {
				health.Status = "warning"
			}
		}
	}

	resolved, err := vfs.ResolveWorkspace(vfs.ResolveWorkspaceOptions{Workspace: plan.layout.workspaceDir})
	if err != nil {
		add("workspace_layout", CheckFail, "Workspace and selected root do not resolve: "+err.Error())
	} else if resolved.WorkspaceDir != plan.layout.workspaceDir || resolved.RootBundleDir != plan.layout.rootBundleDir {
		add("workspace_layout", CheckFail, "Resolved workspace or root bundle differs from the initialization plan.")
	} else {
		add("workspace_layout", CheckPass, "Workspace manifest selects root bundle `"+plan.RootBundlePath+"`.")
	}

	manifest, err := vfs.LoadManifest(plan.layout.rootBundleDir)
	if err != nil || manifest.Bundle == nil {
		message := "Root bundle manifest is missing or invalid."
		if err != nil {
			message = "Root bundle manifest is invalid: " + err.Error()
		}
		add("bundle_metadata", CheckFail, message)
	} else if manifest.Bundle.Name != plan.Bundle.Name || manifest.Bundle.Title != plan.Bundle.Title || manifest.Bundle.Description != plan.Bundle.Description {
		add("bundle_metadata", CheckFail, "Root bundle metadata differs from the initialization plan.")
	} else {
		add("bundle_metadata", CheckPass, "Root bundle metadata is valid for "+plan.Bundle.Name+".")
	}

	reader := factile.NewWorkspace(factile.WorkspaceOptions{Workspace: plan.layout.workspaceDir, ReadOnly: true})
	var documentError error
	indexData, err := os.ReadFile(filepath.Join(plan.layout.rootBundleDir, "index.md"))
	if err != nil {
		documentError = fmt.Errorf("/index.md: %w", err)
	} else if _, err := okf.ParseConcept("", indexData); err != nil {
		documentError = fmt.Errorf("/index.md: %w", err)
	} else if _, err := reader.List(ctx, "/", factile.ListOptions{}); err != nil {
		documentError = fmt.Errorf("/: %w", err)
	} else if _, err := reader.Read(ctx, "/overview", factile.ReadOptions{}); err != nil {
		documentError = fmt.Errorf("/overview: %w", err)
	}
	if documentError != nil {
		add("required_documents", CheckFail, "Required root documents cannot be read: "+documentError.Error())
	} else {
		add("required_documents", CheckPass, "Root bundle is listable and its overview is readable.")
	}

	validation, err := reader.ValidateRootBundle(ctx)
	if err != nil {
		add("local_root_validation", CheckFail, "Local root validation could not run: "+err.Error())
	} else if !validation.Valid {
		add("local_root_validation", CheckFail, validationSummary("Local root validation failed", validation.Issues))
	} else if len(validation.Issues) > 0 {
		add("local_root_validation", CheckWarning, validationSummary("Local root is valid with warnings", validation.Issues))
	} else {
		add("local_root_validation", CheckPass, "Local root validation passed without accessing mounted remote sources.")
	}

	verifyAgentIntegration(plan, add)
	return health
}

func validationSummary(prefix string, issues []factile.ValidationIssue) string {
	if len(issues) == 0 {
		return prefix + "."
	}
	issue := issues[0]
	message := fmt.Sprintf("%s: %s at %s: %s", prefix, issue.Code, issue.Path, issue.Message)
	if len(issues) > 1 {
		message += fmt.Sprintf(" (%d issues total)", len(issues))
	}
	return message
}

func verifyAgentIntegration(plan InitPlan, add func(string, string, string)) {
	intent := skill.InspectRepoInstall(plan.layout.workspaceDir)
	if plan.Agent == nil {
		if !intent.Managed {
			if plan.AgentSelection == AgentNone {
				add("agent_integration", CheckPass, "Repo agent guidance was skipped by request.")
			} else {
				add("agent_integration", CheckWarning, "No supported repo agent was detected; rerun with --agent codex to install guidance.")
			}
			return
		}
		if intent.Current {
			add("agent_integration", CheckPass, "Existing repo agent guidance is current; reconciliation was skipped by request.")
			return
		}
		add("agent_integration", CheckFail, "Existing Factile repo agent guidance is incomplete or drifted; rerun `factile init --agent codex` to repair it.")
		return
	}
	if !intent.Managed || !intent.Trusted {
		add("agent_integration", CheckFail, "Factile repo agent guidance is missing trusted generated install metadata; rerun `factile init --agent codex`.")
		return
	}
	if intent.Mode != plan.Agent.Mode || intent.Profile != plan.Agent.Profile {
		add("agent_integration", CheckFail, "Factile repo agent mode or profile differs from the preserved installation intent.")
		return
	}
	if !intent.Current {
		var drifted []string
		if !intent.SkillCurrent {
			drifted = append(drifted, "skill")
		}
		if !intent.AgentsCurrent {
			drifted = append(drifted, "AGENTS.md")
		}
		if !intent.MCPCurrent {
			drifted = append(drifted, "MCP config")
		}
		add("agent_integration", CheckFail, "Generated repo agent integration remains drifted in "+strings.Join(drifted, ", ")+"; rerun `factile init --agent codex`.")
		return
	}
	message := "Codex " + intent.Mode + " guidance matches the current generator"
	if intent.Profile != "" {
		message += " with the " + intent.Profile + " profile"
	}
	add("agent_integration", CheckPass, message+".")
}

type agentInstallPlan struct {
	target   string
	detected bool
	mode     string
	profile  string
	prepared skill.RepoInstallPlan
}

func prepareAgentInstall(workDir string, plan *agentInstallPlan) error {
	if plan == nil {
		return nil
	}
	prepared, err := skill.PrepareRepoInstallAt(workDir, skill.InstallOptions{Scope: "repo", Mode: plan.mode, Profile: plan.profile})
	if err != nil {
		return initError(
			"Repo agent integration cannot be reconciled safely.",
			map[string]string{"cause": err.Error()},
		)
	}
	plan.prepared = prepared
	return nil
}

func normalizeAgent(agent string) (string, error) {
	if agent == "" {
		return AgentAuto, nil
	}
	switch agent {
	case AgentAuto, AgentCodex, AgentNone:
		return agent, nil
	default:
		return "", factile.NewError(factile.ErrInvalidPath, "Unsupported init agent: "+agent)
	}
}

func planAgent(workDir, agent string) *agentInstallPlan {
	if agent == AgentNone {
		return nil
	}
	intent := skill.InspectRepoInstall(workDir)
	if agent == AgentAuto && !intent.Installed && !detectCodex(workDir) {
		return nil
	}
	plan := &agentInstallPlan{target: skill.TargetCodex, detected: agent == AgentAuto, mode: skill.ModeReader}
	if intent.Trusted {
		plan.mode = intent.Mode
		plan.profile = intent.Profile
	}
	return plan
}

func detectCodex(workDir string) bool {
	for _, path := range []string{".codex", ".agents/skills", "AGENTS.md"} {
		if pathExists(filepath.Join(workDir, path)) {
			return true
		}
	}
	return false
}

type initLayout struct {
	invocationDir     string
	workspaceDir      string
	rootBundleDir     string
	root              string
	combined          bool
	rootChanged       bool
	bundle            vfs.BundleConfig
	workspaceManifest *vfs.Manifest
	rootManifest      *vfs.Manifest
}

func inspectInitLayout(opts Options) (initLayout, error) {
	invocationDir, err := initWorkDir(opts.WorkDir)
	if err != nil {
		return initLayout{}, err
	}

	workspaceDir := invocationDir
	var workspaceManifest *vfs.Manifest
	if opts.Workspace != "" {
		workspaceTarget := opts.Workspace
		if !filepath.IsAbs(workspaceTarget) {
			workspaceTarget = filepath.Join(invocationDir, workspaceTarget)
		}
		workspaceDir, err = initWorkDir(workspaceTarget)
		if err != nil {
			return initLayout{}, err
		}
		workspaceManifest, err = loadInitManifest(workspaceDir, "Workspace")
		if err != nil {
			return initLayout{}, err
		}
	} else {
		workspaceDir, workspaceManifest, err = findContainingInitWorkspace(invocationDir)
		if err != nil {
			return initLayout{}, err
		}
		if workspaceDir == "" {
			workspaceDir = invocationDir
			workspaceManifest, err = loadInitManifest(workspaceDir, "Workspace")
			if err != nil {
				return initLayout{}, err
			}
		}
	}

	root := "docs"
	if opts.RootExplicit {
		root = opts.Root
	} else if workspaceManifest != nil && workspaceManifest.Workspace != nil {
		root = workspaceManifest.Workspace.Root
	} else if workspaceManifest != nil && workspaceManifest.Bundle != nil {
		root = "."
	}
	if !vfs.ValidWorkspaceRoot(root) {
		return initLayout{}, initError(
			"Root bundle must be . or a normalized relative directory inside the workspace.",
			map[string]string{"root": root},
		)
	}
	if workspaceManifest != nil && workspaceManifest.Workspace == nil && workspaceManifest.Bundle != nil && root != "." {
		return initLayout{}, initError(
			"A directory that is already a bundle can only become a combined workspace with --root .",
			map[string]string{"manifest": vfs.ManifestPath(workspaceDir)},
		)
	}
	if workspaceManifest != nil && workspaceManifest.Workspace != nil && workspaceManifest.Bundle != nil && root != "." {
		return initLayout{}, initError(
			"A combined workspace cannot select a separate root without removing its existing bundle identity.",
			map[string]string{"manifest": vfs.ManifestPath(workspaceDir), "root": root},
		)
	}

	rootBundleDir := workspaceDir
	if root != "." {
		rootBundleDir = filepath.Join(workspaceDir, filepath.FromSlash(root))
	}
	if err := validateRootDirectory(workspaceDir, root); err != nil {
		return initLayout{}, err
	}

	rootManifest := workspaceManifest
	if root != "." {
		rootManifest, err = loadInitManifest(rootBundleDir, "Root bundle")
		if err != nil {
			return initLayout{}, err
		}
		if rootManifest != nil && (rootManifest.Bundle == nil || rootManifest.Workspace != nil) {
			return initLayout{}, initError(
				"Selected root directory is not a standalone Factile bundle.",
				map[string]string{"manifest": vfs.ManifestPath(rootBundleDir)},
			)
		}
	}
	bundle, err := resolveBundleMetadata(workspaceDir, rootManifest, opts)
	if err != nil {
		return initLayout{}, err
	}

	layout := initLayout{
		invocationDir:     invocationDir,
		workspaceDir:      workspaceDir,
		rootBundleDir:     rootBundleDir,
		root:              root,
		combined:          root == ".",
		bundle:            bundle,
		workspaceManifest: workspaceManifest,
		rootManifest:      rootManifest,
	}
	if workspaceManifest != nil && workspaceManifest.Workspace != nil {
		layout.rootChanged = workspaceManifest.Workspace.Root != root
	}

	for _, filename := range legacyInitPaths(workspaceDir, rootBundleDir) {
		if _, err := os.Lstat(filename); err == nil {
			return initLayout{}, initError(
				"Cannot initialize over a legacy Factile layout. Migrate or remove the legacy files first.",
				map[string]string{"legacy_path": filename},
			)
		} else if !os.IsNotExist(err) {
			return initLayout{}, err
		}
	}
	return validateInitOutputPaths(layout)
}

func findContainingInitWorkspace(start string) (string, *vfs.Manifest, error) {
	for dir := start; ; dir = filepath.Dir(dir) {
		manifest, err := loadInitManifest(dir, "Workspace")
		if err != nil {
			return "", nil, err
		}
		if manifest != nil && manifest.Workspace != nil {
			return dir, manifest, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", nil, nil
		}
	}
}

func loadInitManifest(dir, label string) (*vfs.Manifest, error) {
	filename := vfs.ManifestPath(dir)
	info, err := os.Lstat(filename)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, initError(label+" factile.toml must be a regular file.", map[string]string{"manifest": filename})
	}
	manifest, err := vfs.LoadManifest(dir)
	if err != nil {
		return nil, initError(label+" factile.toml is invalid.", map[string]string{"manifest": filename, "cause": err.Error()})
	}
	return &manifest, nil
}

func resolveBundleMetadata(workspaceDir string, manifest *vfs.Manifest, opts Options) (vfs.BundleConfig, error) {
	var bundle vfs.BundleConfig
	if manifest != nil && manifest.Bundle != nil {
		bundle = *manifest.Bundle
	}

	if opts.NameExplicit {
		name, err := initMetadataValue("Bundle name", opts.Name)
		if err != nil {
			return vfs.BundleConfig{}, err
		}
		if bundle.Name != "" && bundle.Name != name {
			return vfs.BundleConfig{}, initError(
				"Bundle name is stable and cannot be changed by init.",
				map[string]string{"existing_name": bundle.Name, "requested_name": name},
			)
		}
		bundle.Name = name
	}
	if bundle.Name == "" {
		bundle.Name = slugFromName(filepath.Base(workspaceDir))
	}

	if opts.TitleExplicit {
		title, err := initMetadataValue("Bundle title", opts.Title)
		if err != nil {
			return vfs.BundleConfig{}, err
		}
		bundle.Title = title
	}
	if strings.TrimSpace(bundle.Title) == "" {
		bundle.Title = titleFromName(bundle.Name)
	}

	if opts.DescriptionExplicit {
		description, err := initMetadataValue("Bundle description", opts.Description)
		if err != nil {
			return vfs.BundleConfig{}, err
		}
		bundle.Description = description
	}
	if strings.TrimSpace(bundle.Description) == "" {
		bundle.Description = "Documentation and knowledge for " + bundle.Title + "."
	}
	return bundle, nil
}

func initMetadataValue(label, value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", initError(label+" must not be empty.", nil)
	}
	for _, r := range value {
		if unicode.IsControl(r) {
			return "", initError(label+" must be a single line without control characters.", nil)
		}
	}
	return value, nil
}

func slugFromName(value string) string {
	var b strings.Builder
	separator := false
	for _, r := range value {
		switch {
		case r >= 'A' && r <= 'Z':
			if separator && b.Len() > 0 {
				b.WriteByte('-')
			}
			b.WriteRune(r + ('a' - 'A'))
			separator = false
		case (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9'):
			if separator && b.Len() > 0 {
				b.WriteByte('-')
			}
			b.WriteRune(r)
			separator = false
		default:
			separator = true
		}
	}
	if b.Len() == 0 {
		return "project"
	}
	return b.String()
}

func validateRootDirectory(workspaceDir, root string) error {
	if root == "." {
		return nil
	}
	current := workspaceDir
	segments := strings.Split(root, "/")
	for index, segment := range segments {
		current = filepath.Join(current, segment)
		info, err := os.Lstat(current)
		if os.IsNotExist(err) {
			return nil
		}
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
			return initError("Root bundle path must contain only real directories.", map[string]string{"path": current})
		}
		if index == len(segments)-1 {
			continue
		}
		manifest, err := loadInitManifest(current, "Root path component")
		if err != nil {
			return err
		}
		if manifest != nil && manifest.Workspace != nil {
			return initError(
				"Root bundle path must not cross another Factile workspace.",
				map[string]string{"workspace": current},
			)
		}
	}
	return nil
}

func validateInitOutputPaths(layout initLayout) (initLayout, error) {
	for _, filename := range []string{
		filepath.Join(layout.workspaceDir, ".gitignore"),
		vfs.ManifestPath(layout.workspaceDir),
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

func ensureInitLayout(layout initLayout, now time.Time) ([]FileChange, error) {
	workDir := layout.workspaceDir

	var changes []FileChange
	ignore, err := ensureStateIgnored(workDir)
	if err != nil {
		return nil, err
	}
	changes = append(changes, ignore)

	if layout.combined {
		manifest := vfs.Manifest{Version: 2}
		if layout.workspaceManifest != nil {
			manifest = *layout.workspaceManifest
		}
		changed := manifest.Workspace == nil || manifest.Workspace.Root != "." || manifest.Bundle == nil || *manifest.Bundle != layout.bundle
		manifest.Workspace = &vfs.WorkspaceConfig{Root: "."}
		bundle := layout.bundle
		manifest.Bundle = &bundle
		change, err := writeManifest(workDir, vfs.ManifestPath(workDir), manifest, changed)
		if err != nil {
			return nil, err
		}
		changes = append(changes, change)
	} else {
		workspace := vfs.Manifest{Version: 2, Workspace: &vfs.WorkspaceConfig{Root: layout.root}}
		workspaceChanged := layout.workspaceManifest == nil || layout.workspaceManifest.Workspace == nil || layout.workspaceManifest.Workspace.Root != layout.root
		change, err := writeManifest(workDir, vfs.ManifestPath(workDir), workspace, workspaceChanged)
		if err != nil {
			return nil, err
		}
		changes = append(changes, change)

		bundle := vfs.Manifest{Version: 2}
		if layout.rootManifest != nil {
			bundle = *layout.rootManifest
		}
		bundleChanged := bundle.Bundle == nil || *bundle.Bundle != layout.bundle
		bundleConfig := layout.bundle
		bundle.Bundle = &bundleConfig
		change, err = writeManifest(workDir, vfs.ManifestPath(layout.rootBundleDir), bundle, bundleChanged)
		if err != nil {
			return nil, err
		}
		changes = append(changes, change)
	}

	knowledge, err := ensureKnowledgeBundle(workDir, layout.rootBundleDir, layout.bundle, now)
	if err != nil {
		return nil, err
	}
	return append(changes, knowledge...), nil
}

func writeManifest(workDir, filename string, manifest vfs.Manifest, changed bool) (FileChange, error) {
	if !changed {
		return FileChange{Path: relPath(workDir, filename), Action: "unchanged"}, nil
	}
	return writeFileIfChanged(workDir, filename, formatManifest(manifest))
}

func formatManifest(manifest vfs.Manifest) []byte {
	var b strings.Builder
	b.WriteString("version = 2\n")
	if manifest.Workspace != nil {
		b.WriteString("\n[workspace]\nroot = ")
		b.WriteString(quoteTOMLString(manifest.Workspace.Root))
		b.WriteString("\n")
	}
	if manifest.Bundle != nil {
		b.WriteString("\n[bundle]\nname = ")
		b.WriteString(quoteTOMLString(manifest.Bundle.Name))
		b.WriteString("\n")
		writeManifestString(&b, "title", manifest.Bundle.Title)
		writeManifestString(&b, "description", manifest.Bundle.Description)
		writeManifestString(&b, "when_to_use", manifest.Bundle.WhenToUse)
	}
	if manifest.Defaults != nil {
		b.WriteString("\n[defaults]\n")
		writeManifestString(&b, "format", manifest.Defaults.Format)
	}
	return []byte(b.String())
}

func writeManifestString(b *strings.Builder, key, value string) {
	if value == "" {
		return
	}
	b.WriteString(key)
	b.WriteString(" = ")
	b.WriteString(quoteTOMLString(value))
	b.WriteString("\n")
}

func quoteTOMLString(value string) string {
	var b strings.Builder
	b.WriteByte('"')
	for _, r := range value {
		switch r {
		case '\\':
			b.WriteString(`\\`)
		case '"':
			b.WriteString(`\"`)
		case '\b':
			b.WriteString(`\b`)
		case '\t':
			b.WriteString(`\t`)
		case '\n':
			b.WriteString(`\n`)
		case '\f':
			b.WriteString(`\f`)
		case '\r':
			b.WriteString(`\r`)
		default:
			if r < 0x20 || r == 0x7f {
				fmt.Fprintf(&b, `\u%04X`, r)
			} else {
				b.WriteRune(r)
			}
		}
	}
	b.WriteByte('"')
	return b.String()
}

func ensureKnowledgeBundle(workDir, bundlePath string, bundle vfs.BundleConfig, now time.Time) ([]FileChange, error) {
	var changes []FileChange
	for _, file := range []struct {
		path string
		data string
	}{
		{path: filepath.Join(bundlePath, "index.md"), data: indexMarkdown(bundle)},
		{path: filepath.Join(bundlePath, "overview.md"), data: overviewMarkdown(bundle, now)},
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
	created, err := createInitFile(filename, data, 0o644)
	if err != nil {
		return FileChange{}, err
	}
	if !created {
		info, err := os.Lstat(filename)
		if err != nil || !info.Mode().IsRegular() {
			return FileChange{}, fmt.Errorf("authored output changed during initialization: %s", filename)
		}
		return FileChange{Path: relPath(workDir, filename), Action: "unchanged"}, nil
	}
	return FileChange{Path: relPath(workDir, filename), Action: "created"}, nil
}

func writeFileIfChanged(workDir, filename string, data []byte) (FileChange, error) {
	info, err := os.Lstat(filename)
	if err == nil {
		if !info.Mode().IsRegular() {
			return FileChange{}, fmt.Errorf("cannot replace non-regular file %s", filename)
		}
		existing, readErr := os.ReadFile(filename)
		if readErr != nil {
			return FileChange{}, readErr
		}
		if string(existing) == string(data) {
			return FileChange{Path: relPath(workDir, filename), Action: "unchanged"}, nil
		}
		if err := replaceInitFile(filename, data, info.Mode().Perm()); err != nil {
			return FileChange{}, err
		}
		return FileChange{Path: relPath(workDir, filename), Action: "updated"}, nil
	}
	if !os.IsNotExist(err) {
		return FileChange{}, err
	}
	if err := os.MkdirAll(filepath.Dir(filename), 0o755); err != nil {
		return FileChange{}, err
	}
	created, err := createInitFile(filename, data, 0o644)
	if err != nil {
		return FileChange{}, err
	}
	if !created {
		return FileChange{}, fmt.Errorf("initialization output changed during planning: %s", filename)
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
		return writeFileIfChanged(workDir, filename, updated)
	}
	if !os.IsNotExist(err) {
		return FileChange{}, err
	}
	return writeFileIfChanged(workDir, filename, []byte("/.factile/\n"))
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
		runes := []rune(part)
		runes[0] = unicode.ToUpper(runes[0])
		parts[i] = string(runes)
	}
	return strings.Join(parts, " ")
}

func indexMarkdown(bundle vfs.BundleConfig) string {
	title := bundle.Title + " Knowledge"
	return fmt.Sprintf(`---
type: Index
title: %s
description: %s
tags: [factile, project]
---

# %s

- [Overview](overview.md)
`, strconv.Quote(title), strconv.Quote(bundle.Description), title)
}

func overviewMarkdown(bundle vfs.BundleConfig, now time.Time) string {
	title := bundle.Title + " Overview"
	return fmt.Sprintf(`---
type: Reference
title: %s
description: %s
tags: [factile, project]
timestamp: %s
---

# %s

%s
`, strconv.Quote(title), strconv.Quote(bundle.Description), now.Format(time.RFC3339), title, bundle.Description)
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
