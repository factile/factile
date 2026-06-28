package cli

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	clirender "github.com/factile/factile/internal/cli/render"
	"github.com/factile/factile/pkg/bootstrap"
	"github.com/factile/factile/pkg/factile"
	"github.com/factile/factile/pkg/mcpserver"
	"github.com/factile/factile/pkg/okf"
	"github.com/factile/factile/pkg/skill"
	"github.com/factile/factile/pkg/trace"
	"github.com/factile/factile/pkg/version"
)

type globals struct {
	MountFile string
	Format    string
	Color     clirender.ColorMode
	Quiet     bool
	Help      bool
	Version   bool
}

type stringListFlag []string

func (f *stringListFlag) String() string {
	return strings.Join(*f, ",")
}

func (f *stringListFlag) Set(value string) error {
	*f = append(*f, value)
	return nil
}

const (
	formatText = "text"
	formatJSON = "json"
)

func (g globals) structuredOutput() bool {
	return g.Format == formatJSON
}

func Run(ctx context.Context, args []string, stdin io.Reader, stdout io.Writer, stderr io.Writer) int {
	started := time.Now()
	global, rest, err := parseGlobals(args)
	if err != nil {
		code := writeError(stderr, global, factile.NewError(factile.ErrInvalidPath, err.Error()))
		traceCLI(rest, code, started)
		return code
	}
	if global.Version {
		code, err := writeVersionResult(stdout, global)
		if err != nil {
			code = writeError(stderr, global, err)
			traceCLI(rest, code, started)
			return code
		}
		traceCLI(rest, code, started)
		return code
	}
	if global.Help {
		if err := writeHelp(stdout, global); err != nil {
			code := writeError(stderr, global, err)
			traceCLI(rest, code, started)
			return code
		}
		traceCLI(rest, 0, started)
		return 0
	}
	if global.Format == "" {
		global.Format = formatText
	}
	ws := factile.NewWorkspace(factile.WorkspaceOptions{MountFile: global.MountFile})
	if len(rest) == 0 {
		result, err := ws.Summary(ctx)
		if err != nil {
			code := writeError(stderr, global, err)
			traceCLI(rest, code, started)
			return code
		}
		code, err := writeSummaryResult(stdout, global, result)
		if err != nil {
			code = writeError(stderr, global, err)
			traceCLI(rest, code, started)
			return code
		}
		traceCLI(rest, code, started)
		return code
	}
	code, err := runCommand(ctx, ws, rest, global, stdin, stdout, stderr)
	if err != nil {
		code = writeError(stderr, global, err)
		traceCLI(rest, code, started)
		return code
	}
	traceCLI(rest, code, started)
	return code
}

func runCommand(ctx context.Context, ws factile.Workspace, args []string, global globals, stdin io.Reader, stdout io.Writer, stderr io.Writer) (int, error) {
	if isPathShortcut(args) {
		return runPathShortcut(ctx, ws, args[0], global, stdout)
	}
	switch args[0] {
	case "version":
		if hasHelp(args) {
			return showUsage(stdout, "factile version")
		}
		if len(args) != 1 {
			return usage(global, stdout, "factile version")
		}
		return writeVersionResult(stdout, global)
	case "status":
		if hasHelp(args) {
			return showUsage(stdout, "factile status")
		}
		if len(args) != 1 {
			return usage(global, stdout, "factile status")
		}
		result, err := ws.Summary(ctx)
		if err != nil {
			return 0, err
		}
		return writeSummaryResult(stdout, global, result)
	case "init":
		return runInit(ctx, args, global, stdout)
	case "list":
		if hasHelp(args) {
			return showUsage(stdout, "factile list [path] [--brief] [--view <id>]")
		}
		fs := flag.NewFlagSet("list", flag.ContinueOnError)
		fs.SetOutput(io.Discard)
		brief := fs.Bool("brief", false, "")
		view := fs.String("view", "", "")
		ordered, orderErr := reorderFlags(args[1:], map[string]bool{"--brief": false, "--view": true})
		if orderErr != nil {
			return 2, orderErr
		}
		if err := fs.Parse(ordered); err != nil {
			return 2, err
		}
		if fs.NArg() > 1 {
			return usage(global, stdout, "factile list [path] [--brief] [--view <id>]")
		}
		path := "/"
		if fs.NArg() == 1 {
			path = fs.Arg(0)
		}
		result, err := ws.List(ctx, path, factile.ListOptions{Brief: *brief, View: *view})
		if err != nil {
			return 0, err
		}
		return writeListResult(stdout, global, result)
	case "stat":
		if hasHelp(args) {
			return showUsage(stdout, "factile stat <path>")
		}
		if len(args) != 2 {
			return usage(global, stdout, "factile stat <path>")
		}
		result, err := ws.Stat(ctx, args[1], factile.StatOptions{})
		if err != nil {
			return 0, err
		}
		return writeStatResult(stdout, global, result)
	case "read":
		if hasHelp(args) {
			return showUsage(stdout, "factile read <concept-path>")
		}
		if len(args) != 2 {
			return usage(global, stdout, "factile read <concept-path>")
		}
		result, err := ws.Read(ctx, args[1], factile.ReadOptions{})
		if err != nil {
			return 0, err
		}
		return writeReadResult(stdout, global, result)
	case "search":
		if hasHelp(args) {
			return showUsage(stdout, "factile search <path> <query> [--view <id>]")
		}
		fs := flag.NewFlagSet("search", flag.ContinueOnError)
		fs.SetOutput(io.Discard)
		view := fs.String("view", "", "")
		ordered, orderErr := reorderFlags(args[1:], map[string]bool{"--view": true})
		if orderErr != nil {
			return 2, orderErr
		}
		if err := fs.Parse(ordered); err != nil {
			return 2, err
		}
		if fs.NArg() != 2 {
			return usage(global, stdout, "factile search <path> <query> [--view <id>]")
		}
		result, err := ws.Search(ctx, fs.Arg(0), fs.Arg(1), factile.SearchOptions{View: *view})
		if err != nil {
			return 0, err
		}
		return writeSearchResult(stdout, global, result)
	case "context":
		return runContext(ctx, ws, args, global, stdout)
	case "graph":
		return runGraph(ctx, ws, args, global, stdout)
	case "validate":
		if hasHelp(args) {
			return showUsage(stdout, "factile validate <path> [--view <id>]")
		}
		fs := flag.NewFlagSet("validate", flag.ContinueOnError)
		fs.SetOutput(io.Discard)
		view := fs.String("view", "", "")
		ordered, orderErr := reorderFlags(args[1:], map[string]bool{"--view": true})
		if orderErr != nil {
			return 2, orderErr
		}
		if err := fs.Parse(ordered); err != nil {
			return 2, err
		}
		if fs.NArg() != 1 {
			return usage(global, stdout, "factile validate <path> [--view <id>]")
		}
		result, err := ws.Validate(ctx, fs.Arg(0), factile.ValidateOptions{View: *view})
		if err != nil {
			return 0, err
		}
		if _, err := writeValidationResult(stdout, global, result); err != nil {
			return 1, err
		}
		if !result.Valid {
			return 3, nil
		}
		return 0, nil
	case "create":
		return runCreate(ctx, ws, args, global, stdout)
	case "write":
		return runWrite(ctx, ws, args, global, stdout)
	case "patch":
		return runPatch(ctx, ws, args, global, stdout)
	case "rename":
		return runRename(ctx, ws, args, global, stdout)
	case "delete":
		return runDelete(ctx, ws, args, global, stdout)
	case "deprecate":
		return runDeprecate(ctx, ws, args, global, stdout)
	case "bundle":
		return runBundle(ctx, ws, args[1:], global, stdout)
	case "kb":
		return runKB(ctx, ws, args[1:], global, stdout)
	case "view":
		return runView(ctx, ws, args[1:], global, stdout)
	case "skill":
		return runSkill(ctx, args[1:], global, stdout)
	case "mcp":
		return runMCP(ctx, global, args[1:], stdin, stdout)
	default:
		return 0, factile.NewError(factile.ErrUnsupportedCommand, "Unsupported command: "+args[0])
	}
}

