package handlers

import (
	"net/http"

	"ytdm/backend/internal/api/response"
	"ytdm/backend/internal/apperr"
)

// GetUpdateStatus returns the current or cached update check status.
func (h *Handlers) GetUpdateStatus(w http.ResponseWriter, r *http.Request) {
	if h.deps.Updates == nil {
		response.Error(w, r, apperr.New(apperr.CodeInternal, "Update service is not configured."))
		return
	}

	status, err := h.deps.Updates.GetStatus(r.Context(), false)
	if err != nil {
		response.Error(w, r, err)
		return
	}

	response.JSON(w, http.StatusOK, map[string]any{
		"data": status,
	})
}

// CheckUpdate triggers a fresh update check against the official release repository.
func (h *Handlers) CheckUpdate(w http.ResponseWriter, r *http.Request) {
	if h.deps.Updates == nil {
		response.Error(w, r, apperr.New(apperr.CodeInternal, "Update service is not configured."))
		return
	}

	status, err := h.deps.Updates.GetStatus(r.Context(), true)
	if err != nil {
		response.Error(w, r, err)
		return
	}

	response.JSON(w, http.StatusOK, map[string]any{
		"data": status,
	})
}
