package handlers

import (
	"net/http"

	"ytdm/backend/internal/api/response"
	"ytdm/backend/internal/settings"
)

// GetSettings answers GET /settings with the effective configuration.
func (h *Handlers) GetSettings(w http.ResponseWriter, r *http.Request) {
	response.Data(w, http.StatusOK, h.deps.Settings.Current())
}

// UpdateSettings answers PUT /settings. Only the values that can genuinely be
// changed while the server runs are accepted; everything else belongs in the
// configuration file and needs a restart.
func (h *Handlers) UpdateSettings(w http.ResponseWriter, r *http.Request) {
	var update settings.Update
	if err := decodeJSON(r, &update); err != nil {
		response.Error(w, r, err)
		return
	}

	current, err := h.deps.Settings.Apply(r.Context(), update)
	if err != nil {
		response.Error(w, r, err)
		return
	}
	response.Data(w, http.StatusOK, current)
}
