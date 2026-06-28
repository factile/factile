package factile

import (
	"context"

	"github.com/factile/factile/pkg/catalog"
	"github.com/factile/factile/pkg/vfs"
)

type TargetKind = vfs.TargetKind

const (
	TargetVirtualRoot = vfs.TargetVirtualRoot
	TargetBundle      = vfs.TargetBundle
	TargetPath        = vfs.TargetPath
	TargetConcept     = vfs.TargetConcept
)

type ValidationIssue struct {
	Severity  string         `json:"severity"`
	Code      string         `json:"code"`
	Message   string         `json:"message"`
	Path      string         `json:"path,omitempty"`
	ConceptID string         `json:"concept_id,omitempty"`
	Details   map[string]any `json:"details,omitempty"`
}

type ConceptSummary struct {
	Path        string   `json:"path"`
	ConceptID   string   `json:"concept_id"`
	Type        string   `json:"type"`
	Title       string   `json:"title,omitempty"`
	Description string   `json:"description,omitempty"`
	Tags        []string `json:"tags,omitempty"`
	Resource    string   `json:"resource,omitempty"`
	Revision    string   `json:"revision,omitempty"`
}

type Concept struct {
	Path        string         `json:"path"`
	ConceptID   string         `json:"concept_id"`
	Revision    string         `json:"revision"`
	Frontmatter map[string]any `json:"frontmatter"`
	Markdown    string         `json:"markdown"`
}

type Mount = vfs.Mount
type KnowledgeBase = catalog.KnowledgeBase
type KnowledgeBaseRef = catalog.KnowledgeBaseRef
type BundleLink = catalog.BundleLink
type LibraryView = catalog.LibraryView

type SearchResult struct {
	Concept ConceptSummary `json:"concept"`
	Score   float64        `json:"score"`
	Snippet string         `json:"snippet,omitempty"`
}

type OmittedItem struct {
	Path   string `json:"path,omitempty"`
	Reason string `json:"reason"`
}

type FolderSummary struct {
	Path        string `json:"path"`
	Title       string `json:"title,omitempty"`
	Description string `json:"description,omitempty"`
}

type DocumentSummary struct {
	Path        string   `json:"path"`
	Type        string   `json:"type,omitempty"`
	Title       string   `json:"title,omitempty"`
	Description string   `json:"description,omitempty"`
	Tags        []string `json:"tags,omitempty"`
	Resource    string   `json:"resource,omitempty"`
	Revision    string   `json:"revision,omitempty"`
}

type CardSummary struct {
	Path        string   `json:"path"`
	Title       string   `json:"title,omitempty"`
	Description string   `json:"description,omitempty"`
	Tags        []string `json:"tags,omitempty"`
	WhenToUse   string   `json:"when_to_use,omitempty"`
	Writable    *bool    `json:"writable,omitempty"`
	Revision    string   `json:"revision,omitempty"`
}

type ListResult struct {
	Path      string            `json:"path"`
	Folders   []FolderSummary   `json:"folders,omitempty"`
	Documents []DocumentSummary `json:"documents,omitempty"`
	Cards     []CardSummary     `json:"cards,omitempty"`

	// Legacy fields remain during the reader-navigation migration.
	Mounts   []Mount          `json:"mounts,omitempty"`
	Concepts []ConceptSummary `json:"concepts,omitempty"`
	Paths    []string         `json:"paths,omitempty"`
}

type ConceptResult struct {
	Concept Concept `json:"concept"`
}

type SearchResults struct {
	Path    string         `json:"path"`
	Query   string         `json:"query"`
	Results []SearchResult `json:"results"`
}

type ContextPack struct {
	Path      string           `json:"path"`
	Query     string           `json:"query"`
	Concepts  []Concept        `json:"concepts"`
	Summaries []ConceptSummary `json:"summaries,omitempty"`
	Omitted   []OmittedItem    `json:"omitted,omitempty"`
}

type GraphNode struct {
	Concept ConceptSummary `json:"concept"`
}

type GraphEdge struct {
	From string `json:"from"`
	To   string `json:"to"`
	Kind string `json:"kind"`
}

type GraphResult struct {
	Path    string            `json:"path"`
	Nodes   []GraphNode       `json:"nodes"`
	Edges   []GraphEdge       `json:"edges"`
	Omitted []OmittedItem     `json:"omitted,omitempty"`
	Issues  []ValidationIssue `json:"issues,omitempty"`
}

type ValidationResult struct {
	Path   string            `json:"path"`
	Valid  bool              `json:"valid"`
	Issues []ValidationIssue `json:"issues"`
}

type RenameResult struct {
	Concept  Concept           `json:"concept"`
	Warnings []ValidationIssue `json:"warnings,omitempty"`
}

type DeleteResult struct {
	Path    string `json:"path"`
	Deleted bool   `json:"deleted"`
}

type MountResult struct {
	Mount Mount `json:"mount"`
}

type UnmountResult struct {
	MountPath string `json:"mount_path"`
	Removed   bool   `json:"removed"`
}

type MountListResult struct {
	Mounts []Mount `json:"mounts"`
}

type SummaryResult struct {
	Workspace    WorkspaceSummary `json:"workspace"`
	Knowledge    []CardSummary    `json:"knowledge"`
	Views        []LibraryView    `json:"views"`
	Sources      []Mount          `json:"sources"`
	Health       []HealthSummary  `json:"health"`
	NextCommands []string         `json:"next_commands"`
}

type WorkspaceSummary struct {
	Path    string `json:"path"`
	Version string `json:"version"`
}

type HealthSummary struct {
	Status  string `json:"status"`
	Message string `json:"message"`
}

type StatResult struct {
	Card CardSummary `json:"card"`
}

