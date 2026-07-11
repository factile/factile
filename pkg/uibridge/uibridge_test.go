package uibridge

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/factile/factile/pkg/factile"
)

type fakeReader struct{}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (fn roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return fn(request)
}

func (fakeReader) List(ctx context.Context, path string, opts factile.ListOptions) (factile.ListResult, error) {
	_ = ctx
	if path == "" || path == "/" {
		return factile.ListResult{
			Path: "/",
			Folders: []factile.FolderSummary{
				{Path: "/guides", Title: "Guides"},
			},
			Documents: []factile.DocumentSummary{
				{Path: "/guides/onboarding", Type: "Guide", Title: "Onboarding"},
			},
		}, nil
	}
	return factile.ListResult{Path: path}, nil
}

func (fakeReader) Stat(ctx context.Context, path string, opts factile.StatOptions) (factile.StatResult, error) {
	_ = ctx
	_ = opts
	return factile.StatResult{Card: factile.CardSummary{Path: path, Title: "Guides"}}, nil
}

func (fakeReader) Read(ctx context.Context, path string, opts factile.ReadOptions) (factile.ConceptResult, error) {
	_ = ctx
	_ = opts
	if path != "/guides/onboarding" {
		return factile.ConceptResult{}, factile.NewError(factile.ErrConceptNotFound, "Concept not found: "+path)
	}
	return factile.ConceptResult{
		Concept: factile.Concept{
			Path:        path,
			ConceptID:   "guides/onboarding",
			Revision:    "fixture:onboarding",
			Frontmatter: map[string]any{"type": "Guide", "title": "Onboarding"},
			Markdown:    "# Onboarding\n",
		},
	}, nil
}

func (fakeReader) Search(ctx context.Context, path string, query string, opts factile.SearchOptions) (factile.SearchResults, error) {
	_ = ctx
	_ = opts
	return factile.SearchResults{
		Path:  path,
		Query: query,
		Results: []factile.SearchResult{
			{Concept: factile.ConceptSummary{Path: "/guides/onboarding", ConceptID: "guides/onboarding", Type: "Guide"}, Score: 1},
		},
	}, nil
}

func (fakeReader) Context(ctx context.Context, path string, query string, opts factile.ContextOptions) (factile.ContextPack, error) {
	_ = ctx
	_ = opts
	return factile.ContextPack{
		Path:  path,
		Query: query,
		Concepts: []factile.Concept{
			{Path: "/guides/onboarding", ConceptID: "guides/onboarding", Revision: "fixture:onboarding", Frontmatter: map[string]any{}, Markdown: "# Onboarding\n"},
		},
	}, nil
}

func (fakeReader) Graph(ctx context.Context, path string, opts factile.GraphOptions) (factile.GraphResult, error) {
	_ = ctx
	_ = opts
	return factile.GraphResult{
		Path: path,
		Nodes: []factile.GraphNode{
			{Concept: factile.ConceptSummary{Path: "/guides/onboarding", ConceptID: "guides/onboarding", Type: "Guide"}},
		},
		Edges: []factile.GraphEdge{
			{From: "/guides/onboarding", To: "/runbooks/repair-loop", Kind: "markdown_link"},
		},
	}, nil
}

func (fakeReader) Validate(ctx context.Context, path string, opts factile.ValidateOptions) (factile.ValidationResult, error) {
	_ = ctx
	_ = opts
	return factile.ValidationResult{Path: path, Valid: true, Issues: []factile.ValidationIssue{}}, nil
}

func (fakeReader) Summary(ctx context.Context) (factile.SummaryResult, error) {
	_ = ctx
	return factile.SummaryResult{
		Workspace: factile.WorkspaceSummary{Path: "/tmp/factile", Version: "test"},
		Sources:   []factile.Mount{{MountPath: "/", Source: ".", Kind: "local", Writable: true}},
	}, nil
}

func (fakeReader) ListViews(ctx context.Context) (factile.ViewListResult, error) {
	_ = ctx
	return factile.ViewListResult{Views: []factile.View{{ID: "support", Title: "Support", Paths: []string{"/guides"}}}}, nil
}

