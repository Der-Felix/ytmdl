package handlers

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"ytdm/backend/internal/api/middleware"
	"ytdm/backend/internal/api/response"
)

// PersonalMixes answers GET /library/mixes with the current user's mixes.
func (h *Handlers) PersonalMixes(w http.ResponseWriter, r *http.Request) {
	mixes, err := h.deps.Catalog.PersonalMixes(r.Context(), middleware.UserFromContext(r.Context()).ID)
	if err != nil {
		response.Error(w, r, err)
		return
	}
	response.OK(w, r, mixes)
}

// PersonalMix answers GET /library/mixes/{id} with one mix and its tracks.
func (h *Handlers) PersonalMix(w http.ResponseWriter, r *http.Request) {
	mix, err := h.deps.Catalog.PersonalMix(r.Context(), middleware.UserFromContext(r.Context()).ID, chi.URLParam(r, "id"))
	if err != nil {
		response.Error(w, r, err)
		return
	}
	response.OK(w, r, mix)
}
