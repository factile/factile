package uibridge

import (
	"context"
	"encoding/json"
	"io/fs"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os/exec"
	"path"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/factile/factile/pkg/factile"
)

const APIPrefix = "/api/local/v1"

type Options struct {
	Port      int
	DevAssets string
	Curator   bool
}

type Result struct {
	URL       string `json:"url"`
	API       string `json:"api"`
	Mode      string `json:"mode"`
	DevAssets string `json:"dev_assets,omitempty"`
}

type Server struct {
	result   Result
	listener net.Listener
	server   *http.Server
}

type readerWorkspace interface {
	List(ctx context.Context, path string, opts factile.ListOptions) (factile.ListResult, error)
	Stat(ctx context.Context, path string, opts factile.StatOptions) (factile.StatResult, error)
	Read(ctx context.Context, path string, opts factile.ReadOptions) (factile.ConceptResult, error)
	Search(ctx context.Context, path string, query string, opts factile.SearchOptions) (factile.SearchResults, error)
	Context(ctx context.Context, path string, query string, opts factile.ContextOptions) (factile.ContextPack, error)
	Graph(ctx context.Context, path string, opts factile.GraphOptions) (factile.GraphResult, error)
	Validate(ctx context.Context, path string, opts factile.ValidateOptions) (factile.ValidationResult, error)
	Summary(ctx context.Context) (factile.SummaryResult, error)
	ListViews(ctx context.Context) (factile.ViewListResult, error)
	InspectView(ctx context.Context, id string) (factile.ViewResult, error)
}

type curatorWorkspace interface {
	readerWorkspace
	Create(ctx context.Context, path string, input factile.CreateConceptInput) (factile.ConceptResult, error)
	Write(ctx context.Context, path string, input factile.WriteConceptInput) (factile.ConceptResult, error)
	Patch(ctx context.Context, path string, input factile.PatchConceptInput) (factile.ConceptResult, error)
	Rename(ctx context.Context, oldPath string, newPath string, opts factile.RenameOptions) (factile.RenameResult, error)
	Deprecate(ctx context.Context, path string, opts factile.DeprecateOptions) (factile.ConceptResult, error)
}

func Start(ws factile.Workspace, opts Options) (*Server, error) {
	listener, err := listenLoopback(opts.Port)
	if err != nil {
		return nil, err
	}

	baseURL := "http://" + listener.Addr().String()
	result := Result{
		URL:       baseURL + "/",
		API:       baseURL + APIPrefix,
		Mode:      mode(opts),
		DevAssets: opts.DevAssets,
	}
	httpServer := &http.Server{
		Handler: NewHandler(ws, opts),
	}
	return &Server{result: result, listener: listener, server: httpServer}, nil
}

func (s *Server) Result() Result {
	return s.result
}

func (s *Server) Serve(ctx context.Context) error {
	errc := make(chan error, 1)
	go func() {
		errc <- s.server.Serve(s.listener)
	}()

	select {
	case err := <-errc:
		if err == http.ErrServerClosed {
			return nil
		}
		return err
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		if err := s.server.Shutdown(shutdownCtx); err != nil {
			return err
		}
		if err := <-errc; err != nil && err != http.ErrServerClosed {
			return err
		}
		return nil
	}
}