func (fakeReader) InspectView(ctx context.Context, id string) (factile.ViewResult, error) {
	_ = ctx
	if id != "support" {
		return factile.ViewResult{}, factile.NewError(factile.ErrMountNotFound, "View not found: "+id)
	}
	return factile.ViewResult{View: factile.View{ID: "support", Title: "Support", Paths: []string{"/guides"}}}, nil
}

func (fakeReader) Create(ctx context.Context, path string, input factile.CreateConceptInput) (factile.ConceptResult, error) {
	_ = ctx
	return conceptResult(path, input.Markdown), nil
}

func (fakeReader) Write(ctx context.Context, path string, input factile.WriteConceptInput) (factile.ConceptResult, error) {
	_ = ctx
	if input.ExpectedRevision != "fixture:onboarding" {
		return factile.ConceptResult{}, factile.NewError(factile.ErrRevisionMismatch, "Revision mismatch")
	}
	return conceptResult(path, input.Markdown), nil
}

func (fakeReader) Patch(ctx context.Context, path string, input factile.PatchConceptInput) (factile.ConceptResult, error) {
	_ = ctx
	if input.ReplaceBody != nil && *input.ReplaceBody == "invalid" {
		err := factile.NewError(factile.ErrValidationFailed, "Validation failed")
		err.Details = map[string]any{
			"issues": []map[string]any{
				{"severity": "error", "code": "invalid_path", "message": "Invalid fixture content", "path": path},
			},
		}
		return factile.ConceptResult{}, err
	}
	body := "# Patched\n"
	if input.ReplaceBody != nil {
		body = *input.ReplaceBody
	}
	return conceptResult(path, body), nil
}

func (fakeReader) Rename(ctx context.Context, oldPath string, newPath string, opts factile.RenameOptions) (factile.RenameResult, error) {
	_ = ctx
	_ = oldPath
	if opts.ExpectedRevision != "fixture:onboarding" {
		return factile.RenameResult{}, factile.NewError(factile.ErrRevisionMismatch, "Revision mismatch")
	}
	return factile.RenameResult{Concept: conceptResult(newPath, "# Renamed\n").Concept}, nil
}

func (fakeReader) Deprecate(ctx context.Context, path string, opts factile.DeprecateOptions) (factile.ConceptResult, error) {
	_ = ctx
	if strings.TrimSpace(opts.Reason) == "" {
		return factile.ConceptResult{}, factile.NewError(factile.ErrValidationFailed, "Deprecation reason is required")
	}
	return conceptResult(path, "# Deprecated\n"), nil
}

func conceptResult(path string, markdown string) factile.ConceptResult {
	return factile.ConceptResult{
		Concept: factile.Concept{
			Path:        path,
			ConceptID:   strings.TrimPrefix(path, "/"),
			Revision:    "fixture:updated",
			Frontmatter: map[string]any{"type": "Guide", "title": "Updated"},
			Markdown:    markdown,
		},
	}
}

type noActiveRootReader struct {
	fakeReader
}

func (noActiveRootReader) Summary(ctx context.Context) (factile.SummaryResult, error) {
	_ = ctx
	return factile.SummaryResult{}, factile.NewError(factile.ErrNoActiveRoot, "No active Factile root")
}

func (noActiveRootReader) ListViews(ctx context.Context) (factile.ViewListResult, error) {
	_ = ctx
	return factile.ViewListResult{}, factile.NewError(factile.ErrNoActiveRoot, "No active Factile root")
}

