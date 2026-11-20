package cli

import (
	"flag"
	"fmt"
	"io"

	"github.com/factile/factile/pkg/factile"
)

func runAdopt(ws factile.Workspace, args []string, global globals, stdout io.Writer) (int, error) {
	const help = "factile adopt <document-path>... --type <type> [--apply]\nPreview adding frontmatter to existing Markdown. Use --apply to write."
	if hasHelp(args) {
		return showUsage(stdout, help)
	}
	fs := flag.NewFlagSet("adopt", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	typ := fs.String("type", "", "")
	apply := fs.Bool("apply", false, "")
	ordered, err := reorderFlags(args[1:], map[string]bool{"--type": true, "--apply": false})
	if err != nil {
		return 2, err
	}
	if err := fs.Parse(ordered); err != nil {
		return 2, err
	}
	if fs.NArg() == 0 || *typ == "" {
		return usage(global, stdout, help)
	}
	result, err := ws.Adopt(fs.Args(), *typ, *apply)
	if err != nil {
		return 0, err
	}
	if global.structuredOutput() {
		return writeResult(stdout, global, result)
	}
	if global.Quiet {
		return 0, nil
	}
	if len(result.Files) == 0 {
		_, err := fmt.Fprintln(stdout, "No frontmatter additions needed.")
		return 0, err
	}
	action := "Proposed"
	if result.Applied {
		action = "Added"
	}
	for _, file := range result.Files {
		if _, err := fmt.Fprintf(stdout, "%s frontmatter for %s\n%s", action, file.Path, file.Frontmatter); err != nil {
			return 0, err
		}
	}
	return 0, nil
}