func NewHandler(ws curatorWorkspace, opts Options) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc(APIPrefix+"/health", healthHandler(opts))
	mux.HandleFunc(APIPrefix+"/capabilities", capabilitiesHandler(opts))
	mux.HandleFunc(APIPrefix+"/source", sourceHandler(ws, opts))
	mux.HandleFunc(APIPrefix+"/views", viewsHandler(ws))
	mux.HandleFunc(APIPrefix+"/view", viewHandler(ws))
	mux.HandleFunc(APIPrefix+"/reader/list", listHandler(ws))
	mux.HandleFunc(APIPrefix+"/reader/stat", statHandler(ws))
	mux.HandleFunc(APIPrefix+"/reader/read", readHandler(ws))
	mux.HandleFunc(APIPrefix+"/reader/search", searchHandler(ws))
	mux.HandleFunc(APIPrefix+"/reader/context", contextHandler(ws))
	mux.HandleFunc(APIPrefix+"/reader/graph", graphHandler(ws))
	mux.HandleFunc(APIPrefix+"/reader/validate", validateHandler(ws))

	if opts.Curator {
		mux.HandleFunc(APIPrefix+"/writer/create", createHandler(ws))
		mux.HandleFunc(APIPrefix+"/writer/write", writeHandler(ws))
		mux.HandleFunc(APIPrefix+"/writer/update", writeHandler(ws))
		mux.HandleFunc(APIPrefix+"/writer/patch", patchHandler(ws))
		mux.HandleFunc(APIPrefix+"/writer/rename", renameHandler(ws))
		mux.HandleFunc(APIPrefix+"/writer/deprecate", deprecateHandler(ws))
		mux.HandleFunc(APIPrefix+"/writer/validate", writerValidateHandler(ws))
	} else {
		for _, path := range []string{
			"/writer/create",
			"/writer/write",
			"/writer/update",
			"/writer/patch",
			"/writer/rename",
			"/writer/deprecate",
			"/writer/validate",
		} {
			mux.HandleFunc(APIPrefix+path, unsupportedOperationHandler(strings.TrimPrefix(path, "/writer/")))
		}
	}
	for _, path := range []string{
		"/writer/delete",
		"/writer/mount",
		"/writer/unmount",
		"/writer/view-set",
		"/writer/view-delete",
	} {
		mux.HandleFunc(APIPrefix+path, unsupportedOperationHandler(strings.TrimPrefix(path, "/writer/")))
	}

	var devProxy http.Handler
	if strings.TrimSpace(opts.DevAssets) != "" {
		if target, err := url.Parse(opts.DevAssets); err == nil && target.Scheme != "" && target.Host != "" {
			devProxy = httputil.NewSingleHostReverseProxy(target)
		}
	}

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, APIPrefix+"/") {
			mux.ServeHTTP(w, r)
			return
		}
		if devProxy != nil {
			devProxy.ServeHTTP(w, r)
			return
		}
		serveEmbeddedApp(w, r)
	})
}

func listenLoopback(port int) (net.Listener, error) {
	if port < 0 || port > 65535 {
		return nil, factile.NewError(factile.ErrInvalidPath, "Invalid UI port: "+strconv.Itoa(port))
	}
	return net.Listen("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(port)))
}

func healthHandler(opts Options) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !requireGet(w, r) {
			return
		}
		writeJSON(w, map[string]any{
			"status":    "ok",
			"mode":      mode(opts),
			"read_only": !opts.Curator,
		})
	}
}

func capabilitiesHandler(opts Options) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !requireGet(w, r) {
			return
		}
		writeJSON(w, map[string]any{
			"transport": "local_http",
			"mode":      mode(opts),
			"reader": map[string]bool{
				"list":            true,
				"stat":            true,
				"read":            true,
				"search":          true,
				"context":         true,
				"graph":           true,
				"validate":        true,
				"views":           true,
				"source_metadata": true,
			},
			"writer": map[string]bool{
				"create":      opts.Curator,
				"write":       opts.Curator,
				"patch":       opts.Curator,
				"rename":      opts.Curator,
				"delete":      false,
				"deprecate":   opts.Curator,
				"mount":       false,
				"unmount":     false,
				"view_set":    false,
				"view_delete": false,
			},
		})
	}
}

