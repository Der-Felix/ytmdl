package handlers

import (
	"errors"
	"io"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"

	"ytdm/backend/internal/api/response"
	"ytdm/backend/internal/apperr"
	"ytdm/backend/internal/mediasession"
)

const maxCookieUploadBytes = 1024 * 1024 // 1 MiB conservative limit

// ListMediaSessions returns all configured media sessions.
func (h *Handlers) ListMediaSessions(w http.ResponseWriter, r *http.Request) {
	if h.deps.MediaSessions == nil {
		response.Fail(w, r, apperr.CodeInternal, "Media session service not configured.")
		return
	}

	sessions, err := h.deps.MediaSessions.ListSessions(r.Context())
	if err != nil {
		response.Error(w, r, err)
		return
	}

	response.OK(w, r, sessions)
}

// GetMediaSession returns details for a specific media session.
func (h *Handlers) GetMediaSession(w http.ResponseWriter, r *http.Request) {
	if h.deps.MediaSessions == nil {
		response.Fail(w, r, apperr.CodeInternal, "Media session service not configured.")
		return
	}

	id := chi.URLParam(r, "id")
	if id == "" {
		response.Fail(w, r, apperr.CodeInvalidRequest, "Session ID is required.")
		return
	}

	session, err := h.deps.MediaSessions.GetSession(r.Context(), id)
	if err != nil {
		response.Error(w, r, err)
		return
	}

	response.OK(w, r, session)
}

// CreateMediaSession creates a new media session record.
func (h *Handlers) CreateMediaSession(w http.ResponseWriter, r *http.Request) {
	if h.deps.MediaSessions == nil {
		response.Fail(w, r, apperr.CodeInternal, "Media session service not configured.")
		return
	}

	var req mediasession.CreateSessionRequest
	if err := decodeJSON(r, &req); err != nil {
		response.Error(w, r, err)
		return
	}

	created, err := h.deps.MediaSessions.CreateSession(r.Context(), req)
	if err != nil {
		response.Error(w, r, err)
		return
	}

	response.Created(w, r, created)
}

// UploadMediaSessionCookies installs or replaces Netscape cookies for a session.
// Accepts multipart/form-data with a file field, or raw text body.
func (h *Handlers) UploadMediaSessionCookies(w http.ResponseWriter, r *http.Request) {
	if h.deps.MediaSessions == nil {
		response.Fail(w, r, apperr.CodeInternal, "Media session service not configured.")
		return
	}

	id := chi.URLParam(r, "id")
	if id == "" {
		response.Fail(w, r, apperr.CodeInvalidRequest, "Session ID is required.")
		return
	}

	contentType := r.Header.Get("Content-Type")
	var cookieBytes []byte

	if strings.HasPrefix(contentType, "multipart/form-data") {
		// Limit multipart upload body to 2 MiB (1 MiB file + multipart framing)
		r.Body = http.MaxBytesReader(w, r.Body, 2*1024*1024)
		if err := r.ParseMultipartForm(2 * 1024 * 1024); err != nil {
			response.Fail(w, r, apperr.CodeInvalidRequest, "Failed to parse multipart cookie upload: request too large or malformed.")
			return
		}

		var fileFound bool
		// Check common form file field names
		fileKeys := []string{"file", "cookies", "cookie_file"}
		for _, key := range fileKeys {
			file, header, err := r.FormFile(key)
			if err == nil {
				defer file.Close()
				if header.Size > maxCookieUploadBytes {
					response.Fail(w, r, apperr.CodeInvalidRequest, "Cookie file exceeds 1 MiB limit.")
					return
				}
				data, err := io.ReadAll(io.LimitReader(file, maxCookieUploadBytes+1))
				if err != nil {
					response.Fail(w, r, apperr.CodeInvalidRequest, "Failed to read uploaded cookie file.")
					return
				}
				if len(data) > maxCookieUploadBytes {
					response.Fail(w, r, apperr.CodeInvalidRequest, "Cookie file exceeds 1 MiB limit.")
					return
				}
				cookieBytes = data
				fileFound = true
				break
			}
		}

		// If not found by named key, check any file in multipart form
		if !fileFound && r.MultipartForm != nil {
			for _, files := range r.MultipartForm.File {
				if len(files) > 0 {
					f, err := files[0].Open()
					if err == nil {
						defer f.Close()
						data, err := io.ReadAll(io.LimitReader(f, maxCookieUploadBytes+1))
						if err == nil && len(data) <= maxCookieUploadBytes {
							cookieBytes = data
							fileFound = true
							break
						}
					}
				}
			}
		}

		if !fileFound || len(cookieBytes) == 0 {
			response.Fail(w, r, apperr.CodeInvalidRequest, "No cookie file uploaded in multipart form.")
			return
		}
	} else {
		// Raw body upload
		r.Body = http.MaxBytesReader(w, r.Body, maxCookieUploadBytes+1)
		data, err := io.ReadAll(r.Body)
		if err != nil {
			var maxBytesErr *http.MaxBytesError
			if errors.As(err, &maxBytesErr) {
				response.Fail(w, r, apperr.CodeInvalidRequest, "Cookie file exceeds 1 MiB limit.")
				return
			}
			response.Fail(w, r, apperr.CodeInvalidRequest, "Failed to read cookie payload.")
			return
		}
		if len(data) > maxCookieUploadBytes {
			response.Fail(w, r, apperr.CodeInvalidRequest, "Cookie file exceeds 1 MiB limit.")
			return
		}
		cookieBytes = data
	}

	view, probeRes, err := h.deps.MediaSessions.UploadCookies(r.Context(), id, cookieBytes)
	if err != nil {
		response.Error(w, r, err)
		return
	}

	response.OK(w, r, map[string]any{
		"session": view,
		"probe":   probeRes,
	})
}

