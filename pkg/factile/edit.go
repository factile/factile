package factile

import (
	"fmt"
	"sort"

	"github.com/factile/factile/pkg/okf"
	"github.com/factile/factile/pkg/patch"
)

func applyPatch(id string, data []byte, input PatchConceptInput) ([]byte, []string, error) {
	summary := []string{}
	next, err := okf.PatchFrontmatter(id, data, input.Set, input.DeleteKeys)
	if err != nil {
		return nil, nil, err
	}
	if len(input.Set) > 0 {
		summary = append(summary, "set")
	}
	if len(input.DeleteKeys) > 0 {
		summary = append(summary, "delete_key")
	}
	// Legacy maps have deterministic order. Ordered operations run afterwards.
	operations := []PatchOperation{}
	addSections := func(op string, values map[string]string) {
		keys := make([]string, 0, len(values))
		for key := range values {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			operations = append(operations, PatchOperation{Op: op, Heading: key, Markdown: values[key]})
		}
	}
	addSections("replace_section", input.ReplaceSections)
	addSections("append_section", input.AppendSections)
	if input.ReplaceBody != nil {
		operations = append(operations, PatchOperation{Op: "replace_body", Markdown: *input.ReplaceBody})
	}
	operations = append(operations, input.Operations...)
	for i, op := range operations {
		if err := validateOperation(op); err != nil {
			return nil, nil, errorf("invalid_patch", "Operation %d: %s", i+1, err)
		}
		if op.Op == "set" {
			next, err = okf.PatchFrontmatter(id, next, map[string]any{op.Key: op.Value}, nil)
		} else if op.Op == "delete_key" {
			next, err = okf.PatchFrontmatter(id, next, nil, []string{op.Key})
		} else {
			var doc okf.Document
			doc, err = okf.ParseConcept(id, next)
			if err == nil {
				var body string
				switch op.Op {
				case "replace_text":
					body, err = patch.ReplaceText(doc.Markdown, op.Old, op.New)
				case "replace_section":
					body, err = patch.ReplaceSection(doc.Markdown, op.Heading, op.Markdown)
				case "append_section":
					body, err = patch.AppendSection(doc.Markdown, op.Heading, op.Markdown)
				case "replace_body":
					body = op.Markdown
				}
				if err == nil {
					next, err = okf.ReplaceMarkdown(id, next, body)
				}
			}
		}
		if err != nil {
			return nil, nil, err
		}
		summary = append(summary, op.Op)
	}
	return next, summary, nil
}

func validateOperation(op PatchOperation) error {
	text, section, body, metadata := false, false, false, false
	switch op.Op {
	case "replace_text":
		text = true
	case "replace_section", "append_section":
		section = true
	case "replace_body":
		body = true
	case "set", "delete_key":
		metadata = true
	default:
		return fmt.Errorf("unknown op %q", op.Op)
	}
	if !text && (op.Old != "" || op.New != "") || !section && op.Heading != "" || !section && !body && op.Markdown != "" || !metadata && op.Key != "" || op.Op != "set" && op.Value != nil {
		return fmt.Errorf("fields do not match %s", op.Op)
	}
	if text && op.Old == "" {
		return fmt.Errorf("old text must not be empty")
	}
	if section && op.Heading == "" {
		return fmt.Errorf("heading is required")
	}
	if metadata && op.Key == "" {
		return fmt.Errorf("key is required")
	}
	return nil
}
