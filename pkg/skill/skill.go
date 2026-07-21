package skill

import (
	"bytes"
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/factile/factile/internal/atomicfile"
	"github.com/factile/factile/pkg/factile"
	"github.com/factile/factile/pkg/vfs"
)

const TargetCodex = "codex"
const ModeReader = "reader"
const ModeCurator = "curator"

const AgentsBlockStart = "<!-- factile:codex:start -->"
const AgentsBlockEnd = "<!-- factile:codex:end -->"
const MCPBlockStart = "# factile:codex-mcp:start"
const MCPBlockEnd = "# factile:codex-mcp:end"

const Summary = "Use local Factile OKF knowledge when a task depends on repository-specific architecture, design decisions, domain concepts, workflows, runbooks, standards, policy, legal, compliance, or documentation knowledge."

const Description = "Use local Factile OKF knowledge for architecture, design, documentation, review, runbook, standards, policy, legal, compliance, domain, or implementation-choice tasks that need repository knowledge. Discover local knowledge paths, retrieve focused context, and cite relevant concepts. Do not use for mechanical renames, formatting, syntax fixes, or obvious local edits."

const legacyV040SkillSignature = "Factile exposes one workspace's OKF knowledge as a virtual filesystem."

//go:embed assets/codex/SKILL.md
var BaseSkillMarkdown string

//go:embed assets/codex/AGENTS.md.tmpl
var agentsManagedBlockTemplate string

//go:embed assets/codex/config.toml.tmpl
var mcpConfigBlockTemplate string

var SkillMarkdown = skillMarkdown(ModeReader, "")
var AgentsManagedBlock = agentsManagedBlock(ModeReader, "")
var MCPConfigBlock = mcpConfigBlock(ModeReader)

var replaceManagedFile = atomicfile.Write
var createManagedFile = atomicfile.Create

type SummaryItem struct {
	Target      string `json:"target"`
	Name        string `json:"name"`
	Summary     string `json:"summary"`
	Description string `json:"description"`
}

type ListResult struct {
	Skills []SummaryItem `json:"skills"`
}

type InspectResult struct {
	Target         string   `json:"target"`
	Name           string   `json:"name"`
	Summary        string   `json:"summary"`
	Description    string   `json:"description"`
	Files          []string `json:"files"`
	SkillMarkdown  string   `json:"skill_markdown"`
	AgentsBlock    string   `json:"agents_block"`
	MCPConfigBlock string   `json:"mcp_config_block"`
}

type InstallOptions struct {
	Scope   string
	WorkDir string
	Mode    string
	Profile string
}

type FileChange struct {
	Path   string `json:"path"`
	Action string `json:"action"`
}

type InstallResult struct {
	Target  string       `json:"target"`
	Scope   string       `json:"scope"`
	Mode    string       `json:"mode,omitempty"`
	Profile string       `json:"profile,omitempty"`
	Files   []FileChange `json:"files"`
	Message string       `json:"message"`
}

// RepoInstallPlan is a read-only, fully prepared repo-scope reconciliation.
// Its fields are private so only a plan produced by PrepareRepoInstall can be
// applied.
type RepoInstallPlan struct {
	workDir string
	mode    string
	profile string
	changes []preparedRepoChange
}

type preparedRepoChange struct {
	path   string
	action string
	data   []byte
	mode   os.FileMode
	remove bool
}

type UninstallResult struct {
	Target  string       `json:"target"`
	Scope   string       `json:"scope"`
	Files   []FileChange `json:"files"`
	Message string       `json:"message"`
}

type DoctorOptions struct {
	WorkDir string
	Probe   string
}

type DoctorCheck struct {
	Name    string `json:"name"`
	Status  string `json:"status"`
	Message string `json:"message"`
}

type DoctorResult struct {
	Target string        `json:"target"`
	OK     bool          `json:"ok"`
	Checks []DoctorCheck `json:"checks"`
}

type installedSkillState struct {
	Exists     bool
	Recognized bool
	Current    bool
	Mode       string
	Profile    string
}

type managedBlockKind uint8

const (
	managedBlockAbsent managedBlockKind = iota
	managedBlockSingle
	managedBlockMultiple
	managedBlockMalformed
)

type managedBlockRange struct {
	start int
	end   int
}

type managedBlockLayout struct {
	kind   managedBlockKind
	ranges []managedBlockRange
}

type InstallIntent struct {
	Installed     bool   `json:"installed"`
	Managed       bool   `json:"managed"`
	Trusted       bool   `json:"trusted"`
	Current       bool   `json:"current"`
	SkillCurrent  bool   `json:"skill_current"`
	AgentsCurrent bool   `json:"agents_current"`
	MCPCurrent    bool   `json:"mcp_current"`
	Mode          string `json:"mode,omitempty"`
	Profile       string `json:"profile,omitempty"`
}

func List() ListResult {
	return ListResult{Skills: []SummaryItem{{
		Target:      TargetCodex,
		Name:        "factile",
		Summary:     Summary,
		Description: Description,
	}}}
}

func Inspect(target string) (InspectResult, error) {
	if err := validateTarget(target); err != nil {
		return InspectResult{}, err
	}
	mode, err := normalizeMode("")
	if err != nil {
		return InspectResult{}, err
	}
	return InspectResult{
		Target:      TargetCodex,
		Name:        "factile",
		Summary:     Summary,
		Description: Description,
		Files: []string{
			".agents/skills/factile/SKILL.md",
			"AGENTS.md",
			".codex/config.toml",
		},
		SkillMarkdown:  skillMarkdown(mode, ""),
		AgentsBlock:    agentsManagedBlock(mode, ""),
		MCPConfigBlock: mcpConfigBlock(mode),
	}, nil
}

