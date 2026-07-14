package factile

import (
	"context"
	"sort"
	"strings"

	"github.com/factile/factile/pkg/storage"
	"github.com/factile/factile/pkg/vfs"
)

func (w *LocalWorkspace) ListViews(ctx context.Context) (ViewListResult, error) {
	_ = ctx
	viewsPath, err := w.viewsPath()
	if err != nil {
		return ViewListResult{}, err
	}
	views, err := loadViewsAllowMissing(viewsPath)
	if err != nil {
		return ViewListResult{}, err
	}
	return ViewListResult{Views: sortedViews(views)}, nil
}

func (w *LocalWorkspace) InspectView(ctx context.Context, id string) (ViewResult, error) {
	_ = ctx
	viewID := strings.TrimSpace(id)
	if viewID == "" {
		return ViewResult{}, errorf(ErrValidationFailed, "View id is required")
	}
	viewsPath, err := w.viewsPath()
	if err != nil {
		return ViewResult{}, err
	}
	views, err := loadViewsAllowMissing(viewsPath)
	if err != nil {
		return ViewResult{}, err
	}
	view, ok := findView(views, viewID)
	if !ok {
		return ViewResult{}, viewNotFound(viewID)
	}
	return ViewResult{View: view}, nil
}

func (w *LocalWorkspace) SetView(ctx context.Context, id string, input ViewInput) (ViewResult, error) {
	_ = ctx
	viewID := strings.TrimSpace(id)
	if viewID == "" {
		return ViewResult{}, errorf(ErrValidationFailed, "View id is required")
	}
	paths, err := normalizeViewPaths(input.Paths)
	if err != nil {
		return ViewResult{}, NormalizeError(err)
	}
	view := View{
		ID:          viewID,
		Title:       input.Title,
		Description: input.Description,
		Status:      input.Status,
		Paths:       paths,
	}
	viewsPath, err := w.viewsPath()
	if err != nil {
		return ViewResult{}, err
	}
	action := "created"
	err = storage.WithFileLock(viewsPath, func() error {
		views, err := loadViewsAllowMissing(viewsPath)
		if err != nil {
			return err
		}
		for i := range views {
			if views[i].ID == view.ID {
				views[i] = view
				action = "updated"
				return writeViewsFile(viewsPath, views)
			}
		}
		views = append(views, view)
		return writeViewsFile(viewsPath, views)
	})
	if err != nil {
		return ViewResult{}, NormalizeError(err)
	}
	return ViewResult{View: view, Action: action}, nil
}

func (w *LocalWorkspace) DeleteView(ctx context.Context, id string) (ViewDeleteResult, error) {
	_ = ctx
	viewID := strings.TrimSpace(id)
	if viewID == "" {
		return ViewDeleteResult{}, errorf(ErrValidationFailed, "View id is required")
	}
	viewsPath, err := w.viewsPath()
	if err != nil {
		return ViewDeleteResult{}, err
	}
	err = storage.WithFileLock(viewsPath, func() error {
		views, err := loadViewsAllowMissing(viewsPath)
		if err != nil {
			return err
		}
		next := views[:0]
		removed := false
		for _, view := range views {
			if view.ID == viewID {
				removed = true
				continue
			}
			next = append(next, view)
		}
		if !removed {
			return viewNotFound(viewID)
		}
		return writeViewsFile(viewsPath, next)
	})
	if err != nil {
		return ViewDeleteResult{}, NormalizeError(err)
	}
	return ViewDeleteResult{ID: viewID, Deleted: true}, nil
}

func (w *LocalWorkspace) scopeForView(ctx context.Context, inputPath string, viewID string) (scopedSet, error) {
	viewID = strings.TrimSpace(viewID)
	if viewID == "" {
		return w.scope(ctx, inputPath)
	}
	normalized, selectedPaths, err := w.selectedViewPaths(inputPath, viewID)
	if err != nil {
		return scopedSet{}, err
	}
	scoped := scopedSet{Path: normalized}
	seen := map[string]bool{}
	for _, selected := range selectedPaths {
		scoped.Paths = append(scoped.Paths, selected)
		part, err := w.scope(ctx, selected)
		if err != nil {
			return scopedSet{}, err
		}
		for _, item := range part.Concepts {
			if seen[item.Concept.Path] {
				continue
			}
			seen[item.Concept.Path] = true
			scoped.Concepts = append(scoped.Concepts, item)
			scoped.Summaries = append(scoped.Summaries, item.Summary)
		}
	}
	return scoped, nil
}

func (w *LocalWorkspace) selectedViewPaths(inputPath string, viewID string) (string, []string, error) {
	if inputPath == "" {
		inputPath = "/"
	}
	normalized, err := vfs.NormalizePath(inputPath)
	if err != nil {
		return "", nil, NormalizeError(err)
	}
	viewsPath, err := w.viewsPath()
	if err != nil {
		return "", nil, err
	}
	views, err := loadViewsAllowMissing(viewsPath)
	if err != nil {
		return "", nil, err
	}
	view, ok := findView(views, viewID)
	if !ok {
		return "", nil, viewNotFound(viewID)
	}
	var selected []string
	for _, viewPath := range view.Paths {
		selection, ok, err := intersectViewPath(normalized, viewPath)
		if err != nil {
			return "", nil, NormalizeError(err)
		}
		if ok {
			selected = append(selected, selection)
		}
	}
	return normalized, selected, nil
}

func (w *LocalWorkspace) scopeWithView(ctx context.Context, inputPath string, viewID string) (scopedSet, error) {
	if strings.TrimSpace(viewID) == "" {
		return w.scope(ctx, inputPath)
	}
	return w.scopeForView(ctx, inputPath, viewID)
}

func intersectViewPath(commandPath string, viewPath string) (string, bool, error) {
	viewPath, err := vfs.NormalizePath(viewPath)
	if err != nil {
		return "", false, err
	}
	if commandPath == viewPath {
		return commandPath, true, nil
	}
	if strings.HasPrefix(commandPath, viewPath+"/") {
		return commandPath, true, nil
	}
	if strings.HasPrefix(viewPath, commandPath+"/") || commandPath == "/" {
		return viewPath, true, nil
	}
	return "", false, nil
}

func normalizeViewPaths(inputs []string) ([]string, error) {
	if len(inputs) == 0 {
		return nil, errorf(ErrValidationFailed, "View paths are required")
	}
	paths := make([]string, 0, len(inputs))
	seen := map[string]bool{}
	for _, input := range inputs {
		p, err := vfs.ValidateMountPath(strings.TrimSpace(input))
		if err != nil {
			return nil, err
		}
		if seen[p] {
			return nil, errorf(ErrValidationFailed, "Duplicate view path: %s", p)
		}
		seen[p] = true
		paths = append(paths, p)
	}
	return paths, nil
}

func sortedViews(views []View) []View {
	out := append([]View{}, views...)
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

func findView(views []View, id string) (View, bool) {
	viewID := strings.TrimSpace(id)
	for _, view := range views {
		if view.ID == viewID {
			return view, true
		}
	}
	return View{}, false
}

func viewNotFound(id string) error {
	return errorf(ErrMountNotFound, "View not found: %s", strings.TrimSpace(id))
}
