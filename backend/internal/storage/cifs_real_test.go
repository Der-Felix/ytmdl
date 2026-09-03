package storage_test

import (
	"os"
	"path/filepath"
	"testing"

	"ytdm/backend/internal/music"
	"ytdm/backend/internal/storage"
)

func TestCIFSRealStorage(t *testing.T) {
	cifsRoot := os.Getenv("CIFS_TEST_DIR")
	if cifsRoot == "" {
		t.Skip("CIFS_TEST_DIR not set, skipping real CIFS mount test")
	}

	lib, err := storage.NewLibrary(cifsRoot)
	if err != nil {
		t.Fatalf("NewLibrary on CIFS: %v", err)
	}

	testRelDir := filepath.Join(cifsRoot, "RealCIFSArtist", "2026 - TestAlbum")
	testData := []byte("REAL-CIFS-COVER-DATA")
	coverPath, err := lib.WriteCover(testRelDir, ".jpg", testData)
	if err != nil {
		t.Fatalf("WriteCover on CIFS failed: %v", err)
	}
	if filepath.Base(coverPath) != "cover.jpg" {
		t.Errorf("unexpected coverPath: %s", coverPath)
	}

	testAudio := filepath.Join(testRelDir, "01 - Song.opus")
	writeFile(t, testAudio, "audio-data")

	lyrics := music.Lyrics{
		Synced: true,
		LRC:    "[00:01.00] Line 1\n[00:05.00] Line 2\n",
	}
	lyricsPath, err := lib.WriteLyrics(testAudio, lyrics)
	if err != nil {
		t.Fatalf("WriteLyrics on CIFS failed: %v", err)
	}
	if filepath.Ext(lyricsPath) != ".lrc" {
		t.Errorf("unexpected lyricsPath: %s", lyricsPath)
	}

	// Clean up
	_ = os.RemoveAll(filepath.Join(cifsRoot, "RealCIFSArtist"))
}

func TestCIFSRealNoReplace(t *testing.T) {
	cifsRoot := os.Getenv("CIFS_TEST_DIR")
	if cifsRoot == "" {
		t.Skip("CIFS_TEST_DIR not set, skipping real CIFS mount test")
	}

	lib, err := storage.NewLibrary(cifsRoot)
	if err != nil {
		t.Fatalf("NewLibrary on CIFS: %v", err)
	}

	destination := filepath.Join(cifsRoot, "NoReplaceArtist", "2026 - Album", "01 - Track.opus")
	originalContent := "CRITICAL-ORIGINAL-TRACK-DATA"
	writeFile(t, destination, originalContent)

	source := filepath.Join(t.TempDir(), "new_download.opus")
	writeFile(t, source, "NEW-UNWANTED-OVERWRITE-DATA")

	// Attempt Place over existing destination
	placeErr := lib.Place(source, destination)
	if placeErr == nil {
		t.Fatal("expected Place on existing destination to fail")
	}

	// Verify original file is 100% intact
	afterContent := readFile(t, destination)
	if afterContent != originalContent {
		t.Fatalf("original file corrupted! got %q, want %q", afterContent, originalContent)
	}

	// Clean up
	_ = os.RemoveAll(filepath.Join(cifsRoot, "NoReplaceArtist"))
}

func TestCIFSRealGuard(t *testing.T) {
	cifsRoot := os.Getenv("CIFS_TEST_DIR")
	if cifsRoot == "" {
		t.Skip("CIFS_TEST_DIR not set, skipping real CIFS mount test")
	}

	markerFile := filepath.Join(cifsRoot, ".ytmdl-storage-id")
	originalMarkerData, err := os.ReadFile(markerFile)
	if err != nil {
		t.Fatalf("read marker: %v", err)
	}

	// 1. Guard with valid marker passes
	guard := storage.NewStorageGuard(cifsRoot, string(originalMarkerData), 0)
	status, err := guard.ValidateIdentity()
	if err != nil || status != storage.GuardVerified {
		t.Fatalf("ValidateIdentity with correct marker failed: status=%s, err=%v", status, err)
	}

	// 2. Guard with mismatched marker fails
	badGuard := storage.NewStorageGuard(cifsRoot, "wrong-marker-id-12345", 0)
	status, err = badGuard.ValidateIdentity()
	if err == nil || status != storage.GuardMismatch {
		t.Fatalf("expected ValidateIdentity with mismatched marker to fail: status=%s, err=%v", status, err)
	}
}
