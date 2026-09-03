package library

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"ytdm/backend/internal/music"
	"ytdm/backend/internal/storage"
)

type mockLyricsResolver struct {
	resolved *music.Lyrics
	err      error
	calls    int
}

func (m *mockLyricsResolver) Resolve(_ context.Context, _ music.Track, _ string) (*music.Lyrics, error) {
	m.calls++
	return m.resolved, m.err
}

func setupLyricsTestService(t *testing.T, resolver *mockLyricsResolver) (*Service, string, *mockCatalog, *mockFiles) {
	t.Helper()
	root := t.TempDir()
	lib, err := storage.NewLibrary(root)
	if err != nil {
		t.Fatal(err)
	}

	catalog := newMockCatalog()
	files := newMockFiles()
	jobMgr := &mockJobs{unfinishedMap: make(map[string]bool)}
	prober := &mockProber{}
	tagger := &mockTagger{}
	broker := &mockBroker{}

	svc, err := NewService(ServiceOptions{
		Library:     lib,
		Catalog:     catalog,
		Files:       files,
		Jobs:        jobMgr,
		Prober:      prober,
		Tagger:      tagger,
		Broker:      broker,
		Lyrics:      resolver,
		Concurrency: 2,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		svc.Stop()
	})

	return svc, root, catalog, files
}

func TestRefreshTrackLyrics_ProtectsExistingLRC(t *testing.T) {
	resolver := &mockLyricsResolver{
		resolved: &music.Lyrics{
			Provider:  "genius",
			PlainText: "new genius lyrics that should not overwrite",
			Synced:    false,
		},
	}
	svc, root, catalog, files := setupLyricsTestService(t, resolver)
	ctx := context.Background()

	// 1. Create artist/album dir and audio file with existing .lrc sidecar
	relDir := filepath.Join("Artist A", "Album 1")
	fullDir := filepath.Join(root, relDir)
	if err := os.MkdirAll(fullDir, 0755); err != nil {
		t.Fatal(err)
	}

	audioRel := filepath.Join(relDir, "01 - Track One.opus")
	audioFull := filepath.Join(root, audioRel)
	if err := os.WriteFile(audioFull, []byte("fake audio"), 0644); err != nil {
		t.Fatal(err)
	}

	lrcRel := filepath.Join(relDir, "01 - Track One.lrc")
	lrcFull := filepath.Join(root, lrcRel)
	originalLRC := "[00:01.00]Original Synced Lyric Line"
	if err := os.WriteFile(lrcFull, []byte(originalLRC), 0644); err != nil {
		t.Fatal(err)
	}

	trackID := "track-lrc-1"
	catalog.tracks[trackID] = music.Track{
		ID:          trackID,
		Title:       "Track One",
		Artists:     []string{"Artist A"},
		LyricsState: music.LyricsAvailableSynced,
	}
	files.files[audioRel] = music.File{
		ID:      "file-1",
		TrackID: trackID,
		Path:    audioRel,
	}

	// 2. Call RefreshTrackLyrics
	res, err := svc.RefreshTrackLyrics(ctx, trackID)
	if err != nil {
		t.Fatalf("RefreshTrackLyrics: %v", err)
	}

	// Resolver must NOT have been called because existing LRC is protected
	if resolver.calls != 0 {
		t.Errorf("expected 0 resolver calls, got %d", resolver.calls)
	}

	// Verify file content on disk is unchanged
	data, err := os.ReadFile(lrcFull)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != originalLRC {
		t.Errorf("expected original LRC preserved, got %q", string(data))
	}
	if res.State != music.LyricsAvailableSynced {
		t.Errorf("expected state 'available_synced', got %s", res.State)
	}
}

func TestRefreshTrackLyrics_ProtectsExistingTXT(t *testing.T) {
	resolver := &mockLyricsResolver{
		resolved: &music.Lyrics{
			Provider:  "genius",
			PlainText: "should not overwrite",
			Synced:    false,
		},
	}
	svc, root, catalog, files := setupLyricsTestService(t, resolver)
	ctx := context.Background()

	relDir := filepath.Join("Artist B", "Album 1")
	fullDir := filepath.Join(root, relDir)
	if err := os.MkdirAll(fullDir, 0755); err != nil {
		t.Fatal(err)
	}

	audioRel := filepath.Join(relDir, "01 - Track Two.opus")
	audioFull := filepath.Join(root, audioRel)
	if err := os.WriteFile(audioFull, []byte("fake audio"), 0644); err != nil {
		t.Fatal(err)
	}

	txtRel := filepath.Join(relDir, "01 - Track Two.txt")
	txtFull := filepath.Join(root, txtRel)
	originalTXT := "Existing plain text lyrics line."
	if err := os.WriteFile(txtFull, []byte(originalTXT), 0644); err != nil {
		t.Fatal(err)
	}

	trackID := "track-txt-1"
	catalog.tracks[trackID] = music.Track{
		ID:          trackID,
		Title:       "Track Two",
		Artists:     []string{"Artist B"},
		LyricsState: music.LyricsAvailablePlain,
	}
	files.files[audioRel] = music.File{
		ID:      "file-2",
		TrackID: trackID,
		Path:    audioRel,
	}

	res, err := svc.RefreshTrackLyrics(ctx, trackID)
	if err != nil {
		t.Fatalf("RefreshTrackLyrics: %v", err)
	}

	if resolver.calls != 0 {
		t.Errorf("expected 0 resolver calls, got %d", resolver.calls)
	}

	data, err := os.ReadFile(txtFull)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != originalTXT {
		t.Errorf("expected original TXT preserved, got %q", string(data))
	}
	if res.State != music.LyricsAvailablePlain {
		t.Errorf("expected state 'available_plain', got %s", res.State)
	}
}