// ProbeMediaSession executes an explicit test probe against a media session.
func (h *Handlers) ProbeMediaSession(w http.ResponseWriter, r *http.Request) {
	if h.deps.MediaSessions == nil {
		response.Fail(w, r, apperr.CodeInternal, "Media session service not configured.")
		return
	}

	id := chi.URLParam(r, "id")
	if id == "" {
		response.Fail(w, r, apperr.CodeInvalidRequest, "Session ID is required.")
		return
	}

	probeRes, view, err := h.deps.MediaSessions.ProbeSession(r.Context(), id)
	if err != nil {
		response.Error(w, r, err)
		return
	}

	response.OK(w, r, map[string]any{
		"probe":   probeRes,
		"session": view,
	})
}

// UpdateMediaSession updates session metadata (name, enabled).
func (h *Handlers) UpdateMediaSession(w http.ResponseWriter, r *http.Request) {
	if h.deps.MediaSessions == nil {
		response.Fail(w, r, apperr.CodeInternal, "Media session service not configured.")
		return
	}

	id := chi.URLParam(r, "id")
	if id == "" {
		response.Fail(w, r, apperr.CodeInvalidRequest, "Session ID is required.")
		return
	}

	var req mediasession.UpdateSessionRequest
	if err := decodeJSON(r, &req); err != nil {
		response.Error(w, r, err)
		return
	}

	updated, err := h.deps.MediaSessions.UpdateSession(r.Context(), id, req)
	if err != nil {
		response.Error(w, r, err)
		return
	}

	response.OK(w, r, updated)
}

// DeleteMediaSession deletes a media session.
func (h *Handlers) DeleteMediaSession(w http.ResponseWriter, r *http.Request) {
	if h.deps.MediaSessions == nil {
		response.Fail(w, r, apperr.CodeInternal, "Media session service not configured.")
		return
	}

	id := chi.URLParam(r, "id")
	if id == "" {
		response.Fail(w, r, apperr.CodeInvalidRequest, "Session ID is required.")
		return
	}

	if err := h.deps.MediaSessions.DeleteSession(r.Context(), id); err != nil {
		response.Error(w, r, err)
		return
	}

	response.NoContent(w)
}