// InspectRepoInstall reports generated repo integration intent and drift.
// Only recognized generated skill formats are trusted for mode and profile.
func InspectRepoInstall(workDir string) InstallIntent {
	state := inspectInstalledSkill(filepath.Join(workDir, ".agents", "skills", "factile", "SKILL.md"))
	agentsPath := filepath.Join(workDir, "AGENTS.md")
	mcpPath := filepath.Join(workDir, ".codex", "config.toml")
	agentsLayout, _, _ := inspectManagedFile(agentsPath, AgentsBlockStart, AgentsBlockEnd)
	mcpLayout, _, _ := inspectManagedFile(mcpPath, MCPBlockStart, MCPBlockEnd)
	intent := InstallIntent{
		Installed:    state.Exists,
		Managed:      state.Recognized || agentsLayout.kind != managedBlockAbsent || mcpLayout.kind != managedBlockAbsent,
		Trusted:      state.Recognized,
		SkillCurrent: state.Current,
	}
	if state.Recognized {
		intent.Mode = state.Mode
		intent.Profile = state.Profile
		intent.AgentsCurrent = managedFileBlockMatches(agentsPath, AgentsBlockStart, AgentsBlockEnd, agentsManagedBlock(state.Mode, state.Profile))
		intent.MCPCurrent = managedFileBlockMatches(mcpPath, MCPBlockStart, MCPBlockEnd, mcpConfigBlock(state.Mode))
	}
	intent.Current = intent.SkillCurrent && intent.AgentsCurrent && intent.MCPCurrent
	return intent
}

// PreflightRepoInstall rejects repo integration paths that could escape the
// workspace or fail predictably after another init surface has been written.
func PreflightRepoInstall(workDir string) error {
	skillPath := filepath.Join(workDir, ".agents", "skills", "factile", "SKILL.md")
	for _, filename := range []string{
		skillPath,
		filepath.Join(workDir, ".agents", "skills", "factile", "scripts", "factile-discover.sh"),
		filepath.Join(workDir, "AGENTS.md"),
		filepath.Join(workDir, ".codex", "config.toml"),
	} {
		if err := validateRepoInstallPath(workDir, filename); err != nil {
			return err
		}
	}
	for _, filename := range []string{
		skillPath,
		filepath.Join(workDir, "AGENTS.md"),
		filepath.Join(workDir, ".codex", "config.toml"),
	} {
		if err := validateReadableManagedFile(filename); err != nil {
			return err
		}
	}
	if err := validateManagedSkillOwnership(skillPath); err != nil {
		return err
	}
	for _, block := range []struct {
		filename string
		start    string
		end      string
	}{
		{filepath.Join(workDir, "AGENTS.md"), AgentsBlockStart, AgentsBlockEnd},
		{filepath.Join(workDir, ".codex", "config.toml"), MCPBlockStart, MCPBlockEnd},
	} {
		if err := preflightManagedFile(block.filename, block.start, block.end); err != nil {
			return err
		}
	}
	return nil
}

func validateRepoInstallPath(workDir, filename string) error {
	return validateManagedPath(workDir, filename, "Repo agent")
}

func validateManagedPath(anchor, filename, label string) error {
	rel, err := filepath.Rel(anchor, filename)
	if err != nil || filepath.IsAbs(rel) || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return factile.NewError(factile.ErrInvalidPath, label+" output path must stay inside its scope.")
	}
	parts := strings.Split(rel, string(filepath.Separator))
	current := anchor
	for _, part := range parts[:len(parts)-1] {
		current = filepath.Join(current, part)
		info, err := os.Lstat(current)
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
			return factile.NewError(factile.ErrInvalidPath, label+" output path must use real directories: "+filepath.ToSlash(rel))
		}
	}
	info, err := os.Lstat(filename)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return factile.NewError(factile.ErrInvalidPath, label+" output path must be a regular file: "+filepath.ToSlash(rel))
	}
	return nil
}

func validateManagedSkillOwnership(filename string) error {
	state := inspectInstalledSkill(filename)
	if state.Exists && !state.Recognized {
		return factile.NewError(factile.ErrInvalidPath, "Factile cannot replace an unrecognized skill at "+filename+". Move or remove it, then retry.")
	}
	return nil
}

func validateReadableManagedFile(filename string) error {
	info, err := os.Lstat(filename)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if info.Mode().Perm()&0o444 == 0 {
		return factile.NewError(factile.ErrInvalidPath, "Managed input must be readable: "+filename)
	}
	return nil
}

func preflightManagedFile(filename, start, end string) error {
	layout, _, err := inspectManagedFile(filename, start, end)
	if err != nil {
		return err
	}
	if layout.kind == managedBlockMalformed {
		return factile.NewError(factile.ErrInvalidPath, "Factile managed markers are malformed in "+filename+". Repair them before retrying.")
	}
	return nil
}

func Install(target string, opts InstallOptions) (InstallResult, error) {
	if err := validateTarget(target); err != nil {
		return InstallResult{}, err
	}
	mode, err := normalizeMode(opts.Mode)
	if err != nil {
		return InstallResult{}, err
	}
	if err := validateProfile(opts.Profile); err != nil {
		return InstallResult{}, err
	}
	opts.Mode = mode
	scope := opts.Scope
	if scope == "" {
		scope = "repo"
	}
	switch scope {
	case "repo":
		return installRepo(opts)
	case "user":
		return installUser(opts)
	default:
		return InstallResult{}, factile.NewError(factile.ErrInvalidPath, "Unsupported skill install scope: "+scope)
	}
}

func Uninstall(target string, opts InstallOptions) (UninstallResult, error) {
	if err := validateTarget(target); err != nil {
		return UninstallResult{}, err
	}
	scope := opts.Scope
	if scope == "" {
		scope = "repo"
	}
	switch scope {
	case "repo":
		return uninstallRepo(opts)
	case "user":
		return uninstallUser(opts)
	default:
		return UninstallResult{}, factile.NewError(factile.ErrInvalidPath, "Unsupported skill uninstall scope: "+scope)
	}
}

