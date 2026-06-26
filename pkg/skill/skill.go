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

	"github.com/factile/factile/pkg/factile"
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

//go:embed assets/codex/SKILL.md
var BaseSkillMarkdown string

//go:embed assets/codex/scripts/factile-discover.sh
var DiscoverScript string

//go:embed assets/codex/AGENTS.md.tmpl
var agentsManagedBlockTemplate string

//go:embed assets/codex/config.toml.tmpl
var mcpConfigBlockTemplate string

var SkillMarkdown = skillMarkdown(ModeReader, "")
var AgentsManagedBlock = agentsManagedBlock(ModeReader, "")
var MCPConfigBlock = mcpConfigBlock(ModeReader)

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
			".agents/skills/factile/scripts/factile-discover.sh",
			"AGENTS.md",
			".codex/config.toml",
		},
		SkillMarkdown:  skillMarkdown(mode, ""),
		AgentsBlock:    agentsManagedBlock(mode, ""),
		MCPConfigBlock: mcpConfigBlock(mode),
	}, nil
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
	workDir, err := defaultWorkDir(opts.WorkDir)
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
	repoSkill := filepath.Join(workDir, ".agents", "skills", "factile", "SKILL.md")
	userSkill := filepath.Join(codexHome(), "skills", "factile", "SKILL.md")
	repoSkillExists := fileExists(repoSkill)
	userSkillExists := fileExists(userSkill)
	if repoSkillExists || userSkillExists {
		add("skill_installed", "pass", installedSkillMessage(workDir, repoSkillExists, userSkillExists, userSkill))
	} else {
		add("skill_installed", "fail", "Factile skill is not installed in this repo or user Codex skills")
	}
	repoExpected := repoSkillExists
	agentsPath := filepath.Join(workDir, "AGENTS.md")
	agentsHasBlock := fileContains(agentsPath, AgentsBlockStart) && fileContains(agentsPath, AgentsBlockEnd)
	if repoExpected && agentsHasBlock {
		add("agents_managed_block", "pass", "AGENTS.md contains the Factile managed block")
	} else if repoExpected {
		add("agents_managed_block", "fail", "Repo skill exists but AGENTS.md does not contain the Factile managed block")
	} else if agentsHasBlock {
		add("agents_managed_block", "pass", "AGENTS.md contains the Factile managed block")
	} else {
		add("agents_managed_block", "warning", "Repo-scope Factile guidance is not installed")
	}
	configPath := filepath.Join(workDir, ".codex", "config.toml")
	configOK := fileContains(configPath, "[mcp_servers.factile]") && fileContains(configPath, "mcp") && fileContains(configPath, "serve")
	if repoExpected && configOK {
		add("mcp_config", "pass", ".codex/config.toml contains a local Factile MCP server entry")
	} else if repoExpected {
		add("mcp_config", "fail", "Repo skill exists but .codex/config.toml does not contain the Factile MCP server entry")
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
	workDir, err := defaultWorkDir(opts.WorkDir)
	if err != nil {
		return InstallResult{}, err
	}
	var files []FileChange
	if change, err := writeFileIfChanged(filepath.Join(workDir, ".agents", "skills", "factile", "SKILL.md"), []byte(skillMarkdown(opts.Mode, opts.Profile)), 0o644); err != nil {
		return InstallResult{}, err
	} else {
		files = append(files, displayChange(workDir, change))
	}
	if change, err := writeFileIfChanged(filepath.Join(workDir, ".agents", "skills", "factile", "scripts", "factile-discover.sh"), []byte(DiscoverScript), 0o755); err != nil {
		return InstallResult{}, err
	} else {
		files = append(files, displayChange(workDir, change))
	}
	if change, err := upsertFileBlock(filepath.Join(workDir, "AGENTS.md"), AgentsBlockStart, AgentsBlockEnd, agentsManagedBlock(opts.Mode, opts.Profile)); err != nil {
		return InstallResult{}, err
	} else {
		files = append(files, displayChange(workDir, change))
	}
	if change, err := upsertFileBlock(filepath.Join(workDir, ".codex", "config.toml"), MCPBlockStart, MCPBlockEnd, mcpConfigBlock(opts.Mode)); err != nil {
		return InstallResult{}, err
	} else {
		files = append(files, displayChange(workDir, change))
	}
	return InstallResult{Target: TargetCodex, Scope: "repo", Mode: opts.Mode, Profile: opts.Profile, Files: files, Message: "Installed repo-local Factile Codex skill, AGENTS.md guidance, and local MCP config."}, nil
}