func sourceHandler(ws readerWorkspace, opts Options) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !requireGet(w, r) {
			return
		}
		if selectorFromQuery(r).has() {
			writeUnsupportedSource(w)
			return
		}
		summary, err := ws.Summary(r.Context())
		if err != nil {
			if factile.ErrorCode(factile.NormalizeError(err)) != factile.ErrNoActiveRoot {
				writeError(w, errorStatus(err), err)
				return
			}
			writeJSON(w, map[string]any{
				"source": map[string]any{
					"title":       "Local Factile workspace",
					"description": "Active local workspace",
					"writable":    opts.Curator,
					"metadata":    map[string]any{},
				},
			})
			return
		}
		writeJSON(w, map[string]any{
			"source": map[string]any{
				"title":       "Local Factile workspace",
				"description": summary.Workspace.Path,
				"writable":    opts.Curator,
				"metadata": map[string]any{
					"workspace": summary.Workspace,
					"sources":   summary.Sources,
				},
			},
		})
	}
}

func viewsHandler(ws readerWorkspace) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !requireGet(w, r) {
			return
		}
		if selectorFromQuery(r).has() {
			writeUnsupportedSource(w)
			return
		}
		result, err := ws.ListViews(r.Context())
		if err != nil {
			if factile.ErrorCode(factile.NormalizeError(err)) == factile.ErrNoActiveRoot {
				writeJSON(w, factile.ViewListResult{Views: []factile.View{}})
				return
			}
			writeError(w, errorStatus(err), err)
			return
		}
		writeJSON(w, result)
	}
}

func viewHandler(ws readerWorkspace) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !requireGet(w, r) {
			return
		}
		if selectorFromQuery(r).has() {
			writeUnsupportedSource(w)
			return
		}
		id := r.URL.Query().Get("id")
		if strings.TrimSpace(id) == "" {
			writeError(w, http.StatusBadRequest, factile.NewError(factile.ErrInvalidPath, "id is required"))
			return
		}
		result, err := ws.InspectView(r.Context(), id)
		if err != nil {
			writeError(w, errorStatus(err), err)
			return
		}
		writeJSON(w, result)
	}
}

func listHandler(ws readerWorkspace) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !requireGet(w, r) {
			return
		}
		selector := selectorFromQuery(r)
		if selector.has() {
			writeUnsupportedSource(w)
			return
		}
		brief, err := boolQuery(r, "brief")
		if err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		path := r.URL.Query().Get("path")
		if path == "" {
			path = "/"
		}
		result, err := ws.List(r.Context(), path, factile.ListOptions{Brief: brief, View: r.URL.Query().Get("view")})
		if err != nil {
			writeError(w, errorStatus(err), err)
			return
		}
		writeJSON(w, result)
	}
}

func statHandler(ws readerWorkspace) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !requireGet(w, r) {
			return
		}
		if selectorFromQuery(r).has() {
			writeUnsupportedSource(w)
			return
		}
		path := r.URL.Query().Get("path")
		if path == "" {
			path = "/"
		}
		result, err := ws.Stat(r.Context(), path, factile.StatOptions{})
		if err != nil {
			writeError(w, errorStatus(err), err)
			return
		}
		writeJSON(w, result)
	}
}

func readHandler(ws readerWorkspace) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !requireGet(w, r) {
			return
		}
		if selectorFromQuery(r).has() {
			writeUnsupportedSource(w)
			return
		}
		path := r.URL.Query().Get("path")
		if strings.TrimSpace(path) == "" {
			writeError(w, http.StatusBadRequest, factile.NewError(factile.ErrInvalidPath, "path is required"))
			return
		}
		result, err := ws.Read(r.Context(), path, factile.ReadOptions{})
		if err != nil {
			writeError(w, errorStatus(err), err)
			return
		}
		writeJSON(w, result)
	}
}

func searchHandler(ws readerWorkspace) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !requirePost(w, r) {
			return
		}
		var input searchInput
		if !decodeBody(w, r, &input) {
			return
		}
		if input.sourceSelector.has() {
			writeUnsupportedSource(w)
			return
		}
		if strings.TrimSpace(input.Path) == "" || strings.TrimSpace(input.Query) == "" {
			writeError(w, http.StatusBadRequest, factile.NewError(factile.ErrInvalidPath, "path and query are required"))
			return
		}
		result, err := ws.Search(r.Context(), input.Path, input.Query, factile.SearchOptions{View: input.View})
		if err != nil {
			writeError(w, errorStatus(err), err)
			return
		}
		writeJSON(w, result)
	}
}