func Doctor(ctx context.Context, target string, opts DoctorOptions) (DoctorResult, error) {
	if err := validateTarget(target); err != nil {
		return DoctorResult{}, err
	}
	workDir, err := doctorWorkDir(opts.WorkDir)
	if err != nil {
		return DoctorResult{}, err
	}
	if opts.Probe == "" {
		opts.Probe = "local knowledge discovery probe"
	}
	result := DoctorResult{Target: TargetCodex, OK: true}
	add := func(name, status, message string) {
		if status == "fail" {
			result.OK = false
		}
		result.Checks = append(result.Checks, DoctorCheck{Name: name, Status: status, Message: message})
	}
	if path, err := exec.LookPath("factile"); err == nil {
		add("factile_on_path", "pass", "factile found at "+path)
	} else {
		add("factile_on_path", "fail", "factile is not on PATH")
	}
	addWorkspaceLayoutCheck(workDir, opts.WorkDir != "", add)
	repoSkill := filepath.Join(workDir, ".agents", "skills", "factile", "SKILL.md")
	userSkill := filepath.Join(codexHome(), "skills", "factile", "SKILL.md")
	repoSkillExists := fileExists(repoSkill)
	userSkillExists := fileExists(userSkill)
	repoSkillState := inspectInstalledSkill(repoSkill)
	userSkillState := inspectInstalledSkill(userSkill)
	if repoSkillExists || userSkillExists {
		add("skill_installed", "pass", installedSkillMessage(workDir, repoSkillExists, userSkillExists, userSkill))
	} else {
		add("skill_installed", "fail", "Factile skill is not installed in this repo or user Codex skills")
	}
	repoExpected := repoSkillExists
	agentsPath := filepath.Join(workDir, "AGENTS.md")
	agentsLayout, _, agentsErr := inspectManagedFile(agentsPath, AgentsBlockStart, AgentsBlockEnd)
	if repoExpected && repoSkillState.Recognized && managedFileBlockMatches(agentsPath, AgentsBlockStart, AgentsBlockEnd, agentsManagedBlock(repoSkillState.Mode, repoSkillState.Profile)) {
		add("agents_managed_block", "pass", "AGENTS.md contains the current Factile managed block")
	} else if repoExpected {
		add("agents_managed_block", "fail", "AGENTS.md does not match the installed Factile skill mode and profile; rerun `factile skill install codex --scope repo` with the intended options.")
	} else if agentsErr != nil || agentsLayout.kind == managedBlockMalformed {
		add("agents_managed_block", "fail", "AGENTS.md Factile managed markers are unreadable or malformed.")
	} else if agentsLayout.kind != managedBlockAbsent {
		add("agents_managed_block", "fail", "AGENTS.md contains orphan Factile managed guidance without a recognized repo skill.")
	} else {
		add("agents_managed_block", "warning", "Repo-scope Factile guidance is not installed")
	}
	addGuidanceLayoutCheck(repoSkillState, userSkillState, add)
	configPath := filepath.Join(workDir, ".codex", "config.toml")
	configOK := fileContains(configPath, "[mcp_servers.factile]") && fileContains(configPath, "mcp") && fileContains(configPath, "serve")
	mcpLayout, _, mcpErr := inspectManagedFile(configPath, MCPBlockStart, MCPBlockEnd)
	if repoExpected && repoSkillState.Recognized && managedFileBlockMatches(configPath, MCPBlockStart, MCPBlockEnd, mcpConfigBlock(repoSkillState.Mode)) {
		add("mcp_config", "pass", ".codex/config.toml matches the installed Factile skill mode")
	} else if repoExpected {
		add("mcp_config", "fail", ".codex/config.toml does not match the installed Factile skill mode; rerun `factile skill install codex --scope repo` with the intended options.")
	} else if mcpErr != nil || mcpLayout.kind == managedBlockMalformed {
		add("mcp_config", "fail", ".codex/config.toml Factile managed markers are unreadable or malformed.")
	} else if mcpLayout.kind != managedBlockAbsent {
		add("mcp_config", "fail", ".codex/config.toml contains an orphan Factile managed block without a recognized repo skill.")
	} else if configOK {
		add("mcp_config", "pass", ".codex/config.toml contains a local Factile MCP server entry")
	} else {
		add("mcp_config", "warning", "Local Factile MCP server entry is not configured")
	}
	addFactileCommandCheck(ctx, workDir, "factile_list_root", []string{"list", "/", "--json"}, add)
	addFactileCommandCheck(ctx, workDir, "factile_context_root", []string{"context", "/", opts.Probe, "--json"}, add)
	return result, nil
}

func installRepo(opts InstallOptions) (InstallResult, error) {
	plan, err := PrepareRepoInstall(opts)
	if err != nil {
		return InstallResult{}, err
	}
	return ApplyRepoInstall(plan)
}

// PrepareRepoInstall reads and validates every repo-scope input and computes
// every desired change without mutating the workspace.
func PrepareRepoInstall(opts InstallOptions) (RepoInstallPlan, error) {
	mode, err := normalizeMode(opts.Mode)
	if err != nil {
		return RepoInstallPlan{}, err
	}
	if err := validateProfile(opts.Profile); err != nil {
		return RepoInstallPlan{}, err
	}
	workDir, err := repoWorkDir(opts.WorkDir)
	if err != nil {
		return RepoInstallPlan{}, err
	}
	return buildRepoInstallPlan(workDir, mode, opts.Profile)
}

// PrepareRepoInstallAt prepares reconciliation at an already validated repo
// directory. It is used by init before the workspace manifest exists.
func PrepareRepoInstallAt(workDir string, opts InstallOptions) (RepoInstallPlan, error) {
	mode, err := normalizeMode(opts.Mode)
	if err != nil {
		return RepoInstallPlan{}, err
	}
	if err := validateProfile(opts.Profile); err != nil {
		return RepoInstallPlan{}, err
	}
	workDir, err = filepath.Abs(workDir)
	if err != nil {
		return RepoInstallPlan{}, err
	}
	workDir, err = filepath.EvalSymlinks(workDir)
	if err != nil {
		return RepoInstallPlan{}, err
	}
	info, err := os.Stat(workDir)
	if err != nil || !info.IsDir() {
		return RepoInstallPlan{}, factile.NewError(factile.ErrInvalidPath, "Repo skill target must be an existing directory: "+workDir)
	}
	return buildRepoInstallPlan(workDir, mode, opts.Profile)
}

