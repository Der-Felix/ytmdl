package jobs

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"testing"

	"ytdm/backend/internal/apperr"
	"ytdm/backend/internal/downloader"
	"ytdm/backend/internal/lyrics"
	"ytdm/backend/internal/metadata"
	"ytdm/backend/internal/music"
	"ytdm/backend/internal/provider"
	"ytdm/backend/internal/storage"
)

// fakeCatalog answers the two questions the placement rule asks.
type fakeCatalog struct {
	Catalog
	known *music.Track
}

func (c *fakeCatalog) FindTrack(context.Context, music.Track, int) (*music.Track, error) {
	return c.known, nil
}

// fakeFiles answers who owns a library path.
type fakeFiles struct {
	byPath map[string]*music.File
	err    error
}

func (f *fakeFiles) ListByTrack(context.Context, string) ([]music.File, error) { return nil, nil }

func (f *fakeFiles) FindByPath(_ context.Context, path string) (*music.File, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.byPath[path], nil
}

type fakeLyrics struct {
	result *music.Lyrics
	err    error
	calls  int
	media  string
}

func (l *fakeLyrics) Resolve(_ context.Context, _ music.Track, mediaID string) (*music.Lyrics, error) {
	l.calls++
	l.media = mediaID
	return l.result, l.err
}

func newPlaceManager(t *testing.T, catalog Catalog, files FileStore) (*Manager, string) {
	t.Helper()
	root := t.TempDir()
	library, err := storage.NewLibrary(root)
	if err != nil {
		t.Fatalf("NewLibrary: %v", err)
	}
	m := &Manager{
		catalog:     catalog,
		files:       files,
		library:     library,
		logger:      slog.New(slog.NewTextHandler(io.Discard, nil)),
		toleranceMS: 2000,
	}
	m.writeCoverFile.Store(true)
	m.lyricsEnabled.Store(true)
	return m, root
}

func aRelease() music.Release {
	return music.Release{
		Title: "Album", AlbumArtist: "Artist", Artists: []string{"Artist"},
		ReleaseType: music.ReleaseAlbum, Year: 2001,
	}
}

func aWorkerTrack() music.Track {
	return music.Track{
		Title: "Song", Artists: []string{"Artist"}, Album: "Album", AlbumArtist: "Artist",
		TrackNumber: 1, TrackTotal: 1, DiscNumber: 1, DiscTotal: 1, DurationMS: 200000,
	}
}

func aDownload(t *testing.T, content string) *downloader.Result {
	t.Helper()
	path := filepath.Join(t.TempDir(), "download.opus")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write download: %v", err)
	}
	return &downloader.Result{Path: path}
}

// TestPlaceRefusesToOverwriteAForeignFile is the download-side half of the
// silent overwrite fix: a target that belongs to something else stops the item
// instead of costing the library a track.
func TestPlaceRefusesToOverwriteAForeignFile(t *testing.T) {
	m, root := newPlaceManager(t, &fakeCatalog{}, &fakeFiles{})

	target := filepath.Join(root, "Artist", "2001 - Album", "01 - Song.opus")
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(target, []byte("a different recording"), 0o644); err != nil {
		t.Fatal(err)
	}

	_, err := m.place(context.Background(), aRelease(), aWorkerTrack(),
		aDownload(t, "new audio"), nil, provider.MediaSource{})
	if apperr.CodeOf(err) != apperr.CodePathConflict {
		t.Fatalf("error code = %q, want %q", apperr.CodeOf(err), apperr.CodePathConflict)
	}
	data, readErr := os.ReadFile(target)
	if readErr != nil || string(data) != "a different recording" {
		t.Fatalf("the existing file was modified: %q, %v", data, readErr)
	}
}

// TestPlaceReplacesTheRecordingsOwnFile is the other side: a re-download of the
// same recording is allowed to replace its own file.
func TestPlaceReplacesTheRecordingsOwnFile(t *testing.T) {
	relPath := filepath.Join("Artist", "2001 - Album", "01 - Song.opus")
	catalog := &fakeCatalog{known: &music.Track{ID: "track-1"}}
	files := &fakeFiles{byPath: map[string]*music.File{
		relPath: {ID: "file-1", TrackID: "track-1", Path: relPath},
	}}
	m, root := newPlaceManager(t, catalog, files)

	target := filepath.Join(root, relPath)
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(target, []byte("old audio"), 0o644); err != nil {
		t.Fatal(err)
	}

	file, err := m.place(context.Background(), aRelease(), aWorkerTrack(),
		aDownload(t, "new audio"), nil, provider.MediaSource{})
	if err != nil {
		t.Fatalf("place: %v", err)
	}
	if file.Path != relPath {
		t.Errorf("stored path = %q, want %q", file.Path, relPath)
	}
	data, _ := os.ReadFile(target)
	if string(data) != "new audio" {
		t.Fatalf("the file was not replaced: %q", data)
	}
}