func contextHandler(ws readerWorkspace) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !requirePost(w, r) {
			return
		}
		var input contextInput
		if !decodeBody(w, r, &input) {
			return
		}
		if input.sourceSelector.has() {
			writeUnsupportedSource(w)
			return
		}
		if strings.TrimSpace(input.Path) == "" || strings.TrimSpace(input.Query) == "" {
			writeError(w, http.StatusBadRequest, factile.NewError(factile.ErrInvalidPath, "path and query are required"))
			return
		}
		maxTokens := input.MaxTokens
		if maxTokens == 0 {
			maxTokens = 4000
		}
		depth := input.Depth
		if depth == 0 && !input.DepthSet {
			depth = 1
		}
		result, err := ws.Context(r.Context(), input.Path, input.Query, factile.ContextOptions{
			MaxTokens: maxTokens,
			Depth:     depth,
			View:      input.View,
		})
		if err != nil {
			writeError(w, errorStatus(err), err)
			return
		}
		writeJSON(w, result)
	}
}

func graphHandler(ws readerWorkspace) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !requireGet(w, r) {
			return
		}
		if selectorFromQuery(r).has() {
			writeUnsupportedSource(w)
			return
		}
		path := r.URL.Query().Get("path")
		if strings.TrimSpace(path) == "" {
			writeError(w, http.StatusBadRequest, factile.NewError(factile.ErrInvalidPath, "path is required"))
			return
		}
		depth, err := intQuery(r, "depth", 1)
		if err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		result, err := ws.Graph(r.Context(), path, factile.GraphOptions{Depth: depth, View: r.URL.Query().Get("view")})
		if err != nil {
			writeError(w, errorStatus(err), err)
			return
		}
		writeJSON(w, result)
	}
}

func validateHandler(ws readerWorkspace) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !requireGet(w, r) {
			return
		}
		if selectorFromQuery(r).has() {
			writeUnsupportedSource(w)
			return
		}
		path := r.URL.Query().Get("path")
		if strings.TrimSpace(path) == "" {
			writeError(w, http.StatusBadRequest, factile.NewError(factile.ErrInvalidPath, "path is required"))
			return
		}
		result, err := ws.Validate(r.Context(), path, factile.ValidateOptions{View: r.URL.Query().Get("view")})
		if err != nil {
			writeError(w, errorStatus(err), err)
			return
		}
		writeJSON(w, result)
	}
}

func createHandler(ws curatorWorkspace) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !requirePost(w, r) {
			return
		}
		var input createInput
		if !decodeBody(w, r, &input) {
			return
		}
		if input.sourceSelector.has() {
			writeUnsupportedSource(w)
			return
		}
		if strings.TrimSpace(input.Path) == "" || strings.TrimSpace(input.Type) == "" || strings.TrimSpace(input.Title) == "" {
			writeError(w, http.StatusBadRequest, factile.NewError(factile.ErrValidationFailed, "path, type, and title are required"))
			return
		}
		result, err := ws.Create(r.Context(), input.Path, factile.CreateConceptInput{
			Type:        input.Type,
			Title:       input.Title,
			Description: input.Description,
			Tags:        input.Tags,
			Resource:    input.Resource,
			Markdown:    input.Markdown,
		})
		if err != nil {
			writeError(w, errorStatus(err), err)
			return
		}
		writeJSON(w, result)
	}
}

func writeHandler(ws curatorWorkspace) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !requirePost(w, r) {
			return
		}
		var input writeInput
		if !decodeBody(w, r, &input) {
			return
		}
		if input.sourceSelector.has() {
			writeUnsupportedSource(w)
			return
		}
		if strings.TrimSpace(input.Path) == "" {
			writeError(w, http.StatusBadRequest, factile.NewError(factile.ErrInvalidPath, "path is required"))
			return
		}
		result, err := ws.Write(r.Context(), input.Path, factile.WriteConceptInput{
			ExpectedRevision: input.ExpectedRevision,
			Markdown:         input.Markdown,
		})
		if err != nil {
			writeError(w, errorStatus(err), err)
			return
		}
		writeJSON(w, result)
	}
}

