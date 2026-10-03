package handlers

import (
	"net/http"
	"ytdm/backend/internal/api/middleware"
	"ytdm/backend/internal/api/response"
)

func (h *Handlers) LocalRadio(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	tracks, err := h.deps.Catalog.LocalRadio(r.Context(), middleware.UserFromContext(r.Context()).ID, q.Get("seed"), q.Get("genre"), q.Get("nonce"))
	if err != nil {
		response.Error(w, r, err)
		return
	}
	response.OK(w, r, tracks)
}
