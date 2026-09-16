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

	// A running download keeps its unfinished file in its attempt directory.
	dir3, _ := mgr.EnsureItemDir("item-3")
	attempt := filepath.Join(dir3, DownloadAttemptPrefix+"123")
	_ = os.MkdirAll(attempt, 0o755)
	_ = os.WriteFile(filepath.Join(attempt, "source.webm.part"), []byte("partial"), 0o644)
	// Other subdirectories are not attempts and are not searched.
	other := filepath.Join(dir2, "nested")
	_ = os.MkdirAll(other, 0o755)
	_ = os.WriteFile(filepath.Join(other, "x.part"), []byte("partial"), 0o644)

	partials, err = mgr.CountPartials()
	if err != nil {
		t.Fatalf("CountPartials: %v", err)
	}
	if partials != 2 {
		t.Fatalf("got %d partials, want 2", partials)
	}
}

// Only real directories directly below the root are item staging; a link or a
// file is not, and removing an item directory never follows a link or leaves
// the root.
func TestStagingManager_ItemDirsAreRemovedSafely(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	precious := filepath.Join(outside, "precious.opus")
	if err := os.WriteFile(precious, []byte("keep"), 0o644); err != nil {
		t.Fatal(err)
	}
	mgr, err := NewStagingManager(root, 0, 0)
	if err != nil {
		t.Fatal(err)
	}

	real, _ := mgr.EnsureItemDir("aaaa")
	// A link inside a real item directory is removed as a link.
	if err := os.Symlink(outside, filepath.Join(real, "link")); err != nil {
		t.Fatal(err)
	}
	// A link in place of an item directory is never treated as one.
	if err := os.Symlink(outside, filepath.Join(root, "bbbb")); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "cccc"), []byte("file"), 0o644); err != nil {
		t.Fatal(err)
	}

	names, err := mgr.ItemDirNames()
	if err != nil {
		t.Fatal(err)
	}
	if len(names) != 1 || names[0] != "aaaa" {
		t.Fatalf("item dirs = %v, want only the real directory", names)
	}

	if err := mgr.RemoveItemDir("bbbb"); err == nil {
		t.Fatal("a symbolic link was accepted as an item directory")
	}
	for _, bad := range []string{"", "..", "../x", "a/b", `a\b`} {
		if err := mgr.RemoveItemDir(bad); err == nil {
			t.Fatalf("id %q was accepted", bad)
		}
	}
	if err := mgr.RemoveItemDir("aaaa"); err != nil {
		t.Fatalf("remove: %v", err)
	}
	if _, err := os.Lstat(real); !os.IsNotExist(err) {
		t.Fatalf("the item directory still exists: %v", err)
	}
	if err := mgr.RemoveItemDir("aaaa"); err != nil {
		t.Fatalf("removing a missing directory: %v", err)
	}
	if data, err := os.ReadFile(precious); err != nil || string(data) != "keep" {
		t.Fatalf("a file outside the staging root was touched: %v", err)
	}
	if _, err := os.Lstat(filepath.Join(root, "bbbb")); err != nil {
		t.Fatalf("the link in the staging root was removed: %v", err)
	}
}