func TestRefreshTrackLyrics_WritesTXTForGeniusPlainLyrics(t *testing.T) {
	geniusLyrics := "This is a plain lyrics text from Genius provider.\nNo timestamps.\n"
	resolver := &mockLyricsResolver{
		resolved: &music.Lyrics{
			Provider:  "genius",
			PlainText: geniusLyrics,
			Synced:    false,
		},
	}
	svc, root, catalog, files := setupLyricsTestService(t, resolver)
	ctx := context.Background()

	relDir := filepath.Join("Artist C", "Album 1")
	fullDir := filepath.Join(root, relDir)
	if err := os.MkdirAll(fullDir, 0755); err != nil {
		t.Fatal(err)
	}

	audioRel := filepath.Join(relDir, "01 - Track Three.opus")
	audioFull := filepath.Join(root, audioRel)
	if err := os.WriteFile(audioFull, []byte("fake audio"), 0644); err != nil {
		t.Fatal(err)
	}

	trackID := "track-genius-1"
	catalog.tracks[trackID] = music.Track{
		ID:          trackID,
		Title:       "Track Three",
		Artists:     []string{"Artist C"},
		LyricsState: music.LyricsNotFound,
	}
	files.files[audioRel] = music.File{
		ID:      "file-3",
		TrackID: trackID,
		Path:    audioRel,
	}

	res, err := svc.RefreshTrackLyrics(ctx, trackID)
	if err != nil {
		t.Fatalf("RefreshTrackLyrics: %v", err)
	}

	if resolver.calls != 1 {
		t.Errorf("expected 1 resolver call, got %d", resolver.calls)
	}
	if res.State != music.LyricsAvailablePlain {
		t.Errorf("expected state 'available_plain', got %s", res.State)
	}
	if res.Provider != "genius" {
		t.Errorf("expected provider 'genius', got %s", res.Provider)
	}
	if res.Synced {
		t.Errorf("expected Synced=false, got true")
	}

	// Verify .txt sidecar exists and .lrc sidecar does NOT exist
	txtFull := filepath.Join(root, filepath.Join(relDir, "01 - Track Three.txt"))
	lrcFull := filepath.Join(root, filepath.Join(relDir, "01 - Track Three.lrc"))

	if _, err := os.Stat(lrcFull); !os.IsNotExist(err) {
		t.Errorf(".lrc file must NOT exist for plain lyrics")
	}

	data, err := os.ReadFile(txtFull)
	if err != nil {
		t.Fatalf("expected .txt file to exist: %v", err)
	}
	if string(data) != geniusLyrics {
		t.Errorf("expected .txt content %q, got %q", geniusLyrics, string(data))
	}
}

func TestPreviewBackfillLyrics_StatsBreakdown(t *testing.T) {
	svc, _, catalog, _ := setupLyricsTestService(t, &mockLyricsResolver{})
	ctx := context.Background()

	now := time.Now().UTC()
	old := now.Add(-30 * 24 * time.Hour)
	recent := now.Add(-1 * 24 * time.Hour)

	catalog.tracks["t1"] = music.Track{ID: "t1", LyricsState: music.LyricsAvailableSynced}
	catalog.tracks["t2"] = music.Track{ID: "t2", LyricsState: music.LyricsAvailablePlain}
	catalog.tracks["t3"] = music.Track{ID: "t3", LyricsState: music.LyricsInstrumental}
	catalog.tracks["t4"] = music.Track{ID: "t4", LyricsState: music.LyricsNotFound, LyricsCheckedAt: &old}
	catalog.tracks["t5"] = music.Track{ID: "t5", LyricsState: music.LyricsNotFound, LyricsCheckedAt: &recent}
	catalog.tracks["t6"] = music.Track{ID: "t6", LyricsState: music.LyricsUnknown} // nil checked_at

	preview, err := svc.PreviewBackfillLyrics(ctx)
	if err != nil {
		t.Fatalf("PreviewBackfillLyrics: %v", err)
	}

	if preview.TracksScanned != 6 {
		t.Errorf("TracksScanned = %d, want 6", preview.TracksScanned)
	}
	if preview.AlreadyLRC != 1 {
		t.Errorf("AlreadyLRC = %d, want 1", preview.AlreadyLRC)
	}
	if preview.AlreadyTXT != 1 {
		t.Errorf("AlreadyTXT = %d, want 1", preview.AlreadyTXT)
	}
	if preview.Instrumental != 1 {
		t.Errorf("Instrumental = %d, want 1", preview.Instrumental)
	}
	if preview.Missing != 3 {
		t.Errorf("Missing = %d, want 3", preview.Missing)
	}
	if preview.Eligible != 2 {
		t.Errorf("Eligible = %d, want 2 (t4 old + t6 nil)", preview.Eligible)
	}
}
