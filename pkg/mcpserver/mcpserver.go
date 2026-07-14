package mcpserver

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"time"

	"github.com/factile/factile/pkg/factile"
	"github.com/factile/factile/pkg/trace"
	"github.com/factile/factile/pkg/version"
)

type Options struct {
	ReadOnly bool
}

type Tool struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	InputSchema map[string]any `json:"inputSchema"`
}

type Server struct {
	workspace factile.Workspace
	opts      Options
}

const Instructions = "Use Factile for local and read-only Git OKF knowledge. For architecture, design, documentation, review, runbook, standards, policy, legal, compliance, or domain tasks, discover paths, retrieve focused context, then read specific concepts as needed. Do not edit knowledge unless explicitly asked."

func New(workspace factile.Workspace, opts Options) *Server {
	return &Server{workspace: workspace, opts: opts}
}

func (s *Server) Tools() []Tool {
	tools := []Tool{
		tool("factile_list", "List knowledge paths, optionally as compact reader cards.", objectSchema(map[string]any{
			"path":  stringSchema("Virtual Factile path to list. Defaults to /."),
			"brief": boolSchema("Return compact reader cards instead of folder and document details."),
			"view":  stringSchema("View id used to narrow the listed scope."),
		})),
		tool("factile_stat", "Return one compact reader card for a path.", objectSchema(map[string]any{
			"path": stringSchema("Virtual Factile path to inspect."),
		})),
		tool("factile_context", "Retrieve focused OKF context for a task or question.", objectSchema(map[string]any{
			"path":       stringSchema("Virtual Factile path to search from."),
			"query":      stringSchema("Task or question to retrieve context for."),
			"max_tokens": integerSchema("Approximate maximum context size."),
			"depth":      integerSchema("Related-link traversal depth: 0 disables expansion, 1 adds one-hop links and backlinks."),
			"view":       stringSchema("View id used to narrow context selection."),
		}, "path", "query")),
		tool("factile_search", "Search OKF knowledge.", objectSchema(map[string]any{
			"path":  stringSchema("Virtual Factile path to search from."),
			"query": stringSchema("Search query."),
			"view":  stringSchema("View id used to narrow search candidates."),
		}, "path", "query")),
		tool("factile_read", "Read a specific OKF concept.", objectSchema(map[string]any{
			"path": stringSchema("Virtual Factile concept path."),
		}, "path")),
		tool("factile_validate", "Validate a bundle or concept.", objectSchema(map[string]any{
			"path": stringSchema("Virtual Factile path to validate."),
			"view": stringSchema("View id used to narrow validation scope."),
		}, "path")),
		tool("factile_graph", "Build a Markdown link graph.", objectSchema(map[string]any{
			"path":  stringSchema("Virtual Factile path to graph."),
			"depth": integerSchema("Related-link traversal depth: 0 disables expansion, 1 adds one-hop links and backlinks."),
			"view":  stringSchema("View id used to narrow graph nodes and edges."),
		}, "path")),
		tool("factile_mounts", "List configured mounts and cached Git source status without refreshing.", objectSchema(map[string]any{})),
		tool("factile_refresh", "Immediately check and refresh generated state for one Git mount.", objectSchema(map[string]any{
			"mount_path": stringSchema("Git mount path to refresh."),
		}, "mount_path")),
		tool("factile_view_list", "List views.", objectSchema(map[string]any{})),
		tool("factile_view_inspect", "Inspect one view.", objectSchema(map[string]any{
			"id": stringSchema("View id."),
		}, "id")),
	}
	if !s.opts.ReadOnly {
		tools = append(tools,
			tool("factile_mount", "Create or replace a read-only-by-default local or Git path mount.", objectSchema(map[string]any{
				"source":      stringSchema("Local source path, native Git remote, or git+ compatibility source."),
				"mount_path":  stringSchema("Factile path where the source should appear."),
				"writable":    boolSchema("Request deliberate write access for a local source; defaults to false."),
				"read_only":   boolSchema("Deprecated compatibility input; false retains the legacy writable-local request."),
				"title":       stringSchema("Optional mount title; defaults from source metadata when available."),
				"description": stringSchema("Optional mount description; defaults from source metadata when available."),
				"ref":         stringSchema("Optional floating Git branch or tag; mutually exclusive with revision."),
				"revision":    stringSchema("Optional pinned full 40-hex SHA-1 Git commit identifier; mutually exclusive with ref."),
			}, "source", "mount_path")),
			tool("factile_unmount", "Remove one path mount descriptor.", objectSchema(map[string]any{
				"mount_path": stringSchema("Factile mount path to remove."),
			}, "mount_path")),
			tool("factile_view_set", "Create or replace a view.", objectSchema(map[string]any{
				"id":          stringSchema("View id."),
				"title":       stringSchema("View title."),
				"description": stringSchema("View description."),
				"status":      stringSchema("View status."),
				"paths":       stringArraySchema("Ordered Factile paths in the view."),
			}, "id", "paths")),
			tool("factile_view_delete", "Delete one view.", objectSchema(map[string]any{
				"id": stringSchema("View id."),
			}, "id")),
			tool("factile_mkdir", "Create a directory scaffold.", objectSchema(map[string]any{
				"path":     stringSchema("Virtual Factile path for the new directory."),
				"title":    stringSchema("Directory title."),
				"log":      boolSchema("Create log.md."),
				"overview": boolSchema("Create overview.md."),
				"bundle":   boolSchema("Create the standard bundle scaffold."),
			}, "path")),
			tool("factile_create", "Create a concept.", objectSchema(map[string]any{
				"path":     stringSchema("Virtual Factile concept path."),
				"type":     stringSchema("OKF type value."),
				"title":    stringSchema("Concept title."),
				"markdown": stringSchema("Markdown body."),
			}, "path", "type", "title", "markdown")),
			tool("factile_write", "Replace a concept body.", objectSchema(map[string]any{
				"path":              stringSchema("Virtual Factile concept path."),
				"expected_revision": stringSchema("Current concept revision."),
				"markdown":          stringSchema("Replacement Markdown body."),
			}, "path", "expected_revision", "markdown")),
			tool("factile_patch", "Patch concept frontmatter or Markdown.", objectSchema(map[string]any{
				"path":              stringSchema("Virtual Factile concept path."),
				"expected_revision": stringSchema("Current concept revision."),
				"set":               objectValueSchema("Frontmatter keys to set."),
				"delete_keys":       stringArraySchema("Frontmatter keys to delete."),
				"replace_sections":  stringMapSchema("Markdown sections to replace."),
				"append_sections":   stringMapSchema("Markdown sections to append to."),
				"replace_body":      stringSchema("Replacement Markdown body."),
			}, "path", "expected_revision")),
			tool("factile_rename", "Rename one concept.", objectSchema(map[string]any{
				"old_path":          stringSchema("Current virtual Factile concept path."),
				"new_path":          stringSchema("New virtual Factile concept path."),
				"expected_revision": stringSchema("Current concept revision."),
			}, "old_path", "new_path", "expected_revision")),
			tool("factile_delete", "Delete one concept.", objectSchema(map[string]any{
				"path":              stringSchema("Virtual Factile concept path."),
				"expected_revision": stringSchema("Current concept revision."),
			}, "path", "expected_revision")),
			tool("factile_deprecate", "Mark one concept deprecated.", objectSchema(map[string]any{
				"path":              stringSchema("Virtual Factile concept path."),
				"expected_revision": stringSchema("Current concept revision."),
				"reason":            stringSchema("Deprecation reason."),
			}, "path", "expected_revision", "reason")),
		)
	}
	return tools
}

