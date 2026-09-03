package handlers

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	"ytdm/backend/internal/api/middleware"
	"ytdm/backend/internal/api/response"
	"ytdm/backend/internal/apperr"
	"ytdm/backend/internal/auth"
)

// GetProfile returns the profile of the current user.
func (h *Handlers) GetProfile(w http.ResponseWriter, r *http.Request) {
	user := middleware.UserFromContext(r.Context())
	if user == nil {
		response.Fail(w, r, apperr.CodeUnauthenticated, "Authentifizierung erforderlich.")
		return
	}
	response.OK(w, r, user.Summary())
}

// UpdateProfile updates the display name of the current user.
func (h *Handlers) UpdateProfile(w http.ResponseWriter, r *http.Request) {
	user := middleware.UserFromContext(r.Context())
	if user == nil {
		response.Fail(w, r, apperr.CodeUnauthenticated, "Authentifizierung erforderlich.")
		return
	}

	var req auth.UpdateProfileRequest
	if err := decodeJSON(r, &req); err != nil {
		response.Error(w, r, err)
		return
	}

	updated, err := h.deps.Auth.UpdateProfile(r.Context(), user.ID, req)
	if err != nil {
		response.Error(w, r, err)
		return
	}

	response.OK(w, r, updated)
}

// ChangePassword updates the password of the current user and revokes other sessions.
func (h *Handlers) ChangePassword(w http.ResponseWriter, r *http.Request) {
	user := middleware.UserFromContext(r.Context())
	session := middleware.SessionFromContext(r.Context())
	if user == nil || session == nil {
		response.Fail(w, r, apperr.CodeUnauthenticated, "Authentifizierung erforderlich.")
		return
	}

	var req auth.ChangePasswordRequest
	if err := decodeJSON(r, &req); err != nil {
		response.Error(w, r, err)
		return
	}

	if err := h.deps.Auth.ChangePassword(r.Context(), user.ID, session.ID, req); err != nil {
		response.Error(w, r, err)
		return
	}

	response.NoContent(w)
}

// ListSessions returns all active sessions of the current user.
func (h *Handlers) ListSessions(w http.ResponseWriter, r *http.Request) {
	user := middleware.UserFromContext(r.Context())
	session := middleware.SessionFromContext(r.Context())
	if user == nil || session == nil {
		response.Fail(w, r, apperr.CodeUnauthenticated, "Authentifizierung erforderlich.")
		return
	}

	sessions, err := h.deps.Auth.ListSessions(r.Context(), user.ID, session.ID)
	if err != nil {
		response.Error(w, r, err)
		return
	}

	response.OK(w, r, sessions)
}

// RevokeSession terminates a specific session of the current user.
func (h *Handlers) RevokeSession(w http.ResponseWriter, r *http.Request) {
	user := middleware.UserFromContext(r.Context())
	session := middleware.SessionFromContext(r.Context())
	if user == nil || session == nil {
		response.Fail(w, r, apperr.CodeUnauthenticated, "Authentifizierung erforderlich.")
		return
	}

	targetSessionID := chi.URLParam(r, "id")
	if targetSessionID == "" {
		response.Fail(w, r, apperr.CodeInvalidRequest, "Sitzungs-ID ist erforderlich.")
		return
	}

	if err := h.deps.Auth.RevokeSession(r.Context(), user.ID, targetSessionID); err != nil {
		response.Error(w, r, err)
		return
	}

	// If the user revoked their own current session, clear the cookie
	if targetSessionID == session.ID {
		isSecure := middleware.IsSecure(r, h.deps.CookieSecure)
		middleware.ClearSessionCookie(w, isSecure)
	}

	response.NoContent(w)
}

// RevokeOtherSessions terminates all sessions of the current user except the active one.
func (h *Handlers) RevokeOtherSessions(w http.ResponseWriter, r *http.Request) {
	user := middleware.UserFromContext(r.Context())
	session := middleware.SessionFromContext(r.Context())
	if user == nil || session == nil {
		response.Fail(w, r, apperr.CodeUnauthenticated, "Authentifizierung erforderlich.")
		return
	}

	if err := h.deps.Auth.RevokeOtherSessions(r.Context(), user.ID, session.ID); err != nil {
		response.Error(w, r, err)
		return
	}

	response.NoContent(w)
}