func buildRepoInstallPlan(workDir, mode, profile string) (RepoInstallPlan, error) {
	if err := PreflightRepoInstall(workDir); err != nil {
		return RepoInstallPlan{}, err
	}
	plan := RepoInstallPlan{workDir: workDir, mode: mode, profile: profile}
	skillPath := filepath.Join(workDir, ".agents", "skills", "factile", "SKILL.md")
	change, err := prepareRepoWrite(skillPath, []byte(skillMarkdown(mode, profile)), 0o644)
	if err != nil {
		return RepoInstallPlan{}, err
	}
	plan.changes = append(plan.changes, change)
	legacyScript := filepath.Join(workDir, ".agents", "skills", "factile", "scripts", "factile-discover.sh")
	legacy, err := prepareRepoRemoval(legacyScript)
	if err != nil {
		return RepoInstallPlan{}, err
	}
	if legacy.action == "removed" {
		plan.changes = append(plan.changes, legacy)
	}
	agents, err := prepareRepoBlock(filepath.Join(workDir, "AGENTS.md"), AgentsBlockStart, AgentsBlockEnd, agentsManagedBlock(mode, profile))
	if err != nil {
		return RepoInstallPlan{}, err
	}
	plan.changes = append(plan.changes, agents)
	mcp, err := prepareRepoBlock(filepath.Join(workDir, ".codex", "config.toml"), MCPBlockStart, MCPBlockEnd, mcpConfigBlock(mode))
	if err != nil {
		return RepoInstallPlan{}, err
	}
	plan.changes = append(plan.changes, mcp)
	return plan, nil
}

// ApplyRepoInstall publishes a previously prepared repo reconciliation.
func ApplyRepoInstall(plan RepoInstallPlan) (InstallResult, error) {
	if plan.workDir == "" {
		return InstallResult{}, factile.NewError(factile.ErrInvalidPath, "Invalid empty repo skill plan")
	}
	files := make([]FileChange, 0, len(plan.changes))
	for _, prepared := range plan.changes {
		switch {
		case prepared.action == "unchanged":
		case prepared.remove:
			if err := os.Remove(prepared.path); err != nil {
				return InstallResult{}, err
			}
		case prepared.action == "created":
			if err := os.MkdirAll(filepath.Dir(prepared.path), 0o755); err != nil {
				return InstallResult{}, err
			}
			created, err := createManagedFile(prepared.path, prepared.data, prepared.mode)
			if err != nil {
				return InstallResult{}, err
			}
			if !created {
				return InstallResult{}, fmt.Errorf("managed output changed during planning: %s", prepared.path)
			}
		case prepared.action == "updated":
			if err := replaceManagedFile(prepared.path, prepared.data, prepared.mode); err != nil {
				return InstallResult{}, err
			}
		}
		files = append(files, displayChange(plan.workDir, FileChange{Path: prepared.path, Action: prepared.action}))
	}
	return InstallResult{Target: TargetCodex, Scope: "repo", Mode: plan.mode, Profile: plan.profile, Files: files, Message: "Installed repo-local Factile Codex skill, AGENTS.md guidance, and local MCP config."}, nil
}

func prepareRepoWrite(filename string, data []byte, mode os.FileMode) (preparedRepoChange, error) {
	info, err := os.Lstat(filename)
	if errors.Is(err, os.ErrNotExist) {
		return preparedRepoChange{path: filename, action: "created", data: data, mode: mode}, nil
	}
	if err != nil {
		return preparedRepoChange{}, err
	}
	if !info.Mode().IsRegular() {
		return preparedRepoChange{}, fmt.Errorf("managed output path must be a regular file: %s", filename)
	}
	current, err := os.ReadFile(filename)
	if err != nil {
		return preparedRepoChange{}, err
	}
	if bytes.Equal(current, data) {
		return preparedRepoChange{path: filename, action: "unchanged", mode: mode}, nil
	}
	return preparedRepoChange{path: filename, action: "updated", data: data, mode: mode}, nil
}

func prepareRepoBlock(filename, start, end, block string) (preparedRepoChange, error) {
	info, err := os.Lstat(filename)
	mode := os.FileMode(0o644)
	var content string
	action := "created"
	if err == nil {
		if !info.Mode().IsRegular() {
			return preparedRepoChange{}, fmt.Errorf("managed output path must be a regular file: %s", filename)
		}
		data, readErr := os.ReadFile(filename)
		if readErr != nil {
			return preparedRepoChange{}, readErr
		}
		content = string(data)
		mode = info.Mode().Perm()
		action = "updated"
	} else if !errors.Is(err, os.ErrNotExist) {
		return preparedRepoChange{}, err
	}
	next, err := upsertManagedBlock(content, start, end, block)
	if err != nil {
		return preparedRepoChange{}, err
	}
	if action == "updated" && next == content {
		action = "unchanged"
	}
	return preparedRepoChange{path: filename, action: action, data: []byte(next), mode: mode}, nil
}

func prepareRepoRemoval(filename string) (preparedRepoChange, error) {
	info, err := os.Lstat(filename)
	if errors.Is(err, os.ErrNotExist) {
		return preparedRepoChange{path: filename, action: "missing"}, nil
	}
	if err != nil {
		return preparedRepoChange{}, err
	}
	if !info.Mode().IsRegular() {
		return preparedRepoChange{}, fmt.Errorf("managed output path must be a regular file: %s", filename)
	}
	parent, err := os.Stat(filepath.Dir(filename))
	if err != nil {
		return preparedRepoChange{}, err
	}
	if parent.Mode().Perm()&0o222 == 0 {
		return preparedRepoChange{}, fmt.Errorf("managed legacy output cannot be removed from read-only directory: %s", filename)
	}
	return preparedRepoChange{path: filename, action: "removed", remove: true}, nil
}

func installUser(opts InstallOptions) (InstallResult, error) {
	anchor, root, legacyScript, err := userSkillPaths()
	if err != nil {
		return InstallResult{}, err
	}
	if err := preflightUserSkill(anchor, filepath.Join(root, "SKILL.md"), legacyScript); err != nil {
		return InstallResult{}, err
	}
	var files []FileChange
	if change, err := writeFileIfChanged(filepath.Join(root, "SKILL.md"), []byte(skillMarkdown(opts.Mode, opts.Profile)), 0o644); err != nil {
		return InstallResult{}, err
	} else {
		files = append(files, change)
	}
	if change, err := removeFileIfExists(legacyScript); err != nil {
		return InstallResult{}, err
	} else if change.Action == "removed" {
		files = append(files, change)
	}
	return InstallResult{Target: TargetCodex, Scope: "user", Mode: opts.Mode, Profile: opts.Profile, Files: files, Message: "Installed the user-level Factile Codex skill. Verify discovery with `factile skill doctor codex --json`."}, nil
}