func installUser(opts InstallOptions) (InstallResult, error) {
	root := filepath.Join(codexHome(), "skills", "factile")
	var files []FileChange
	if change, err := writeFileIfChanged(filepath.Join(root, "SKILL.md"), []byte(skillMarkdown(opts.Mode, opts.Profile)), 0o644); err != nil {
		return InstallResult{}, err
	} else {
		files = append(files, change)
	}
	if change, err := writeFileIfChanged(filepath.Join(root, "scripts", "factile-discover.sh"), []byte(DiscoverScript), 0o755); err != nil {
		return InstallResult{}, err
	} else {
		files = append(files, change)
	}
	return InstallResult{Target: TargetCodex, Scope: "user", Mode: opts.Mode, Profile: opts.Profile, Files: files, Message: "Installed the user-level Factile Codex skill. Verify discovery with `factile skill doctor codex --json`."}, nil
}

func uninstallRepo(opts InstallOptions) (UninstallResult, error) {
	workDir, err := defaultWorkDir(opts.WorkDir)
	if err != nil {
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
	root := filepath.Join(codexHome(), "skills", "factile")
	action := "missing"
	if fileExists(root) {
		if err := os.RemoveAll(root); err != nil {
			return UninstallResult{}, err
		}
		action = "removed"
	}
	return UninstallResult{Target: TargetCodex, Scope: "user", Files: []FileChange{{Path: root, Action: action}}, Message: "Removed the user-level Factile Codex skill."}, nil
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
	b.WriteString(skillModeSection(mode))
	if profile != "" {
		b.WriteString("\n")
		b.WriteString(skillProfileSection(profile))
	}
	return b.String()
}

func skillModeSection(mode string) string {
	var b strings.Builder
	b.WriteString("## Mode\n\n")
	switch mode {
	case ModeCurator:
		b.WriteString("Curator mode is installed. Use Factile to manage local knowledge catalogs and OKF documents when the user asks for curation work.\n\n")
		b.WriteString("- Use `factile kb list`, `factile kb inspect`, `factile kb create`, `factile kb link`, `factile kb unlink`, and `factile kb view ...` for Knowledge Base catalog work.\n")
		b.WriteString("- Use `factile list --brief`, `factile stat`, `factile validate`, and `factile context` before changing knowledge.\n")
		b.WriteString("- Use `--view <id>` on reader commands when a named Knowledge Base View is relevant.\n")
		b.WriteString("- Use write commands only with required revisions and only when the user asked to change knowledge.\n")
		b.WriteString("- Validate the affected path after catalog or content changes.\n")
	default:
		b.WriteString("Reader mode is installed. Use Factile to discover and consume local knowledge without mutating catalogs or OKF documents.\n\n")
		b.WriteString("- Prefer `factile list / --brief --json`, `factile stat <path> --json`, and `factile context / '<task>' --json`.\n")
		b.WriteString("- Use `--view <id>` with `list`, `search`, `context`, or `graph` when a named Knowledge Base View is relevant.\n")
		b.WriteString("- Do not use `factile create`, `write`, `patch`, `rename`, `delete`, `deprecate`, `kb`, `bundle mount`, or `bundle unmount` commands unless the user explicitly asks to curate knowledge.\n")
		b.WriteString("- Treat the configured MCP server as read-only.\n")
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
		return "Mode: curator. Catalog curation may use `factile kb ...`, including `factile kb view set` and `factile kb view delete`, low-level `factile bundle ...`, and revision-aware write commands when the user asks to change knowledge. Validate affected paths after changes."
	}
	return "Mode: reader. Do not edit Factile/OKF knowledge or catalog state unless the user explicitly asks to curate knowledge."
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

func defaultWorkDir(workDir string) (string, error) {
	if workDir != "" {
		return filepath.Abs(workDir)
	}
	return os.Getwd()
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
	current, err := os.ReadFile(filename)
	if err == nil {
		if bytes.Equal(current, data) {
			_ = os.Chmod(filename, mode)
			return FileChange{Path: filename, Action: "unchanged"}, nil
		}
		action = "updated"
	} else if !errors.Is(err, os.ErrNotExist) {
		return FileChange{}, err
	}
	if err := os.MkdirAll(filepath.Dir(filename), 0o755); err != nil {
		return FileChange{}, err
	}
	if err := os.WriteFile(filename, data, mode); err != nil {
		return FileChange{}, err
	}
	return FileChange{Path: filename, Action: action}, nil
}

func upsertFileBlock(filename, start, end, block string) (FileChange, error) {
	action := "created"
	data, err := os.ReadFile(filename)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return FileChange{}, err
	}
	if err == nil {
		action = "updated"
	}
	next := upsertManagedBlock(string(data), start, end, block)
	if err == nil && next == string(data) {
		return FileChange{Path: filename, Action: "unchanged"}, nil
	}
	if err := os.MkdirAll(filepath.Dir(filename), 0o755); err != nil {
		return FileChange{}, err
	}
	if err := os.WriteFile(filename, []byte(next), 0o644); err != nil {
		return FileChange{}, err
	}
	return FileChange{Path: filename, Action: action}, nil
}

func upsertManagedBlock(content, start, end, block string) string {
	block = ensureTrailingNewline(block)
	startIdx := strings.Index(content, start)
	endIdx := strings.Index(content, end)
	if startIdx >= 0 && endIdx > startIdx {
		endIdx += len(end)
		next := content[:startIdx] + strings.TrimRight(block, "\n") + content[endIdx:]
		return ensureTrailingNewline(next)
	}
	if strings.TrimSpace(content) == "" {
		return block
	}
	next := ensureTrailingNewline(content)
	if !strings.HasSuffix(next, "\n\n") {
		next += "\n"
	}
	return next + block
}

func removeFileBlock(filename, start, end string) (FileChange, error) {
	data, err := os.ReadFile(filename)
	if errors.Is(err, os.ErrNotExist) {
		return FileChange{Path: filename, Action: "missing"}, nil
	}
	if err != nil {
		return FileChange{}, err
	}
	next, removed := removeManagedBlock(string(data), start, end)
	if !removed {
		return FileChange{Path: filename, Action: "unchanged"}, nil
	}
	if err := os.WriteFile(filename, []byte(next), 0o644); err != nil {
		return FileChange{}, err
	}
	return FileChange{Path: filename, Action: "updated"}, nil
}

func removeManagedBlock(content, start, end string) (string, bool) {
	startIdx := strings.Index(content, start)
	endIdx := strings.Index(content, end)
	if startIdx < 0 || endIdx <= startIdx {
		return content, false
	}
	endIdx += len(end)
	if endIdx < len(content) && content[endIdx] == '\n' {
		endIdx++
	}
	next := content[:startIdx] + content[endIdx:]
	next = strings.TrimRight(next, "\n")
	if next != "" {
		next += "\n"
	}
	return next, true
}

func removeFileIfExists(filename string) (FileChange, error) {
	if err := os.Remove(filename); err == nil {
		return FileChange{Path: filename, Action: "removed"}, nil
	} else if errors.Is(err, os.ErrNotExist) {
		return FileChange{Path: filename, Action: "missing"}, nil
	} else {
		return FileChange{}, err
	}
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
