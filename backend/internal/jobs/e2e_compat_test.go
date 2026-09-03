package jobs

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"ytdm/backend/internal/metadata"
	"ytdm/backend/internal/music"
	"ytdm/backend/internal/provider"
	"ytdm/backend/internal/storage"
)

type recordedCatalog struct {
	fakeCatalog
	stored []music.LibraryEntry
}

func (r *recordedCatalog) PersistDownload(_ context.Context, entry music.LibraryEntry, _ int) (music.StoredEntry, error) {
	r.stored = append(r.stored, entry)
	var artistID, releaseID string
	if entry.Artist != nil {
		artistID = "artist-" + entry.Artist.Name
	}
	if entry.Release != nil {
		releaseID = "release-" + entry.Release.Title
	}
	return music.StoredEntry{
		ArtistID:  artistID,
		ReleaseID: releaseID,
		TrackID:   "track-" + entry.Track.Title,
		File: music.File{
			ID:   "file-" + entry.Track.Title,
			Path: entry.File.Path,
		},
	}, nil
}

// TestE2ECompatibilityMatrixSingleDisc verifies the complete single-disc placement,
// cover art writing, and LRC sidecar creation matching Plex, Jellyfin, and Emby specifications.
func TestE2ECompatibilityMatrixSingleDisc(t *testing.T) {
	catalog := &recordedCatalog{}
	files := &fakeFiles{byPath: make(map[string]*music.File)}
	m, root := newPlaceManager(t, catalog, files)
	m.lyrics = &fakeLyrics{result: &music.Lyrics{
		Provider:  "lrclib",
		Synced:    true,
		LRC:       "[00:01.00] When you're weary\n[00:04.00] Feeling small",
		PlainText: "When you're weary\nFeeling small",
	}}

	release := music.Release{
		Title:       "Bridge Over Troubled Water",
		AlbumArtist: "Simon & Garfunkel",
		Artists:     []string{"Simon & Garfunkel"},
		ReleaseType: music.ReleaseAlbum,
		Year:        1970,
	}

	track := music.Track{
		Title:       "Bridge Over Troubled Water",
		Artists:     []string{"Simon & Garfunkel"},
		Album:       "Bridge Over Troubled Water",
		AlbumArtist: "Simon & Garfunkel",
		TrackNumber: 1,
		TrackTotal:  11,
		DiscNumber:  1,
		DiscTotal:   1,
		DurationMS:  295000,
	}

	artwork := &metadata.Artwork{
		Data:   []byte("fake-jpeg-cover-data"),
		MIME:   "image/jpeg",
		Width:  500,
		Height: 500,
	}

	download := aDownload(t, "opus-audio-data")

	storedFile, err := m.place(context.Background(), release, track, download, artwork, provider.MediaSource{ID: "video-123"})
	if err != nil {
		t.Fatalf("place: %v", err)
	}

	track = m.attachLyrics(context.Background(), track, filepath.Join(root, storedFile.Path), "video-123", m.logger)
	_, err = m.persist(context.Background(), track, release, provider.MediaSource{ID: "video-123"}, storedFile)
	if err != nil {
		t.Fatalf("persist: %v", err)
	}

	expectedRelDir := filepath.Join("Simon & Garfunkel", "1970 - Bridge Over Troubled Water")
	expectedAudioRel := filepath.Join(expectedRelDir, "01 - Bridge Over Troubled Water.opus")

	if storedFile.Path != expectedAudioRel {
		t.Fatalf("file path = %q, want %q", storedFile.Path, expectedAudioRel)
	}

	// 1. Check audio file exists
	fullAudioPath := filepath.Join(root, expectedAudioRel)
	if _, err := os.Stat(fullAudioPath); err != nil {
		t.Fatalf("audio file does not exist on disk: %v", err)
	}

	// 2. Check cover file exists as cover.jpg
	fullCoverPath := filepath.Join(root, expectedRelDir, "cover.jpg")
	coverData, err := os.ReadFile(fullCoverPath)
	if err != nil {
		t.Fatalf("cover.jpg was not written: %v", err)
	}
	if string(coverData) != "fake-jpeg-cover-data" {
		t.Fatalf("cover data mismatch: %q", string(coverData))
	}

	// 3. Check sidecar .lrc exists and has identical basename
	fullLrcPath := storage.SidecarPathFor(fullAudioPath, ".lrc")
	lrcData, err := os.ReadFile(fullLrcPath)
	if err != nil {
		t.Fatalf(".lrc sidecar was not written: %v", err)
	}
	if string(lrcData) != "[00:01.00] When you're weary\n[00:04.00] Feeling small" {
		t.Fatalf("lrc content mismatch: %q", string(lrcData))
	}

	// 4. Verify catalog record
	if len(catalog.stored) != 1 {
		t.Fatalf("catalog stored entries = %d, want 1", len(catalog.stored))
	}
	entry := catalog.stored[0]
	if entry.Track.LyricsState != music.LyricsAvailableSynced {
		t.Errorf("lyrics state = %q, want available_synced", entry.Track.LyricsState)
	}
	if entry.Track.LyricsProvider != "lrclib" {
		t.Errorf("lyrics provider = %q, want lrclib", entry.Track.LyricsProvider)
	}
}