func isPathShortcut(args []string) bool {
	return len(args) == 1 && strings.HasPrefix(args[0], "/")
}

func runPathShortcut(ctx context.Context, ws factile.Workspace, path string, global globals, stdout io.Writer) (int, error) {
	readResult, err := ws.Read(ctx, path, factile.ReadOptions{})
	if err == nil {
		return writeReadResult(stdout, global, readResult)
	}
	if factile.ErrorCode(err) != factile.ErrConceptNotFound {
		return 0, err
	}
	listResult, listErr := ws.List(ctx, path, factile.ListOptions{})
	if listErr != nil {
		return 0, listErr
	}
	return writeListResult(stdout, global, listResult)
}

func runInit(ctx context.Context, args []string, global globals, stdout io.Writer) (int, error) {
	if hasHelp(args) {
		return showUsage(stdout, "factile init [--knowledge-base <path>] [--agent <agent>]")
	}
	fs := flag.NewFlagSet("init", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	agent := fs.String("agent", "", "")
	knowledge := fs.String("knowledge", "", "")
	knowledgeBase := fs.String("knowledge-base", "", "")
	ordered, orderErr := reorderFlags(args[1:], map[string]bool{"--agent": true, "--knowledge": true, "--knowledge-base": true})
	if orderErr != nil {
		return 2, orderErr
	}
	if err := fs.Parse(ordered); err != nil {
		return 2, err
	}
	if fs.NArg() != 0 {
		return usage(global, stdout, "factile init [--knowledge-base <path>] [--agent <agent>]")
	}
	knowledgePath := *knowledge
	if *knowledgeBase != "" {
		knowledgePath = *knowledgeBase
	}
	var agents []string
	if *agent != "" {
		agents = []string{*agent}
	}
	result, err := bootstrap.Init(ctx, bootstrap.Options{KnowledgePath: knowledgePath, Agents: agents})
	if err != nil {
		return 0, err
	}
	if !global.structuredOutput() {
		return writeInitResult(stdout, global, result)
	}
	return writeResult(stdout, global, result)
}

func runContext(ctx context.Context, ws factile.Workspace, args []string, global globals, stdout io.Writer) (int, error) {
	if hasHelp(args) {
		return showUsage(stdout, "factile context <path> <query> [--max-tokens <n>] [--depth 0|1] [--view <id>]")
	}
	fs := flag.NewFlagSet("context", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	maxTokens := fs.Int("max-tokens", 4000, "")
	depth := fs.Int("depth", 1, "")
	view := fs.String("view", "", "")
	ordered, orderErr := reorderFlags(args[1:], map[string]bool{"--max-tokens": true, "--depth": true, "--view": true})
	if orderErr != nil {
		return 2, orderErr
	}
	if err := fs.Parse(ordered); err != nil {
		return 2, err
	}
	if fs.NArg() != 2 {
		return usage(global, stdout, "factile context <path> <query> [--max-tokens <n>] [--depth 0|1] [--view <id>]")
	}
	result, err := ws.Context(ctx, fs.Arg(0), fs.Arg(1), factile.ContextOptions{MaxTokens: *maxTokens, Depth: *depth, View: *view})
	if err != nil {
		return 0, err
	}
	return writeContextResult(stdout, global, result)
}

func runGraph(ctx context.Context, ws factile.Workspace, args []string, global globals, stdout io.Writer) (int, error) {
	if hasHelp(args) {
		return showUsage(stdout, "factile graph <path> [--depth 0|1] [--view <id>]")
	}
	fs := flag.NewFlagSet("graph", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	depth := fs.Int("depth", 1, "")
	view := fs.String("view", "", "")
	ordered, orderErr := reorderFlags(args[1:], map[string]bool{"--depth": true, "--view": true})
	if orderErr != nil {
		return 2, orderErr
	}
	if err := fs.Parse(ordered); err != nil {
		return 2, err
	}
	if fs.NArg() != 1 {
		return usage(global, stdout, "factile graph <path> [--depth 0|1] [--view <id>]")
	}
	result, err := ws.Graph(ctx, fs.Arg(0), factile.GraphOptions{Depth: *depth, View: *view})
	if err != nil {
		return 0, err
	}
	return writeGraphResult(stdout, global, result)
}

func runCreate(ctx context.Context, ws factile.Workspace, args []string, global globals, stdout io.Writer) (int, error) {
	if hasHelp(args) {
		return showUsage(stdout, "factile create <concept-path> --type <type> --title <title> --body <file>")
	}
	fs := flag.NewFlagSet("create", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	typeValue := fs.String("type", "", "")
	title := fs.String("title", "", "")
	bodyFile := fs.String("body", "", "")
	ordered, orderErr := reorderFlags(args[1:], map[string]bool{"--type": true, "--title": true, "--body": true})
	if orderErr != nil {
		return 2, orderErr
	}
	if err := fs.Parse(ordered); err != nil {
		return 2, err
	}
	if fs.NArg() != 1 || *typeValue == "" || *title == "" || *bodyFile == "" {
		return usage(global, stdout, "factile create <concept-path> --type <type> --title <title> --body <file>")
	}
	body, err := os.ReadFile(*bodyFile)
	if err != nil {
		return 0, err
	}
	result, err := ws.Create(ctx, fs.Arg(0), factile.CreateConceptInput{Type: *typeValue, Title: *title, Markdown: string(body)})
	if err != nil {
		return 0, err
	}
	return writeConceptConfirmation(stdout, global, "Created", result)
}

func runWrite(ctx context.Context, ws factile.Workspace, args []string, global globals, stdout io.Writer) (int, error) {
	if hasHelp(args) {
		return showUsage(stdout, "factile write <concept-path> --rev <rev> --body <file>")
	}
	fs := flag.NewFlagSet("write", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	rev := fs.String("rev", "", "")
	bodyFile := fs.String("body", "", "")
	ordered, orderErr := reorderFlags(args[1:], map[string]bool{"--rev": true, "--body": true})
	if orderErr != nil {
		return 2, orderErr
	}
	if err := fs.Parse(ordered); err != nil {
		return 2, err
	}
	if fs.NArg() != 1 || *bodyFile == "" {
		return usage(global, stdout, "factile write <concept-path> --rev <rev> --body <file>")
	}
	body, err := os.ReadFile(*bodyFile)
	if err != nil {
		return 0, err
	}
	result, err := ws.Write(ctx, fs.Arg(0), factile.WriteConceptInput{ExpectedRevision: *rev, Markdown: string(body)})
	if err != nil {
		return 0, err
	}
	return writeConceptConfirmation(stdout, global, "Wrote", result)
}

func runPatch(ctx context.Context, ws factile.Workspace, args []string, global globals, stdout io.Writer) (int, error) {
	if hasHelp(args) {
		return showUsage(stdout, "factile patch <concept-path> --rev <rev> [patch options]")
	}
	if len(args) < 2 {
		return usage(global, stdout, "factile patch <concept-path> --rev <rev> [patch options]")
	}
	path := args[1]
	input := factile.PatchConceptInput{Set: map[string]any{}, ReplaceSections: map[string]string{}, AppendSections: map[string]string{}}
	for i := 2; i < len(args); i++ {
		switch args[i] {
		case "--rev":
			i++
			if i >= len(args) {
				return 2, fmt.Errorf("--rev requires a value")
			}
			input.ExpectedRevision = args[i]
		case "--set":
			i++
			if i >= len(args) {
				return 2, fmt.Errorf("--set requires key=value")
			}
			parts := strings.SplitN(args[i], "=", 2)
			if len(parts) != 2 || parts[0] == "" {
				return 2, fmt.Errorf("--set requires key=value")
			}
			value, err := okf.ParseValue(parts[1])
			if err != nil {
				return 2, err
			}
			input.Set[parts[0]] = value
		case "--delete-key":
			i++
			if i >= len(args) {
				return 2, fmt.Errorf("--delete-key requires a key")
			}
			input.DeleteKeys = append(input.DeleteKeys, args[i])
		case "--replace-section":
			if i+2 >= len(args) {
				return 2, fmt.Errorf("--replace-section requires heading and file")
			}
			heading := args[i+1]
			data, err := os.ReadFile(args[i+2])
			if err != nil {
				return 0, err
			}
			input.ReplaceSections[heading] = string(data)
			i += 2
		case "--append-section":
			if i+2 >= len(args) {
				return 2, fmt.Errorf("--append-section requires heading and file")
			}
			heading := args[i+1]
			data, err := os.ReadFile(args[i+2])
			if err != nil {
				return 0, err
			}
			input.AppendSections[heading] = string(data)
			i += 2
		case "--replace-body":
			i++
			if i >= len(args) {
				return 2, fmt.Errorf("--replace-body requires a file")
			}
			data, err := os.ReadFile(args[i])
			if err != nil {
				return 0, err
			}
			body := string(data)
			input.ReplaceBody = &body
		default:
			return 2, fmt.Errorf("unknown patch option: %s", args[i])
		}
	}
	result, err := ws.Patch(ctx, path, input)
	if err != nil {
		return 0, err
	}
	return writeConceptConfirmation(stdout, global, "Patched", result)
}

func runRename(ctx context.Context, ws factile.Workspace, args []string, global globals, stdout io.Writer) (int, error) {
	if hasHelp(args) {
		return showUsage(stdout, "factile rename <old-path> <new-path> --rev <rev>")
	}
	fs := flag.NewFlagSet("rename", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	rev := fs.String("rev", "", "")
	ordered, orderErr := reorderFlags(args[1:], map[string]bool{"--rev": true})
	if orderErr != nil {
		return 2, orderErr
	}
	if err := fs.Parse(ordered); err != nil {
		return 2, err
	}
	if fs.NArg() != 2 {
		return usage(global, stdout, "factile rename <old-path> <new-path> --rev <rev>")
	}
	result, err := ws.Rename(ctx, fs.Arg(0), fs.Arg(1), factile.RenameOptions{ExpectedRevision: *rev})
	if err != nil {
		return 0, err
	}
	return writeRenameResult(stdout, global, result)
}

func runDelete(ctx context.Context, ws factile.Workspace, args []string, global globals, stdout io.Writer) (int, error) {
	if hasHelp(args) {
		return showUsage(stdout, "factile delete <concept-path> --rev <rev>")
	}
	fs := flag.NewFlagSet("delete", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	rev := fs.String("rev", "", "")
	ordered, orderErr := reorderFlags(args[1:], map[string]bool{"--rev": true})
	if orderErr != nil {
		return 2, orderErr
	}
	if err := fs.Parse(ordered); err != nil {
		return 2, err
	}
	if fs.NArg() != 1 {
		return usage(global, stdout, "factile delete <concept-path> --rev <rev>")
	}
	result, err := ws.Delete(ctx, fs.Arg(0), factile.DeleteOptions{ExpectedRevision: *rev})
	if err != nil {
		return 0, err
	}
	return writeDeleteResult(stdout, global, result)
}

func runDeprecate(ctx context.Context, ws factile.Workspace, args []string, global globals, stdout io.Writer) (int, error) {
	if hasHelp(args) {
		return showUsage(stdout, "factile deprecate <concept-path> --rev <rev> --reason <text>")
	}
	fs := flag.NewFlagSet("deprecate", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	rev := fs.String("rev", "", "")
	reason := fs.String("reason", "", "")
	ordered, orderErr := reorderFlags(args[1:], map[string]bool{"--rev": true, "--reason": true})
	if orderErr != nil {
		return 2, orderErr
	}
	if err := fs.Parse(ordered); err != nil {
		return 2, err
	}
	if fs.NArg() != 1 {
		return usage(global, stdout, "factile deprecate <concept-path> --rev <rev> --reason <text>")
	}
	result, err := ws.Deprecate(ctx, fs.Arg(0), factile.DeprecateOptions{ExpectedRevision: *rev, Reason: *reason})
	if err != nil {
		return 0, err
	}
	return writeConceptConfirmation(stdout, global, "Deprecated", result)
}

func runBundle(ctx context.Context, ws factile.Workspace, args []string, global globals, stdout io.Writer) (int, error) {
	if len(args) == 1 && isHelpArg(args[0]) {
		return showUsage(stdout, "factile bundle find|inspect|mount|unmount|list")
	}
	if len(args) == 0 {
		return usage(global, stdout, "factile bundle find|inspect|mount|unmount|list")
	}
	switch args[0] {
	case "list":
		if hasHelp(args) {
			return showUsage(stdout, "factile bundle list")
		}
		if len(args) != 1 {
			return usage(global, stdout, "factile bundle list")
		}
		result, err := ws.ListMounts(ctx)
		if err != nil {
			return 0, err
		}
		return writeMountList(stdout, global, result)
	case "inspect":
		if hasHelp(args) {
			return showUsage(stdout, "factile bundle inspect <source>")
		}
		if len(args) != 2 {
			return usage(global, stdout, "factile bundle inspect <source>")
		}
		result, err := ws.InspectBundle(ctx, args[1])
		if err != nil {
			return 0, err
		}
		return writeBundleInspect(stdout, global, result)
	case "find":
		if hasHelp(args) {
			return showUsage(stdout, "factile bundle find [path]")
		}
		start := "."
		if len(args) > 2 {
			return usage(global, stdout, "factile bundle find [path]")
		}
		if len(args) == 2 {
			start = args[1]
		}
		result, err := ws.FindBundles(ctx, start)
		if err != nil {
			return 0, err
		}
		return writeBundleFind(stdout, global, result)
	case "mount":
		if hasHelp(args) {
			return showUsage(stdout, "factile bundle mount <source> <mount-path>")
		}
		if len(args) != 3 {
			return usage(global, stdout, "factile bundle mount <source> <mount-path>")
		}
		result, err := ws.Mount(ctx, args[1], args[2], factile.MountOptions{Writable: true, Kind: "local"})
		if err != nil {
			return 0, err
		}
		return writeMountResult(stdout, global, result)
	case "unmount":
		if hasHelp(args) {
			return showUsage(stdout, "factile bundle unmount <mount-path>")
		}
		if len(args) != 2 {
			return usage(global, stdout, "factile bundle unmount <mount-path>")
		}
		result, err := ws.Unmount(ctx, args[1], factile.UnmountOptions{})
		if err != nil {
			return 0, err
		}
		return writeUnmountResult(stdout, global, result)
	default:
		return 0, factile.NewError(factile.ErrUnsupportedCommand, "Unsupported bundle command: "+args[0])
	}
}

func runKB(ctx context.Context, ws factile.Workspace, args []string, global globals, stdout io.Writer) (int, error) {
	if len(args) == 1 && isHelpArg(args[0]) {
		return showUsage(stdout, "factile kb list|inspect|create|link|unlink")
	}
	if len(args) == 0 {
		return usage(global, stdout, "factile kb list|inspect|create|link|unlink")
	}
	switch args[0] {
	case "list":
		if hasHelp(args) {
			return showUsage(stdout, "factile kb list")
		}
		if len(args) != 1 {
			return usage(global, stdout, "factile kb list")
		}
		result, err := ws.ListKnowledgeBases(ctx)
		if err != nil {
			return 0, err
		}
		return writeKnowledgeBaseList(stdout, global, result)
	case "inspect":
		if hasHelp(args) {
			return showUsage(stdout, "factile kb inspect <kb-path>")
		}
		if len(args) != 2 {
			return usage(global, stdout, "factile kb inspect <kb-path>")
		}
		result, err := ws.InspectKnowledgeBase(ctx, args[1])
		if err != nil {
			return 0, err
		}
		return writeKnowledgeBase(stdout, global, result)
	case "create":
		if hasHelp(args) {
			return showUsage(stdout, "factile kb create <kb-path> --title <title> [--description <text>]")
		}
		fs := flag.NewFlagSet("kb create", flag.ContinueOnError)
		fs.SetOutput(io.Discard)
		title := fs.String("title", "", "")
		description := fs.String("description", "", "")
		ordered, orderErr := reorderFlags(args[1:], map[string]bool{"--title": true, "--description": true})
		if orderErr != nil {
			return 2, orderErr
		}
		if err := fs.Parse(ordered); err != nil {
			return 2, err
		}
		if fs.NArg() != 1 {
			return usage(global, stdout, "factile kb create <kb-path> --title <title> [--description <text>]")
		}
		result, err := ws.CreateKnowledgeBase(ctx, fs.Arg(0), factile.KnowledgeBaseCreateInput{Title: *title, Description: *description})
		if err != nil {
			return 0, err
		}
		return writeKnowledgeBase(stdout, global, result)
	case "link":
		if hasHelp(args) {
			return showUsage(stdout, "factile kb link <kb-path> <source> <bundle-path> [--title <title>] [--read-only]")
		}
		fs := flag.NewFlagSet("kb link", flag.ContinueOnError)
		fs.SetOutput(io.Discard)
		title := fs.String("title", "", "")
		description := fs.String("description", "", "")
		readOnly := fs.Bool("read-only", false, "")
		ordered, orderErr := reorderFlags(args[1:], map[string]bool{"--title": true, "--description": true, "--read-only": false})
		if orderErr != nil {
			return 2, orderErr
		}
		if err := fs.Parse(ordered); err != nil {
			return 2, err
		}
		if fs.NArg() != 3 {
			return usage(global, stdout, "factile kb link <kb-path> <source> <bundle-path> [--title <title>] [--read-only]")
		}
		result, err := ws.LinkBundle(ctx, fs.Arg(0), fs.Arg(1), fs.Arg(2), factile.BundleLinkInput{
			Title:       *title,
			Description: *description,
			Writable:    !*readOnly,
			Kind:        "local",
		})
		if err != nil {
			return 0, err
		}
		return writeBundleLink(stdout, global, result)
	case "unlink":
		if hasHelp(args) {
			return showUsage(stdout, "factile kb unlink <bundle-path>")
		}
		if len(args) != 2 {
			return usage(global, stdout, "factile kb unlink <bundle-path>")
		}
		result, err := ws.UnlinkBundle(ctx, args[1])
		if err != nil {
			return 0, err
		}
		return writeBundleUnlink(stdout, global, result)
	default:
		return 0, factile.NewError(factile.ErrUnsupportedCommand, "Unsupported kb command: "+args[0])
	}
}

func runView(ctx context.Context, ws factile.Workspace, args []string, global globals, stdout io.Writer) (int, error) {
	if len(args) == 1 && isHelpArg(args[0]) {
		return showUsage(stdout, "factile view list|inspect|set|delete")
	}
	if len(args) == 0 {
		return usage(global, stdout, "factile view list|inspect|set|delete")
	}
	switch args[0] {
	case "list":
		if hasHelp(args) {
			return showUsage(stdout, "factile view list")
		}
		if len(args) != 1 {
			return usage(global, stdout, "factile view list")
		}
		result, err := ws.ListViews(ctx)
		if err != nil {
			return 0, err
		}
		return writeViewList(stdout, global, result)
	case "inspect":
		if hasHelp(args) {
			return showUsage(stdout, "factile view inspect <id>")
		}
		if len(args) != 2 {
			return usage(global, stdout, "factile view inspect <id>")
		}
		result, err := ws.InspectView(ctx, args[1])
		if err != nil {
			return 0, err
		}
		return writeView(stdout, global, result)
	case "set":
		if hasHelp(args) {
			return showUsage(stdout, "factile view set <id> --title <title> --path <path> [--description <text>]")
		}
		fs := flag.NewFlagSet("view set", flag.ContinueOnError)
		fs.SetOutput(io.Discard)
		title := fs.String("title", "", "")
		description := fs.String("description", "", "")
		status := fs.String("status", "", "")
		var paths stringListFlag
		fs.Var(&paths, "path", "")
		ordered, orderErr := reorderFlags(args[1:], map[string]bool{"--title": true, "--description": true, "--status": true, "--path": true})
		if orderErr != nil {
			return 2, orderErr
		}
		if err := fs.Parse(ordered); err != nil {
			return 2, err
		}
		if fs.NArg() != 1 || len(paths) == 0 {
			return usage(global, stdout, "factile view set <id> --title <title> --path <path> [--description <text>]")
		}
		result, err := ws.SetView(ctx, fs.Arg(0), factile.ViewInput{
			Title:       *title,
			Description: *description,
			Status:      *status,
			Paths:       []string(paths),
		})
		if err != nil {
			return 0, err
		}
		return writeView(stdout, global, result)
	case "delete":
		if hasHelp(args) {
			return showUsage(stdout, "factile view delete <id>")
		}
		if len(args) != 2 {
			return usage(global, stdout, "factile view delete <id>")
		}
		result, err := ws.DeleteView(ctx, args[1])
		if err != nil {
			return 0, err
		}
		return writeViewDelete(stdout, global, result)
	default:
		return 0, factile.NewError(factile.ErrUnsupportedCommand, "Unsupported view command: "+args[0])
	}
}

func runSkill(ctx context.Context, args []string, global globals, stdout io.Writer) (int, error) {
	if len(args) == 1 && isHelpArg(args[0]) {
		return showUsage(stdout, "factile skill list|inspect|install|uninstall|doctor")
	}
	if len(args) == 0 {
		return usage(global, stdout, "factile skill list|inspect|install|uninstall|doctor")
	}
	switch args[0] {
	case "list":
		if hasHelp(args) {
			return showUsage(stdout, "factile skill list")
		}
		if len(args) != 1 {
			return usage(global, stdout, "factile skill list")
		}
		return writeSkillList(stdout, global, skill.List())
	case "inspect":
		if hasHelp(args) {
			return showUsage(stdout, "factile skill inspect codex")
		}
		if len(args) != 2 {
			return usage(global, stdout, "factile skill inspect codex")
		}
		result, err := skill.Inspect(args[1])
		if err != nil {
			return 0, err
		}
		return writeSkillInspect(stdout, global, result)
	case "install":
		if hasHelp(args) {
			return showUsage(stdout, "factile skill install codex --scope repo|user [--mode reader|curator] [--profile software]")
		}
		fs := flag.NewFlagSet("skill install", flag.ContinueOnError)
		fs.SetOutput(io.Discard)
		scope := fs.String("scope", "repo", "")
		mode := fs.String("mode", "", "")
		profile := fs.String("profile", "", "")
		ordered, orderErr := reorderFlags(args[1:], map[string]bool{"--scope": true, "--mode": true, "--profile": true})
		if orderErr != nil {
			return 2, orderErr
		}
		if err := fs.Parse(ordered); err != nil {
			return 2, err
		}
		if fs.NArg() != 1 {
			return usage(global, stdout, "factile skill install codex --scope repo|user [--mode reader|curator] [--profile software]")
		}
		result, err := skill.Install(fs.Arg(0), skill.InstallOptions{Scope: *scope, Mode: *mode, Profile: *profile})
		if err != nil {
			return 0, err
		}
		return writeSkillInstall(stdout, global, result)
	case "uninstall":
		if hasHelp(args) {
			return showUsage(stdout, "factile skill uninstall codex --scope repo|user")
		}
		fs := flag.NewFlagSet("skill uninstall", flag.ContinueOnError)
		fs.SetOutput(io.Discard)
		scope := fs.String("scope", "repo", "")
		ordered, orderErr := reorderFlags(args[1:], map[string]bool{"--scope": true})
		if orderErr != nil {
			return 2, orderErr
		}
		if err := fs.Parse(ordered); err != nil {
			return 2, err
		}
		if fs.NArg() != 1 {
			return usage(global, stdout, "factile skill uninstall codex --scope repo|user")
		}
		result, err := skill.Uninstall(fs.Arg(0), skill.InstallOptions{Scope: *scope})
		if err != nil {
			return 0, err
		}
		return writeSkillUninstall(stdout, global, result)
	case "doctor":
		if hasHelp(args) {
			return showUsage(stdout, "factile skill doctor codex")
		}
		if len(args) != 2 {
			return usage(global, stdout, "factile skill doctor codex")
		}
		result, err := skill.Doctor(ctx, args[1], skill.DoctorOptions{})
		if err != nil {
			return 0, err
		}
		return writeSkillDoctor(stdout, global, result)
	default:
		return 0, factile.NewError(factile.ErrUnsupportedCommand, "Unsupported skill command: "+args[0])
	}
}

func runMCP(ctx context.Context, global globals, args []string, stdin io.Reader, stdout io.Writer) (int, error) {
	if len(args) == 0 || hasHelp(args) {
		return showUsage(stdout, "factile mcp serve --stdio [--read-only]")
	}
	if len(args) < 2 || args[0] != "serve" {
		return 0, factile.NewError(factile.ErrUnsupportedCommand, "Unsupported MCP command")
	}
	readOnly := false
	stdio := false
	for _, arg := range args[1:] {
		switch arg {
		case "--stdio":
			stdio = true
		case "--read-only":
			readOnly = true
		default:
			return 2, fmt.Errorf("unknown MCP option: %s", arg)
		}
	}
	if !stdio {
		return 2, fmt.Errorf("MCP serve requires --stdio")
	}
	ws := factile.NewWorkspace(factile.WorkspaceOptions{MountFile: global.MountFile, ReadOnly: readOnly})
	return 0, mcpserver.Serve(ctx, ws, stdin, stdout, mcpserver.Options{ReadOnly: readOnly})
}

func parseGlobals(args []string) (globals, []string, error) {
	global := globals{Format: formatText, Color: clirender.ColorAuto}
	formatSet := false
	jsonSet := false
	var rest []string
	for i := 0; i < len(args); i++ {
		arg := args[i]
		switch arg {
		case "--mount-file":
			i++
			if i >= len(args) {
				return global, nil, fmt.Errorf("--mount-file requires a path")
			}
			global.MountFile = args[i]
		case "--format":
			i++
			if i >= len(args) {
				return global, nil, fmt.Errorf("--format requires text or json")
			}
			if args[i] != formatText && args[i] != formatJSON {
				return global, nil, fmt.Errorf("unsupported format: %s", args[i])
			}
			if jsonSet && args[i] == formatText {
				return global, nil, fmt.Errorf("--json cannot be combined with --format text")
			}
			if formatSet && args[i] != global.Format {
				return global, nil, fmt.Errorf("conflicting output formats: %s and %s", global.Format, args[i])
			}
			global.Format = args[i]
			formatSet = true
		case "--json":
			if formatSet && global.Format == formatText {
				return global, nil, fmt.Errorf("--json cannot be combined with --format text")
			}
			global.Format = formatJSON
			jsonSet = true
		case "--color":
			i++
			if i >= len(args) {
				return global, nil, fmt.Errorf("--color requires auto, always, or never")
			}
			color, err := clirender.ParseColorMode(args[i])
			if err != nil {
				return global, nil, err
			}
			global.Color = color
		case "--quiet":
			global.Quiet = true
		case "--version":
			global.Version = true
		case "--help", "-h":
			if len(args) == 1 {
				global.Help = true
			} else {
				rest = append(rest, arg)
			}
		default:
			rest = append(rest, arg)
		}
	}
	return global, rest, nil
}

func usage(global globals, stdout io.Writer, text string) (int, error) {
	if global.structuredOutput() {
		return 2, factile.NewError(factile.ErrInvalidPath, text)
	}
	_, _ = fmt.Fprintln(stdout, text)
	return 2, nil
}

func showUsage(stdout io.Writer, text string) (int, error) {
	_, _ = fmt.Fprintln(stdout, text)
	return 0, nil
}

func hasHelp(args []string) bool {
	for _, arg := range args {
		if isHelpArg(arg) {
			return true
		}
	}
	return false
}

func isHelpArg(arg string) bool {
	return arg == "--help" || arg == "-h"
}

func reorderFlags(args []string, known map[string]bool) ([]string, error) {
	var flags []string
	var positionals []string
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if strings.HasPrefix(arg, "--") {
			needsValue, ok := known[arg]
			if !ok {
				return nil, fmt.Errorf("unknown option: %s", arg)
			}
			flags = append(flags, arg)
			if needsValue {
				i++
				if i >= len(args) {
					return nil, fmt.Errorf("%s requires a value", arg)
				}
				flags = append(flags, args[i])
			}
			continue
		}
		positionals = append(positionals, arg)
	}
	return append(flags, positionals...), nil
}

func writeResult(stdout io.Writer, global globals, value any) (int, error) {
	if _, err := printResult(stdout, global, value); err != nil {
		return 1, err
	}
	return 0, nil
}

func writeVersionResult(stdout io.Writer, global globals) (int, error) {
	info := version.Current()
	if global.structuredOutput() {
		return writeResult(stdout, global, info)
	}
	if global.Quiet {
		return 0, nil
	}
	if _, err := fmt.Fprintln(stdout, info.String()); err != nil {
		return 1, err
	}
	return 0, nil
}

func writeInitResult(stdout io.Writer, global globals, result bootstrap.Result) (int, error) {
	if global.structuredOutput() {
		return writeResult(stdout, global, result)
	}
	return writeRendered(stdout, global, func(renderer *clirender.Renderer) error {
		return renderer.RenderInit(stdout, result)
	})
}

func writeSummaryResult(stdout io.Writer, global globals, result factile.SummaryResult) (int, error) {
	if global.structuredOutput() {
		return writeResult(stdout, global, result)
	}
	return writeRendered(stdout, global, func(renderer *clirender.Renderer) error {
		return renderer.RenderSummary(stdout, result)
	})
}

func writeListResult(stdout io.Writer, global globals, result factile.ListResult) (int, error) {
	if global.structuredOutput() {
		return writeResult(stdout, global, result)
	}
	return writeRendered(stdout, global, func(renderer *clirender.Renderer) error {
		return renderer.RenderList(stdout, result)
	})
}

func writeStatResult(stdout io.Writer, global globals, result factile.StatResult) (int, error) {
	if global.structuredOutput() {
		return writeResult(stdout, global, result)
	}
	return writeRendered(stdout, global, func(renderer *clirender.Renderer) error {
		return renderer.RenderStat(stdout, result)
	})
}

func writeReadResult(stdout io.Writer, global globals, result factile.ConceptResult) (int, error) {
	if global.structuredOutput() {
		return writeResult(stdout, global, result)
	}
	return writeRendered(stdout, global, func(renderer *clirender.Renderer) error {
		return renderer.RenderRead(stdout, result)
	})
}

func writeSearchResult(stdout io.Writer, global globals, result factile.SearchResults) (int, error) {
	if global.structuredOutput() {
		return writeResult(stdout, global, result)
	}
	return writeRendered(stdout, global, func(renderer *clirender.Renderer) error {
		return renderer.RenderSearch(stdout, result)
	})
}

func writeContextResult(stdout io.Writer, global globals, result factile.ContextPack) (int, error) {
	if global.structuredOutput() {
		return writeResult(stdout, global, result)
	}
	return writeRendered(stdout, global, func(renderer *clirender.Renderer) error {
		return renderer.RenderContext(stdout, result)
	})
}

func writeGraphResult(stdout io.Writer, global globals, result factile.GraphResult) (int, error) {
	if global.structuredOutput() {
		return writeResult(stdout, global, result)
	}
	return writeRendered(stdout, global, func(renderer *clirender.Renderer) error {
		return renderer.RenderGraph(stdout, result)
	})
}

func writeValidationResult(stdout io.Writer, global globals, result factile.ValidationResult) (int, error) {
	if global.structuredOutput() {
		return printResult(stdout, global, result)
	}
	return writeRendered(stdout, global, func(renderer *clirender.Renderer) error {
		return renderer.RenderValidation(stdout, result)
	})
}

func writeConceptConfirmation(stdout io.Writer, global globals, verb string, result factile.ConceptResult) (int, error) {
	if global.structuredOutput() {
		return writeResult(stdout, global, result)
	}
	return writeRendered(stdout, global, func(renderer *clirender.Renderer) error {
		return renderer.RenderConceptConfirmation(stdout, verb, result)
	})
}

func writeRenameResult(stdout io.Writer, global globals, result factile.RenameResult) (int, error) {
	if global.structuredOutput() {
		return writeResult(stdout, global, result)
	}
	return writeRendered(stdout, global, func(renderer *clirender.Renderer) error {
		return renderer.RenderRename(stdout, result)
	})
}

func writeDeleteResult(stdout io.Writer, global globals, result factile.DeleteResult) (int, error) {
	if global.structuredOutput() {
		return writeResult(stdout, global, result)
	}
	return writeRendered(stdout, global, func(renderer *clirender.Renderer) error {
		return renderer.RenderDelete(stdout, result)
	})
}

func writeKnowledgeBaseList(stdout io.Writer, global globals, result factile.KnowledgeBaseListResult) (int, error) {
	if global.structuredOutput() {
		return writeResult(stdout, global, result)
	}
	return writeRendered(stdout, global, func(renderer *clirender.Renderer) error {
		return renderer.RenderKnowledgeBaseList(stdout, result)
	})
}

func writeKnowledgeBase(stdout io.Writer, global globals, result factile.KnowledgeBaseResult) (int, error) {
	if global.structuredOutput() {
		return writeResult(stdout, global, result)
	}
	return writeRendered(stdout, global, func(renderer *clirender.Renderer) error {
		return renderer.RenderKnowledgeBase(stdout, result)
	})
}

func writeViewList(stdout io.Writer, global globals, result factile.ViewListResult) (int, error) {
	if global.structuredOutput() {
		return writeResult(stdout, global, result)
	}
	return writeRendered(stdout, global, func(renderer *clirender.Renderer) error {
		return renderer.RenderViewList(stdout, result)
	})
}

func writeView(stdout io.Writer, global globals, result factile.ViewResult) (int, error) {
	if global.structuredOutput() {
		return writeResult(stdout, global, result)
	}
	return writeRendered(stdout, global, func(renderer *clirender.Renderer) error {
		return renderer.RenderView(stdout, result)
	})
}

func writeViewDelete(stdout io.Writer, global globals, result factile.ViewDeleteResult) (int, error) {
	if global.structuredOutput() {
		return writeResult(stdout, global, result)
	}
	return writeRendered(stdout, global, func(renderer *clirender.Renderer) error {
		return renderer.RenderViewDelete(stdout, result)
	})
}

func writeBundleLink(stdout io.Writer, global globals, result factile.BundleLinkResult) (int, error) {
	if global.structuredOutput() {
		return writeResult(stdout, global, result)
	}
	return writeRendered(stdout, global, func(renderer *clirender.Renderer) error {
		return renderer.RenderBundleLink(stdout, result)
	})
}

func writeBundleUnlink(stdout io.Writer, global globals, result factile.BundleUnlinkResult) (int, error) {
	if global.structuredOutput() {
		return writeResult(stdout, global, result)
	}
	return writeRendered(stdout, global, func(renderer *clirender.Renderer) error {
		return renderer.RenderBundleUnlink(stdout, result)
	})
}

func writeMountResult(stdout io.Writer, global globals, result factile.MountResult) (int, error) {
	if global.structuredOutput() {
		return writeResult(stdout, global, result)
	}
	return writeRendered(stdout, global, func(renderer *clirender.Renderer) error {
		return renderer.RenderMount(stdout, result)
	})
}

func writeUnmountResult(stdout io.Writer, global globals, result factile.UnmountResult) (int, error) {
	if global.structuredOutput() {
		return writeResult(stdout, global, result)
	}
	return writeRendered(stdout, global, func(renderer *clirender.Renderer) error {
		return renderer.RenderUnmount(stdout, result)
	})
}

func writeMountList(stdout io.Writer, global globals, result factile.MountListResult) (int, error) {
	if global.structuredOutput() {
		return writeResult(stdout, global, result)
	}
	return writeRendered(stdout, global, func(renderer *clirender.Renderer) error {
		return renderer.RenderMountList(stdout, result)
	})
}

func writeBundleInspect(stdout io.Writer, global globals, result factile.BundleInspectResult) (int, error) {
	if global.structuredOutput() {
		return writeResult(stdout, global, result)
	}
	return writeRendered(stdout, global, func(renderer *clirender.Renderer) error {
		return renderer.RenderBundleInspect(stdout, result)
	})
}

func writeBundleFind(stdout io.Writer, global globals, result factile.BundleFindResult) (int, error) {
	if global.structuredOutput() {
		return writeResult(stdout, global, result)
	}
	return writeRendered(stdout, global, func(renderer *clirender.Renderer) error {
		return renderer.RenderBundleFind(stdout, result)
	})
}

func writeSkillList(stdout io.Writer, global globals, result skill.ListResult) (int, error) {
	if global.structuredOutput() {
		return writeResult(stdout, global, result)
	}
	return writeRendered(stdout, global, func(renderer *clirender.Renderer) error {
		return renderer.RenderSkillList(stdout, result)
	})
}

func writeSkillInspect(stdout io.Writer, global globals, result skill.InspectResult) (int, error) {
	if global.structuredOutput() {
		return writeResult(stdout, global, result)
	}
	return writeRendered(stdout, global, func(renderer *clirender.Renderer) error {
		return renderer.RenderSkillInspect(stdout, result)
	})
}

func writeSkillInstall(stdout io.Writer, global globals, result skill.InstallResult) (int, error) {
	if global.structuredOutput() {
		return writeResult(stdout, global, result)
	}
	return writeRendered(stdout, global, func(renderer *clirender.Renderer) error {
		return renderer.RenderSkillInstall(stdout, result)
	})
}

func writeSkillUninstall(stdout io.Writer, global globals, result skill.UninstallResult) (int, error) {
	if global.structuredOutput() {
		return writeResult(stdout, global, result)
	}
	return writeRendered(stdout, global, func(renderer *clirender.Renderer) error {
		return renderer.RenderSkillUninstall(stdout, result)
	})
}

func writeSkillDoctor(stdout io.Writer, global globals, result skill.DoctorResult) (int, error) {
	if global.structuredOutput() {
		if _, err := printResult(stdout, global, result); err != nil {
			return 1, err
		}
		if !result.OK {
			return 1, nil
		}
		return 0, nil
	}
	code, err := writeRendered(stdout, global, func(renderer *clirender.Renderer) error {
		return renderer.RenderSkillDoctor(stdout, result)
	})
	if err != nil {
		return code, err
	}
	if !result.OK {
		return 1, nil
	}
	return code, nil
}

func writeRendered(stdout io.Writer, global globals, render func(*clirender.Renderer) error) (int, error) {
	if global.Quiet {
		return 0, nil
	}
	renderer, err := newRenderer(global, stdout)
	if err != nil {
		return 1, err
	}
	if err := render(renderer); err != nil {
		return 1, err
	}
	return 0, nil
}

func newRenderer(global globals, stdout io.Writer) (*clirender.Renderer, error) {
	return clirender.New(clirender.Options{
		ColorMode:        global.Color,
		StdoutIsTerminal: clirender.IsTerminal(stdout),
		Env: map[string]string{
			"NO_COLOR": os.Getenv("NO_COLOR"),
			"TERM":     os.Getenv("TERM"),
		},
	})
}

func printResult(stdout io.Writer, global globals, value any) (int, error) {
	if global.structuredOutput() {
		data, err := json.MarshalIndent(value, "", "  ")
		if err != nil {
			return 0, err
		}
		_, err = fmt.Fprintln(stdout, string(data))
		return 0, err
	}
	if global.Quiet {
		return 0, nil
	}
	switch v := value.(type) {
	case factile.ConceptResult:
		_, _ = fmt.Fprintln(stdout, v.Concept.Path)
	case factile.ListResult:
		if len(v.Cards) > 0 {
			for _, card := range v.Cards {
				if card.Title != "" {
					_, _ = fmt.Fprintf(stdout, "%s %s\n", card.Path, card.Title)
				} else {
					_, _ = fmt.Fprintln(stdout, card.Path)
				}
			}
		} else {
			data, _ := json.MarshalIndent(value, "", "  ")
			_, _ = fmt.Fprintln(stdout, string(data))
		}
	case factile.StatResult:
		if v.Card.Title != "" {
			_, _ = fmt.Fprintf(stdout, "%s %s\n", v.Card.Path, v.Card.Title)
		} else {
			_, _ = fmt.Fprintln(stdout, v.Card.Path)
		}
	case factile.KnowledgeBaseListResult:
		for _, kb := range v.KnowledgeBases {
			if kb.Title != "" {
				_, _ = fmt.Fprintf(stdout, "%s %s\n", kb.Path, kb.Title)
			} else {
				_, _ = fmt.Fprintln(stdout, kb.Path)
			}
		}
	case factile.KnowledgeBaseResult:
		_, _ = fmt.Fprintln(stdout, v.KnowledgeBase.Path)
	case factile.BundleLinkResult:
		_, _ = fmt.Fprintln(stdout, v.Bundle.Path)
	case factile.BundleUnlinkResult:
		_, _ = fmt.Fprintf(stdout, "removed %s\n", v.BundlePath)
	case factile.SearchResults:
		for _, result := range v.Results {
			_, _ = fmt.Fprintf(stdout, "%.1f %s\n", result.Score, result.Concept.Path)
		}
	case factile.ValidationResult:
		if v.Valid {
			_, _ = fmt.Fprintln(stdout, "valid")
		} else {
			_, _ = fmt.Fprintln(stdout, "invalid")
		}
	default:
		data, _ := json.MarshalIndent(value, "", "  ")
		_, _ = fmt.Fprintln(stdout, string(data))
	}
	return 0, nil
}

func writeError(stderr io.Writer, global globals, err error) int {
	normalized := factile.NormalizeError(err)
	app, ok := normalized.(*factile.AppError)
	if !ok {
		app = factile.NewError("general_failure", normalized.Error())
	}
	if global.structuredOutput() {
		_ = json.NewEncoder(stderr).Encode(map[string]any{"error": app})
	} else {
		_, _ = fmt.Fprintln(stderr, app.Message)
	}
	return exitCode(app.Code)
}

func traceCLI(args []string, code int, started time.Time) {
	command, path, query := traceCLIArgs(args)
	trace.Append(trace.Event{
		Surface:     "cli",
		Command:     command,
		Path:        path,
		Query:       query,
		ExitCode:    code,
		DurationMS:  time.Since(started).Milliseconds(),
		ResultCount: 0,
	})
}

func traceCLIArgs(args []string) (string, string, string) {
	if len(args) == 0 {
		return "help", "", ""
	}
	command := args[0]
	switch command {
	case "list":
		commandName := command
		for _, arg := range args[1:] {
			if arg == "--brief" {
				commandName = "list --brief"
				break
			}
		}
		for _, arg := range args[1:] {
			if !strings.HasPrefix(arg, "--") {
				return commandName, arg, ""
			}
		}
		return commandName, "/", ""
	case "read", "graph", "validate", "stat":
		if len(args) > 1 {
			return command, args[1], ""
		}
	case "search", "context":
		if len(args) > 2 {
			return command, args[1], args[2]
		}
	case "bundle":
		if len(args) > 1 {
			command = "bundle " + args[1]
		}
		if len(args) > 2 {
			return command, args[len(args)-1], ""
		}
	case "kb":
		if len(args) > 1 {
			command = "kb " + args[1]
		}
		if len(args) > 2 {
			return command, args[len(args)-1], ""
		}
	case "skill":
		if len(args) > 1 {
			return "skill " + args[1], "", ""
		}
	case "mcp":
		if len(args) > 1 {
			return "mcp " + args[1], "", ""
		}
	}
	return command, "", ""
}

func exitCode(code string) int {
	switch code {
	case factile.ErrInvalidPath, factile.ErrUnsupportedCommand:
		return 2
	case factile.ErrValidationFailed, factile.ErrOKFParse:
		return 3
	case factile.ErrMountNotFound, factile.ErrAmbiguousTarget, factile.ErrConceptNotFound, factile.ErrPathIsNotBundle, factile.ErrPathIsNotConcept:
		return 4
	case factile.ErrConceptAlreadyExist, factile.ErrRevisionRequired, factile.ErrRevisionMismatch, factile.ErrSectionNotFound:
		return 5
	case factile.ErrSourceReadOnly, factile.ErrUnsafeSourcePath, factile.ErrUnsupportedSource:
		return 6
	case factile.ErrPartialFailure:
		return 7
	case factile.ErrLockTimeout:
		return 8
	default:
		return 1
	}
}

func writeHelp(stdout io.Writer, global globals) error {
	renderer, err := newRenderer(global, stdout)
	if err != nil {
		return err
	}
	return renderer.RenderHelp(stdout)
}
