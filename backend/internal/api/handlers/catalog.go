package handlers

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	"ytdm/backend/internal/api/response"
	"ytdm/backend/internal/apperr"
)

// SearchArtists answers GET /search/artists.
func (h *Handlers) SearchArtists(w http.ResponseWriter, r *http.Request) {
	query := queryString(r, "q")
	if query == "" {
		response.Fail(w, r, apperr.CodeInvalidRequest, "The query parameter q is required.")
		return
	}

	artists, err := h.deps.Discography.SearchArtists(r.Context(), queryString(r, "provider"), query)
	if err != nil {
		response.Error(w, r, err)
		return
	}
	response.List(w, artists, response.Meta{Count: len(artists)})
}

// GetArtist answers GET /artists/{id}.
func (h *Handlers) GetArtist(w http.ResponseWriter, r *http.Request) {
	artist, err := h.deps.Discography.Artist(r.Context(), queryString(r, "provider"), chi.URLParam(r, "id"))
	if err != nil {
		response.Error(w, r, err)
		return
	}
	response.Data(w, http.StatusOK, artist)
}

// GetDiscography answers GET /artists/{id}/discography.
func (h *Handlers) GetDiscography(w http.ResponseWriter, r *http.Request) {
	filter, err := releaseFilterFromQuery(r)
	if err != nil {
		response.Error(w, r, err)
		return
	}

	releases, err := h.deps.Discography.Discography(r.Context(),
		queryString(r, "provider"), chi.URLParam(r, "id"), filter)
	if err != nil {
		response.Error(w, r, err)
		return
	}
	response.List(w, releases, response.Meta{Count: len(releases)})
}

// GetRelease answers GET /releases/{id}, including the track list.
func (h *Handlers) GetRelease(w http.ResponseWriter, r *http.Request) {
	release, tracks, err := h.deps.Discography.Release(r.Context(),
		queryString(r, "provider"), chi.URLParam(r, "id"))
	if err != nil {
		response.Error(w, r, err)
		return
	}
	response.Data(w, http.StatusOK, map[string]any{
		"release": release,
		"tracks":  tracks,
	})
}
