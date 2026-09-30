package api

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"ytdm/backend/internal/api/handlers"
	"ytdm/backend/internal/auth"
	"ytdm/backend/internal/mediasession"
)

func TestRequestBodyLimit(t *testing.T) {
	payload := strings.Repeat("x", mediasession.MaxCookieUploadBodySize+1)
	for _, tc := range []struct {
		name, method, path string
		size               int
		chunked            bool
		want               int
	}{
		{"admin upload", "POST", "/api/v1/admin/media-sessions/session/cookies", 25 << 20, false, 204},
		{"upload alias", "POST", "/api/v1/media-sessions/session/cookies", 25 << 20, false, 204},
		{"chunked upload", "POST", "/api/v1/admin/media-sessions/session/cookies", 25 << 20, true, 204},
		{"multipart framing allowance", "POST", "/api/v1/admin/media-sessions/session/cookies", 26 << 20, false, 204},
		{"oversized upload", "POST", "/api/v1/admin/media-sessions/session/cookies", (26 << 20) + 1, false, 400},
		{"oversized chunked upload", "POST", "/api/v1/admin/media-sessions/session/cookies", (26 << 20) + 1, true, 400},
		{"ordinary request", "POST", "/api/v1/admin/settings", (1 << 20) + 1, false, 400},
		{"probe request", "POST", "/api/v1/admin/media-sessions/session/probe", (1 << 20) + 1, false, 400},
		{"wrong method", "PATCH", "/api/v1/admin/media-sessions/session/cookies", (1 << 20) + 1, false, 400},
		{"extra path segment", "POST", "/api/v1/admin/media-sessions/session/cookies/extra", (1 << 20) + 1, false, 400},
	} {
		t.Run(tc.name, func(t *testing.T) {
			next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if _, err := io.Copy(io.Discard, r.Body); err != nil {
					w.WriteHeader(http.StatusBadRequest)
					return
				}
				w.WriteHeader(http.StatusNoContent)
			})
			req := httptest.NewRequest(tc.method, tc.path, strings.NewReader(payload[:tc.size]))
			if tc.chunked {
				req.ContentLength = -1
			}
			rec := httptest.NewRecorder()
			requestBodyLimit(1<<20)(next).ServeHTTP(rec, req)
			if rec.Code != tc.want {
				t.Fatalf("status = %d, want %d", rec.Code, tc.want)
			}
		})
	}
}

// Exercise the real router without a database or provider calls: an allowed
// body must reach authentication, while oversized bodies fail before it.
func TestRouterAllowsLargeCookieUploadsToReachAuthentication(t *testing.T) {
	router, err := NewRouter(RouterOptions{
		Handlers: handlers.NewForTest(handlers.Deps{}),
		Auth:     &auth.Service{},
		Logger:   slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	if err != nil {
		t.Fatal(err)
	}
	payload := strings.Repeat("x", 25<<20)
	for _, path := range []string{
		"/api/v1/admin/media-sessions/session/cookies",
		"/api/v1/media-sessions/session/cookies",
	} {
		req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(payload))
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("status = %d, want authentication rejection instead of a body-limit error", rec.Code)
		}
	}
}
