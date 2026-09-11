package handlers

import (
	"net/http"
	"strings"

	"ytdm/backend/internal/api/response"
	"ytdm/backend/internal/apperr"
	"ytdm/backend/internal/update"
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

// updateChannelRequest selects the update channel.
type updateChannelRequest struct {
	Channel string `json:"channel"`
}

// SetUpdateChannel stores the update channel an administrator chose. It only
// records the choice: it installs nothing, restarts nothing and never
// downgrades. The installation itself stays with ytmdlctl on the host.
func (h *Handlers) SetUpdateChannel(w http.ResponseWriter, r *http.Request) {
	if h.deps.Updates == nil {
		response.Error(w, r, apperr.New(apperr.CodeInternal, "Update service is not configured."))
		return
	}

	var req updateChannelRequest
	if err := decodeJSON(r, &req); err != nil {
		response.Error(w, r, err)
		return
	}
	channel, err := update.ParseChannel(req.Channel)
	if err != nil || strings.TrimSpace(req.Channel) == "" {
		response.Error(w, r, apperr.New(apperr.CodeInvalidRequest, "The update channel must be \"stable\" or \"development\"."))
		return
	}
	if err := h.deps.Updates.SetChannel(r.Context(), channel); err != nil {
		response.Error(w, r, apperr.Wrap(apperr.CodeInternal, "The update channel could not be stored.", err))
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
