package handlers

import (
	"github.com/go-chi/chi/v5"
	"net/http"
	"strconv"
	"ytdm/backend/internal/api/response"
	"ytdm/backend/internal/apperr"
)

func (h *Handlers) LibraryTrash(w http.ResponseWriter, r *http.Request) {
	offset, _ := strconv.Atoi(r.URL.Query().Get("offset"))
	entries, err := h.deps.Catalog.ListTrashPage(r.Context(), 20, offset, true)
	if err != nil {
		response.Error(w, r, err)
		return
	}
	response.OK(w, r, entries)
}
func (h *Handlers) RestoreLibraryTrash(w http.ResponseWriter, r *http.Request) {
	if h.deps.LibraryService == nil {
		response.Error(w, r, apperr.New(apperr.CodeInternal, "Papierkorb nicht verfügbar."))
		return
	}
	if err := h.deps.LibraryService.RestoreTrash(r.Context(), chi.URLParam(r, "id")); err != nil {
		response.Error(w, r, err)
		return
	}
	response.NoContent(w)
}
func (h *Handlers) PurgeLibraryTrash(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Confirmed bool `json:"confirmed"`
	}
	if err := decodeJSON(r, &req); err != nil {
		response.Error(w, r, err)
		return
	}
	if !req.Confirmed {
		response.Error(w, r, apperr.New(apperr.CodeInvalidRequest, "Endgültiges Löschen bitte ausdrücklich bestätigen."))
		return
	}
	if h.deps.LibraryService == nil {
		response.Error(w, r, apperr.New(apperr.CodeInternal, "Papierkorb nicht verfügbar."))
		return
	}
	if err := h.deps.LibraryService.PurgeTrash(r.Context(), chi.URLParam(r, "id"), false); err != nil {
		response.Error(w, r, err)
		return
	}
	response.NoContent(w)
}
func (h *Handlers) RecoverLibraryTrash(w http.ResponseWriter, r *http.Request) {
	if h.deps.LibraryService == nil {
		response.Error(w, r, apperr.New(apperr.CodeInternal, "Papierkorb nicht verfügbar."))
		return
	}
	if err := h.deps.LibraryService.RecoverTrash(r.Context()); err != nil {
		response.Error(w, r, err)
		return
	}
	response.NoContent(w)
}