func tool(name, description string, inputSchema map[string]any) Tool {
	return Tool{Name: name, Description: description, InputSchema: inputSchema}
}

func objectSchema(properties map[string]any, required ...string) map[string]any {
	schema := map[string]any{
		"type":                 "object",
		"properties":           properties,
		"additionalProperties": false,
	}
	if len(required) > 0 {
		schema["required"] = required
	}
	return schema
}

func stringSchema(description string) map[string]any {
	return map[string]any{"type": "string", "description": description}
}

func integerSchema(description string) map[string]any {
	return map[string]any{"type": "integer", "description": description}
}

func boolSchema(description string) map[string]any {
	return map[string]any{"type": "boolean", "description": description}
}

func objectValueSchema(description string) map[string]any {
	return map[string]any{"type": "object", "description": description, "additionalProperties": true}
}

func stringArraySchema(description string) map[string]any {
	return map[string]any{
		"type":        "array",
		"description": description,
		"items":       map[string]any{"type": "string"},
	}
}

func stringMapSchema(description string) map[string]any {
	return map[string]any{
		"type":                 "object",
		"description":          description,
		"additionalProperties": map[string]any{"type": "string"},
	}
}

func Serve(ctx context.Context, workspace factile.Workspace, stdin io.Reader, stdout io.Writer, opts Options) error {
	server := New(workspace, opts)
	decoder := json.NewDecoder(stdin)
	encoder := json.NewEncoder(stdout)
	for {
		var req rpcRequest
		if err := decoder.Decode(&req); err != nil {
			if err == io.EOF {
				return nil
			}
			return err
		}
		if !req.HasID {
			continue
		}
		resp := server.handle(ctx, req)
		if err := encoder.Encode(resp); err != nil {
			return err
		}
	}
}

type rpcRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	HasID   bool            `json:"-"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

func (r *rpcRequest) UnmarshalJSON(data []byte) error {
	var raw struct {
		JSONRPC string           `json:"jsonrpc"`
		ID      *json.RawMessage `json:"id"`
		Method  string           `json:"method"`
		Params  json.RawMessage  `json:"params,omitempty"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	r.JSONRPC = raw.JSONRPC
	r.Method = raw.Method
	r.Params = raw.Params
	r.HasID = raw.ID != nil
	r.ID = nil
	if raw.ID != nil {
		r.ID = append(r.ID, (*raw.ID)...)
	}
	return nil
}

type rpcResponse struct {
	JSONRPC string    `json:"jsonrpc"`
	ID      any       `json:"id,omitempty"`
	Result  any       `json:"result,omitempty"`
	Error   *rpcError `json:"error,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    any    `json:"data,omitempty"`
}

func (s *Server) handle(ctx context.Context, req rpcRequest) rpcResponse {
	result, err := s.dispatch(ctx, req)
	if err != nil {
		app := factile.NormalizeError(err)
		if typed, ok := app.(*factile.AppError); ok {
			return rpcResponse{JSONRPC: "2.0", ID: req.ID, Error: &rpcError{Code: -32000, Message: typed.Message, Data: map[string]any{"code": typed.Code, "details": typed.Details}}}
		}
		return rpcResponse{JSONRPC: "2.0", ID: req.ID, Error: &rpcError{Code: -32000, Message: app.Error()}}
	}
	return rpcResponse{JSONRPC: "2.0", ID: req.ID, Result: result}
}

func (s *Server) dispatch(ctx context.Context, req rpcRequest) (any, error) {
	switch req.Method {
	case "initialize":
		return map[string]any{
			"protocolVersion": "2025-06-18",
			"serverInfo":      map[string]any{"name": version.Name, "version": version.Current().Version},
			"capabilities":    map[string]any{"tools": map[string]any{}},
			"instructions":    Instructions,
		}, nil
	case "ping":
		return map[string]any{}, nil
	case "tools/list":
		return map[string]any{"tools": s.Tools()}, nil
	case "tools/call":
		var params struct {
			Name      string         `json:"name"`
			Arguments map[string]any `json:"arguments"`
		}
		if err := json.Unmarshal(req.Params, &params); err != nil {
			return nil, err
		}
		started := time.Now()
		value, err := s.callTool(ctx, params.Name, params.Arguments)
		code := 0
		if err != nil {
			code = 1
			traceMCP(params.Name, params.Arguments, code, started, nil)
			return nil, err
		}
		traceMCP(params.Name, params.Arguments, code, started, value)
		return map[string]any{
			"content":           []map[string]any{{"type": "text", "text": mustJSON(value)}},
			"structuredContent": value,
		}, nil
	default:
		return nil, factile.NewError(factile.ErrUnsupportedCommand, "Unsupported MCP method: "+req.Method)
	}
}

func (s *Server) callTool(ctx context.Context, name string, args map[string]any) (any, error) {
	switch name {
	case "factile_list":
		return s.workspace.List(ctx, stringArg(args, "path"), factile.ListOptions{Brief: boolArg(args, "brief"), View: stringArg(args, "view")})
	case "factile_stat":
		return s.workspace.Stat(ctx, stringArg(args, "path"), factile.StatOptions{})
	case "factile_read":
		return s.workspace.Read(ctx, stringArg(args, "path"), factile.ReadOptions{})
	case "factile_search":
		return s.workspace.Search(ctx, stringArg(args, "path"), stringArg(args, "query"), factile.SearchOptions{View: stringArg(args, "view")})
	case "factile_context":
		return s.workspace.Context(ctx, stringArg(args, "path"), stringArg(args, "query"), factile.ContextOptions{MaxTokens: intArg(args, "max_tokens"), Depth: intArgDefault(args, "depth", 1), View: stringArg(args, "view")})
	case "factile_graph":
		return s.workspace.Graph(ctx, stringArg(args, "path"), factile.GraphOptions{Depth: intArgDefault(args, "depth", 1), View: stringArg(args, "view")})
	case "factile_validate":
		return s.workspace.Validate(ctx, stringArg(args, "path"), factile.ValidateOptions{View: stringArg(args, "view")})
	case "factile_mounts":
		return s.workspace.ListMounts(ctx)
	case "factile_refresh":
		return s.workspace.Refresh(ctx, stringArg(args, "mount_path"))
	case "factile_view_list":
		return s.workspace.ListViews(ctx)
	case "factile_view_inspect":
		return s.workspace.InspectView(ctx, stringArg(args, "id"))
	case "factile_mount":
		if s.opts.ReadOnly {
			return nil, factile.NewError(factile.ErrSourceReadOnly, "MCP server is read-only")
		}
		writable, err := mountWritableArg(args)
		if err != nil {
			return nil, err
		}
		return s.workspace.Mount(ctx, stringArg(args, "source"), stringArg(args, "mount_path"), factile.MountOptions{
			Writable:    writable,
			Title:       stringArg(args, "title"),
			Description: stringArg(args, "description"),
			Ref:         stringArg(args, "ref"),
			Revision:    stringArg(args, "revision"),
			RefSet:      hasArg(args, "ref"),
			RevisionSet: hasArg(args, "revision"),
		})
	case "factile_unmount":
		if s.opts.ReadOnly {
			return nil, factile.NewError(factile.ErrSourceReadOnly, "MCP server is read-only")
		}
		return s.workspace.Unmount(ctx, stringArg(args, "mount_path"), factile.UnmountOptions{})
	case "factile_view_set":
		if s.opts.ReadOnly {
			return nil, factile.NewError(factile.ErrSourceReadOnly, "MCP server is read-only")
		}
		return s.workspace.SetView(ctx, stringArg(args, "id"), factile.ViewInput{
			Title:       stringArg(args, "title"),
			Description: stringArg(args, "description"),
			Status:      stringArg(args, "status"),
			Paths:       stringSliceArg(args, "paths"),
		})
	case "factile_view_delete":
		if s.opts.ReadOnly {
			return nil, factile.NewError(factile.ErrSourceReadOnly, "MCP server is read-only")
		}
		return s.workspace.DeleteView(ctx, stringArg(args, "id"))
	case "factile_mkdir":
		if s.opts.ReadOnly {
			return nil, factile.NewError(factile.ErrSourceReadOnly, "MCP server is read-only")
		}
		return s.workspace.Mkdir(ctx, stringArg(args, "path"), factile.MkdirOptions{
			Title:    stringArg(args, "title"),
			Log:      boolArg(args, "log"),
			Overview: boolArg(args, "overview"),
			Bundle:   boolArg(args, "bundle"),
		})
	case "factile_create":
		if s.opts.ReadOnly {
			return nil, factile.NewError(factile.ErrSourceReadOnly, "MCP server is read-only")
		}
		return s.workspace.Create(ctx, stringArg(args, "path"), factile.CreateConceptInput{Type: stringArg(args, "type"), Title: stringArg(args, "title"), Markdown: stringArg(args, "markdown")})
	case "factile_write":
		if s.opts.ReadOnly {
			return nil, factile.NewError(factile.ErrSourceReadOnly, "MCP server is read-only")
		}
		return s.workspace.Write(ctx, stringArg(args, "path"), factile.WriteConceptInput{ExpectedRevision: stringArg(args, "expected_revision"), Markdown: stringArg(args, "markdown")})
	case "factile_patch":
		if s.opts.ReadOnly {
			return nil, factile.NewError(factile.ErrSourceReadOnly, "MCP server is read-only")
		}
		return s.workspace.Patch(ctx, stringArg(args, "path"), factile.PatchConceptInput{
			ExpectedRevision: stringArg(args, "expected_revision"),
			Set:              anyMapArg(args, "set"),
			DeleteKeys:       stringSliceArg(args, "delete_keys"),
			ReplaceSections:  stringMapArg(args, "replace_sections"),
			AppendSections:   stringMapArg(args, "append_sections"),
			ReplaceBody:      optionalStringArg(args, "replace_body"),
		})
	case "factile_rename":
		if s.opts.ReadOnly {
			return nil, factile.NewError(factile.ErrSourceReadOnly, "MCP server is read-only")
		}
		return s.workspace.Rename(ctx, stringArg(args, "old_path"), stringArg(args, "new_path"), factile.RenameOptions{ExpectedRevision: stringArg(args, "expected_revision")})
	case "factile_delete":
		if s.opts.ReadOnly {
			return nil, factile.NewError(factile.ErrSourceReadOnly, "MCP server is read-only")
		}
		return s.workspace.Delete(ctx, stringArg(args, "path"), factile.DeleteOptions{ExpectedRevision: stringArg(args, "expected_revision")})
	case "factile_deprecate":
		if s.opts.ReadOnly {
			return nil, factile.NewError(factile.ErrSourceReadOnly, "MCP server is read-only")
		}
		return s.workspace.Deprecate(ctx, stringArg(args, "path"), factile.DeprecateOptions{ExpectedRevision: stringArg(args, "expected_revision"), Reason: stringArg(args, "reason")})
	default:
		return nil, factile.NewError(factile.ErrUnsupportedCommand, "Unsupported MCP tool: "+name)
	}
}

func traceMCP(name string, args map[string]any, code int, started time.Time, value any) {
	trace.Append(trace.Event{
		Surface:     "mcp",
		Command:     traceCommand(name, args),
		Path:        tracePath(args),
		Query:       stringArg(args, "query"),
		ExitCode:    code,
		DurationMS:  time.Since(started).Milliseconds(),
		ResultCount: resultCount(value),
	})
}

func traceCommand(name string, args map[string]any) string {
	if name == "factile_list" && boolArg(args, "brief") {
		return "factile_list --brief"
	}
	return name
}

func tracePath(args map[string]any) string {
	if path := stringArg(args, "path"); path != "" {
		return path
	}
	if path := stringArg(args, "old_path"); path != "" {
		return path
	}
	if path := stringArg(args, "mount_path"); path != "" {
		return path
	}
	return ""
}

func resultCount(value any) int {
	switch v := value.(type) {
	case factile.ListResult:
		return len(v.Folders) + len(v.Documents) + len(v.Cards) + len(v.Mounts) + len(v.Paths) + len(v.Concepts)
	case factile.StatResult:
		if v.Card.Path != "" {
			return 1
		}
		return 0
	case factile.SearchResults:
		return len(v.Results)
	case factile.ContextPack:
		return len(v.Concepts)
	case factile.GraphResult:
		return len(v.Nodes)
	case factile.ValidationResult:
		return len(v.Issues)
	case factile.ViewListResult:
		return len(v.Views)
	case factile.ViewResult:
		if v.View.ID != "" {
			return 1
		}
		return 0
	case factile.ViewDeleteResult:
		if v.ID != "" {
			return 1
		}
		return 0
	case factile.DirectoryResult:
		if v.Directory.Path != "" {
			return 1
		}
		return 0
	case factile.MountResult:
		if v.Mount.MountPath != "" {
			return 1
		}
		return 0
	case factile.UnmountResult:
		if v.MountPath != "" {
			return 1
		}
		return 0
	case factile.MountListResult:
		return len(v.Mounts)
	case factile.RefreshResult:
		if v.MountPath != "" {
			return 1
		}
		return 0
	case factile.BundleInspectResult:
		return len(v.Concepts) + len(v.Issues)
	case factile.BundleFindResult:
		return len(v.Sources)
	default:
		return 0
	}
}

func stringArg(args map[string]any, key string) string {
	if args == nil {
		return ""
	}
	if value, ok := args[key]; ok {
		return fmt.Sprint(value)
	}
	return ""
}

func hasArg(args map[string]any, key string) bool {
	if args == nil {
		return false
	}
	_, ok := args[key]
	return ok
}

func intArg(args map[string]any, key string) int {
	if args == nil {
		return 0
	}
	switch value := args[key].(type) {
	case int:
		return value
	case float64:
		return int(value)
	default:
		return 0
	}
}

func intArgDefault(args map[string]any, key string, fallback int) int {
	if args == nil {
		return fallback
	}
	if _, ok := args[key]; !ok {
		return fallback
	}
	return intArg(args, key)
}

func boolArg(args map[string]any, key string) bool {
	if args == nil {
		return false
	}
	switch value := args[key].(type) {
	case bool:
		return value
	case string:
		return value == "true"
	default:
		return false
	}
}

func mountWritableArg(args map[string]any) (bool, error) {
	_, hasWritable := args["writable"]
	_, hasReadOnly := args["read_only"]
	if hasWritable && hasReadOnly {
		return false, factile.NewError(factile.ErrValidationFailed, "Mount capability inputs are contradictory.")
	}
	if hasWritable {
		return boolArg(args, "writable"), nil
	}
	if hasReadOnly {
		return !boolArg(args, "read_only"), nil
	}
	return false, nil
}

func anyMapArg(args map[string]any, key string) map[string]any {
	value, ok := args[key]
	if !ok {
		return nil
	}
	typed, ok := value.(map[string]any)
	if !ok {
		return nil
	}
	return typed
}

func stringMapArg(args map[string]any, key string) map[string]string {
	value, ok := args[key]
	if !ok {
		return nil
	}
	typed, ok := value.(map[string]any)
	if !ok {
		return nil
	}
	out := map[string]string{}
	for k, v := range typed {
		out[k] = fmt.Sprint(v)
	}
	return out
}

func stringSliceArg(args map[string]any, key string) []string {
	value, ok := args[key]
	if !ok {
		return nil
	}
	typed, ok := value.([]any)
	if !ok {
		return nil
	}
	out := make([]string, 0, len(typed))
	for _, item := range typed {
		out = append(out, fmt.Sprint(item))
	}
	return out
}

func optionalStringArg(args map[string]any, key string) *string {
	value, ok := args[key]
	if !ok {
		return nil
	}
	text := fmt.Sprint(value)
	return &text
}

func mustJSON(value any) string {
	data, err := json.Marshal(value)
	if err != nil {
		return "{}"
	}
	return string(data)
}
