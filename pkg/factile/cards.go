package factile

import (
	"context"

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
	card, err := w.cardForPath(ctx, normalized)
	if err != nil {
		return StatResult{}, err
	}
	return StatResult{Card: card}, nil
}

func (w *LocalWorkspace) listResult(ctx context.Context, path string, folders []FolderSummary, documents []DocumentSummary, opts ListOptions) (ListResult, error) {
	if opts.Brief {
		cards, err := w.cardsForList(ctx, folders, documents)
		if err != nil {
			return ListResult{}, err
		}
		return ListResult{Path: path, Cards: cards}, nil
	}
	return ListResult{Path: path, Folders: folders, Documents: documents}, nil
}

func (w *LocalWorkspace) cardsForList(ctx context.Context, folders []FolderSummary, documents []DocumentSummary) ([]CardSummary, error) {
	cards := make([]CardSummary, 0, len(folders)+len(documents))
	for _, folder := range folders {
		card, err := w.cardForPath(ctx, folder.Path)
		if err != nil {
			return nil, err
		}
		cards = append(cards, card)
	}
	for _, document := range documents {
		card, err := w.cardForPath(ctx, document.Path)
		if err != nil {
			return nil, err
		}
		cards = append(cards, card)
	}
	return cards, nil
}

func (w *LocalWorkspace) cardForPath(ctx context.Context, normalized string) (CardSummary, error) {
	if normalized == "/" {
		return w.rootCard()
	}
	card := CardSummary{Path: normalized, Title: titleFromPath(normalized)}
	mounts, err := w.mounts()
	if err != nil {
		return CardSummary{}, NormalizeError(err)
	}
	if mount, ok := mountByPath(mounts, normalized); ok {
		return mergeCard(card, cardFromMount(mount)), nil
	}
	mounts, err = w.mountsForTarget(ctx, normalized)
	if err != nil {
		return CardSummary{}, err
	}
	target, err := vfs.Resolve(mounts, normalized)
	if err != nil {
		if len(mountsForVirtualPath(mounts, normalized)) > 0 {
			return card, nil
		}
		return CardSummary{}, NormalizeError(err)
	}
	if target.Kind == TargetConcept {
		if err := ensureReadable(target.Mount); err != nil {
			return CardSummary{}, err
		}
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
	workspace, err := w.resolvedWorkspace()
	if err != nil {
		return CardSummary{}, err
	}
	manifest, err := vfs.LoadManifest(workspace.RootBundleDir)
	if err != nil {
		return CardSummary{}, NormalizeError(err)
	}
	metadata := manifest.Bundle
	if metadata == nil {
		return CardSummary{}, NormalizeError(&vfs.Error{Code: vfs.ErrInvalidBundle, Message: "Workspace root bundle has no valid factile.toml."})
	}
	title := metadata.Title
	if title == "" {
		title = "Factile Root Bundle"
	}
	return CardSummary{Path: "/", Title: title, Description: metadata.Description, WhenToUse: metadata.WhenToUse}, nil
}

func cardFromMount(mount vfs.Mount) CardSummary {
	return CardSummary{
		Path:        mount.MountPath,
		Title:       mount.Title,
		Description: mount.Description,
		WhenToUse:   mount.WhenToUse,
		Writable:    writablePtr(mount.Writable),
	}
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
