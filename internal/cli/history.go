package cli

import (
	"context"
	"flag"
	"fmt"
	"io"

	"github.com/factile/factile/pkg/factile"
)

func runHistory(ctx context.Context, ws factile.Workspace, args []string, global globals, stdout io.Writer) (int, error) {
	const help = "factile history <document-path> [--diff <commit>]\nShow local Git history or compare a commit with the current file."
	if hasHelp(args) {
		return showUsage(stdout, help)
	}
	fs := flag.NewFlagSet("history", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	diff := fs.String("diff", "", "")
	ordered, err := reorderFlags(args[1:], map[string]bool{"--diff": true})
	if err != nil {
		return 2, err
	}
	if err := fs.Parse(ordered); err != nil {
		return 2, err
	}
	if fs.NArg() != 1 {
		return usage(global, stdout, help)
	}
	result, err := ws.History(ctx, fs.Arg(0), *diff)
	if err != nil {
		return 0, err
	}
	if global.structuredOutput() {
		return writeResult(stdout, global, result)
	}
	if global.Quiet {
		return 0, nil
	}
	if !result.Available {
		_, err := fmt.Fprintln(stdout, result.Reason)
		return 0, err
	}
	if result.Compared != "" {
		if result.Diff == "" {
			_, err := fmt.Fprintf(stdout, "No changes since %s.\n", result.Compared)
			return 0, err
		}
		_, err := fmt.Fprint(stdout, result.Diff)
		return 0, err
	}
	for _, commit := range result.Commits {
		if _, err := fmt.Fprintf(stdout, "%s  %s  %s\n", commit.Revision, commit.Date, commit.Subject); err != nil {
			return 0, err
		}
	}
	return 0, nil
}
