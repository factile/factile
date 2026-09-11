package cli

import (
	"fmt"
	"io"

	"github.com/factile/factile/pkg/skill"
)

func writeSkillWarnings(stderr io.Writer, global globals, args []string) {
	if global.structuredOutput() || global.Quiet || hasHelp(args) {
		return
	}
	if len(args) > 0 {
		switch args[0] {
		case "init", "skill", "version", "mcp", "help":
			return
		}
	}
	for _, warning := range skill.GuidanceWarnings(global.Workspace) {
		fmt.Fprintln(stderr, "Warning: "+warning)
	}
}
