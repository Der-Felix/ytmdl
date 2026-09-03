package library

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"ytdm/backend/internal/apperr"
	"ytdm/backend/internal/lyrics"
	"ytdm/backend/internal/music"
)

type countingResolver struct {
	mu            sync.Mutex
	current       int
	maxConcurrent int
	result        *music.Lyrics
	err           error
	blockUntil    chan struct{}
}

func (c *countingResolver) Resolve(ctx context.Context, _ music.Track, _ string) (*music.Lyrics, error) {
	c.mu.Lock()
	c.current++
	if c.current > c.maxConcurrent {
		c.maxConcurrent = c.current
	}
	c.mu.Unlock()

	if c.blockUntil != nil {
		select {
		case <-c.blockUntil:
		case <-ctx.Done():
			c.mu.Lock()
			c.current--
			c.mu.Unlock()
			return nil, ctx.Err()
		}
	}

	defer func() {
		c.mu.Lock()
		c.current--
		c.mu.Unlock()
	}()

	if c.err != nil {
		return nil, c.err
	}
	return c.result, nil
}

func seedTrackWithFile(t *testing.T, catalog *mockCatalog, files *mockFiles, relPath string) string {
	t.Helper()
	trackID := music.NewID()
	track := music.Track{
		ID:          trackID,
		Title:       filepath.Base(relPath),
		Artists:     []string{"Test Artist"},
		Album:       "Test Album",
		AlbumArtist: "Test Artist",
		TrackNumber: 1,
		LyricsState: music.LyricsUnknown,
	}
	catalog.tracks[trackID] = track
	files.files[relPath] = music.File{
		ID:        music.NewID(),
		TrackID:   trackID,
		Path:      relPath,
		Codec:     "opus",
		Container: "ogg",
	}
	return trackID
}

func TestBackfillProcessesCandidatesSequentially(t *testing.T) {
	svc, root, catalog, files, _, _, _ := setupTestService(t)
	resolver := &countingResolver{result: &music.Lyrics{Provider: "lrclib", PlainText: "line"}}
	svc.SetLyricsResolver(resolver)

	for i := 0; i < 3; i++ {
		rel := fmt.Sprintf("X/2001 - B/0%d - A.opus", i+1)
		abs := filepath.Join(root, filepath.FromSlash(rel))
		_ = os.MkdirAll(filepath.Dir(abs), 0o755)
		_ = os.WriteFile(abs, []byte("x"), 0o644)
		seedTrackWithFile(t, catalog, files, rel)
	}

	result, err := svc.BackfillLyrics(context.Background())
	if err != nil {
		t.Fatalf("BackfillLyrics: %v", err)
	}
	if result.Processed != 3 || result.Written != 3 {
		t.Fatalf("result = %+v, want 3 processed and 3 written", result)
	}
	if resolver.maxConcurrent > 1 {
		t.Errorf("max concurrency = %d; LRCLIB requires sequential requests", resolver.maxConcurrent)
	}
}