// TestPlaceRefusesAnUnregisteredFile: an orphan on disk is somebody's data the
// backend did not put there.
func TestPlaceRefusesAnUnregisteredFile(t *testing.T) {
	catalog := &fakeCatalog{known: &music.Track{ID: "track-1"}}
	m, root := newPlaceManager(t, catalog, &fakeFiles{})

	target := filepath.Join(root, "Artist", "2001 - Album", "01 - Song.opus")
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(target, []byte("orphan"), 0o644); err != nil {
		t.Fatal(err)
	}

	_, err := m.place(context.Background(), aRelease(), aWorkerTrack(),
		aDownload(t, "new"), nil, provider.MediaSource{})
	if apperr.CodeOf(err) != apperr.CodePathConflict {
		t.Fatalf("error code = %q, want %q", apperr.CodeOf(err), apperr.CodePathConflict)
	}
}

func TestPlaceRefusesAFileOfAnotherTrack(t *testing.T) {
	relPath := filepath.Join("Artist", "2001 - Album", "01 - Song.opus")
	catalog := &fakeCatalog{known: &music.Track{ID: "track-1"}}
	files := &fakeFiles{byPath: map[string]*music.File{
		relPath: {ID: "file-9", TrackID: "another-track", Path: relPath},
	}}
	m, root := newPlaceManager(t, catalog, files)

	target := filepath.Join(root, relPath)
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(target, []byte("another track"), 0o644); err != nil {
		t.Fatal(err)
	}

	_, err := m.place(context.Background(), aRelease(), aWorkerTrack(),
		aDownload(t, "new"), nil, provider.MediaSource{})
	if apperr.CodeOf(err) != apperr.CodePathConflict {
		t.Fatalf("error code = %q, want %q", apperr.CodeOf(err), apperr.CodePathConflict)
	}
	data, _ := os.ReadFile(target)
	if string(data) != "another track" {
		t.Fatalf("the other track's file was overwritten: %q", data)
	}
}

func TestPlaceWritesTheCoverUnderItsRealExtension(t *testing.T) {
	m, root := newPlaceManager(t, &fakeCatalog{}, &fakeFiles{})

	artwork := &metadata.Artwork{Data: []byte("png-bytes"), MIME: "image/png", Width: 4, Height: 4}
	if _, err := m.place(context.Background(), aRelease(), aWorkerTrack(),
		aDownload(t, "audio"), artwork, provider.MediaSource{}); err != nil {
		t.Fatalf("place: %v", err)
	}

	if _, err := os.Stat(filepath.Join(root, "Artist", "2001 - Album", "cover.png")); err != nil {
		t.Fatalf("cover.png was not written: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "Artist", "2001 - Album", "cover.jpg")); !os.IsNotExist(err) {
		t.Error("a PNG cover must not be written as cover.jpg")
	}
}

func TestPlaceKeepsAnExistingCover(t *testing.T) {
	m, root := newPlaceManager(t, &fakeCatalog{}, &fakeFiles{})
	dir := filepath.Join(root, "Artist", "2001 - Album")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "cover.jpg"), []byte("existing"), 0o644); err != nil {
		t.Fatal(err)
	}

	artwork := &metadata.Artwork{Data: []byte("png-bytes"), MIME: "image/png"}
	if _, err := m.place(context.Background(), aRelease(), aWorkerTrack(),
		aDownload(t, "audio"), artwork, provider.MediaSource{}); err != nil {
		t.Fatalf("place: %v", err)
	}
	data, _ := os.ReadFile(filepath.Join(dir, "cover.jpg"))
	if string(data) != "existing" {
		t.Fatalf("the existing cover was replaced: %q", data)
	}
	if _, err := os.Stat(filepath.Join(dir, "cover.png")); !os.IsNotExist(err) {
		t.Error("a second cover file was written next to the existing one")
	}
}

