package handlers

import (
	"net/http"
	"ytdm/backend/internal/api/middleware"
	"ytdm/backend/internal/api/response"
	"ytdm/backend/internal/auth"
)

func (h *Handlers) StartDevice(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	var req struct {
		DeviceName string `json:"device_name"`
	}
	if err := decodeJSON(r, &req); err != nil {
		response.Error(w, r, err)
		return
	}
	result, err := h.deps.Auth.StartDevice(req.DeviceName, middleware.ClientIP(r))
	if err != nil {
		response.Error(w, r, err)
		return
	}
	response.Created(w, r, result)
}

func (h *Handlers) PollDevice(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	var req struct {
		DeviceCode string `json:"device_code"`
	}
	if err := decodeJSON(r, &req); err != nil {
		response.Error(w, r, err)
		return
	}
	result, state, err := h.deps.Auth.PollDevice(r.Context(), req.DeviceCode, middleware.ClientIP(r))
	if err != nil {
		response.Error(w, r, err)
		return
	}
	if result != nil {
		secure := middleware.IsSecure(r, h.deps.CookieSecure)
		middleware.SetSessionCookie(w, result.SessionToken, result.ExpiresAt, secure)
		if csrf, err := auth.GenerateCSRFToken(); err == nil {
			middleware.SetCSRFCookie(w, csrf, secure)
		}
	}
	response.OK(w, r, map[string]string{"status": state})
}

func (h *Handlers) PreviewDevice(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	var req struct {
		UserCode string `json:"user_code"`
	}
	if err := decodeJSON(r, &req); err != nil {
		response.Error(w, r, err)
		return
	}
	user := middleware.UserFromContext(r.Context())
	result, err := h.deps.Auth.PreviewDevice(req.UserCode, user.ID)
	if err != nil {
		response.Error(w, r, err)
		return
	}
	response.OK(w, r, result)
}

func (h *Handlers) ConfirmDevice(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	var req struct {
		UserCode string `json:"user_code"`
	}
	if err := decodeJSON(r, &req); err != nil {
		response.Error(w, r, err)
		return
	}
	if err := h.deps.Auth.ConfirmDevice(req.UserCode, middleware.UserFromContext(r.Context()), middleware.SessionFromContext(r.Context())); err != nil {
		response.Error(w, r, err)
		return
	}
	response.NoContent(w)
}