func TestBackfillIsSingleFlight(t *testing.T) {
	svc, root, catalog, files, _, _, _ := setupTestService(t)
	rel := "X/2001 - B/01 - A.opus"
	abs := filepath.Join(root, filepath.FromSlash(rel))
	_ = os.MkdirAll(filepath.Dir(abs), 0o755)
	_ = os.WriteFile(abs, []byte("x"), 0o644)
	seedTrackWithFile(t, catalog, files, rel)

	blockChan := make(chan struct{})
	defer close(blockChan)

	svc.SetLyricsResolver(&countingResolver{blockUntil: blockChan})

	go func() { _, _ = svc.BackfillLyrics(context.Background()) }()

	// Wait for backfill to become active
	for i := 0; i < 50; i++ {
		if status := svc.BackfillStatusOf(); status != nil && status.Status == BackfillRunning {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	if _, err := svc.BackfillLyrics(context.Background()); err == nil ||
		apperr.CodeOf(err) != apperr.CodeAlreadyExists {
		t.Fatalf("a second run must be refused, got %v", err)
	}
}

func TestBackfillAbortsOnRepeatedRateLimit(t *testing.T) {
	svc, root, catalog, files, _, _, _ := setupTestService(t)
	rateLimitErr := lyrics.NewRateLimitError("lrclib", 1*time.Millisecond)
	svc.SetLyricsResolver(&countingResolver{err: rateLimitErr})

	abs := filepath.Join(root, "X", "01 - A.opus")
	_ = os.MkdirAll(filepath.Dir(abs), 0o755)
	_ = os.WriteFile(abs, []byte("x"), 0o644)
	seedTrackWithFile(t, catalog, files, "X/01 - A.opus")

	result, err := svc.BackfillLyrics(context.Background())
	if err != nil {
		t.Fatalf("a rate limit must end the run cleanly, not error out: %v", err)
	}
	if result.Status != BackfillFailed || len(result.Warnings) == 0 {
		t.Fatalf("result = %+v, want a failed run with a warning", result)
	}
}

// Test G: Server shutdown (app context cancellation / Stop) stops the run cleanly without leaks.
func TestBackfillServerShutdownStopsCleanly(t *testing.T) {
	appCtx, cancelApp := context.WithCancel(context.Background())
	svc, root, catalog, files, _, _, _ := setupTestServiceWithLifecycle(t, appCtx)

	for i := 0; i < 5; i++ {
		rel := fmt.Sprintf("X/2001 - B/0%d - A.opus", i+1)
		abs := filepath.Join(root, filepath.FromSlash(rel))
		_ = os.MkdirAll(filepath.Dir(abs), 0o755)
		_ = os.WriteFile(abs, []byte("x"), 0o644)
		seedTrackWithFile(t, catalog, files, rel)
	}

	blockChan := make(chan struct{})
	defer close(blockChan)
	svc.SetLyricsResolver(&countingResolver{
		blockUntil: blockChan,
	})

	res, err := svc.StartBackfillLyrics()
	if err != nil {
		t.Fatalf("StartBackfillLyrics: %v", err)
	}
	if res.Status != BackfillRunning {
		t.Fatalf("expected running status, got %s", res.Status)
	}

	// Trigger shutdown
	cancelApp()
	svc.Stop() // Must wait and return cleanly

	status := svc.BackfillStatusOf()
	if status == nil {
		t.Fatal("expected status, got nil")
	}
	if status.Status != BackfillFailed {
		t.Fatalf("expected status 'failed', got %s", status.Status)
	}

	// After shutdown, StartBackfillLyrics must reject new runs with CodeShuttingDown
	if _, err := svc.StartBackfillLyrics(); err == nil || apperr.CodeOf(err) != apperr.CodeShuttingDown {
		t.Fatalf("expected CodeShuttingDown after stop, got %v", err)
	}
}

// Test H: Concurrent reads of BackfillStatusOf while updates are written under -race
func TestBackfillStatusConcurrentRace(t *testing.T) {
	svc, root, catalog, files, _, _, _ := setupTestService(t)

	for i := 0; i < 20; i++ {
		rel := fmt.Sprintf("X/2001 - B/0%d - A.opus", i+1)
		abs := filepath.Join(root, filepath.FromSlash(rel))
		_ = os.MkdirAll(filepath.Dir(abs), 0o755)
		_ = os.WriteFile(abs, []byte("x"), 0o644)
		seedTrackWithFile(t, catalog, files, rel)
	}

	svc.SetLyricsResolver(&countingResolver{
		result: &music.Lyrics{Provider: "lrclib", Synced: true, LRC: "[00:01.00] text", PlainText: "text"},
	})

	_, err := svc.StartBackfillLyrics()
	if err != nil {
		t.Fatalf("StartBackfillLyrics: %v", err)
	}

	// Spawn multiple concurrent readers
	var wg sync.WaitGroup
	for r := 0; r < 10; r++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 50; j++ {
				st := svc.BackfillStatusOf()
				if st != nil {
					_ = st.Status
					_ = len(st.Warnings)
					_ = st.Processed
				}
				time.Sleep(1 * time.Millisecond)
			}
		}()
	}

	wg.Wait()
	svc.Stop()
}

// Test J: Transient Provider Error (e.g. 503 / network failure) does NOT set not_found
// and does NOT set a 14-day cooldown timestamp.
func TestBackfillTransientErrorDoesNotSetCooldown(t *testing.T) {
	svc, root, catalog, files, _, _, _ := setupTestService(t)

	rel := "Artist/Album/01 - Transient.opus"
	abs := filepath.Join(root, filepath.FromSlash(rel))
	_ = os.MkdirAll(filepath.Dir(abs), 0o755)
	_ = os.WriteFile(abs, []byte("x"), 0o644)
	trackID := seedTrackWithFile(t, catalog, files, rel)

	// Resolver returns transient error (HTTP 503 Provider Unavailable)
	transientErr := apperr.New(apperr.CodeProviderUnavailable, "upstream 503 Service Unavailable")
	svc.SetLyricsResolver(&countingResolver{err: transientErr})

	result, err := svc.BackfillLyrics(context.Background())
	if err != nil {
		t.Fatalf("BackfillLyrics: %v", err)
	}

	if result.Status != BackfillCompleted {
		t.Fatalf("expected completed run, got %s", result.Status)
	}
	if len(result.Warnings) == 0 {
		t.Fatalf("expected warning for transient error, got 0")
	}

	// Verify the track in catalog was NOT marked not_found and NO checked_at was set
	trk, ok := catalog.tracks[trackID]
	if !ok {
		t.Fatalf("track %s not found in catalog", trackID)
	}
	if trk.LyricsState != music.LyricsUnknown {
		t.Fatalf("expected LyricsUnknown, got %v", trk.LyricsState)
	}
	if trk.LyricsCheckedAt != nil {
		t.Fatalf("expected LyricsCheckedAt to be nil, got %v", trk.LyricsCheckedAt)
	}

	// Verify track is STILL returned as a candidate for the next backfill
	candidates, err := catalog.ListTracksNeedingLyrics(context.Background(),
		time.Now().UTC().Add(-BackfillCooldown), 10)
	if err != nil {
		t.Fatalf("ListTracksNeedingLyrics: %v", err)
	}
	found := false
	for _, c := range candidates {
		if c.Track.ID == trackID {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("track %s must still be a candidate because transient errors do not trigger 14-day cooldown", trackID)
	}
}