func uninstallRepo(opts InstallOptions) (UninstallResult, error) {
	workDir, err := repoWorkDir(opts.WorkDir)
	if err != nil {
		return UninstallResult{}, err
	}
	if err := PreflightRepoInstall(workDir); err != nil {
		return UninstallResult{}, err
	}
	var files []FileChange
	for _, filename := range []string{
		filepath.Join(workDir, ".agents", "skills", "factile", "scripts", "factile-discover.sh"),
		filepath.Join(workDir, ".agents", "skills", "factile", "SKILL.md"),
	} {
		change, err := removeFileIfExists(filename)
		if err != nil {
			return UninstallResult{}, err
		}
		files = append(files, displayChange(workDir, change))
	}
	if change, err := removeFileBlock(filepath.Join(workDir, "AGENTS.md"), AgentsBlockStart, AgentsBlockEnd); err != nil {
		return UninstallResult{}, err
	} else {
		files = append(files, displayChange(workDir, change))
	}
	if change, err := removeFileBlock(filepath.Join(workDir, ".codex", "config.toml"), MCPBlockStart, MCPBlockEnd); err != nil {
		return UninstallResult{}, err
	} else {
		files = append(files, displayChange(workDir, change))
	}
	return UninstallResult{Target: TargetCodex, Scope: "repo", Files: files, Message: "Removed repo-local Factile Codex managed files and blocks."}, nil
}

func uninstallUser(opts InstallOptions) (UninstallResult, error) {
	_ = opts
	anchor, root, legacyScript, err := userSkillPaths()
	if err != nil {
		return UninstallResult{}, err
	}
	skillPath := filepath.Join(root, "SKILL.md")
	if err := preflightUserSkill(anchor, skillPath, legacyScript); err != nil {
		return UninstallResult{}, err
	}
	var files []FileChange
	for _, filename := range []string{legacyScript, skillPath} {
		change, err := removeFileIfExists(filename)
		if err != nil {
			return UninstallResult{}, err
		}
		files = append(files, change)
	}
	return UninstallResult{Target: TargetCodex, Scope: "user", Files: files, Message: "Removed the user-level Factile Codex skill."}, nil
}

