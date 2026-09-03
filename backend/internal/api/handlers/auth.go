package handlers

import (
	"net/http"

	"ytdm/backend/internal/api/middleware"
	"ytdm/backend/internal/api/response"
	"ytdm/backend/internal/apperr"
	"ytdm/backend/internal/auth"
)

// Status reports the authentication state and whether first-run setup is needed.
func (h *Handlers) Status(w http.ResponseWriter, r *http.Request) {
	var token string
	if cookie, err := r.Cookie(middleware.SessionCookieName); err == nil {
		token = cookie.Value
	}

	status, err := h.deps.Auth.Status(r.Context(), token)
	if err != nil {
		response.Error(w, r, err)
		return
	}

	response.OK(w, r, status)
}

// Setup creates the initial administrator user account during first run.
func (h *Handlers) Setup(w http.ResponseWriter, r *http.Request) {
	var req auth.SetupRequest
	if err := decodeJSON(r, &req); err != nil {
		response.Error(w, r, err)
		return
	}

	clientIP := middleware.ClientIP(r)
	userAgent := r.UserAgent()

	result, err := h.deps.Auth.Setup(r.Context(), req, clientIP, userAgent)
	if err != nil {
		response.Error(w, r, err)
		return
	}

	isSecure := middleware.IsSecure(r, h.deps.CookieSecure)
	middleware.SetSessionCookie(w, result.SessionToken, result.ExpiresAt, isSecure)

	// Issue fresh CSRF cookie
	if csrfToken, err := auth.GenerateCSRFToken(); err == nil {
		middleware.SetCSRFCookie(w, csrfToken, isSecure)
	}

	response.Created(w, r, result.User)
}

// Login authenticates a user with username and password.
func (h *Handlers) Login(w http.ResponseWriter, r *http.Request) {
	var req auth.LoginRequest
	if err := decodeJSON(r, &req); err != nil {
		response.Error(w, r, err)
		return
	}

	clientIP := middleware.ClientIP(r)
	userAgent := r.UserAgent()

	result, err := h.deps.Auth.Login(r.Context(), req, clientIP, userAgent)
	if err != nil {
		response.Error(w, r, err)
		return
	}

	isSecure := middleware.IsSecure(r, h.deps.CookieSecure)
	middleware.SetSessionCookie(w, result.SessionToken, result.ExpiresAt, isSecure)

	// Issue fresh CSRF cookie upon login
	if csrfToken, err := auth.GenerateCSRFToken(); err == nil {
		middleware.SetCSRFCookie(w, csrfToken, isSecure)
	}

	response.OK(w, r, result.User)
}

// Logout terminates the current session and clears the session cookie.
func (h *Handlers) Logout(w http.ResponseWriter, r *http.Request) {
	var token string
	if cookie, err := r.Cookie(middleware.SessionCookieName); err == nil {
		token = cookie.Value
	}

	if err := h.deps.Auth.Logout(r.Context(), token); err != nil {
		response.Error(w, r, err)
		return
	}

	isSecure := middleware.IsSecure(r, h.deps.CookieSecure)
	middleware.ClearSessionCookie(w, isSecure)

	response.NoContent(w)
}

// Me returns the summary of the currently authenticated user.
func (h *Handlers) Me(w http.ResponseWriter, r *http.Request) {
	user := middleware.UserFromContext(r.Context())
	if user == nil {
		response.Fail(w, r, apperr.CodeUnauthenticated, "Authentifizierung erforderlich.")
		return
	}

	response.OK(w, r, user.Summary())
}
