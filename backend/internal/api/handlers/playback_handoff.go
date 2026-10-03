package handlers

import (
	"net/http"
	"ytdm/backend/internal/api/middleware"
	"ytdm/backend/internal/api/response"
	"ytdm/backend/internal/database/repository"
)

func (h *Handlers) GetPlaybackHandoff(w http.ResponseWriter, r *http.Request) {
	v, err := h.deps.Catalog.PlaybackHandoff(r.Context(), middleware.UserFromContext(r.Context()).ID)
	if err != nil {
		response.Error(w, r, err)
		return
	}
	response.OK(w, r, v)
}
func (h *Handlers) SavePlaybackHandoff(w http.ResponseWriter, r *http.Request) {
	var req repository.PlaybackHandoff
	if err := decodeJSON(r, &req); err != nil {
		response.Error(w, r, err)
		return
	}
	v, err := h.deps.Catalog.SavePlaybackHandoff(r.Context(), middleware.UserFromContext(r.Context()).ID, req)
	if err != nil {
		response.Error(w, r, err)
		return
	}
	response.OK(w, r, v)
}
func (h *Handlers) DeletePlaybackHandoff(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ID string `json:"id"`
	}
	if err := decodeJSON(r, &req); err != nil {
		response.Error(w, r, err)
		return
	}
	if err := h.deps.Catalog.DeletePlaybackHandoff(r.Context(), middleware.UserFromContext(r.Context()).ID, req.ID); err != nil {
		response.Error(w, r, err)
		return
	}
	response.NoContent(w)
}
