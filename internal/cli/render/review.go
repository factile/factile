package render

import (
	"fmt"
	"io"
	"strings"

	"github.com/factile/factile/pkg/okf"
)

func renderReview(w io.Writer, state *okf.ReviewState) error {
	if state == nil {
		return nil
	}
	flags := []string{state.Status, state.Tier}
	if state.ChangedSinceReview != nil && *state.ChangedSinceReview {
		flags = append(flags, "changed since review")
	}
	if state.Stale != nil && *state.Stale {
		flags = append(flags, "stale")
	}
	_, err := fmt.Fprintf(w, "Review: %s (evaluated %s; authored review claims)\n", strings.Join(flags, "; "), state.EvaluatedAt)
	return err
}