func patchHandler(ws curatorWorkspace) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !requirePost(w, r) {
			return
		}
		var input patchInput
		if !decodeBody(w, r, &input) {
			return
		}
		if input.sourceSelector.has() {
			writeUnsupportedSource(w)
			return
		}
		if strings.TrimSpace(input.Path) == "" {
			writeError(w, http.StatusBadRequest, factile.NewError(factile.ErrInvalidPath, "path is required"))
			return
		}
		result, err := ws.Patch(r.Context(), input.Path, factile.PatchConceptInput{
			ExpectedRevision: input.ExpectedRevision,
			Set:              input.Set,
			DeleteKeys:       input.DeleteKeys,
			ReplaceSections:  input.ReplaceSections,
			AppendSections:   input.AppendSections,
			ReplaceBody:      input.ReplaceBody,
		})
		if err != nil {
			writeError(w, errorStatus(err), err)
			return
		}
		writeJSON(w, result)
	}
}

func renameHandler(ws curatorWorkspace) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !requirePost(w, r) {
			return
		}
		var input renameInput
		if !decodeBody(w, r, &input) {
			return
		}
		if input.sourceSelector.has() {
			writeUnsupportedSource(w)
			return
		}
		if strings.TrimSpace(input.OldPath) == "" || strings.TrimSpace(input.NewPath) == "" {
			writeError(w, http.StatusBadRequest, factile.NewError(factile.ErrInvalidPath, "old_path and new_path are required"))
			return
		}
		result, err := ws.Rename(r.Context(), input.OldPath, input.NewPath, factile.RenameOptions{ExpectedRevision: input.ExpectedRevision})
		if err != nil {
			writeError(w, errorStatus(err), err)
			return
		}
		writeJSON(w, result)
	}
}

func deprecateHandler(ws curatorWorkspace) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !requirePost(w, r) {
			return
		}
		var input deprecateInput
		if !decodeBody(w, r, &input) {
			return
		}
		if input.sourceSelector.has() {
			writeUnsupportedSource(w)
			return
		}
		if strings.TrimSpace(input.Path) == "" {
			writeError(w, http.StatusBadRequest, factile.NewError(factile.ErrInvalidPath, "path is required"))
			return
		}
		result, err := ws.Deprecate(r.Context(), input.Path, factile.DeprecateOptions{
			ExpectedRevision: input.ExpectedRevision,
			Reason:           input.Reason,
		})
		if err != nil {
			writeError(w, errorStatus(err), err)
			return
		}
		writeJSON(w, result)
	}
}

func writerValidateHandler(ws curatorWorkspace) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !requirePost(w, r) {
			return
		}
		var input writerValidateInput
		if !decodeBody(w, r, &input) {
			return
		}
		if input.sourceSelector.has() {
			writeUnsupportedSource(w)
			return
		}
		if strings.TrimSpace(input.Path) == "" {
			writeError(w, http.StatusBadRequest, factile.NewError(factile.ErrInvalidPath, "path is required"))
			return
		}
		result, err := ws.Validate(r.Context(), input.Path, factile.ValidateOptions{View: input.View})
		if err != nil {
			writeError(w, errorStatus(err), err)
			return
		}
		writeJSON(w, result)
	}
}

func unsupportedOperationHandler(operation string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		writeError(w, http.StatusNotImplemented, &factile.AppError{
			Code:    "unsupported_operation",
			Message: "Unsupported Factile operation: " + operation,
			Details: map[string]any{
				"operation": operation,
			},
		})
	}
}

func requireGet(w http.ResponseWriter, r *http.Request) bool {
	if r.Method == http.MethodGet {
		return true
	}
	writeError(w, http.StatusMethodNotAllowed, factile.NewError(factile.ErrUnsupportedCommand, "Unsupported method: "+r.Method))
	return false
}

