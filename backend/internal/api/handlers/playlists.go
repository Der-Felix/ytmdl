package handlers

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"

	"ytdm/backend/internal/api/middleware"
	"ytdm/backend/internal/api/response"
	"ytdm/backend/internal/apperr"
	"ytdm/backend/internal/database/repository"
)

// CreatePlaylistRequest payload.
type CreatePlaylistRequest struct {
	SmartRules  *repository.SmartRules `json:"smart_rules"`
	Name        string                 `json:"name"`
	Description string                 `json:"description"`
}

// UpdatePlaylistRequest payload.
type UpdatePlaylistRequest struct {
	Name        string `json:"name"`
	Description string `json:"description"`
}

// AddPlaylistTrackRequest payload.
type AddPlaylistTrackRequest struct {
	TrackID string `json:"track_id"`
}

// ReorderPlaylistTracksRequest payload.
type ReorderPlaylistTracksRequest struct {
	TrackIDs []string `json:"track_ids"`
}

// ListPlaylists answers GET /api/v1/playlists.
func (h *Handlers) ListPlaylists(w http.ResponseWriter, r *http.Request) {
	user := middleware.UserFromContext(r.Context())
	if user == nil {
		response.Fail(w, r, apperr.CodeUnauthenticated, "Authentifizierung erforderlich.")
		return
	}

	playlists, err := h.deps.Playlists.ListPlaylists(r.Context(), user.ID)
	if err != nil {
		response.Error(w, r, err)
		return
	}
	response.OK(w, r, playlists)
}

// CreatePlaylist answers POST /api/v1/playlists.
func (h *Handlers) CreatePlaylist(w http.ResponseWriter, r *http.Request) {
	user := middleware.UserFromContext(r.Context())
	if user == nil {
		response.Fail(w, r, apperr.CodeUnauthenticated, "Authentifizierung erforderlich.")
		return
	}

	var req CreatePlaylistRequest
	if err := decodeJSON(r, &req); err != nil {
		response.Error(w, r, err)
		return
	}

	playlist, err := h.deps.Playlists.CreatePlaylist(r.Context(), user.ID, req.Name, req.Description, req.SmartRules)
	if err != nil {
		response.Error(w, r, err)
		return
	}
	response.Created(w, r, playlist)
}

// GetPlaylist answers GET /api/v1/playlists/{id}.
func (h *Handlers) GetPlaylist(w http.ResponseWriter, r *http.Request) {
	user := middleware.UserFromContext(r.Context())
	if user == nil {
		response.Fail(w, r, apperr.CodeUnauthenticated, "Authentifizierung erforderlich.")
		return
	}

	id := chi.URLParam(r, "id")
	if id == "" {
		response.Error(w, r, apperr.New(apperr.CodeInvalidRequest, "Playlist-ID ist erforderlich."))
		return
	}

	detail, err := h.deps.Playlists.GetPlaylist(r.Context(), user.ID, id)
	if err != nil {
		response.Error(w, r, err)
		return
	}
	response.OK(w, r, detail)
}

// UpdatePlaylist answers PATCH /api/v1/playlists/{id}.
func (h *Handlers) UpdatePlaylist(w http.ResponseWriter, r *http.Request) {
	user := middleware.UserFromContext(r.Context())
	if user == nil {
		response.Fail(w, r, apperr.CodeUnauthenticated, "Authentifizierung erforderlich.")
		return
	}

	id := chi.URLParam(r, "id")
	if id == "" {
		response.Error(w, r, apperr.New(apperr.CodeInvalidRequest, "Playlist-ID ist erforderlich."))
		return
	}

	var req UpdatePlaylistRequest
	if err := decodeJSON(r, &req); err != nil {
		response.Error(w, r, err)
		return
	}

	updated, err := h.deps.Playlists.UpdatePlaylist(r.Context(), user.ID, id, req.Name, req.Description)
	if err != nil {
		response.Error(w, r, err)
		return
	}
	response.OK(w, r, updated)
}

// DeletePlaylist answers DELETE /api/v1/playlists/{id}.
func (h *Handlers) DeletePlaylist(w http.ResponseWriter, r *http.Request) {
	user := middleware.UserFromContext(r.Context())
	if user == nil {
		response.Fail(w, r, apperr.CodeUnauthenticated, "Authentifizierung erforderlich.")
		return
	}

	id := chi.URLParam(r, "id")
	if id == "" {
		response.Error(w, r, apperr.New(apperr.CodeInvalidRequest, "Playlist-ID ist erforderlich."))
		return
	}

	if err := h.deps.Playlists.DeletePlaylist(r.Context(), user.ID, id); err != nil {
		response.Error(w, r, err)
		return
	}
	response.NoContent(w)
}