func userSkillPaths() (string, string, string, error) {
	anchor, err := filepath.Abs(codexHome())
	if err != nil {
		return "", "", "", err
	}
	info, err := os.Stat(anchor)
	if err == nil {
		if !info.IsDir() {
			return "", "", "", factile.NewError(factile.ErrInvalidPath, "Codex home must be a directory: "+anchor)
		}
		anchor, err = filepath.EvalSymlinks(anchor)
		if err != nil {
			return "", "", "", err
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return "", "", "", err
	}
	root := filepath.Join(anchor, "skills", "factile")
	return anchor, root, filepath.Join(root, "scripts", "factile-discover.sh"), nil
}

func preflightUserSkill(anchor, skillPath, legacyScript string) error {
	for _, filename := range []string{skillPath, legacyScript} {
		if err := validateManagedPath(anchor, filename, "User skill"); err != nil {
			return err
		}
	}
	if err := validateReadableManagedFile(skillPath); err != nil {
		return err
	}
	return validateManagedSkillOwnership(skillPath)
}

func normalizeMode(mode string) (string, error) {
	if mode == "" {
		return ModeReader, nil
	}
	switch mode {
	case ModeReader, ModeCurator:
		return mode, nil
	default:
		return "", factile.NewError(factile.ErrInvalidPath, "Unsupported skill mode: "+mode)
	}
}

func validateProfile(profile string) error {
	switch profile {
	case "", "software":
		return nil
	default:
		return factile.NewError(factile.ErrInvalidPath, "Unsupported skill profile: "+profile)
	}
}

func skillMarkdown(mode string, profile string) string {
	var b strings.Builder
	b.WriteString(BaseSkillMarkdown)
	b.WriteString("\n")
	b.WriteString(skillInstallMarker(mode, profile))
	b.WriteString("\n\n")
	b.WriteString(skillModeSection(mode))
	if profile != "" {
		b.WriteString("\n")
		b.WriteString(skillProfileSection(profile))
	}
	return b.String()
}

func skillInstallMarker(mode string, profile string) string {
	if profile == "" {
		profile = "none"
	}
	return "<!-- factile:install mode=" + mode + " profile=" + profile + " -->"
}

func skillModeSection(mode string) string {
	var b strings.Builder
	b.WriteString("## Mode\n\n")
	switch mode {
	case ModeCurator:
		b.WriteString("Curator mode is installed. Use Factile to manage local and read-only Git path mounts, views, and OKF documents when the user asks for curation work.\n\n")
		b.WriteString("- Use `factile mount`, `factile unmount`, and `factile mounts` to manage `<name>.mount.toml` path mounts.\n")
		b.WriteString("- Git mounts are always read-only; use `factile refresh <mount-path>` only for an immediate upstream check.\n")
		b.WriteString("- Use `factile view list`, `factile view inspect`, `factile view set`, and `factile view delete` to manage workspace-level `factile.views.toml`.\n")
		b.WriteString("- Use `factile list / --brief --json`, `factile stat <path> --json`, and focused `factile context` before changing knowledge.\n")
		b.WriteString("- Use narrower paths or `--view <id>` when the task needs a smaller reader scope.\n")
		b.WriteString("- Use write commands only with required revisions and only when the user asked to change knowledge.\n")
		b.WriteString("- Validate the affected path after mount, view, or content changes.\n")
	default:
		b.WriteString("Reader mode is installed. Use Factile to discover and consume workspace knowledge without mutating workspace or bundle manifests, mount descriptors, views, or OKF documents.\n\n")
		b.WriteString("- Inspect Git mount status with `factile mounts --json`; explicit refresh changes generated cache state, not source content.\n")
		b.WriteString("- Do not use `factile create`, `write`, `patch`, `rename`, `delete`, `deprecate`, `mount`, `unmount`, `view set`, or `view delete` unless the user explicitly asks to curate knowledge.\n")
		b.WriteString("- The configured MCP server must include `--read-only`; `factile skill doctor codex --json` verifies the generated mode and config agree.\n")
	}
	return b.String()
}

func skillProfileSection(profile string) string {
	var b strings.Builder
	b.WriteString("## Profile\n\n")
	b.WriteString("Profile: `" + profile + "`.\n\n")
	b.WriteString("Use profile templates, examples, and recipes when they are installed as extension data. Do not assume recipe commands or profile-specific engine APIs exist.\n")
	return b.String()
}

func agentsManagedBlock(mode string, profile string) string {
	return renderTemplate(
		agentsManagedBlockTemplate,
		"{{MODE_BLOCK}}", agentsModeBlock(mode),
		"{{PROFILE_BLOCK}}", agentsProfileBlock(profile),
	)
}

func agentsModeBlock(mode string) string {
	if mode == ModeCurator {
		return "Mode: curator. Mutate Factile knowledge only when the user explicitly asks; preserve source capabilities, required revisions, and affected-path validation."
	}
	return "Mode: reader. Do not mutate Factile manifests, views, mount descriptors, or OKF documents unless the user explicitly asks to curate knowledge; the configured MCP server must remain read-only."
}

func agentsProfileBlock(profile string) string {
	if profile == "" {
		return ""
	}
	return "Profile: `" + profile + "`. Use installed profile recipes and templates as extension data; do not assume recipe commands or profile-specific engine APIs.\n\n"
}

func mcpConfigBlock(mode string) string {
	args := "[\"mcp\", \"serve\", \"--stdio\"]"
	if mode == ModeReader {
		args = "[\"mcp\", \"serve\", \"--stdio\", \"--read-only\"]"
	}
	return renderTemplate(mcpConfigBlockTemplate, "{{ARGS}}", args)
}

func renderTemplate(template string, pairs ...string) string {
	return strings.NewReplacer(pairs...).Replace(template)
}

func validateTarget(target string) error {
	if target != TargetCodex {
		return factile.NewError(factile.ErrUnsupportedCommand, "Unsupported skill target: "+target)
	}
	return nil
}

func repoWorkDir(workDir string) (string, error) {
	if workDir != "" {
		workspace, err := vfs.ResolveWorkspace(vfs.ResolveWorkspaceOptions{Workspace: workDir})
		if err != nil {
			return "", factile.NormalizeError(err)
		}
		return workspace.WorkspaceDir, nil
	}
	cwd, err := defaultWorkDir("")
	if err != nil {
		return "", err
	}
	workspace, err := vfs.ResolveWorkspace(vfs.ResolveWorkspaceOptions{WorkDir: cwd})
	if err == nil {
		return workspace.WorkspaceDir, nil
	}
	if factile.ErrorCode(factile.NormalizeError(err)) == factile.ErrNoActiveWorkspace {
		return cwd, nil
	}
	return "", factile.NormalizeError(err)
}

func doctorWorkDir(workDir string) (string, error) {
	base, err := defaultWorkDir(workDir)
	if err != nil {
		return "", err
	}
	opts := vfs.ResolveWorkspaceOptions{WorkDir: base}
	if workDir != "" {
		opts = vfs.ResolveWorkspaceOptions{Workspace: base}
	}
	workspace, err := vfs.ResolveWorkspace(opts)
	if err == nil {
		return workspace.WorkspaceDir, nil
	}
	return base, nil
}

func defaultWorkDir(workDir string) (string, error) {
	if workDir != "" {
		return filepath.Abs(workDir)
	}
	return os.Getwd()
}

func addWorkspaceLayoutCheck(workDir string, exact bool, add func(string, string, string)) {
	resolveOptions := vfs.ResolveWorkspaceOptions{WorkDir: workDir}
	if exact {
		resolveOptions = vfs.ResolveWorkspaceOptions{Workspace: workDir}
	}
	workspace, err := vfs.ResolveWorkspace(resolveOptions)
	if err == nil {
		root := workspace.RootBundleDir
		if rel, relErr := filepath.Rel(workspace.WorkspaceDir, root); relErr == nil {
			root = filepath.ToSlash(rel)
		}
		add("workspace_layout", "pass", "Workspace factile.toml selects root bundle "+root)
		return
	}

	normalized := factile.NormalizeError(err)
	if legacyPath, migration := legacyLayoutDetails(workDir, normalized); legacyPath != "" {
		message := "Legacy Factile layout found at " + legacyPath + "."
		if migration != "" {
			message += " " + migration
		} else {
			message += " Create a workspace factile.toml and root-bundle factile.toml, then move views to workspace-level factile.views.toml."
		}
		add("workspace_layout", "fail", message)
		return
	}

	if factile.ErrorCode(normalized) == factile.ErrNoActiveWorkspace {
		add("workspace_layout", "warning", "No Factile workspace is active; run `factile init` before using contextual reader or MCP commands.")
		return
	}
	add("workspace_layout", "fail", normalized.Error())
}

func legacyLayoutDetails(workDir string, err error) (string, string) {
	if app, ok := err.(*factile.AppError); ok {
		legacy, _ := app.Details["legacy_path"].(string)
		migration, _ := app.Details["migration"].(string)
		if legacy != "" {
			return legacy, migration
		}
	}
	for _, filename := range []string{
		filepath.Join(workDir, ".factile", "config.toml"),
		filepath.Join(workDir, ".factile", "views.toml"),
		filepath.Join(workDir, "docs", ".factile", "config.toml"),
		filepath.Join(workDir, "docs", ".factile", "views.toml"),
	} {
		if fileExists(filename) {
			return filename, ""
		}
	}
	return "", ""
}

func addGuidanceLayoutCheck(repoSkill, userSkill installedSkillState, add func(string, string, string)) {
	if !repoSkill.Exists && !userSkill.Exists {
		add("guidance_layout", "warning", "No installed Factile guidance is available to check")
		return
	}
	var drifted []string
	if repoSkill.Exists && (!repoSkill.Recognized || !repoSkill.Current) {
		drifted = append(drifted, "repo skill")
	}
	if userSkill.Exists && (!userSkill.Recognized || !userSkill.Current) {
		drifted = append(drifted, "user skill")
	}
	if len(drifted) > 0 {
		add("guidance_layout", "fail", "Generated Factile guidance is missing install metadata or differs from the current generator in "+strings.Join(drifted, ", ")+"; rerun `factile skill install codex --scope repo` or `--scope user` for the affected scope.")
		return
	}
	add("guidance_layout", "pass", "Installed Factile skill files match the current generator")
}

func inspectInstalledSkill(filename string) installedSkillState {
	data, err := os.ReadFile(filename)
	if err != nil {
		return installedSkillState{}
	}
	state := installedSkillState{Exists: true}
	for _, mode := range []string{ModeReader, ModeCurator} {
		for _, profile := range []string{"", "software"} {
			if !strings.Contains(string(data), skillInstallMarker(mode, profile)) {
				continue
			}
			if state.Recognized {
				return state
			}
			state.Recognized = true
			state.Mode = mode
			state.Profile = profile
		}
	}
	if !state.Recognized {
		if mode, profile, ok := legacyV040InstallIntent(string(data)); ok {
			state.Recognized = true
			state.Mode = mode
			state.Profile = profile
		}
	}
	if state.Recognized {
		state.Current = bytes.Equal(data, []byte(skillMarkdown(state.Mode, state.Profile)))
	}
	return state
}

func legacyV040InstallIntent(content string) (string, string, bool) {
	if !strings.Contains(content, "# Factile local knowledge workflow") || !strings.Contains(content, legacyV040SkillSignature) {
		return "", "", false
	}
	reader := strings.Contains(content, "Reader mode is installed.")
	curator := strings.Contains(content, "Curator mode is installed.")
	if reader == curator {
		return "", "", false
	}
	mode := ModeReader
	if curator {
		mode = ModeCurator
	}
	profile := ""
	if strings.Contains(content, "## Profile") {
		if !strings.Contains(content, "Profile: `software`.") {
			return "", "", false
		}
		profile = "software"
	}
	return mode, profile, true
}

func managedFileBlockMatches(filename, start, end, expected string) bool {
	layout, data, err := inspectManagedFile(filename, start, end)
	if err != nil || layout.kind != managedBlockSingle {
		return false
	}
	managed := layout.ranges[0]
	return string(data[managed.start:managed.end]) == strings.TrimRight(expected, "\r\n")
}

func inspectManagedFile(filename, start, end string) (managedBlockLayout, []byte, error) {
	data, err := os.ReadFile(filename)
	if errors.Is(err, os.ErrNotExist) {
		return managedBlockLayout{kind: managedBlockAbsent}, nil, nil
	}
	if err != nil {
		return managedBlockLayout{kind: managedBlockMalformed}, nil, err
	}
	return classifyManagedBlocks(string(data), start, end), data, nil
}

func classifyManagedBlocks(content, start, end string) managedBlockLayout {
	if start == "" || end == "" || start == end {
		return managedBlockLayout{kind: managedBlockMalformed}
	}
	layout := managedBlockLayout{kind: managedBlockAbsent}
	cursor := 0
	open := -1
	for cursor < len(content) {
		nextStart := strings.Index(content[cursor:], start)
		if nextStart >= 0 {
			nextStart += cursor
		}
		nextEnd := strings.Index(content[cursor:], end)
		if nextEnd >= 0 {
			nextEnd += cursor
		}
		if nextStart < 0 && nextEnd < 0 {
			break
		}
		if nextStart >= 0 && (nextEnd < 0 || nextStart < nextEnd) {
			if open >= 0 {
				return managedBlockLayout{kind: managedBlockMalformed}
			}
			open = nextStart
			cursor = nextStart + len(start)
			continue
		}
		if open < 0 {
			return managedBlockLayout{kind: managedBlockMalformed}
		}
		layout.ranges = append(layout.ranges, managedBlockRange{start: open, end: nextEnd + len(end)})
		open = -1
		cursor = nextEnd + len(end)
	}
	if open >= 0 {
		return managedBlockLayout{kind: managedBlockMalformed}
	}
	switch len(layout.ranges) {
	case 0:
		layout.kind = managedBlockAbsent
	case 1:
		layout.kind = managedBlockSingle
	default:
		layout.kind = managedBlockMultiple
	}
	return layout
}

func codexHome() string {
	if value := os.Getenv("CODEX_HOME"); value != "" {
		return value
	}
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return filepath.Join(".", ".codex")
	}
	return filepath.Join(home, ".codex")
}

