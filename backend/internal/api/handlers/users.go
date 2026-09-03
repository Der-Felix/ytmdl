package handlers

import (
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	"ytdm/backend/internal/api/response"
	"ytdm/backend/internal/apperr"
	"ytdm/backend/internal/auth"
)

// ListUsers returns a paginated list of user accounts (Admin only).
func (h *Handlers) ListUsers(w http.ResponseWriter, r *http.Request) {
	limit := 50
	if l := r.URL.Query().Get("limit"); l != "" {
		if n, err := strconv.Atoi(l); err == nil && n > 0 {
			limit = n
		}
	}
	offset := 0
	if o := r.URL.Query().Get("offset"); o != "" {
		if n, err := strconv.Atoi(o); err == nil && n >= 0 {
			offset = n
		}
	}

	users, total, err := h.deps.Auth.ListUsers(r.Context(), limit, offset)
	if err != nil {
		response.Error(w, r, err)
		return
	}

	response.List(w, users, response.Meta{Count: total, Limit: limit, Offset: offset})
}

// CreateUser creates a new user account (Admin only).
func (h *Handlers) CreateUser(w http.ResponseWriter, r *http.Request) {
	var req auth.CreateUserRequest
	if err := decodeJSON(r, &req); err != nil {
		response.Error(w, r, err)
		return
	}

	created, err := h.deps.Auth.CreateUser(r.Context(), req)
	if err != nil {
		response.Error(w, r, err)
		return
	}

	response.Created(w, r, created)
}

// GetUser returns details for a specific user (Admin only).
func (h *Handlers) GetUser(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if id == "" {
		response.Fail(w, r, apperr.CodeInvalidRequest, "Benutzer-ID ist erforderlich.")
		return
	}

	user, err := h.deps.Auth.GetUser(r.Context(), id)
	if err != nil {
		response.Error(w, r, err)
		return
	}

	response.OK(w, r, user)
}

// UpdateUser updates role, enabled status or display name of a user (Admin only).
func (h *Handlers) UpdateUser(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if id == "" {
		response.Fail(w, r, apperr.CodeInvalidRequest, "Benutzer-ID ist erforderlich.")
		return
	}

	var req auth.UpdateUserStatusRequest
	if err := decodeJSON(r, &req); err != nil {
		response.Error(w, r, err)
		return
	}

	updated, err := h.deps.Auth.UpdateUser(r.Context(), id, req)
	if err != nil {
		response.Error(w, r, err)
		return
	}

	response.OK(w, r, updated)
}

type resetPasswordBody struct {
	Password string `json:"password"`
}

// ResetPassword resets a user's password and revokes all active sessions (Admin only).
func (h *Handlers) ResetPassword(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if id == "" {
		response.Fail(w, r, apperr.CodeInvalidRequest, "Benutzer-ID ist erforderlich.")
		return
	}

	var body resetPasswordBody
	if err := decodeJSON(r, &body); err != nil {
		response.Error(w, r, err)
		return
	}

	if err := h.deps.Auth.ResetPassword(r.Context(), id, body.Password); err != nil {
		response.Error(w, r, err)
		return
	}

	response.NoContent(w)
}

// DeleteUser deletes a user account (Admin only).
func (h *Handlers) DeleteUser(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if id == "" {
		response.Fail(w, r, apperr.CodeInvalidRequest, "Benutzer-ID ist erforderlich.")
		return
	}

	if err := h.deps.Auth.DeleteUser(r.Context(), id); err != nil {
		response.Error(w, r, err)
		return
	}

	response.NoContent(w)
}
