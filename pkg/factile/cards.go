package factile

import (
	"context"
	"errors"
	"os"
	"path/filepath"

	"github.com/factile/factile/pkg/catalog"
	"github.com/factile/factile/pkg/vfs"
)

func (w *LocalWorkspace) Stat(ctx context.Context, inputPath string, opts StatOptions) (StatResult, error) {
	_, _ = ctx, opts
	if inputPath == "" {
		inputPath = "/"
	}
	normalized, err := vfs.NormalizePath(inputPath)
	if err != nil {
		return StatResult{}, NormalizeError(err)
	}
	card, err := w.cardForPath(normalized)
	if err != nil {
		return StatResult{}, err
	}
	return StatResult{Card: card}, nil
}

func (w *LocalWorkspace) listResult(path string, folders []FolderSummary, documents []DocumentSummary, opts ListOptions) (ListResult, error) {
	if opts.Brief {
		cards, err := w.cardsForList(folders, documents)
		if err != nil {
			return ListResult{}, err
		}
		return ListResult{Path: path, Cards: cards}, nil
	}
	return ListResult{Path: path, Folders: folders, Documents: documents}, nil
}

func (w *LocalWorkspace) cardsForList(folders []FolderSummary, documents []DocumentSummary) ([]CardSummary, error) {
	cards := make([]CardSummary, 0, len(folders)+len(documents))
	for _, folder := range folders {
		card, err := w.cardForPath(folder.Path)
		if err != nil {
			return nil, err
		}
		cards = append(cards, card)
	}
	for _, document := range documents {
		card, err := w.cardForPath(document.Path)
		if err != nil {
			return nil, err
		}
		cards = append(cards, card)
	}
	return cards, nil
}

func (w *LocalWorkspace) cardForPath(normalized string) (CardSummary, error) {
	if normalized == "/" {
		return w.rootCard()
	}
	card := CardSummary{Path: normalized, Title: titleFromPath(normalized)}
	if catalogCard, ok, err := w.catalogCard(normalized); err != nil {
		return CardSummary{}, err
	} else if ok {
		card = mergeCard(card, catalogCard)
	}
	mounts, err := w.mounts()
	if err != nil {
		return CardSummary{}, NormalizeError(err)
	}
	target, err := vfs.Resolve(mounts, normalized)
	if err != nil {
		if len(mountsForVirtualPath(mounts, normalized)) > 0 {
			return card, nil
		}
		return CardSummary{}, NormalizeError(err)
	}
	if target.Kind == TargetConcept {
		concept, err := w.readConcept(target.Mount, target.ConceptID)
		if err != nil {
			return CardSummary{}, err
		}
		summary := summaryFromConcept(concept)
		card = CardSummary{
			Path:        summary.Path,
			Title:       summary.Title,
			Description: summary.Description,
			Tags:        summary.Tags,
			Revision:    summary.Revision,
			Writable:    writablePtr(w.effectiveWritable(target.Mount)),
		}
		return card, nil
	}
	if target.Kind == TargetPath && !target.Exists {
		return CardSummary{}, errorf(ErrMountNotFound, "Path not found: %s", target.Path)
	}
	card.Path = target.Path
	card.Writable = writablePtr(w.effectiveWritable(target.Mount))
	return card, nil
}

func (w *LocalWorkspace) rootCard() (CardSummary, error) {
	paths, err := w.catalogPaths()
	if err != nil {
		return CardSummary{}, err
	}
	library, err := loadLibraryAllowMissing(paths.libraryPath)
	if err != nil {
		return CardSummary{}, catalogLoadError(err)
	}
	title := library.Title
	if title == "" {
		title = "Library"
	}
	return CardSummary{Path: "/", Title: title, Description: library.Description}, nil
}

func (w *LocalWorkspace) catalogCard(normalized string) (CardSummary, bool, error) {
	paths, err := w.catalogPaths()
	if err != nil {
		return CardSummary{}, false, err
	}
	library, err := catalog.LoadLibraryFile(paths.libraryPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return CardSummary{}, false, nil
		}
		return CardSummary{}, false, catalogLoadError(err)
	}
	var candidate CardSummary
	found := false
	for _, ref := range library.KnowledgeBases {
		if sameFactilePath(ref.Path, normalized) {
			candidate = CardSummary{
				Path:        normalized,
				Title:       ref.Title,
				Description: ref.Description,
			}
			found = true
		}
		filename := ref.Catalog
		if !filepath.IsAbs(filename) {
			filename = filepath.Join(filepath.Dir(paths.libraryPath), filename)
		}
		kb, err := catalog.LoadKnowledgeBaseFile(filename)
		if err != nil {
			return CardSummary{}, false, catalogLoadError(err)
		}
		for _, link := range kb.Bundles {
			if sameFactilePath(link.Path, normalized) {
				return CardSummary{
					Path:        normalized,
					Title:       link.Title,
					Description: link.Description,
					WhenToUse:   link.WhenToUse,
					Writable:    writablePtr(link.Writable && !w.opts.ReadOnly),
				}, true, nil
			}
		}
	}
	return candidate, found, nil
}

func mergeCard(base CardSummary, overlay CardSummary) CardSummary {
	if overlay.Path != "" {
		base.Path = overlay.Path
	}
	if overlay.Title != "" {
		base.Title = overlay.Title
	}
	if overlay.Description != "" {
		base.Description = overlay.Description
	}
	if len(overlay.Tags) > 0 {
		base.Tags = overlay.Tags
	}
	if overlay.WhenToUse != "" {
		base.WhenToUse = overlay.WhenToUse
	}
	if overlay.Writable != nil {
		base.Writable = overlay.Writable
	}
	if overlay.Revision != "" {
		base.Revision = overlay.Revision
	}
	return base
}

func writablePtr(value bool) *bool {
	return &value
}

func (w *LocalWorkspace) effectiveWritable(mount vfs.Mount) bool {
	return mount.Writable && !w.opts.ReadOnly
}