// TestE2ECompatibilityMatrixMultiDisc verifies that multi-disc releases are placed
// in a flat album folder using the DNN track prefix (e.g. 101, 201) without disc subdirectories.
func TestE2ECompatibilityMatrixMultiDisc(t *testing.T) {
	catalog := &recordedCatalog{}
	files := &fakeFiles{byPath: make(map[string]*music.File)}
	m, root := newPlaceManager(t, catalog, files)
	m.lyrics = &fakeLyrics{result: &music.Lyrics{
		Provider:  "ytmusic",
		Synced:    false,
		PlainText: "Plain text lyrics",
	}}

	release := music.Release{
		Title:       "The Beatles",
		AlbumArtist: "The Beatles",
		Artists:     []string{"The Beatles"},
		ReleaseType: music.ReleaseAlbum,
		Year:        1968,
	}

	// Disc 1, Track 1 -> 101
	trackD1 := music.Track{
		Title:       "Back in the USSR",
		Artists:     []string{"The Beatles"},
		Album:       "The Beatles",
		AlbumArtist: "The Beatles",
		TrackNumber: 1,
		TrackTotal:  17,
		DiscNumber:  1,
		DiscTotal:   2,
		DurationMS:  163000,
	}

	// Disc 2, Track 1 -> 201
	trackD2 := music.Track{
		Title:       "Birthday",
		Artists:     []string{"The Beatles"},
		Album:       "The Beatles",
		AlbumArtist: "The Beatles",
		TrackNumber: 1,
		TrackTotal:  13,
		DiscNumber:  2,
		DiscTotal:   2,
		DurationMS:  162000,
	}

	file1, err := m.place(context.Background(), release, trackD1, aDownload(t, "d1-audio"), nil, provider.MediaSource{ID: "v1"})
	if err != nil {
		t.Fatalf("place disc 1: %v", err)
	}
	trackD1 = m.attachLyrics(context.Background(), trackD1, filepath.Join(root, file1.Path), "v1", m.logger)

	file2, err := m.place(context.Background(), release, trackD2, aDownload(t, "d2-audio"), nil, provider.MediaSource{ID: "v2"})
	if err != nil {
		t.Fatalf("place disc 2: %v", err)
	}
	trackD2 = m.attachLyrics(context.Background(), trackD2, filepath.Join(root, file2.Path), "v2", m.logger)

	expectedRelDir := filepath.Join("The Beatles", "1968 - The Beatles")
	expectedD1 := filepath.Join(expectedRelDir, "101 - Back in the USSR.opus")
	expectedD2 := filepath.Join(expectedRelDir, "201 - Birthday.opus")

	if file1.Path != expectedD1 {
		t.Errorf("file 1 = %q, want %q", file1.Path, expectedD1)
	}
	if file2.Path != expectedD2 {
		t.Errorf("file 2 = %q, want %q", file2.Path, expectedD2)
	}

	// Verify no disc subdirectories exist
	entries, err := os.ReadDir(filepath.Join(root, expectedRelDir))
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if entry.IsDir() {
			t.Errorf("unexpected directory in album folder: %s", entry.Name())
		}
	}

	// Verify .txt sidecars for plain lyrics
	sidecar1 := filepath.Join(root, expectedRelDir, "101 - Back in the USSR.txt")
	sidecar2 := filepath.Join(root, expectedRelDir, "201 - Birthday.txt")
	if _, err := os.Stat(sidecar1); err != nil {
		t.Errorf("sidecar 1 missing: %v", err)
	}
	if _, err := os.Stat(sidecar2); err != nil {
		t.Errorf("sidecar 2 missing: %v", err)
	}
}

// TestE2ECompatibilityMatrixCompilation verifies compilation folder naming rules.
func TestE2ECompatibilityMatrixCompilation(t *testing.T) {
	catalog := &recordedCatalog{}
	files := &fakeFiles{byPath: make(map[string]*music.File)}
	m, root := newPlaceManager(t, catalog, files)

	release := music.Release{
		Title:       "Top Hits 2024",
		AlbumArtist: "Various Artists",
		Artists:     []string{"Various Artists"},
		ReleaseType: music.ReleaseCompilation,
		Year:        2024,
	}

	track := music.Track{
		Title:       "Hit Track",
		Artists:     []string{"Artist Alpha", "Artist Beta"},
		Album:       "Top Hits 2024",
		AlbumArtist: "Various Artists",
		TrackNumber: 1,
		TrackTotal:  20,
		DiscNumber:  1,
		DiscTotal:   1,
		DurationMS:  180000,
	}

	file, err := m.place(context.Background(), release, track, aDownload(t, "audio"), nil, provider.MediaSource{})
	if err != nil {
		t.Fatalf("place compilation: %v", err)
	}

	expectedRelDir := filepath.Join("Various Artists", "2024 - Top Hits 2024 [Compilation]")
	expectedAudio := filepath.Join(expectedRelDir, "01 - Hit Track.opus")

	if file.Path != expectedAudio {
		t.Errorf("compilation file path = %q, want %q", file.Path, expectedAudio)
	}

	if _, err := os.Stat(filepath.Join(root, expectedAudio)); err != nil {
		t.Fatalf("file not found on disk: %v", err)
	}
}
