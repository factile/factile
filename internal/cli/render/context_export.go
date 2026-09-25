package render

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/factile/factile/pkg/factile"
)

func fencedContext(w io.Writer, text, language string) error {
	longest, run := 2, 0
	for _, character := range text {
		if character == '`' {
			run++
			if run > longest {
				longest = run
			}
		} else {
			run = 0
		}
	}
	fence := strings.Repeat("`", longest+1)
	_, err := fmt.Fprintf(w, "%s%s\n%s\n%s\n", fence, language, text, fence)
	return err
}

func renderContextEvidence(w io.Writer, concept factile.Concept, index int) error {
	data, err := json.Marshal(concept)
	if err != nil {
		return err
	}
	var evidence map[string]json.RawMessage
	if err := json.Unmarshal(data, &evidence); err != nil {
		return err
	}
	delete(evidence, "markdown")
	metadata, err := json.MarshalIndent(evidence, "", "  ")
	if err != nil {
		return err
	}
	if _, err := fmt.Fprintf(w, "\n## Document %d: %s\n\nEvidence and identity for this document:\n", index+1, concept.Path); err != nil {
		return err
	}
	if err := fencedContext(w, string(metadata), "json"); err != nil {
		return err
	}
	if _, err := fmt.Fprintln(w, "\nThe following Markdown is a separate document; its footnotes refer only to its own sources."); err != nil {
		return err
	}
	return fencedContext(w, concept.Markdown, "markdown")
}