func requirePost(w http.ResponseWriter, r *http.Request) bool {
	if r.Method == http.MethodPost {
		return true
	}
	writeError(w, http.StatusMethodNotAllowed, factile.NewError(factile.ErrUnsupportedCommand, "Unsupported method: "+r.Method))
	return false
}

type sourceSelector struct {
	SourceURI string `json:"source_uri,omitempty"`
	Ref       string `json:"ref,omitempty"`
	Revision  string `json:"revision,omitempty"`
}

func (s sourceSelector) has() bool {
	return s.SourceURI != "" || s.Ref != "" || s.Revision != ""
}

type searchInput struct {
	sourceSelector
	Path  string `json:"path"`
	Query string `json:"query"`
	View  string `json:"view,omitempty"`
}

type contextInput struct {
	sourceSelector
	Path      string `json:"path"`
	Query     string `json:"query"`
	MaxTokens int    `json:"max_tokens,omitempty"`
	Depth     int    `json:"depth,omitempty"`
	DepthSet  bool   `json:"-"`
	View      string `json:"view,omitempty"`
}

type createInput struct {
	sourceSelector
	Path        string   `json:"path"`
	Type        string   `json:"type"`
	Title       string   `json:"title"`
	Description string   `json:"description,omitempty"`
	Tags        []string `json:"tags,omitempty"`
	Resource    string   `json:"resource,omitempty"`
	Markdown    string   `json:"markdown"`
}

type writeInput struct {
	sourceSelector
	Path             string `json:"path"`
	ExpectedRevision string `json:"expected_revision"`
	Markdown         string `json:"markdown"`
}

type patchInput struct {
	sourceSelector
	Path             string            `json:"path"`
	ExpectedRevision string            `json:"expected_revision"`
	Set              map[string]any    `json:"set,omitempty"`
	DeleteKeys       []string          `json:"delete_keys,omitempty"`
	ReplaceSections  map[string]string `json:"replace_sections,omitempty"`
	AppendSections   map[string]string `json:"append_sections,omitempty"`
	ReplaceBody      *string           `json:"replace_body,omitempty"`
}

type deprecateInput struct {
	sourceSelector
	Path             string `json:"path"`
	ExpectedRevision string `json:"expected_revision"`
	Reason           string `json:"reason"`
}

type renameInput struct {
	sourceSelector
	OldPath          string `json:"old_path"`
	NewPath          string `json:"new_path"`
	ExpectedRevision string `json:"expected_revision"`
}

type writerValidateInput struct {
	sourceSelector
	Path string `json:"path"`
	View string `json:"view,omitempty"`
}

