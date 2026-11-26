package handlers

import (
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"ytdm/backend/internal/api/middleware"
	"ytdm/backend/internal/api/response"
)

// PersonalMixes answers GET /library/mixes with the current user's mixes.
// The optional tz (IANA zone) sets the user's day and time of day: the day
// the mixes change and the daypart mix offered.
func (h *Handlers) PersonalMixes(w http.ResponseWriter, r *http.Request) {
	loc, err := requestLocation(r)
	if err != nil {
		response.Error(w, r, err)
		return
	}
	mixes, err := h.deps.Catalog.PersonalMixesOn(r.Context(), middleware.UserFromContext(r.Context()).ID, time.Now().In(loc))
	if err != nil {
		response.Error(w, r, err)
		return
	}
	response.OK(w, r, mixes)
}

// PersonalMix answers GET /library/mixes/{id} with one mix and its tracks.
func (h *Handlers) PersonalMix(w http.ResponseWriter, r *http.Request) {
	loc, err := requestLocation(r)
	if err != nil {
		response.Error(w, r, err)
		return
	}
	mix, err := h.deps.Catalog.PersonalMixOn(r.Context(), middleware.UserFromContext(r.Context()).ID, chi.URLParam(r, "id"), time.Now().In(loc))
	if err != nil {
		response.Error(w, r, err)
		return
	}
	response.OK(w, r, mix)
}
