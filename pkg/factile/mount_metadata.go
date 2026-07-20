package factile

import (
	"path/filepath"
	"strings"

	"github.com/factile/factile/pkg/okf"
	"github.com/factile/factile/pkg/storage"
	"github.com/factile/factile/pkg/vfs"
)

func applyMountMetadataDefaults(sourcePath string, mountPath string, opts MountOptions) MountOptions {
	if hasText(opts.Title) && hasText(opts.Description) {
		return opts
	}

	if manifest, err := vfs.LoadManifest(sourcePath); err == nil && manifest.Bundle != nil {
		setIfBlank(&opts.Title, manifest.Bundle.Title)
		setIfBlank(&opts.Description, manifest.Bundle.Description)
	}

	if !hasText(opts.Title) || !hasText(opts.Description) {
		title, description := overviewMetadata(sourcePath)
		setIfBlank(&opts.Title, title)
		setIfBlank(&opts.Description, description)
	}

	if !hasText(opts.Title) {
		opts.Title = titleFromPath(mountPath)
	}
	return opts
}

func overviewMetadata(sourcePath string) (string, string) {
	store, err := storage.NewLocal(sourcePath)
	if err != nil {
		return "", ""
	}
	data, _, err := store.ReadConcept("overview")
	if err != nil {
		return "", ""
	}
	document, err := okf.ParseConcept("overview", data)
	if err != nil {
		return "", ""
	}
	return okf.StringField(document.Frontmatter, "title"), okf.StringField(document.Frontmatter, "description")
}

func resolveMountSourcePath(source string, baseDir string) (string, error) {
	candidate := source
	if !filepath.IsAbs(candidate) {
		candidate = filepath.Join(baseDir, candidate)
	}
	abs, err := filepath.Abs(candidate)
	if err != nil {
		return "", err
	}
	return filepath.Clean(abs), nil
}

func setIfBlank(target *string, value string) {
	if !hasText(*target) && hasText(value) {
		*target = value
	}
}

func hasText(value string) bool {
	return strings.TrimSpace(value) != ""
}
