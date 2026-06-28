package render

import (
	"fmt"
	"io"
	"strings"
)

type helpItem struct {
	command     string
	description string
}

func (r *Renderer) RenderHelp(w io.Writer) error {
	if _, err := fmt.Fprintln(w, r.helpTitle("Factile local OKF tool")); err != nil {
		return err
	}
	if _, err := fmt.Fprintln(w, "Local OKF knowledge as paths. Read by default; curate only when you mean to change catalogs or documents."); err != nil {
		return err
	}
	if _, err := fmt.Fprintln(w); err != nil {
		return err
	}
	if err := r.renderHelpSection(w, "Start here", []helpItem{
		{command: "factile init", description: "Create .factile knowledge in this repo"},
		{command: "factile", description: "Show this workspace summary"},
		{command: "factile status", description: "Show this workspace summary"},
		{command: "factile /", description: "Browse or read from a path"},
		{command: "factile list /", description: "Browse available knowledge"},
		{command: "factile list / --brief", description: "Show compact reader cards"},
		{command: "factile context / \"what should I know?\"", description: "Gather task context"},
		{command: "factile version", description: "Show build version"},
	}); err != nil {
		return err
	}
	if _, err := fmt.Fprintln(w); err != nil {
		return err
	}
	if _, err := fmt.Fprintln(w, r.helpTitle("Usage")); err != nil {
		return err
	}
	if _, err := fmt.Fprintln(w, "  factile [global options] (<command> [args] | <path>)"); err != nil {
		return err
	}
	if _, err := fmt.Fprintln(w, "  global options: --mount-file <path>, --json, --format text|json, --color auto|always|never, --quiet, --version"); err != nil {
		return err
	}
	if _, err := fmt.Fprintln(w, "  Global options may appear before or after the command."); err != nil {
		return err
	}
	if _, err := fmt.Fprintln(w); err != nil {
		return err
	}
	for _, section := range []struct {
		title string
		items []helpItem
	}{
		{title: "Reader commands", items: []helpItem{
			{command: "status", description: "Show workspace knowledge, views, sources, and next commands"},
			{command: "list [path] [--brief] [--view <id>]", description: "Browse folders and documents"},
			{command: "stat <path>", description: "Show one compact card"},
			{command: "read <concept-path>", description: "Read one document"},
			{command: "search <path> <query> [--view <id>]", description: "Search local documents"},
			{command: "context <path> <query> [--depth 0|1] [--view <id>]", description: "Assemble relevant context"},
			{command: "graph <path> [--depth 0|1] [--view <id>]", description: "Inspect local links"},
			{command: "validate <path> [--view <id>]", description: "Validate an OKF scope"},
		}},
		{title: "Curator commands", items: []helpItem{
			{command: "kb list", description: "List Knowledge Bases"},
			{command: "kb inspect <kb-path>", description: "Inspect a Knowledge Base"},
			{command: "kb create <kb-path> --title <title>", description: "Create a Knowledge Base"},
			{command: "kb link <kb-path> <source> <bundle-path>", description: "Add a local Bundle"},
			{command: "kb unlink <bundle-path>", description: "Remove a Bundle link"},
			{command: "view list", description: "List library views"},
			{command: "view inspect <id>", description: "Inspect a library view"},
			{command: "view set <id> --title <title> --path <path>", description: "Create or replace a library view"},
			{command: "view delete <id>", description: "Delete a library view"},
		}},
		{title: "Write commands", items: []helpItem{
			{command: "create <concept-path> --type <type> --title <title> --body <file>", description: "Create a document"},
			{command: "write <concept-path> --rev <rev> --body <file>", description: "Replace Markdown body"},
			{command: "patch <concept-path> --rev <rev> [patch options]", description: "Edit frontmatter or sections"},
			{command: "rename <old-path> <new-path> --rev <rev>", description: "Move one document"},
			{command: "delete <concept-path> --rev <rev>", description: "Delete one document"},
			{command: "deprecate <concept-path> --rev <rev> --reason <text>", description: "Mark a document deprecated"},
		}},
		{title: "Bundle admin", items: []helpItem{
			{command: "bundle find [path]", description: "Find local OKF bundles"},
			{command: "bundle inspect <source>", description: "Inspect a source directory"},
			{command: "bundle list", description: "List direct mounts"},
			{command: "bundle mount <source> <mount-path>", description: "Add a direct mount"},
			{command: "bundle unmount <mount-path>", description: "Remove a direct mount"},
		}},
		{title: "Agents and MCP", items: []helpItem{
			{command: "skill install codex --scope repo|user", description: "Install agent guidance"},
			{command: "skill doctor codex", description: "Check agent setup"},
			{command: "mcp serve --stdio [--read-only]", description: "Run the local MCP server"},
		}},
	} {
		if err := r.renderHelpSection(w, section.title, section.items); err != nil {
			return err
		}
		if _, err := fmt.Fprintln(w); err != nil {
			return err
		}
	}
	if _, err := fmt.Fprintln(w, "Use --json for scripts and agents. Use '<command> --help' for command-specific usage."); err != nil {
		return err
	}
	return nil
}

func (r *Renderer) renderHelpSection(w io.Writer, title string, items []helpItem) error {
	if _, err := fmt.Fprintln(w, r.helpTitle(title)); err != nil {
		return err
	}
	columns := make([]helpCommandColumns, 0, len(items))
	commandWidth := 0
	argsWidth := 0
	for _, item := range items {
		command, args := splitHelpCommand(item.command)
		columns = append(columns, helpCommandColumns{command: command, args: args})
		if len(command) > commandWidth {
			commandWidth = len(command)
		}
		if len(args) > argsWidth {
			argsWidth = len(args)
		}
	}
	for i, item := range items {
		parts := columns[i]
		command := fmt.Sprintf("%-*s", commandWidth, parts.command)
		if argsWidth > 0 {
			command += " " + fmt.Sprintf("%-*s", argsWidth, parts.args)
		}
		description := item.description
		if r.colorEnabled {
			command = r.styles.Value.Render(command)
			description = r.styles.Muted.Render(description)
		}
		if _, err := fmt.Fprintf(w, "  %s  %s\n", command, description); err != nil {
			return err
		}
	}
	return nil
}

type helpCommandColumns struct {
	command string
	args    string
}

func splitHelpCommand(command string) (string, string) {
	idx := -1
	for _, marker := range []string{" <", " ["} {
		if found := strings.Index(command, marker); found >= 0 && (idx < 0 || found < idx) {
			idx = found
		}
	}
	if idx < 0 {
		return command, ""
	}
	return command[:idx], strings.TrimSpace(command[idx:])
}

func (r *Renderer) helpTitle(value string) string {
	if !r.colorEnabled {
		return value
	}
	return r.styles.Heading.Render(value)
}
