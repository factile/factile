package factile

import (
	"errors"
	"os"
	"path/filepath"
	"strings"

	"github.com/factile/factile/pkg/catalog"
	"github.com/factile/factile/pkg/vfs"
)

func (w *LocalWorkspace) mountsForView(inputPath string, view string) ([]vfs.Mount, error) {
	view = strings.TrimSpace(view)
	mounts, err := w.mounts()
	if err != nil || view == "" {
		return mounts, err
	}
	if w.opts.MountFile != "" {
		return nil, NewError(ErrValidationFailed, "View selection is not available with an explicit mount file")
	}
	normalized, err := vfs.NormalizePath(inputPath)
	if err != nil {
		return nil, err
	}
	_, kb, err := w.knowledgeBaseForPath(normalized)
	if err != nil {
		return nil, err
	}
	bundleIDs, err := selectedViewBundleIDs(kb, view)
	if err != nil {
		return nil, err
	}
	bundlePathByID := map[string]string{}
	for _, bundle := range kb.Bundles {
		bundlePathByID[bundle.ID] = bundle.Path
	}
	mountByPath := map[string]vfs.Mount{}
	for _, mount := range mounts {
		mountByPath[mount.MountPath] = mount
	}
	selected := make([]vfs.Mount, 0, len(bundleIDs))
	for _, bundleID := range bundleIDs {
		bundlePath, ok := bundlePathByID[bundleID]
		if !ok {
			return nil, errorf(ErrValidationFailed, "View references unknown bundle: %s in %s", bundleID, view)
		}
		mount, ok := mountByPath[bundlePath]
		if !ok {
			return nil, errorf(ErrMountNotFound, "Bundle mount not found for View %s: %s", view, bundlePath)
		}
		selected = append(selected, mount)
	}
	return selected, nil
}

func (w *LocalWorkspace) knowledgeBaseForPath(normalized string) (catalog.KnowledgeBaseRef, catalog.KnowledgeBase, error) {
	paths, err := w.catalogPaths()
	if err != nil {
		return catalog.KnowledgeBaseRef{}, catalog.KnowledgeBase{}, err
	}
	library, err := catalog.LoadLibraryFile(paths.libraryPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return catalog.KnowledgeBaseRef{}, catalog.KnowledgeBase{}, errorf(ErrValidationFailed, "View selection requires a Knowledge Base path: %s", normalized)
		}
		return catalog.KnowledgeBaseRef{}, catalog.KnowledgeBase{}, catalogLoadError(err)
	}
	var selected *catalog.KnowledgeBaseRef
	for i := range library.KnowledgeBases {
		ref := &library.KnowledgeBases[i]
		if normalized == ref.Path || strings.HasPrefix(normalized, ref.Path+"/") {
			if selected == nil || len(ref.Path) > len(selected.Path) {
				selected = ref
			}
		}
	}
	if selected == nil {
		return catalog.KnowledgeBaseRef{}, catalog.KnowledgeBase{}, errorf(ErrValidationFailed, "View selection requires a Knowledge Base path: %s", normalized)
	}
	filename := selected.Catalog
	if !filepath.IsAbs(filename) {
		filename = filepath.Join(filepath.Dir(paths.libraryPath), filename)
	}
	kb, err := catalog.LoadKnowledgeBaseFile(filename)
	if err != nil {
		return catalog.KnowledgeBaseRef{}, catalog.KnowledgeBase{}, catalogLoadError(err)
	}
	return *selected, kb, nil
}

func selectedViewBundleIDs(kb catalog.KnowledgeBase, viewID string) ([]string, error) {
	for _, view := range kb.Views {
		if view.ID == viewID {
			return append([]string(nil), view.Bundles...), nil
		}
	}
	if viewID == "default" {
		bundleIDs := make([]string, 0, len(kb.Bundles))
		for _, bundle := range kb.Bundles {
			bundleIDs = append(bundleIDs, bundle.ID)
		}
		return bundleIDs, nil
	}
	return nil, errorf(ErrValidationFailed, "View not found in Knowledge Base %s: %s", kb.Path, viewID)
}
