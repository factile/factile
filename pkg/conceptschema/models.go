// Package conceptschema evaluates optional Concept Schema v1 profiles owned by
// one physical bundle. Callers supply base-OKF-valid concepts and retain their
// separate base validation result. Evaluation never changes documents.
package conceptschema

type Concept struct {
	ID          string
	Frontmatter map[string]any
}

type Summary struct {
	Path        string `json:"path"`
	ID          string `json:"id"`
	ConceptType string `json:"concept_type"`
}

type Issue struct {
	Severity    string            `json:"severity"`
	Code        string            `json:"code"`
	Path        string            `json:"path"`
	ConceptID   string            `json:"concept_id,omitempty"`
	SchemaID    string            `json:"schema_id,omitempty"`
	ConceptType string            `json:"concept_type,omitempty"`
	Details     map[string]string `json:"details,omitempty"`
}

type Report struct {
	Contract          string    `json:"contract,omitempty"`
	Conformant        bool      `json:"conformant"`
	Schemas           []Summary `json:"schemas"`
	EvaluatedConcepts int       `json:"evaluated_concepts"`
	Issues            []Issue   `json:"issues"`
}

// Diagnostic is adapter guidance, separate from the exact portable report.
type Diagnostic struct {
	BundlePath  string `json:"bundle_path,omitempty"`
	Path        string `json:"path"`
	SchemaID    string `json:"schema_id"`
	ConceptType string `json:"concept_type"`
	Field       string `json:"field"`
	Keyword     string `json:"keyword"`
	Message     string `json:"message"`
}

// ResourceError aborts evaluation instead of returning a conformance verdict.
type ResourceError struct{ Limit string }

func (e *ResourceError) Error() string { return "concept schema resource limit exceeded: " + e.Limit }