func TestAttachLyricsWritesTheSyncedSidecarAndRecordsTheState(t *testing.T) {
	m, root := newPlaceManager(t, &fakeCatalog{}, &fakeFiles{})
	m.lyrics = &fakeLyrics{result: &music.Lyrics{
		Provider: "lrclib", Synced: true, LRC: "[00:01.00]a", PlainText: "a",
	}}

	audio := filepath.Join(root, "Artist", "2001 - Album", "01 - Song.opus")
	if err := os.MkdirAll(filepath.Dir(audio), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(audio, []byte("audio"), 0o644); err != nil {
		t.Fatal(err)
	}

	got := m.attachLyrics(context.Background(), aWorkerTrack(), audio, "video-1", m.logger)
	if got.LyricsState != music.LyricsAvailableSynced {
		t.Fatalf("state = %q, want available_synced", got.LyricsState)
	}
	if got.LyricsProvider != "lrclib" || got.LyricsCheckedAt == nil {
		t.Fatalf("provenance = %q / %v", got.LyricsProvider, got.LyricsCheckedAt)
	}
	if _, err := os.Stat(storage.SidecarPathFor(audio, ".lrc")); err != nil {
		t.Fatalf("the .lrc sidecar was not written: %v", err)
	}
}

func TestAttachLyricsHandsTheMediaIDToTheResolver(t *testing.T) {
	m, root := newPlaceManager(t, &fakeCatalog{}, &fakeFiles{})
	resolver := &fakeLyrics{result: &music.Lyrics{Provider: "ytmusic", PlainText: "a"}}
	m.lyrics = resolver

	audio := filepath.Join(root, "01 - Song.opus")
	if err := os.WriteFile(audio, []byte("audio"), 0o644); err != nil {
		t.Fatal(err)
	}
	m.attachLyrics(context.Background(), aWorkerTrack(), audio, "video-42", m.logger)
	if resolver.media != "video-42" {
		t.Fatalf("media id = %q, want video-42", resolver.media)
	}
}

// TestAttachLyricsDefinitiveMissIsRecorded lets the cooldown start.
func TestAttachLyricsDefinitiveMissIsRecorded(t *testing.T) {
	m, root := newPlaceManager(t, &fakeCatalog{}, &fakeFiles{})
	m.lyrics = &fakeLyrics{err: apperr.Wrapf(apperr.CodeFileNotFound, lyrics.ErrNoLyrics, "none")}

	audio := filepath.Join(root, "01 - Song.opus")
	if err := os.WriteFile(audio, []byte("audio"), 0o644); err != nil {
		t.Fatal(err)
	}
	got := m.attachLyrics(context.Background(), aWorkerTrack(), audio, "v", m.logger)
	if got.LyricsState != music.LyricsNotFound || got.LyricsCheckedAt == nil {
		t.Fatalf("state = %q, checked = %v", got.LyricsState, got.LyricsCheckedAt)
	}
}

// TestAttachLyricsTransientFailureRecordsNothing is the state machine rule: an
// outage must not look like a definitive answer and must not start a cooldown.
func TestAttachLyricsTransientFailureRecordsNothing(t *testing.T) {
	m, root := newPlaceManager(t, &fakeCatalog{}, &fakeFiles{})
	m.lyrics = &fakeLyrics{err: apperr.Wrapf(apperr.CodeProviderUnavailable, lyrics.ErrLookupFailed, "down")}

	audio := filepath.Join(root, "01 - Song.opus")
	if err := os.WriteFile(audio, []byte("audio"), 0o644); err != nil {
		t.Fatal(err)
	}
	got := m.attachLyrics(context.Background(), aWorkerTrack(), audio, "v", m.logger)
	if got.LyricsState != "" {
		t.Fatalf("state = %q, want it left untouched", got.LyricsState)
	}
	if got.LyricsCheckedAt != nil {
		t.Fatal("a transient failure must not record a check timestamp")
	}
}

func TestAttachLyricsIsSkippedWhenDisabled(t *testing.T) {
	m, root := newPlaceManager(t, &fakeCatalog{}, &fakeFiles{})
	resolver := &fakeLyrics{result: &music.Lyrics{Provider: "lrclib", PlainText: "a"}}
	m.lyrics = resolver
	m.lyricsEnabled.Store(false)

	audio := filepath.Join(root, "01 - Song.opus")
	if err := os.WriteFile(audio, []byte("audio"), 0o644); err != nil {
		t.Fatal(err)
	}
	got := m.attachLyrics(context.Background(), aWorkerTrack(), audio, "v", m.logger)
	if resolver.calls != 0 {
		t.Error("the resolver must not be called when lyrics are switched off")
	}
	if got.LyricsState != "" {
		t.Errorf("state = %q", got.LyricsState)
	}
}

func TestAttachLyricsInstrumentalWritesNoSidecar(t *testing.T) {
	m, root := newPlaceManager(t, &fakeCatalog{}, &fakeFiles{})
	m.lyrics = &fakeLyrics{result: &music.Lyrics{Provider: "lrclib", Instrumental: true}}

	audio := filepath.Join(root, "01 - Song.opus")
	if err := os.WriteFile(audio, []byte("audio"), 0o644); err != nil {
		t.Fatal(err)
	}
	got := m.attachLyrics(context.Background(), aWorkerTrack(), audio, "v", m.logger)
	if got.LyricsState != music.LyricsInstrumental {
		t.Fatalf("state = %q", got.LyricsState)
	}
	for _, ext := range storage.LyricsExtensions() {
		if _, err := os.Stat(storage.SidecarPathFor(audio, ext)); !errors.Is(err, os.ErrNotExist) {
			t.Errorf("an instrumental track wrote a %s sidecar", ext)
		}
	}
}
