package cli

import (
	"context"
	"flag"
	"fmt"
	"io"
	"strings"

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
	if len(args) > 1 && args[1] == "list" {
		return runReviewList(ctx, ws, args[1:], global, stdout)
	}
	const help = "factile review <document-path> --rev <rev>\nRecord a Factile process review at the current time; retains earlier history.\nLocal actor strings do not establish authenticated human review.\n\nUse factile review list to find documents requiring attention."
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

func runReviewList(ctx context.Context, ws factile.Workspace, args []string, global globals, stdout io.Writer) (int, error) {
	const help = "factile review list [path] [--evaluated-at <datetime>]"
	if hasHelp(args) {
		return showUsage(stdout, help)
	}
	fs := flag.NewFlagSet("review list", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	at := fs.String("evaluated-at", "", "")
	ordered, err := reorderFlags(args[1:], map[string]bool{"--evaluated-at": true})
	if err != nil {
		return 2, err
	}
	if err := fs.Parse(ordered); err != nil {
		return 2, err
	}
	if fs.NArg() > 1 {
		return usage(global, stdout, help)
	}
	path := "/"
	if fs.NArg() == 1 {
		path = fs.Arg(0)
	}
	result, err := ws.ListReview(ctx, path, *at)
	if err != nil {
		return 0, err
	}
	if global.structuredOutput() {
		return writeResult(stdout, global, result)
	}
	if global.Quiet {
		return 0, nil
	}
	if len(result.Documents) == 0 {
		_, err := fmt.Fprintf(stdout, "No documents need review under %s.\n", result.Path)
		return 0, err
	}
	for _, item := range result.Documents {
		if _, err := fmt.Fprintf(stdout, "%s  %s  (source %s)\n", item.Concept.Path, strings.Join(item.Reasons, ", "), item.Origin["bundle_path"]); err != nil {
			return 0, err
		}
	}
	return 0, nil
}
