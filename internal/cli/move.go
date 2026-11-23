package cli

import (
	"fmt"
	"io"

	"github.com/factile/factile/pkg/factile"
)

func runMoveWithLinks(ws factile.Workspace, oldPath, newPath, plan string, keepOld bool, global globals, stdout io.Writer) (int, error) {
	result, err := ws.MoveWithLinks(oldPath, newPath, plan, keepOld)
	if err != nil {
		return 0, err
	}
	if global.structuredOutput() {
		return writeResult(stdout, global, result)
	}
	if global.Quiet {
		return 0, nil
	}
	label := "Proposed move"
	if result.Applied {
		label = "Moved"
	}
	if _, err := fmt.Fprintf(stdout, "%s %s → %s\n", label, result.OldPath, result.NewPath); err != nil {
		return 0, err
	}
	for _, change := range result.Changes {
		if _, err := fmt.Fprint(stdout, change.Diff); err != nil {
			return 0, err
		}
	}
	if !result.Applied {
		_, err := fmt.Fprintf(stdout, "Apply with --apply-plan %s\n", result.Plan)
		return 0, err
	}
	return 0, nil
}
