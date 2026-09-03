package handlers

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"ytdm/backend/internal/api/middleware"
	"ytdm/backend/internal/auth"
	"ytdm/backend/internal/library"
	"ytdm/backend/internal/music"
	"ytdm/backend/internal/storage"
)

func TestAudioMIMEType(t *testing.T) {
	tests := []struct {
		ext      string
		expected string
	}{
		{".opus", "audio/ogg"},
		{".ogg", "audio/ogg"},
		{".m4a", "audio/mp4"},
		{".mp3", "audio/mpeg"},
		{".flac", "audio/flac"},
		{".wav", "audio/wav"},
		{".OPUS", "audio/ogg"},
		{".M4A", "audio/mp4"},
		{".unknown", "application/octet-stream"},
		{"", "application/octet-stream"},
	}

	for _, tc := range tests {
		got := AudioMIMEType(tc.ext)
		if got != tc.expected {
			t.Errorf("AudioMIMEType(%q) = %q; want %q", tc.ext, got, tc.expected)
		}
	}
}

func setupStreamTestEnvironment(t *testing.T) (
	*chi.Mux,
	*library.Service,
	*mockLibFiles,
	*mockLibCatalog,
	string,
	*auth.User,
) {
	tempDir := t.TempDir()
	libStorage, err := storage.NewLibrary(tempDir)
	if err != nil {
		t.Fatalf("failed to create library storage: %v", err)
	}

	catalogMock := newMockLibCatalog()
	filesMock := newMockLibFiles()

	svc, err := library.NewService(library.ServiceOptions{
		Lifecycle: context.Background(),
		Library:   libStorage,
		Catalog:   catalogMock,
		Files:     filesMock,
	})
	if err != nil {
		t.Fatalf("failed to create library service: %v", err)
	}

	h := &Handlers{
		deps: Deps{
			LibraryService: svc,
		},
	}

	user := &auth.User{
		ID:       "user-1",
		Username: "felix",
		Role:     "admin",
	}

	r := chi.NewRouter()
	r.Route("/api/v1/library", func(lib chi.Router) {
		lib.Use(func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
				// Inject test user into context if auth header / cookie is present or default
				if req.Header.Get("X-Unauthenticated") != "true" {
					ctx := middleware.ContextWithUser(req.Context(), user)
					next.ServeHTTP(w, req.WithContext(ctx))
					return
				}
				next.ServeHTTP(w, req)
			})
		})
		lib.Use(func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
				if req.Header.Get("X-Unauthenticated") == "true" {
					w.WriteHeader(http.StatusUnauthorized)
					w.Write([]byte(`{"error":{"code":"UNAUTHENTICATED"}}`))
					return
				}
				next.ServeHTTP(w, req)
			})
		})

		lib.Get("/files/{id}/stream", h.StreamFile)
		lib.Head("/files/{id}/stream", h.StreamFile)
		lib.Get("/tracks/{id}/stream", h.StreamTrack)
		lib.Head("/tracks/{id}/stream", h.StreamTrack)
	})

	return r, svc, filesMock, catalogMock, tempDir, user
}

