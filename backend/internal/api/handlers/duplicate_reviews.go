package handlers

import (
	"github.com/go-chi/chi/v5"
	"net/http"
	"ytdm/backend/internal/api/middleware"
	"ytdm/backend/internal/api/response"
	"ytdm/backend/internal/apperr"
	"ytdm/backend/internal/database/repository"
)

func (h *Handlers) SaveDuplicateReview(w http.ResponseWriter, r *http.Request) {
	var req repository.DuplicateReview
	if err := decodeJSON(r, &req); err != nil {
		response.Error(w, r, err)
		return
	}
	if err := h.deps.Catalog.SaveDuplicateReview(r.Context(), middleware.UserFromContext(r.Context()).ID, req); err != nil {
		response.Error(w, r, err)
		return
	}
	response.NoContent(w)
}
func (h *Handlers) ResetDuplicateReview(w http.ResponseWriter, r *http.Request) {
	if err := h.deps.Catalog.ResetDuplicateReview(r.Context(), middleware.UserFromContext(r.Context()).ID, chi.URLParam(r, "key")); err != nil {
		response.Error(w, r, err)
		return
	}
	response.NoContent(w)
}
func (h *Handlers) RemoveDuplicateVersions(w http.ResponseWriter, r *http.Request) {
	var req struct {
		repository.DuplicateReview
		RemoveTrackIDs []string `json:"remove_track_ids"`
		Confirmed      bool     `json:"confirmed"`
	}
	if err := decodeJSON(r, &req); err != nil {
		response.Error(w, r, err)
		return
	}
	if !req.Confirmed {
		response.Error(w, r, apperr.New(apperr.CodeInvalidRequest, "Bitte das Löschen ausdrücklich bestätigen."))
		return
	}
	if h.deps.LibraryService == nil {
		response.Error(w, r, apperr.New(apperr.CodeInternal, "Bibliotheksverwaltung nicht verfügbar."))
		return
	}
	validate := func() error {
		return h.deps.Catalog.ValidateDuplicateRemoval(r.Context(), middleware.UserFromContext(r.Context()).ID, req.DuplicateReview, req.RemoveTrackIDs)
	}
	deleted, failed, err := h.deps.LibraryService.RemoveDuplicateTracks(r.Context(), req.PreferredTrackID, req.RemoveTrackIDs, validate)
	if err != nil && failed == "" {
		response.Error(w, r, err)
		return
	}
	result := struct {
		Deleted   []string `json:"deleted_track_ids"`
		FailedID  string   `json:"failed_track_id,omitempty"`
		ErrorCode string   `json:"error_code,omitempty"`
		Message   string   `json:"message,omitempty"`
	}{Deleted: deleted, FailedID: failed}
	if err != nil {
		result.ErrorCode = string(apperr.CodeOf(err))
		result.Message = apperr.MessageOf(err)
	}
	response.OK(w, r, result)
}
