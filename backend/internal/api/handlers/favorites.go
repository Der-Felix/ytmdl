package handlers

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	"ytdm/backend/internal/api/middleware"
	"ytdm/backend/internal/api/response"
	"ytdm/backend/internal/apperr"
)

// ListFavorites answers GET /api/v1/favorites.
func (h *Handlers) ListFavorites(w http.ResponseWriter, r *http.Request) {
	user := middleware.UserFromContext(r.Context())
	if user == nil {
		response.Fail(w, r, apperr.CodeUnauthenticated, "Authentifizierung erforderlich.")
		return
	}

	tracks, err := h.deps.Playlists.ListFavoriteTracks(r.Context(), user.ID)
	if err != nil {
		response.Error(w, r, err)
		return
	}
	response.OK(w, r, tracks)
}

// ListFavoriteIDs answers GET /api/v1/favorites/ids.
func (h *Handlers) ListFavoriteIDs(w http.ResponseWriter, r *http.Request) {
	user := middleware.UserFromContext(r.Context())
	if user == nil {
		response.Fail(w, r, apperr.CodeUnauthenticated, "Authentifizierung erforderlich.")
		return
	}

	ids, err := h.deps.Playlists.ListFavoriteTrackIDs(r.Context(), user.ID)
	if err != nil {
		response.Error(w, r, err)
		return
	}
	response.OK(w, r, ids)
}

// FavoriteTrack answers PUT /api/v1/favorites/{track_id}.
func (h *Handlers) FavoriteTrack(w http.ResponseWriter, r *http.Request) {
	user := middleware.UserFromContext(r.Context())
	if user == nil {
		response.Fail(w, r, apperr.CodeUnauthenticated, "Authentifizierung erforderlich.")
		return
	}

	trackID := chi.URLParam(r, "track_id")
	if trackID == "" {
		response.Error(w, r, apperr.New(apperr.CodeInvalidRequest, "Track-ID ist erforderlich."))
		return
	}

	if err := h.deps.Playlists.FavoriteTrack(r.Context(), user.ID, trackID); err != nil {
		response.Error(w, r, err)
		return
	}
	response.NoContent(w)
}

// UnfavoriteTrack answers DELETE /api/v1/favorites/{track_id}.
func (h *Handlers) UnfavoriteTrack(w http.ResponseWriter, r *http.Request) {
	user := middleware.UserFromContext(r.Context())
	if user == nil {
		response.Fail(w, r, apperr.CodeUnauthenticated, "Authentifizierung erforderlich.")
		return
	}

	trackID := chi.URLParam(r, "track_id")
	if trackID == "" {
		response.Error(w, r, apperr.New(apperr.CodeInvalidRequest, "Track-ID ist erforderlich."))
		return
	}

	if err := h.deps.Playlists.UnfavoriteTrack(r.Context(), user.ID, trackID); err != nil {
		response.Error(w, r, err)
		return
	}
	response.NoContent(w)
}

// IsFavoriteTrack answers GET /api/v1/favorites/{track_id}.
func (h *Handlers) IsFavoriteTrack(w http.ResponseWriter, r *http.Request) {
	user := middleware.UserFromContext(r.Context())
	if user == nil {
		response.Fail(w, r, apperr.CodeUnauthenticated, "Authentifizierung erforderlich.")
		return
	}

	trackID := chi.URLParam(r, "track_id")
	if trackID == "" {
		response.Error(w, r, apperr.New(apperr.CodeInvalidRequest, "Track-ID ist erforderlich."))
		return
	}

	isFav, err := h.deps.Playlists.IsFavorite(r.Context(), user.ID, trackID)
	if err != nil {
		response.Error(w, r, err)
		return
	}
	response.OK(w, r, map[string]bool{"favorited": isFav})
}