type KnowledgeBaseSummary struct {
	ID          string `json:"id"`
	Path        string `json:"path"`
	Catalog     string `json:"catalog"`
	Title       string `json:"title,omitempty"`
	Description string `json:"description,omitempty"`
	Status      string `json:"status,omitempty"`
}

type KnowledgeBaseListResult struct {
	KnowledgeBases []KnowledgeBaseSummary `json:"knowledge_bases"`
}

type KnowledgeBaseResult struct {
	KnowledgeBase KnowledgeBase `json:"knowledge_base"`
	Catalog       string        `json:"catalog"`
	Action        string        `json:"action,omitempty"`
}

type BundleLinkResult struct {
	KnowledgeBase KnowledgeBaseSummary `json:"knowledge_base"`
	Bundle        BundleLink           `json:"bundle"`
	Action        string               `json:"action,omitempty"`
}

type BundleUnlinkResult struct {
	KnowledgeBase KnowledgeBaseSummary `json:"knowledge_base"`
	BundlePath    string               `json:"bundle_path"`
	Removed       bool                 `json:"removed"`
}

type ViewListResult struct {
	Views []LibraryView `json:"views"`
}

type ViewResult struct {
	View   LibraryView `json:"view"`
	Action string      `json:"action,omitempty"`
}

type ViewDeleteResult struct {
	ID      string `json:"id"`
	Deleted bool   `json:"deleted"`
}

type BundleInspectResult struct {
	Source       string            `json:"source"`
	Kind         string            `json:"kind"`
	PlausibleOKF bool              `json:"plausible_okf"`
	Concepts     []ConceptSummary  `json:"concepts,omitempty"`
	Issues       []ValidationIssue `json:"issues,omitempty"`
}

type BundleFindResult struct {
	StartPath string   `json:"start_path"`
	Sources   []string `json:"sources"`
}

type ListOptions struct {
	Brief bool
	View  string
}
type ReadOptions struct{}
type SearchOptions struct {
	View string
}
type GraphOptions struct {
	Depth int
	View  string
}
type ValidateOptions struct {
	View string
}
type StatOptions struct{}
type MountOptions struct {
	Writable bool
	Kind     string
}
type KnowledgeBaseCreateInput struct {
	Title       string
	Description string
}
type BundleLinkInput struct {
	Title       string
	Description string
	Writable    bool
	Kind        string
}
type ViewInput struct {
	Title       string
	Description string
	Status      string
	Paths       []string
}
type UnmountOptions struct{}
type RenameOptions struct {
	ExpectedRevision string
}
type DeleteOptions struct {
	ExpectedRevision string
}
type DeprecateOptions struct {
	ExpectedRevision string
	Reason           string
}

type ContextOptions struct {
	MaxTokens int
	Depth     int
	View      string
}

type CreateConceptInput struct {
	Type        string
	Title       string
	Description string
	Tags        []string
	Resource    string
	Markdown    string
}

type WriteConceptInput struct {
	ExpectedRevision string
	Markdown         string
}

type PatchConceptInput struct {
	ExpectedRevision string
	Set              map[string]any
	DeleteKeys       []string
	ReplaceSections  map[string]string
	AppendSections   map[string]string
	ReplaceBody      *string
}

type Workspace interface {
	List(ctx context.Context, path string, opts ListOptions) (ListResult, error)
	Stat(ctx context.Context, path string, opts StatOptions) (StatResult, error)
	Read(ctx context.Context, path string, opts ReadOptions) (ConceptResult, error)
	Search(ctx context.Context, path string, query string, opts SearchOptions) (SearchResults, error)
	Context(ctx context.Context, path string, query string, opts ContextOptions) (ContextPack, error)
	Graph(ctx context.Context, path string, opts GraphOptions) (GraphResult, error)
	Validate(ctx context.Context, path string, opts ValidateOptions) (ValidationResult, error)
	Summary(ctx context.Context) (SummaryResult, error)
	Create(ctx context.Context, path string, input CreateConceptInput) (ConceptResult, error)
	Write(ctx context.Context, path string, input WriteConceptInput) (ConceptResult, error)
	Patch(ctx context.Context, path string, input PatchConceptInput) (ConceptResult, error)
	Rename(ctx context.Context, oldPath string, newPath string, opts RenameOptions) (RenameResult, error)
	Delete(ctx context.Context, path string, opts DeleteOptions) (DeleteResult, error)
	Deprecate(ctx context.Context, path string, opts DeprecateOptions) (ConceptResult, error)
	Mount(ctx context.Context, source string, mountPath string, opts MountOptions) (MountResult, error)
	Unmount(ctx context.Context, mountPath string, opts UnmountOptions) (UnmountResult, error)
	ListMounts(ctx context.Context) (MountListResult, error)
	ListKnowledgeBases(ctx context.Context) (KnowledgeBaseListResult, error)
	InspectKnowledgeBase(ctx context.Context, path string) (KnowledgeBaseResult, error)
	CreateKnowledgeBase(ctx context.Context, path string, input KnowledgeBaseCreateInput) (KnowledgeBaseResult, error)
	LinkBundle(ctx context.Context, knowledgeBasePath string, source string, bundlePath string, input BundleLinkInput) (BundleLinkResult, error)
	UnlinkBundle(ctx context.Context, bundlePath string) (BundleUnlinkResult, error)
	ListViews(ctx context.Context) (ViewListResult, error)
	InspectView(ctx context.Context, id string) (ViewResult, error)
	SetView(ctx context.Context, id string, input ViewInput) (ViewResult, error)
	DeleteView(ctx context.Context, id string) (ViewDeleteResult, error)
	InspectBundle(ctx context.Context, source string) (BundleInspectResult, error)
	FindBundles(ctx context.Context, startPath string) (BundleFindResult, error)
}
