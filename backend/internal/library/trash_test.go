package library

import (
	"os"
	"path/filepath"
	"testing"
	"ytdm/backend/internal/database/repository"
)

func TestTrashMoveJournalHandlesInterruptedHardlinkAndRejectsCollision(t *testing.T) {
	rootPath := t.TempDir()
	stored := filepath.Join(TrashDirName, "songs", "fixture", "audio.opus")
	if err := os.MkdirAll(filepath.Dir(filepath.Join(rootPath, stored)), 0700); err != nil {
		t.Fatal(err)
	}
	original := "audio.opus"
	if err := os.WriteFile(filepath.Join(rootPath, original), []byte("original"), 0600); err != nil {
		t.Fatal(err)
	}
	st, _ := os.Stat(filepath.Join(rootPath, original))
	root, err := os.OpenRoot(rootPath)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	e := &repository.TrashEntry{Moves: []repository.TrashMove{{Original: original, Stored: stored, Size: st.Size(), MTimeNS: st.ModTime().UnixNano()}}}
	if err = root.Link(original, stored); err != nil {
		t.Fatal(err)
	}
	if err = moveTrashPaths(root, e, false); err != nil {
		t.Fatal("double name recovery", err)
	}
	if _, err = root.Stat(stored); !os.IsNotExist(err) {
		t.Fatal("extra journal link retained")
	}
	if err = moveTrashPaths(root, e, true); err != nil {
		t.Fatal(err)
	}
	if err = root.WriteFile(original, []byte("different audio"), 0600); err != nil {
		t.Fatal(err)
	}
	if err = moveTrashPaths(root, e, false); err == nil {
		t.Fatal("collision accepted")
	}
	raw, _ := root.ReadFile(original)
	if string(raw) != "different audio" {
		t.Fatal("destination overwritten")
	}
	raw, _ = root.ReadFile(stored)
	if string(raw) != "original" {
		t.Fatal("archive changed")
	}
}
