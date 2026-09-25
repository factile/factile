package factile

import (
	"reflect"
	"time"

	"github.com/factile/factile/pkg/okf"
	"github.com/factile/factile/pkg/version"
)

// GeneratedMetadata identifies the tool performing the current content change.
// It never asserts a human author or a review.
func GeneratedMetadata(at time.Time) map[string]any {
	return map[string]any{"by": "factile/" + version.Current().Version, "at": at.UTC().Format(time.RFC3339Nano)}
}

func meaningfulContentChanged(before, after okf.Document) bool {
	content := func(fields map[string]any) map[string]any {
		result := make(map[string]any, len(fields))
		for key, value := range fields {
			switch key {
			case "generated", "verified", "status", "stale_after":
				continue
			}
			result[key] = value
		}
		return result
	}
	return before.Markdown != after.Markdown || !reflect.DeepEqual(content(before.Frontmatter), content(after.Frontmatter))
}
