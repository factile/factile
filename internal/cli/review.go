package cli

import (
	"context"
	"flag"
	"fmt"
	"io"

	"github.com/factile/factile/pkg/factile"
)

func optionalReviewBool(value string) (*bool, error) {
	if value == "" {
		return nil, nil
	}
	if value != "true" && value != "false" {
		return nil, fmt.Errorf("review filters require true or false")
	}
	result := value == "true"
	return &result, nil
}

func runReview(ctx context.Context, ws factile.Workspace, args []string, global globals, stdout io.Writer) (int, error) {
	const help = "factile review <document-path> --rev <rev>\nRecord a Factile process review at the current time; retains earlier history.\nLocal actor strings do not establish authenticated human review."
	if hasHelp(args) {
		return showUsage(stdout, help)
	}
	fs := flag.NewFlagSet("review", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	rev := fs.String("rev", "", "")
	ordered, err := reorderFlags(args[1:], map[string]bool{"--rev": true})
	if err != nil {
		return 2, err
	}
	if err = fs.Parse(ordered); err != nil {
		return 2, err
	}
	if fs.NArg() != 1 {
		return usage(global, stdout, help)
	}
	result, err := ws.Review(ctx, fs.Arg(0), factile.ReviewOptions{ExpectedRevision: *rev})
	if err != nil {
		return 0, err
	}
	return writeConceptConfirmation(stdout, global, "Reviewed", result)
}
