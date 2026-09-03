package handlers

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	"ytdm/backend/internal/api/response"
	"ytdm/backend/internal/apperr"
)

// PreviewReleaseArtwork answers POST /library/releases/{id}/artwork/preview.
func (h *Handlers) PreviewReleaseArtwork(w http.ResponseWriter, r *http.Request) {
	if h.deps.LibraryService == nil {
		response.Fail(w, r, apperr.CodeInternal, "Library service is unavailable.")
		return
	}

	releaseID := chi.URLParam(r, "id")
	if releaseID == "" {
		response.Error(w, r, apperr.New(apperr.CodeInvalidRequest, "Release ID is required."))
		return
	}

	preview, err := h.deps.LibraryService.PreviewReleaseArtwork(r.Context(), releaseID)
	if err != nil {
		response.Error(w, r, err)
		return
	}

	response.Data(w, http.StatusOK, preview)
}

// RefreshReleaseArtwork answers POST /library/releases/{id}/artwork/refresh.
func (h *Handlers) RefreshReleaseArtwork(w http.ResponseWriter, r *http.Request) {
	if h.deps.LibraryService == nil {
		response.Fail(w, r, apperr.CodeInternal, "Library service is unavailable.")
		return
	}

	releaseID := chi.URLParam(r, "id")
	if releaseID == "" {
		response.Error(w, r, apperr.New(apperr.CodeInvalidRequest, "Release ID is required."))
		return
	}

	result, err := h.deps.LibraryService.ApplyReleaseArtwork(r.Context(), releaseID, nil)
	if err != nil {
		response.Error(w, r, err)
		return
	}

	response.Data(w, http.StatusOK, result)
}

// BulkArtworkRequest specifies artist or releases for bulk artwork evaluation.
type BulkArtworkRequest struct {
	ArtistID   string   `json:"artist_id,omitempty"`
	ReleaseIDs []string `json:"release_ids,omitempty"`
}

// PreviewBulkArtwork answers POST /library/artwork/preview.
func (h *Handlers) PreviewBulkArtwork(w http.ResponseWriter, r *http.Request) {
	if h.deps.LibraryService == nil {
		response.Fail(w, r, apperr.CodeInternal, "Library service is unavailable.")
		return
	}

	var req BulkArtworkRequest
	if err := decodeJSON(r, &req); err != nil {
		response.Error(w, r, err)
		return
	}

	previews, err := h.deps.LibraryService.PreviewBulkArtwork(r.Context(), req.ArtistID, req.ReleaseIDs)
	if err != nil {
		response.Error(w, r, err)
		return
	}

	response.Data(w, http.StatusOK, previews)
}

// RefreshBulkArtwork answers POST /library/artwork/refresh.
func (h *Handlers) RefreshBulkArtwork(w http.ResponseWriter, r *http.Request) {
	if h.deps.LibraryService == nil {
		response.Fail(w, r, apperr.CodeInternal, "Library service is unavailable.")
		return
	}

	var req BulkArtworkRequest
	if err := decodeJSON(r, &req); err != nil {
		response.Error(w, r, err)
		return
	}

	results, err := h.deps.LibraryService.ApplyBulkArtwork(r.Context(), req.ArtistID, req.ReleaseIDs)
	if err != nil {
		response.Error(w, r, err)
		return
	}

	response.Data(w, http.StatusOK, results)
}