func TestHandlerHealthCapabilitiesAndRead(t *testing.T) {
	handler := NewHandler(fakeReader{}, Options{})

	health := request(handler, http.MethodGet, APIPrefix+"/health")
	if health.Code != http.StatusOK {
		t.Fatalf("health status = %d body=%s", health.Code, health.Body.String())
	}
	if !strings.Contains(health.Body.String(), `"read_only":true`) {
		t.Fatalf("health missing read_only: %s", health.Body.String())
	}

	capabilities := request(handler, http.MethodGet, APIPrefix+"/capabilities")
	if capabilities.Code != http.StatusOK || !strings.Contains(capabilities.Body.String(), `"transport":"local_http"`) {
		t.Fatalf("capabilities response = %d %s", capabilities.Code, capabilities.Body.String())
	}
	if !strings.Contains(capabilities.Body.String(), `"list":true`) || !strings.Contains(capabilities.Body.String(), `"write":false`) {
		t.Fatalf("capabilities missing reader/write flags: %s", capabilities.Body.String())
	}

	read := request(handler, http.MethodGet, APIPrefix+"/reader/read?path=%2Fguides%2Fonboarding")
	if read.Code != http.StatusOK {
		t.Fatalf("read status = %d body=%s", read.Code, read.Body.String())
	}
	var result factile.ConceptResult
	if err := json.Unmarshal(read.Body.Bytes(), &result); err != nil {
		t.Fatalf("read response did not decode: %v", err)
	}
	if result.Concept.Path != "/guides/onboarding" {
		t.Fatalf("read concept path = %q", result.Concept.Path)
	}
}

func TestHandlerReaderOperations(t *testing.T) {
	handler := NewHandler(fakeReader{}, Options{})

	for _, tc := range []struct {
		name   string
		method string
		target string
		body   string
		want   string
	}{
		{name: "source", method: http.MethodGet, target: APIPrefix + "/source", want: `"source"`},
		{name: "views", method: http.MethodGet, target: APIPrefix + "/views", want: `"views"`},
		{name: "view", method: http.MethodGet, target: APIPrefix + "/view?id=support", want: `"id":"support"`},
		{name: "list", method: http.MethodGet, target: APIPrefix + "/reader/list?path=%2F&brief=true", want: `"folders"`},
		{name: "stat", method: http.MethodGet, target: APIPrefix + "/reader/stat?path=%2Fguides", want: `"card"`},
		{name: "search", method: http.MethodPost, target: APIPrefix + "/reader/search", body: `{"path":"/","query":"invoice"}`, want: `"results"`},
		{name: "context", method: http.MethodPost, target: APIPrefix + "/reader/context", body: `{"path":"/","query":"invoice","depth":0}`, want: `"concepts"`},
		{name: "graph", method: http.MethodGet, target: APIPrefix + "/reader/graph?path=%2F&depth=1", want: `"edges"`},
		{name: "validate", method: http.MethodGet, target: APIPrefix + "/reader/validate?path=%2F", want: `"valid":true`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			response := requestWithBody(handler, tc.method, tc.target, tc.body)
			if response.Code != http.StatusOK {
				t.Fatalf("%s status = %d body=%s", tc.name, response.Code, response.Body.String())
			}
			if !strings.Contains(response.Body.String(), tc.want) {
				t.Fatalf("%s response missing %q: %s", tc.name, tc.want, response.Body.String())
			}
		})
	}
}

func TestHandlerSourceFallsBackWithoutActiveRoot(t *testing.T) {
	handler := NewHandler(noActiveRootReader{}, Options{})

	response := request(handler, http.MethodGet, APIPrefix+"/source")
	if response.Code != http.StatusOK {
		t.Fatalf("source status = %d body=%s", response.Code, response.Body.String())
	}
	if !strings.Contains(response.Body.String(), `"title":"Local Factile workspace"`) {
		t.Fatalf("source response missing local title: %s", response.Body.String())
	}

	views := request(handler, http.MethodGet, APIPrefix+"/views")
	if views.Code != http.StatusOK {
		t.Fatalf("views status = %d body=%s", views.Code, views.Body.String())
	}
	if !strings.Contains(views.Body.String(), `"views":[]`) {
		t.Fatalf("views response should be empty without active root: %s", views.Body.String())
	}
}

