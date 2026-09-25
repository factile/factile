package factile

import (
	"context"
	"strings"
	"time"

	"github.com/factile/factile/pkg/okf"
	"github.com/factile/factile/pkg/version"
)

func evaluationTime(value string) (string, error) {
	if value == "" {
		return time.Now().UTC().Format(time.RFC3339Nano), nil
	}
	if _, valid := okf.Datetime(value); !valid {
		return "", NewError(ErrInvalidPath, "evaluated_at must be a timezone-qualified datetime")
	}
	return value, nil
}

// Review is an explicit process review, never an authenticated human assertion.
// Reuse Patch's revision fence and atomic write; retain even malformed history.
func (w *LocalWorkspace) Review(ctx context.Context, inputPath string, opts ReviewOptions) (ConceptResult, error) {
	if opts.ExpectedRevision == "" {
		return ConceptResult{}, NewError(ErrRevisionRequired, "Expected revision is required")
	}
	current, err := w.Read(ctx, inputPath, ReadOptions{})
	if err != nil {
		return ConceptResult{}, err
	}
	if current.Concept.Revision != opts.ExpectedRevision {
		return ConceptResult{}, revisionMismatch(current.Concept.Path, opts.ExpectedRevision, current.Concept.Revision)
	}
	if okf.IsReservedFile(current.Concept.ConceptID + ".md") {
		return ConceptResult{}, NewError(ErrInvalidPath, "Review requires a concept")
	}
	events := []any{}
	if raw, present := current.Concept.Frontmatter["verified"]; present {
		switch value := raw.(type) {
		case map[string]any:
			events = append(events, value)
		case []any:
			events = append(events, value...)
		default:
			return ConceptResult{}, NewError(ErrInvalidPath, "Repair malformed verified metadata before reviewing")
		}
	}
	at := time.Now().UTC().Format(time.RFC3339Nano)
	events = append(events, map[string]any{"by": "process:factile/" + version.Current().Version, "at": at, "observed_revision": opts.ExpectedRevision})
	result, err := w.Patch(ctx, inputPath, PatchConceptInput{ExpectedRevision: opts.ExpectedRevision, Set: map[string]any{"verified": events}})
	if err == nil {
		state, _ := okf.Review(result.Concept.Frontmatter, at)
		result.Concept.ReviewState = &state
	}
	return result, err
}

type ReviewSelection struct {
	EvaluatedAt string         `json:"evaluated_at"`
	Filters     map[string]any `json:"filters"`
	Excluded    []OmittedItem  `json:"excluded"`
}

func reviewSelection(opts SearchOptions) (*ReviewSelection, error) {
	if !opts.IncludeReview && opts.EvaluatedAt == "" && opts.Status == "" && opts.ReviewTier == "" && opts.Stale == nil && opts.ChangedSinceReview == nil {
		return nil, nil
	}
	at, err := evaluationTime(opts.EvaluatedAt)
	if err != nil {
		return nil, err
	}
	filters := map[string]any{}
	if opts.Status != "" {
		if opts.Status != "draft" && opts.Status != "stable" && opts.Status != "deprecated" {
			return nil, NewError(ErrInvalidPath, "status must be draft, stable or deprecated")
		}
		filters["status"] = opts.Status
	}
	if opts.ReviewTier != "" {
		if opts.ReviewTier != "unverified" && opts.ReviewTier != "machine-confirmed" && opts.ReviewTier != "human-reviewed" {
			return nil, NewError(ErrInvalidPath, "Invalid review tier")
		}
		filters["tier"] = opts.ReviewTier
	}
	if opts.Stale != nil {
		filters["stale"] = *opts.Stale
	}
	if opts.ChangedSinceReview != nil {
		filters["changed_since_review"] = *opts.ChangedSinceReview
	}
	return &ReviewSelection{EvaluatedAt: at, Filters: filters, Excluded: []OmittedItem{}}, nil
}

func reviewExclusion(state okf.ReviewState, opts SearchOptions) string {
	reasons := []string{}
	if opts.Status != "" && opts.Status != state.Status {
		reasons = append(reasons, "status")
	}
	if opts.ReviewTier != "" && opts.ReviewTier != state.Tier {
		reasons = append(reasons, "tier")
	}
	if opts.Stale != nil && (state.Stale == nil || *opts.Stale != *state.Stale) {
		reasons = append(reasons, "stale")
	}
	if opts.ChangedSinceReview != nil && (state.ChangedSinceReview == nil || *opts.ChangedSinceReview != *state.ChangedSinceReview) {
		reasons = append(reasons, "changed_since_review")
	}
	return strings.Join(reasons, ",")
}
