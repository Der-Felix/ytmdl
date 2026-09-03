package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"ytdm/backend/internal/api/middleware"
	"ytdm/backend/internal/api/response"
	"ytdm/backend/internal/apperr"
	"ytdm/backend/internal/library"
	"ytdm/backend/internal/music"
	"ytdm/backend/internal/storage"
)

type testEnvelope[T any] struct {
	Data T `json:"data"`
}

type testErrorEnvelope struct {
	Error response.Detail `json:"error"`
}

type mockBackfillResolver struct {
	resolveFn func(ctx context.Context, track music.Track, mediaID string) (*music.Lyrics, error)
}

func (m *mockBackfillResolver) Resolve(ctx context.Context, track music.Track, mediaID string) (*music.Lyrics, error) {
	if m.resolveFn != nil {
		return m.resolveFn(ctx, track, mediaID)
	}
	return &music.Lyrics{Provider: "lrclib", PlainText: "test"}, nil
}

func seedTestTrack(t *testing.T, catalog *mockLibCatalog, mockFiles *mockLibFiles, lib *storage.Library, title, relPath string) string {
	t.Helper()
	trackID := music.NewID()
	catalog.tracks[trackID] = music.Track{
		ID:          trackID,
		Title:       title,
		Artists:     []string{"Test Artist"},
		Album:       "Test Album",
		AlbumArtist: "Test Artist",
		TrackNumber: 1,
		LyricsState: music.LyricsUnknown,
	}
	absPath := filepath.Join(lib.Root(), filepath.FromSlash(relPath))
	if err := os.MkdirAll(filepath.Dir(absPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(absPath, []byte("opus_data"), 0o644); err != nil {
		t.Fatal(err)
	}
	mockFiles.files[relPath] = music.File{
		ID:        music.NewID(),
		TrackID:   trackID,
		Path:      relPath,
		Codec:     "opus",
		Container: "ogg",
	}
	return trackID
}

// Test A: POST /lyrics/backfill starts run and immediately returns 202 Accepted.
func TestBackfill_PostReturns202_AndStartsRun(t *testing.T) {
	_, libSvc, catalog, mockFiles, lib, router := setupLibraryHandlersWithMocks(t)
	seedTestTrack(t, catalog, mockFiles, lib, "Song 1", "Artist/2024 - Album/01 - Song 1.opus")

	unblock := make(chan struct{})
	defer close(unblock)
	libSvc.SetLyricsResolver(&mockBackfillResolver{
		resolveFn: func(ctx context.Context, _ music.Track, _ string) (*music.Lyrics, error) {
			<-unblock
			return &music.Lyrics{Provider: "lrclib", PlainText: "lyrics"}, nil
		},
	})

	req := httptest.NewRequest(http.MethodPost, "/api/v1/library/lyrics/backfill", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusAccepted {
		t.Fatalf("POST /lyrics/backfill returned status %d, want %d", rec.Code, http.StatusAccepted)
	}

	var env testEnvelope[library.BackfillResult]
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if env.Data.Status != library.BackfillRunning {
		t.Fatalf("expected status 'running', got %q", env.Data.Status)
	}
	if env.Data.Candidates != 1 {
		t.Fatalf("expected 1 candidate, got %d", env.Data.Candidates)
	}
}

// Test B & C: Request context cancellation (client disconnect / browser tab closed)
// must NOT abort the server-side backfill run.
func TestBackfill_DecoupledFromRequestContextCancellation(t *testing.T) {
	_, libSvc, catalog, mockFiles, lib, router := setupLibraryHandlersWithMocks(t)
	seedTestTrack(t, catalog, mockFiles, lib, "Song 1", "Artist/2024 - Album/01 - Song 1.opus")
	seedTestTrack(t, catalog, mockFiles, lib, "Song 2", "Artist/2024 - Album/02 - Song 2.opus")

	doneCh := make(chan struct{}, 2)
	libSvc.SetLyricsResolver(&mockBackfillResolver{
		resolveFn: func(_ context.Context, _ music.Track, _ string) (*music.Lyrics, error) {
			time.Sleep(20 * time.Millisecond)
			doneCh <- struct{}{}
			return &music.Lyrics{Provider: "lrclib", PlainText: "lyrics"}, nil
		},
	})

	reqCtx, cancelReq := context.WithCancel(context.Background())
	req := httptest.NewRequest(http.MethodPost, "/api/v1/library/lyrics/backfill", nil).WithContext(reqCtx)
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusAccepted {
		t.Fatalf("POST /lyrics/backfill returned status %d, want %d", rec.Code, http.StatusAccepted)
	}

	// Immediately cancel the client request context (simulating tab close / disconnect)
	cancelReq()

	// Wait for both tracks to be processed by background worker
	for i := 0; i < 2; i++ {
		select {
		case <-doneCh:
		case <-time.After(1 * time.Second):
			t.Fatalf("timeout waiting for background candidate %d to be processed", i+1)
		}
	}

	// Wait for backfill status to become completed
	var finalStatus *library.BackfillResult
	for i := 0; i < 50; i++ {
		finalStatus = libSvc.BackfillStatusOf()
		if finalStatus != nil && finalStatus.Status == library.BackfillCompleted {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	if finalStatus == nil || finalStatus.Status != library.BackfillCompleted {
		t.Fatalf("expected completed status, got %+v", finalStatus)
	}
	if finalStatus.Processed != 2 || finalStatus.Written != 2 {
		t.Fatalf("expected 2 processed and written, got %+v", finalStatus)
	}
}

// Test D: Short HTTP Request Timeout middleware does not abort the background backfill.
func TestBackfill_HttpRequestTimeoutDoesNotAbortBackfill(t *testing.T) {
	h, libSvc, catalog, mockFiles, lib, _ := setupLibraryHandlersWithMocks(t)
	seedTestTrack(t, catalog, mockFiles, lib, "Song 1", "Artist/2024 - Album/01 - Song 1.opus")

	doneCh := make(chan struct{}, 1)
	libSvc.SetLyricsResolver(&mockBackfillResolver{
		resolveFn: func(_ context.Context, _ music.Track, _ string) (*music.Lyrics, error) {
			// Processing takes 60ms, which is longer than the 20ms HTTP timeout
			time.Sleep(60 * time.Millisecond)
			doneCh <- struct{}{}
			return &music.Lyrics{Provider: "lrclib", PlainText: "lyrics"}, nil
		},
	})

	// Setup router with a 20ms request timeout middleware
	r := chi.NewRouter()
	r.Use(middleware.Timeout(20 * time.Millisecond))
	r.Post("/api/v1/library/lyrics/backfill", h.BackfillLyrics)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/library/lyrics/backfill", nil)
	rec := httptest.NewRecorder()
	routerStart := time.Now()
	r.ServeHTTP(rec, req)
	routerDuration := time.Since(routerStart)

	if rec.Code != http.StatusAccepted {
		t.Fatalf("POST /lyrics/backfill returned status %d, want %d", rec.Code, http.StatusAccepted)
	}
	if routerDuration > 15*time.Millisecond {
		t.Fatalf("POST /lyrics/backfill took %v, should return immediately", routerDuration)
	}

	// Verify the background run completes even after HTTP timeout duration has passed
	select {
	case <-doneCh:
	case <-time.After(1 * time.Second):
		t.Fatal("timeout waiting for background candidate to finish processing")
	}

	for i := 0; i < 50; i++ {
		status := libSvc.BackfillStatusOf()
		if status != nil && status.Status == library.BackfillCompleted {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("backfill was not completed; final status: %+v", libSvc.BackfillStatusOf())
}

// Test E: Second POST during active run returns HTTP 409 Conflict.
func TestBackfill_SecondPostReturnsConflict(t *testing.T) {
	_, libSvc, catalog, mockFiles, lib, router := setupLibraryHandlersWithMocks(t)
	seedTestTrack(t, catalog, mockFiles, lib, "Song 1", "Artist/2024 - Album/01 - Song 1.opus")

	unblock := make(chan struct{})
	defer close(unblock)
	libSvc.SetLyricsResolver(&mockBackfillResolver{
		resolveFn: func(_ context.Context, _ music.Track, _ string) (*music.Lyrics, error) {
			<-unblock
			return &music.Lyrics{Provider: "lrclib", PlainText: "lyrics"}, nil
		},
	})

	// First POST: returns 202
	req1 := httptest.NewRequest(http.MethodPost, "/api/v1/library/lyrics/backfill", nil)
	rec1 := httptest.NewRecorder()
	router.ServeHTTP(rec1, req1)
	if rec1.Code != http.StatusAccepted {
		t.Fatalf("first POST returned %d, want %d", rec1.Code, http.StatusAccepted)
	}

	// Second POST while first is still running: returns 409 Conflict
	req2 := httptest.NewRequest(http.MethodPost, "/api/v1/library/lyrics/backfill", nil)
	rec2 := httptest.NewRecorder()
	router.ServeHTTP(rec2, req2)
	if rec2.Code != http.StatusConflict {
		t.Fatalf("second POST returned %d, want %d", rec2.Code, http.StatusConflict)
	}

	var errEnv testErrorEnvelope
	if err := json.Unmarshal(rec2.Body.Bytes(), &errEnv); err != nil {
		t.Fatalf("failed to decode error envelope: %v", err)
	}
	if errEnv.Error.Code != string(apperr.CodeAlreadyExists) {
		t.Fatalf("expected error code %s, got %s", apperr.CodeAlreadyExists, errEnv.Error.Code)
	}
}

// Test F: Run completed -> new POST is allowed and starts a new run.
func TestBackfill_NewPostAllowedAfterCompletion(t *testing.T) {
	_, libSvc, catalog, mockFiles, lib, router := setupLibraryHandlersWithMocks(t)
	seedTestTrack(t, catalog, mockFiles, lib, "Song 1", "Artist/2024 - Album/01 - Song 1.opus")

	libSvc.SetLyricsResolver(&mockBackfillResolver{})

	// First run
	req1 := httptest.NewRequest(http.MethodPost, "/api/v1/library/lyrics/backfill", nil)
	rec1 := httptest.NewRecorder()
	router.ServeHTTP(rec1, req1)
	if rec1.Code != http.StatusAccepted {
		t.Fatalf("first POST returned %d, want %d", rec1.Code, http.StatusAccepted)
	}

	// Wait for completion
	for i := 0; i < 50; i++ {
		st := libSvc.BackfillStatusOf()
		if st != nil && st.Status == library.BackfillCompleted {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	// Second run after completion
	req2 := httptest.NewRequest(http.MethodPost, "/api/v1/library/lyrics/backfill", nil)
	rec2 := httptest.NewRecorder()
	router.ServeHTTP(rec2, req2)
	if rec2.Code != http.StatusAccepted {
		t.Fatalf("second POST after completion returned %d, want %d", rec2.Code, http.StatusAccepted)
	}
}

// Test I: Status endpoint GET /api/v1/library/lyrics/backfill returns progress and results.
func TestBackfill_StatusEndpointReturnsCorrectFields(t *testing.T) {
	_, libSvc, catalog, mockFiles, lib, router := setupLibraryHandlersWithMocks(t)

	// Initially idle
	reqGet0 := httptest.NewRequest(http.MethodGet, "/api/v1/library/lyrics/backfill", nil)
	recGet0 := httptest.NewRecorder()
	router.ServeHTTP(recGet0, reqGet0)
	if recGet0.Code != http.StatusOK {
		t.Fatalf("initial GET returned %d, want 200", recGet0.Code)
	}

	seedTestTrack(t, catalog, mockFiles, lib, "Synced Track", "Artist/2024 - Album/01 - Synced.opus")
	seedTestTrack(t, catalog, mockFiles, lib, "Plain Track", "Artist/2024 - Album/02 - Plain.opus")

	libSvc.SetLyricsResolver(&mockBackfillResolver{
		resolveFn: func(_ context.Context, trk music.Track, _ string) (*music.Lyrics, error) {
			if trk.Title == "Synced Track" {
				return &music.Lyrics{Provider: "lrclib", Synced: true, LRC: "[00:01.00] line", PlainText: "line"}, nil
			}
			return &music.Lyrics{Provider: "lrclib", PlainText: "plain line"}, nil
		},
	})

	reqPost := httptest.NewRequest(http.MethodPost, "/api/v1/library/lyrics/backfill", nil)
	recPost := httptest.NewRecorder()
	router.ServeHTTP(recPost, reqPost)
	if recPost.Code != http.StatusAccepted {
		t.Fatalf("POST returned %d, want %d", recPost.Code, http.StatusAccepted)
	}

	// Wait for completion
	for i := 0; i < 50; i++ {
		st := libSvc.BackfillStatusOf()
		if st != nil && st.Status == library.BackfillCompleted {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	reqGet := httptest.NewRequest(http.MethodGet, "/api/v1/library/lyrics/backfill", nil)
	recGet := httptest.NewRecorder()
	router.ServeHTTP(recGet, reqGet)
	if recGet.Code != http.StatusOK {
		t.Fatalf("GET returned status %d", recGet.Code)
	}

	var env testEnvelope[library.BackfillResult]
	if err := json.Unmarshal(recGet.Body.Bytes(), &env); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	res := env.Data
	if res.Status != library.BackfillCompleted {
		t.Fatalf("expected status 'completed', got %q", res.Status)
	}
	if res.Candidates != 2 || res.Processed != 2 || res.Written != 2 {
		t.Fatalf("expected 2 candidates, processed and written, got %+v", res)
	}
	if res.Synced != 1 || res.Plain != 1 {
		t.Fatalf("expected 1 synced and 1 plain, got synced=%d, plain=%d", res.Synced, res.Plain)
	}
	if res.StartedAt.IsZero() || res.FinishedAt == nil || res.FinishedAt.IsZero() {
		t.Fatalf("timestamps not set: started=%v, finished=%v", res.StartedAt, res.FinishedAt)
	}
}