func TestHandlerReadErrors(t *testing.T) {
	handler := NewHandler(fakeReader{}, Options{})

	missingPath := request(handler, http.MethodGet, APIPrefix+"/reader/read")
	if missingPath.Code != http.StatusBadRequest || !strings.Contains(missingPath.Body.String(), `"code":"invalid_path"`) {
		t.Fatalf("missing path response = %d %s", missingPath.Code, missingPath.Body.String())
	}

	missingConcept := request(handler, http.MethodGet, APIPrefix+"/reader/read?path=%2Fmissing")
	if missingConcept.Code != http.StatusNotFound || !strings.Contains(missingConcept.Body.String(), `"code":"concept_not_found"`) {
		t.Fatalf("missing concept response = %d %s", missingConcept.Code, missingConcept.Body.String())
	}

	unsupportedSource := request(handler, http.MethodGet, APIPrefix+"/reader/read?path=%2Fguides%2Fonboarding&source_uri=factile%3A%2F%2Fpublic%2Fdocs")
	if unsupportedSource.Code != http.StatusBadRequest || !strings.Contains(unsupportedSource.Body.String(), `"code":"unsupported_source"`) {
		t.Fatalf("source selector response = %d %s", unsupportedSource.Code, unsupportedSource.Body.String())
	}

	unsupportedWrite := request(handler, http.MethodPost, APIPrefix+"/writer/write")
	if unsupportedWrite.Code != http.StatusNotImplemented || !strings.Contains(unsupportedWrite.Body.String(), `"code":"unsupported_operation"`) {
		t.Fatalf("unsupported write response = %d %s", unsupportedWrite.Code, unsupportedWrite.Body.String())
	}
}

func TestHandlerCuratorModeExposesWriterRoutes(t *testing.T) {
	handler := NewHandler(fakeReader{}, Options{Curator: true})

	capabilities := request(handler, http.MethodGet, APIPrefix+"/capabilities")
	if capabilities.Code != http.StatusOK || !strings.Contains(capabilities.Body.String(), `"mode":"curator"`) {
		t.Fatalf("capabilities response = %d %s", capabilities.Code, capabilities.Body.String())
	}
	for _, want := range []string{`"create":true`, `"write":true`, `"patch":true`, `"deprecate":true`, `"rename":true`} {
		if !strings.Contains(capabilities.Body.String(), want) {
			t.Fatalf("curator capabilities missing %s: %s", want, capabilities.Body.String())
		}
	}

	create := requestWithBody(handler, http.MethodPost, APIPrefix+"/writer/create", `{"path":"/guides/new","type":"Guide","title":"New","markdown":"# New\n"}`)
	if create.Code != http.StatusOK || !strings.Contains(create.Body.String(), `"path":"/guides/new"`) {
		t.Fatalf("create response = %d %s", create.Code, create.Body.String())
	}

	write := requestWithBody(handler, http.MethodPost, APIPrefix+"/writer/write", `{"path":"/guides/onboarding","expected_revision":"fixture:onboarding","markdown":"# Updated\n"}`)
	if write.Code != http.StatusOK || !strings.Contains(write.Body.String(), `"markdown":"# Updated\n"`) {
		t.Fatalf("write response = %d %s", write.Code, write.Body.String())
	}

	update := requestWithBody(handler, http.MethodPost, APIPrefix+"/writer/update", `{"path":"/guides/onboarding","expected_revision":"fixture:onboarding","markdown":"# Updated\n"}`)
	if update.Code != http.StatusOK || !strings.Contains(update.Body.String(), `"path":"/guides/onboarding"`) {
		t.Fatalf("update alias response = %d %s", update.Code, update.Body.String())
	}

	rename := requestWithBody(handler, http.MethodPost, APIPrefix+"/writer/rename", `{"old_path":"/guides/onboarding","new_path":"/guides/renamed","expected_revision":"fixture:onboarding"}`)
	if rename.Code != http.StatusOK || !strings.Contains(rename.Body.String(), `"path":"/guides/renamed"`) {
		t.Fatalf("rename response = %d %s", rename.Code, rename.Body.String())
	}

	deprecate := requestWithBody(handler, http.MethodPost, APIPrefix+"/writer/deprecate", `{"path":"/guides/onboarding","expected_revision":"fixture:onboarding","reason":"Old"}`)
	if deprecate.Code != http.StatusOK || !strings.Contains(deprecate.Body.String(), `"markdown":"# Deprecated\n"`) {
		t.Fatalf("deprecate response = %d %s", deprecate.Code, deprecate.Body.String())
	}

	validate := requestWithBody(handler, http.MethodPost, APIPrefix+"/writer/validate", `{"path":"/guides/onboarding"}`)
	if validate.Code != http.StatusOK || !strings.Contains(validate.Body.String(), `"valid":true`) {
		t.Fatalf("writer validate response = %d %s", validate.Code, validate.Body.String())
	}
}