func writeFileIfChanged(filename string, data []byte, mode os.FileMode) (FileChange, error) {
	action := "created"
	info, err := os.Lstat(filename)
	if err == nil {
		if !info.Mode().IsRegular() {
			return FileChange{}, fmt.Errorf("managed output path must be a regular file: %s", filename)
		}
		current, err := os.ReadFile(filename)
		if err != nil {
			return FileChange{}, err
		}
		if bytes.Equal(current, data) {
			return FileChange{Path: filename, Action: "unchanged"}, nil
		}
		action = "updated"
	} else if !errors.Is(err, os.ErrNotExist) {
		return FileChange{}, err
	}
	if err := os.MkdirAll(filepath.Dir(filename), 0o755); err != nil {
		return FileChange{}, err
	}
	if action == "updated" {
		if err := replaceManagedFile(filename, data, mode); err != nil {
			return FileChange{}, err
		}
	} else {
		created, err := createManagedFile(filename, data, mode)
		if err != nil {
			return FileChange{}, err
		}
		if !created {
			return FileChange{}, fmt.Errorf("managed output changed during planning: %s", filename)
		}
	}
	return FileChange{Path: filename, Action: action}, nil
}

func upsertFileBlock(filename, start, end, block string) (FileChange, error) {
	action := "created"
	mode := os.FileMode(0o644)
	info, err := os.Lstat(filename)
	var data []byte
	if err == nil {
		if !info.Mode().IsRegular() {
			return FileChange{}, fmt.Errorf("managed output path must be a regular file: %s", filename)
		}
		data, err = os.ReadFile(filename)
	}
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return FileChange{}, err
	}
	if err == nil {
		action = "updated"
		mode = info.Mode().Perm()
	}
	next, err := upsertManagedBlock(string(data), start, end, block)
	if err != nil {
		return FileChange{}, err
	}
	if err == nil && next == string(data) {
		return FileChange{Path: filename, Action: "unchanged"}, nil
	}
	if err := os.MkdirAll(filepath.Dir(filename), 0o755); err != nil {
		return FileChange{}, err
	}
	if action == "updated" {
		if err := replaceManagedFile(filename, []byte(next), mode); err != nil {
			return FileChange{}, err
		}
	} else {
		created, err := createManagedFile(filename, []byte(next), mode)
		if err != nil {
			return FileChange{}, err
		}
		if !created {
			return FileChange{}, fmt.Errorf("managed output changed during planning: %s", filename)
		}
	}
	return FileChange{Path: filename, Action: action}, nil
}

