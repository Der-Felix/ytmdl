package storage

import (
	"os"
	"path/filepath"
	"testing"

	"ytdm/backend/internal/apperr"
)

func TestStagingManager_ItemDirAndConfinement(t *testing.T) {
	tmp := t.TempDir()
	mgr, err := NewStagingManager(tmp, 0, 0)
	if err != nil {
		t.Fatalf("NewStagingManager: %v", err)
	}

	// Valid item ID
	dir, err := mgr.EnsureItemDir("item-uuid-1234")
	if err != nil {
		t.Fatalf("EnsureItemDir: %v", err)
	}
	if dir != filepath.Join(tmp, "item-uuid-1234") {
		t.Fatalf("unexpected dir: %s", dir)
	}

	// Path escape attempt with ..
	_, err = mgr.ItemDir("../escaped")
	if err == nil {
		t.Fatal("expected error on path traversal, got nil")
	}

	// Path escape attempt with slash
	_, err = mgr.ItemDir("foo/bar")
	if err == nil {
		t.Fatal("expected error on path with slash, got nil")
	}
}

func TestStagingManager_MetadataAndChecksum(t *testing.T) {
	tmp := t.TempDir()
	mgr, err := NewStagingManager(tmp, 0, 0)
	if err != nil {
		t.Fatalf("NewStagingManager: %v", err)
	}

	itemID := "item-meta-test"
	dir, err := mgr.EnsureItemDir(itemID)
	if err != nil {
		t.Fatalf("EnsureItemDir: %v", err)
	}

	// Create test file
	audioFile := filepath.Join(dir, "audio.opus")
	testData := []byte("test-audio-content-for-checksum")
	if err := os.WriteFile(audioFile, testData, 0o644); err != nil {
		t.Fatalf("write test audio: %v", err)
	}

	hash, size, err := ComputeChecksum(audioFile)
	if err != nil {
		t.Fatalf("ComputeChecksum: %v", err)
	}
	if size != int64(len(testData)) {
		t.Fatalf("got size %d, want %d", size, len(testData))
	}
	if hash == "" {
		t.Fatal("expected non-empty hash")
	}

	// Save and load metadata
	meta := StagingMeta{
		ItemID:       itemID,
		StagedRel:    "audio.opus",
		StagedSize:   size,
		StagedSHA256: hash,
	}
	if err := mgr.SaveMeta(itemID, meta); err != nil {
		t.Fatalf("SaveMeta: %v", err)
	}

	loaded, err := mgr.LoadMeta(itemID)
	if err != nil {
		t.Fatalf("LoadMeta: %v", err)
	}
	if loaded.StagedSHA256 != hash || loaded.StagedSize != size {
		t.Fatalf("loaded meta mismatch: %+v vs %+v", loaded, meta)
	}

	// Reset corrupted audio
	if err := mgr.ResetCorruptedAudio(itemID); err != nil {
		t.Fatalf("ResetCorruptedAudio: %v", err)
	}
	if _, err := os.Stat(audioFile); !os.IsNotExist(err) {
		t.Fatal("expected audio.opus to be removed by ResetCorruptedAudio")
	}
	// meta.json remains
	if _, err := os.Stat(filepath.Join(dir, "meta.json")); err != nil {
		t.Fatal("expected meta.json to remain")
	}

	// Full cleanup
	if err := mgr.CleanupItem(itemID); err != nil {
		t.Fatalf("CleanupItem: %v", err)
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Fatal("expected item dir to be deleted on cleanup")
	}
}

func TestStagingManager_QuotaAndSpaceChecks(t *testing.T) {
	tmp := t.TempDir()
	// Set 100 bytes quota
	mgr, err := NewStagingManager(tmp, 0, 100)
	if err != nil {
		t.Fatalf("NewStagingManager: %v", err)
	}

	dir, _ := mgr.EnsureItemDir("quota-item")
	// Write 50 bytes (under quota)
	_ = os.WriteFile(filepath.Join(dir, "file.bin"), make([]byte, 50), 0o644)
	if err := mgr.CheckSpace(); err != nil {
		t.Fatalf("expected CheckSpace to pass with 50 bytes: %v", err)
	}

	// Write 100 bytes more (total 150 > 100 max)
	_ = os.WriteFile(filepath.Join(dir, "file2.bin"), make([]byte, 100), 0o644)
	err = mgr.CheckSpace()
	if err == nil {
		t.Fatal("expected CheckSpace to fail when exceeding quota")
	}
	if apperr.CodeOf(err) != apperr.CodeStagingLowSpace {
		t.Fatalf("expected CodeStagingLowSpace, got %v", apperr.CodeOf(err))
	}
}

func TestStagingManager_CountPartials(t *testing.T) {
	tmp := t.TempDir()
	mgr, _ := NewStagingManager(tmp, 0, 0)

	dir1, _ := mgr.EnsureItemDir("item-1")
	dir2, _ := mgr.EnsureItemDir("item-2")
	_ = os.WriteFile(filepath.Join(dir1, "source.opus.part"), []byte("partial"), 0o644)
	_ = os.WriteFile(filepath.Join(dir2, "audio.opus"), []byte("complete"), 0o644)

	partials, err := mgr.CountPartials()
	if err != nil {
		t.Fatalf("CountPartials: %v", err)
	}
	if partials != 1 {
		t.Fatalf("got %d partials, want 1", partials)
	}
}