func TestHandlerCuratorModeWriteErrors(t *testing.T) {
	handler := NewHandler(fakeReader{}, Options{Curator: true})

	conflict := requestWithBody(handler, http.MethodPost, APIPrefix+"/writer/write", `{"path":"/guides/onboarding","expected_revision":"stale","markdown":"# Updated\n"}`)
	if conflict.Code != http.StatusConflict || !strings.Contains(conflict.Body.String(), `"code":"revision_mismatch"`) {
		t.Fatalf("conflict response = %d %s", conflict.Code, conflict.Body.String())
	}

	validation := requestWithBody(handler, http.MethodPost, APIPrefix+"/writer/patch", `{"path":"/guides/onboarding","expected_revision":"fixture:onboarding","replace_body":"invalid"}`)
	if validation.Code != http.StatusUnprocessableEntity || !strings.Contains(validation.Body.String(), `"code":"validation_failed"`) {
		t.Fatalf("validation response = %d %s", validation.Code, validation.Body.String())
	}

	unsupportedSource := requestWithBody(handler, http.MethodPost, APIPrefix+"/writer/write", `{"path":"/guides/onboarding","source_uri":"factile://public/docs","expected_revision":"fixture:onboarding","markdown":"# Updated\n"}`)
	if unsupportedSource.Code != http.StatusBadRequest || !strings.Contains(unsupportedSource.Body.String(), `"code":"unsupported_source"`) {
		t.Fatalf("source selector response = %d %s", unsupportedSource.Code, unsupportedSource.Body.String())
	}
}

func TestHandlerServesEmbeddedSPAFallback(t *testing.T) {
	handler := NewHandler(fakeReader{}, Options{})

	for _, target := range []string{
		"/guides/onboarding",
		"/guides/onboarding?view=support",
		"/guides/onboarding?view=support#related",
	} {
		response := request(handler, http.MethodGet, target)
		if response.Code != http.StatusOK {
			t.Fatalf("fallback %s status = %d body=%s", target, response.Code, response.Body.String())
		}
		if !strings.Contains(response.Body.String(), `<div id="root">`) {
			t.Fatalf("fallback %s did not serve root HTML: %s", target, response.Body.String())
		}
		if strings.Contains(response.Body.String(), "not embedded") {
			t.Fatalf("fallback %s served placeholder instead of embedded UI: %s", target, response.Body.String())
		}
	}
}

func TestHandlerDevAssetsProxyKeepsTheLocalAPI(t *testing.T) {
	previousTransport := http.DefaultTransport
	http.DefaultTransport = roundTripFunc(func(request *http.Request) (*http.Response, error) {
		return &http.Response{
			Body:       io.NopCloser(strings.NewReader("dev:" + request.URL.RequestURI())),
			Header:     make(http.Header),
			StatusCode: http.StatusOK,
		}, nil
	})
	defer func() { http.DefaultTransport = previousTransport }()

	handler := NewHandler(fakeReader{}, Options{DevAssets: "http://dev-assets.test"})
	deepRoute := request(handler, http.MethodGet, "/guides/onboarding?view=support")
	if deepRoute.Code != http.StatusOK || deepRoute.Body.String() != "dev:/guides/onboarding?view=support" {
		t.Fatalf("dev asset proxy response = %d %q", deepRoute.Code, deepRoute.Body.String())
	}

	capabilities := request(handler, http.MethodGet, APIPrefix+"/capabilities")
	if capabilities.Code != http.StatusOK || !strings.Contains(capabilities.Body.String(), `"transport":"local_http"`) {
		t.Fatalf("dev asset mode did not preserve the local API: %d %s", capabilities.Code, capabilities.Body.String())
	}
}

func request(handler http.Handler, method string, target string) *httptest.ResponseRecorder {
	return requestWithBody(handler, method, target, "")
}

func requestWithBody(handler http.Handler, method string, target string, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, target, strings.NewReader(body))
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, req)
	return recorder
}
