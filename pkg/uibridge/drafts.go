package uibridge

import (
	"net/http"

	"github.com/factile/factile/pkg/factile"
)

type draftWorkspace interface {
	LoadCuratorDraft(path, mode string) (*factile.CuratorDraft, error)
	StoreCuratorDraft(path, mode string, draft factile.CuratorDraft, discard bool) error
}

func draftHandler(ws draftWorkspace) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		query := r.URL.Query()
		path, mode := query.Get("path"), query.Get("mode")
		if path == "" {
			writeError(w, http.StatusBadRequest, factile.NewError(factile.ErrInvalidPath, "path is required"))
			return
		}
		switch r.Method {
		case http.MethodGet:
			draft, err := ws.LoadCuratorDraft(path, mode)
			if err != nil {
				writeError(w, errorStatus(err), err)
				return
			}
			writeJSON(w, draft)
		case http.MethodPut, http.MethodDelete:
			var draft *factile.CuratorDraft
			if !decodeBody(w, r, &draft) {
				return
			}
			if draft == nil || draft.Path == "" {
				writeError(w, http.StatusBadRequest, factile.NewError(factile.ErrInvalidPath, "draft path is required"))
				return
			}
			if err := ws.StoreCuratorDraft(path, mode, *draft, r.Method == http.MethodDelete); err != nil {
				writeError(w, errorStatus(err), err)
				return
			}
			writeJSON(w, nil)
		default:
			writeError(w, http.StatusMethodNotAllowed, factile.NewError(factile.ErrUnsupportedCommand, "Unsupported method: "+r.Method))
		}
	}
}