func upsertManagedBlock(content, start, end, block string) (string, error) {
	layout := classifyManagedBlocks(content, start, end)
	if layout.kind == managedBlockMalformed {
		return "", factile.NewError(factile.ErrInvalidPath, "Factile managed markers are malformed. Repair them before retrying.")
	}
	block = ensureTrailingNewline(block)
	if layout.kind != managedBlockAbsent {
		var next strings.Builder
		first := layout.ranges[0]
		next.WriteString(content[:first.start])
		next.WriteString(strings.TrimRight(block, "\r\n"))
		cursor := first.end
		for _, managed := range layout.ranges[1:] {
			next.WriteString(content[cursor:managed.start])
			cursor = managed.end
		}
		next.WriteString(content[cursor:])
		return next.String(), nil
	}
	if content == "" {
		return block, nil
	}
	next := ensureTrailingNewline(content)
	if !strings.HasSuffix(next, "\n\n") {
		next += "\n"
	}
	return next + block, nil
}

func removeFileBlock(filename, start, end string) (FileChange, error) {
	info, err := os.Lstat(filename)
	if errors.Is(err, os.ErrNotExist) {
		return FileChange{Path: filename, Action: "missing"}, nil
	}
	if err != nil {
		return FileChange{}, err
	}
	if !info.Mode().IsRegular() {
		return FileChange{}, fmt.Errorf("managed output path must be a regular file: %s", filename)
	}
	data, err := os.ReadFile(filename)
	if err != nil {
		return FileChange{}, err
	}
	next, removed, err := removeManagedBlock(string(data), start, end)
	if err != nil {
		return FileChange{}, err
	}
	if !removed {
		return FileChange{Path: filename, Action: "unchanged"}, nil
	}
	if err := replaceManagedFile(filename, []byte(next), info.Mode().Perm()); err != nil {
		return FileChange{}, err
	}
	return FileChange{Path: filename, Action: "updated"}, nil
}

func removeManagedBlock(content, start, end string) (string, bool, error) {
	layout := classifyManagedBlocks(content, start, end)
	if layout.kind == managedBlockMalformed {
		return "", false, factile.NewError(factile.ErrInvalidPath, "Factile managed markers are malformed. Repair them before retrying.")
	}
	if layout.kind == managedBlockAbsent {
		return content, false, nil
	}
	var next strings.Builder
	cursor := 0
	for _, managed := range layout.ranges {
		next.WriteString(content[cursor:managed.start])
		cursor = managed.end
	}
	next.WriteString(content[cursor:])
	return next.String(), true, nil
}

func removeFileIfExists(filename string) (FileChange, error) {
	info, err := os.Lstat(filename)
	if errors.Is(err, os.ErrNotExist) {
		return FileChange{Path: filename, Action: "missing"}, nil
	}
	if err != nil {
		return FileChange{}, err
	}
	if !info.Mode().IsRegular() {
		return FileChange{}, fmt.Errorf("managed output path must be a regular file: %s", filename)
	}
	if err := os.Remove(filename); err != nil {
		return FileChange{}, err
	}
	return FileChange{Path: filename, Action: "removed"}, nil
}

func displayChange(workDir string, change FileChange) FileChange {
	if rel, err := filepath.Rel(workDir, change.Path); err == nil && !strings.HasPrefix(rel, "..") {
		change.Path = filepath.ToSlash(rel)
	}
	return change
}

func ensureTrailingNewline(value string) string {
	if value == "" || strings.HasSuffix(value, "\n") {
		return value
	}
	return value + "\n"
}

func fileExists(filename string) bool {
	_, err := os.Stat(filename)
	return err == nil
}

func fileContains(filename, needle string) bool {
	data, err := os.ReadFile(filename)
	return err == nil && strings.Contains(string(data), needle)
}

func installedSkillMessage(workDir string, repoSkillExists, userSkillExists bool, userSkill string) string {
	var parts []string
	if repoSkillExists {
		parts = append(parts, "repo skill at "+filepath.ToSlash(filepath.Join(".agents", "skills", "factile", "SKILL.md")))
	}
	if userSkillExists {
		if rel, err := filepath.Rel(workDir, userSkill); err == nil && !strings.HasPrefix(rel, "..") {
			userSkill = rel
		}
		parts = append(parts, "user skill at "+userSkill)
	}
	return strings.Join(parts, "; ")
}

func addFactileCommandCheck(ctx context.Context, workDir, name string, args []string, add func(string, string, string)) {
	checkCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(checkCtx, "factile", args...)
	cmd.Dir = workDir
	output, err := cmd.CombinedOutput()
	if checkCtx.Err() == context.DeadlineExceeded {
		add(name, "fail", "factile command timed out")
		return
	}
	if err != nil {
		add(name, "fail", strings.TrimSpace(fmt.Sprintf("%v: %s", err, output)))
		return
	}
	var decoded any
	if err := json.Unmarshal(output, &decoded); err != nil {
		add(name, "fail", "factile command did not return JSON")
		return
	}
	add(name, "pass", "factile "+strings.Join(commandDisplayArgs(args), " ")+" returned JSON")
}

func commandDisplayArgs(args []string) []string {
	display := make([]string, 0, len(args))
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--json":
			continue
		case "--format":
			i++
			continue
		default:
			display = append(display, args[i])
		}
	}
	return display
}