func TestStreamFile_FullAndRange(t *testing.T) {
	router, _, filesMock, catalogMock, libRoot, _ := setupStreamTestEnvironment(t)

	// Create test audio content
	payload := make([]byte, 1000)
	for i := range payload {
		payload[i] = byte(i % 256)
	}

	relPath := "Artist/2026 - Album/01 - Test.opus"
	absPath := filepath.Join(libRoot, relPath)
	if err := os.MkdirAll(filepath.Dir(absPath), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(absPath, payload, 0644); err != nil {
		t.Fatal(err)
	}

	testFile := music.File{
		ID:        "file-123",
		TrackID:   "track-456",
		Path:      relPath,
		SizeBytes: int64(len(payload)),
		Codec:     "opus",
		Container: "ogg",
		CreatedAt: time.Now().UTC(),
		UpdatedAt: time.Now().UTC(),
	}
	filesMock.files[relPath] = testFile

	testTrack := music.Track{
		ID:          "track-456",
		Title:       "Test Track",
		DurationMS:  120000,
		TrackNumber: 1,
	}
	catalogMock.tracks[testTrack.ID] = testTrack

	// 1. Full GET request
	t.Run("Full GET", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/library/files/file-123/stream", nil)
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200 OK; got %d: %s", rec.Code, rec.Body.String())
		}
		if rec.Header().Get("Content-Type") != "audio/ogg" {
			t.Errorf("expected Content-Type audio/ogg; got %q", rec.Header().Get("Content-Type"))
		}
		if rec.Header().Get("Accept-Ranges") != "bytes" {
			t.Errorf("expected Accept-Ranges bytes; got %q", rec.Header().Get("Accept-Ranges"))
		}
		if rec.Body.Len() != 1000 {
			t.Errorf("expected body length 1000; got %d", rec.Body.Len())
		}
		if !bytes.Equal(rec.Body.Bytes(), payload) {
			t.Errorf("stream body content mismatch")
		}
	})

	// 2. Range request: bytes=0-49 (first 50 bytes)
	t.Run("Range 0-49", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/library/files/file-123/stream", nil)
		req.Header.Set("Range", "bytes=0-49")
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)

		if rec.Code != http.StatusPartialContent {
			t.Fatalf("expected 206 Partial Content; got %d: %s", rec.Code, rec.Body.String())
		}
		if rec.Header().Get("Content-Range") != "bytes 0-49/1000" {
			t.Errorf("expected Content-Range 'bytes 0-49/1000'; got %q", rec.Header().Get("Content-Range"))
		}
		if rec.Header().Get("Content-Length") != "50" {
			t.Errorf("expected Content-Length 50; got %q", rec.Header().Get("Content-Length"))
		}
		if !bytes.Equal(rec.Body.Bytes(), payload[0:50]) {
			t.Errorf("range body content mismatch")
		}
	})

	// 3. Range request: bytes=500-999
	t.Run("Range 500-999", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/library/files/file-123/stream", nil)
		req.Header.Set("Range", "bytes=500-999")
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)

		if rec.Code != http.StatusPartialContent {
			t.Fatalf("expected 206 Partial Content; got %d: %s", rec.Code, rec.Body.String())
		}
		if rec.Header().Get("Content-Range") != "bytes 500-999/1000" {
			t.Errorf("expected Content-Range 'bytes 500-999/1000'; got %q", rec.Header().Get("Content-Range"))
		}
		if !bytes.Equal(rec.Body.Bytes(), payload[500:1000]) {
			t.Errorf("range body content mismatch")
		}
	})

	// 4. Suffix Range request: bytes=-100
	t.Run("Suffix Range -100", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/library/files/file-123/stream", nil)
		req.Header.Set("Range", "bytes=-100")
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)

		if rec.Code != http.StatusPartialContent {
			t.Fatalf("expected 206 Partial Content; got %d: %s", rec.Code, rec.Body.String())
		}
		if rec.Header().Get("Content-Range") != "bytes 900-999/1000" {
			t.Errorf("expected Content-Range 'bytes 900-999/1000'; got %q", rec.Header().Get("Content-Range"))
		}
		if !bytes.Equal(rec.Body.Bytes(), payload[900:1000]) {
			t.Errorf("suffix range body content mismatch")
		}
	})

	// 5. Invalid Range: bytes=5000-6000 -> 416
	t.Run("Invalid Range 416", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/library/files/file-123/stream", nil)
		req.Header.Set("Range", "bytes=5000-6000")
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)

		if rec.Code != http.StatusRequestedRangeNotSatisfiable {
			t.Fatalf("expected 416 Range Not Satisfiable; got %d", rec.Code)
		}
	})

	// 6. HEAD request
	t.Run("HEAD request", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodHead, "/api/v1/library/files/file-123/stream", nil)
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200 OK; got %d", rec.Code)
		}
		if rec.Header().Get("Content-Type") != "audio/ogg" {
			t.Errorf("expected Content-Type audio/ogg; got %q", rec.Header().Get("Content-Type"))
		}
		if rec.Header().Get("Content-Length") != "1000" {
			t.Errorf("expected Content-Length 1000; got %q", rec.Header().Get("Content-Length"))
		}
		if rec.Body.Len() != 0 {
			t.Errorf("expected empty body for HEAD; got %d bytes", rec.Body.Len())
		}
	})

	// 7. Track Stream: GET /api/v1/library/tracks/{id}/stream
	t.Run("Stream by Track ID", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/library/tracks/track-456/stream", nil)
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200 OK; got %d: %s", rec.Code, rec.Body.String())
		}
		if !bytes.Equal(rec.Body.Bytes(), payload) {
			t.Errorf("track stream body content mismatch")
		}
	})

	// 8. Unknown File ID -> 404
	t.Run("Unknown File ID 404", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/library/files/nonexistent/stream", nil)
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)

		if rec.Code != http.StatusNotFound {
			t.Fatalf("expected 404 Not Found; got %d", rec.Code)
		}
	})

	// 9. Unknown Track ID -> 404
	t.Run("Unknown Track ID 404", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/library/tracks/nonexistent/stream", nil)
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)

		if rec.Code != http.StatusNotFound {
			t.Fatalf("expected 404 Not Found; got %d", rec.Code)
		}
	})

	// 10. File in DB but removed from disk -> 404
	t.Run("Missing from disk 404", func(t *testing.T) {
		missingFile := music.File{
			ID:        "file-missing",
			TrackID:   "track-missing",
			Path:      "Artist/Album/02 - Missing.opus",
			SizeBytes: 500,
		}
		filesMock.files[missingFile.Path] = missingFile

		req := httptest.NewRequest(http.MethodGet, "/api/v1/library/files/file-missing/stream", nil)
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)

		if rec.Code != http.StatusNotFound {
			t.Fatalf("expected 404 Not Found; got %d", rec.Code)
		}
	})

	// 11. Path Traversal escape in DB record -> blocked
	t.Run("Path Traversal Blocked", func(t *testing.T) {
		escapeFile := music.File{
			ID:        "file-escape",
			Path:      "../../etc/passwd",
			SizeBytes: 50,
		}
		filesMock.files[escapeFile.Path] = escapeFile

		req := httptest.NewRequest(http.MethodGet, "/api/v1/library/files/file-escape/stream", nil)
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)

		if rec.Code == http.StatusOK {
			t.Fatalf("path traversal should be blocked; got 200 OK")
		}
	})

	// 12. Unauthenticated -> 401
	t.Run("Unauthenticated 401", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/library/files/file-123/stream", nil)
		req.Header.Set("X-Unauthenticated", "true")
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)

		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("expected 401 Unauthorized; got %d", rec.Code)
		}
	})
}