// AddPlaylistTrack answers POST /api/v1/playlists/{id}/tracks.
func (h *Handlers) AddPlaylistTrack(w http.ResponseWriter, r *http.Request) {
	user := middleware.UserFromContext(r.Context())
	if user == nil {
		response.Fail(w, r, apperr.CodeUnauthenticated, "Authentifizierung erforderlich.")
		return
	}

	id := chi.URLParam(r, "id")
	if id == "" {
		response.Error(w, r, apperr.New(apperr.CodeInvalidRequest, "Playlist-ID ist erforderlich."))
		return
	}

	var req AddPlaylistTrackRequest
	if err := decodeJSON(r, &req); err != nil {
		response.Error(w, r, err)
		return
	}

	detail, err := h.deps.Playlists.AddTrack(r.Context(), user.ID, id, req.TrackID)
	if err != nil {
		response.Error(w, r, err)
		return
	}
	response.OK(w, r, detail)
}

// RemovePlaylistTrack answers DELETE /api/v1/playlists/{id}/tracks/{track_id}.
func (h *Handlers) RemovePlaylistTrack(w http.ResponseWriter, r *http.Request) {
	user := middleware.UserFromContext(r.Context())
	if user == nil {
		response.Fail(w, r, apperr.CodeUnauthenticated, "Authentifizierung erforderlich.")
		return
	}

	id := chi.URLParam(r, "id")
	trackID := chi.URLParam(r, "track_id")
	if id == "" || trackID == "" {
		response.Error(w, r, apperr.New(apperr.CodeInvalidRequest, "Playlist-ID und Track-ID sind erforderlich."))
		return
	}

	detail, err := h.deps.Playlists.RemoveTrack(r.Context(), user.ID, id, trackID)
	if err != nil {
		response.Error(w, r, err)
		return
	}
	response.OK(w, r, detail)
}

// ReorderPlaylistTracks answers PUT /api/v1/playlists/{id}/tracks/reorder.
func (h *Handlers) ReorderPlaylistTracks(w http.ResponseWriter, r *http.Request) {
	user := middleware.UserFromContext(r.Context())
	if user == nil {
		response.Fail(w, r, apperr.CodeUnauthenticated, "Authentifizierung erforderlich.")
		return
	}

	id := chi.URLParam(r, "id")
	if id == "" {
		response.Error(w, r, apperr.New(apperr.CodeInvalidRequest, "Playlist-ID ist erforderlich."))
		return
	}

	var req ReorderPlaylistTracksRequest
	if err := decodeJSON(r, &req); err != nil {
		response.Error(w, r, err)
		return
	}

	detail, err := h.deps.Playlists.ReorderTracks(r.Context(), user.ID, id, req.TrackIDs)
	if err != nil {
		response.Error(w, r, err)
		return
	}
	response.OK(w, r, detail)
}

func (h *Handlers) SetPlaylistRules(w http.ResponseWriter, r *http.Request) {
	user := middleware.UserFromContext(r.Context())
	var body struct {
		Rules json.RawMessage `json:"smart_rules"`
	}
	if err := decodeJSON(r, &body); err != nil {
		response.Error(w, r, err)
		return
	}
	if len(body.Rules) == 0 {
		response.Error(w, r, apperr.New(apperr.CodeInvalidRequest, "Playlist-Regeln fehlen."))
		return
	}
	var rules *repository.SmartRules
	if err := json.Unmarshal(body.Rules, &rules); err != nil {
		response.Error(w, r, apperr.New(apperr.CodeInvalidRequest, "Ungültige Playlist-Regeln."))
		return
	}
	if err := h.deps.Playlists.SetSmartRules(r.Context(), user.ID, chi.URLParam(r, "id"), rules); err != nil {
		response.Error(w, r, err)
		return
	}
	response.NoContent(w)
}
func (h *Handlers) AddPlaylistTracks(w http.ResponseWriter, r *http.Request) {
	user := middleware.UserFromContext(r.Context())
	var req struct {
		IDs []string `json:"track_ids"`
	}
	if err := decodeJSON(r, &req); err != nil {
		response.Error(w, r, err)
		return
	}
	p, err := h.deps.Playlists.AddTracks(r.Context(), user.ID, chi.URLParam(r, "id"), req.IDs)
	if err != nil {
		response.Error(w, r, err)
		return
	}
	response.OK(w, r, p)
}