func (i *contextInput) UnmarshalJSON(data []byte) error {
	type alias contextInput
	var raw struct {
		alias
		Depth *int `json:"depth,omitempty"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	*i = contextInput(raw.alias)
	if raw.Depth != nil {
		i.Depth = *raw.Depth
		i.DepthSet = true
	}
	return nil
}

func selectorFromQuery(r *http.Request) sourceSelector {
	query := r.URL.Query()
	return sourceSelector{
		SourceURI: query.Get("source_uri"),
		Ref:       query.Get("ref"),
		Revision:  query.Get("revision"),
	}
}

func decodeBody(w http.ResponseWriter, r *http.Request, value any) bool {
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(value); err != nil {
		writeError(w, http.StatusBadRequest, factile.NewError(factile.ErrValidationFailed, "Invalid JSON body: "+err.Error()))
		return false
	}
	return true
}

func boolQuery(r *http.Request, name string) (bool, error) {
	raw := r.URL.Query().Get(name)
	if raw == "" {
		return false, nil
	}
	value, err := strconv.ParseBool(raw)
	if err != nil {
		return false, factile.NewError(factile.ErrInvalidPath, "Invalid boolean query parameter: "+name)
	}
	return value, nil
}

func intQuery(r *http.Request, name string, fallback int) (int, error) {
	raw := r.URL.Query().Get(name)
	if raw == "" {
		return fallback, nil
	}
	value, err := strconv.Atoi(raw)
	if err != nil {
		return 0, factile.NewError(factile.ErrInvalidPath, "Invalid integer query parameter: "+name)
	}
	return value, nil
}

func writeUnsupportedSource(w http.ResponseWriter) {
	writeError(w, http.StatusBadRequest, factile.NewError(factile.ErrUnsupportedSource, "Local UI bridge reads the active root only"))
}

func writeJSON(w http.ResponseWriter, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	_ = json.NewEncoder(w).Encode(value)
}

func writeError(w http.ResponseWriter, status int, err error) {
	normalized := factile.NormalizeError(err)
	app, ok := normalized.(*factile.AppError)
	if !ok {
		app = factile.NewError("general_failure", normalized.Error())
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{"error": app})
}

func errorStatus(err error) int {
	switch factile.ErrorCode(factile.NormalizeError(err)) {
	case factile.ErrInvalidPath:
		return http.StatusBadRequest
	case factile.ErrConceptNotFound, factile.ErrMountNotFound, factile.ErrPathIsNotConcept, factile.ErrPathIsNotBundle:
		return http.StatusNotFound
	case factile.ErrConceptAlreadyExist, factile.ErrRevisionMismatch:
		return http.StatusConflict
	case factile.ErrRevisionRequired:
		return http.StatusBadRequest
	case factile.ErrSourceReadOnly:
		return http.StatusForbidden
	case factile.ErrUnsupportedSource, factile.ErrUnsupportedCommand:
		return http.StatusBadRequest
	case factile.ErrValidationFailed, factile.ErrOKFParse, factile.ErrSectionNotFound:
		return http.StatusUnprocessableEntity
	case factile.ErrLockTimeout:
		return http.StatusLocked
	default:
		return http.StatusInternalServerError
	}
}

func serveEmbeddedApp(w http.ResponseWriter, r *http.Request) {
	staticFS, err := fs.Sub(embeddedStatic, "static")
	if err != nil {
		http.Error(w, "Factile UI assets are unavailable", http.StatusInternalServerError)
		return
	}

	assetPath := cleanAssetPath(r.URL.Path)
	if !embeddedAssetExists(staticFS, assetPath) {
		assetPath = "index.html"
	}
	if assetPath == "index.html" {
		serveEmbeddedIndex(w, staticFS)
		return
	}

	next := new(http.Request)
	*next = *r
	nextURL := *r.URL
	nextURL.Path = "/" + assetPath
	next.URL = &nextURL
	http.FileServer(http.FS(staticFS)).ServeHTTP(w, next)
}

func serveEmbeddedIndex(w http.ResponseWriter, staticFS fs.FS) {
	data, err := fs.ReadFile(staticFS, "index.html")
	if err != nil {
		http.Error(w, "Factile UI index is unavailable", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write(data)
}

func cleanAssetPath(rawPath string) string {
	cleaned := path.Clean("/" + rawPath)
	if cleaned == "/" || cleaned == "." {
		return "index.html"
	}
	return strings.TrimPrefix(cleaned, "/")
}

func embeddedAssetExists(staticFS fs.FS, name string) bool {
	file, err := staticFS.Open(name)
	if err != nil {
		return false
	}
	defer file.Close()
	info, err := file.Stat()
	return err == nil && !info.IsDir()
}

func mode(opts Options) string {
	if opts.Curator {
		return "curator"
	}
	return "reader"
}

func OpenBrowser(rawURL string) error {
	var command string
	var args []string
	switch runtime.GOOS {
	case "darwin":
		command = "open"
		args = []string{rawURL}
	case "windows":
		command = "rundll32"
		args = []string{"url.dll,FileProtocolHandler", rawURL}
	default:
		command = "xdg-open"
		args = []string{rawURL}
	}
	return exec.Command(command, args...).Start()
}
