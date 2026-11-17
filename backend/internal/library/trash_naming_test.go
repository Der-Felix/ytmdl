package library

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"ytdm/backend/internal/database/repository"
	"ytdm/backend/internal/music"
	"ytdm/backend/internal/storage"
)

// trashCatalog adds a journal of one trash entry to the mock catalog.
type trashCatalog struct {
	*mockCatalog
	entry     *repository.TrashEntry
	restored  *repository.TrashEntry
	retargets int
}

func (c *trashCatalog) PrepareTrash(context.Context, string, string, []repository.TrashMove) (*repository.TrashEntry, error) {
	return nil, nil
}
func (c *trashCatalog) CommitTrash(context.Context, string) error { return nil }
func (c *trashCatalog) GetTrash(context.Context, string) (*repository.TrashEntry, error) {
	cp := *c.entry
	return &cp, nil
}
func (c *trashCatalog) ListTrash(context.Context, int, bool) ([]repository.TrashEntry, error) {
	return nil, nil
}
func (c *trashCatalog) SetTrashState(_ context.Context, _, _, to string) error {
	c.entry.State = to
	return nil
}
func (c *trashCatalog) ForgetTrash(context.Context, string) error { return nil }
func (c *trashCatalog) CheckTrashRestore(context.Context, *repository.TrashEntry) error {
	return nil
}
func (c *trashCatalog) RestoreTrashMetadata(_ context.Context, e *repository.TrashEntry) error {
	c.restored = e
	return nil
}
func (c *trashCatalog) RetargetTrash(_ context.Context, _ string, moves []repository.TrashMove, payload json.RawMessage) error {
	c.retargets++
	c.entry.Moves = moves
	c.entry.Payload = payload
	return nil
}

// trashFixture puts one song of AC/DC, trashed under the legacy scheme, into
// the trash.
func trashFixture(t *testing.T, keepOldFolder bool) (*Service, string, *trashCatalog, string, string) {
	t.Helper()
	root := t.TempDir()
	lib, err := storage.NewLibrary(root)
	if err != nil {
		t.Fatal(err)
	}
	base := newMockCatalog()
	release := music.Release{ID: "rel", AlbumArtist: "AC/DC", Title: "Who? Live", Year: 1980}
	base.releases[release.ID] = release
	catalog := &trashCatalog{mockCatalog: base}
	svc, err := NewService(ServiceOptions{Library: lib, Catalog: catalog, Files: newMockFiles()})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(svc.Stop)

	track := music.Track{Title: "Song: One", TrackNumber: 2, DiscNumber: 2, DiscTotal: 2}
	oldRel := storage.TrackRelPathFor(storage.NamingSchemeLegacy, release, track, ".opus")
	newRel := storage.TrackRelPathFor(storage.CurrentNamingScheme, release, track, ".opus")

	storedDir := filepath.Join(TrashDirName, "songs", "entry")
	if err := os.MkdirAll(filepath.Join(root, storedDir), 0o700); err != nil {
		t.Fatal(err)
	}
	var moves []repository.TrashMove
	for i, item := range []struct{ original, content string }{
		{oldRel, "audio"},
		{storage.SidecarPathFor(oldRel, ".lrc"), "lyrics"},
	} {
		stored := filepath.Join(storedDir, []string{"000.opus", "001.lrc"}[i])
		if err := os.WriteFile(filepath.Join(root, stored), []byte(item.content), 0o600); err != nil {
			t.Fatal(err)
		}
		st, _ := os.Stat(filepath.Join(root, stored))
		moves = append(moves, repository.TrashMove{Original: item.original, Stored: stored, Size: st.Size(), MTimeNS: st.ModTime().UnixNano()})
	}
	if keepOldFolder {
		if err := os.MkdirAll(filepath.Join(root, filepath.Dir(oldRel)), 0o755); err != nil {
			t.Fatal(err)
		}
	}

	pathJSON, _ := json.Marshal(oldRel)
	payload := `{"track":{"id":"t1","release_id":"rel","title":"Song: One","track_number":2,"disc_number":2,"disc_total":2},` +
		`"files":[{"id":"f1","track_id":"t1","path":` + string(pathJSON) + `,"size_bytes":12345678901,"bitrate_kbps":160.5}],"sources":[]}`
	catalog.entry = &repository.TrashEntry{ID: "entry", TrackID: "t1", State: "ready", Payload: json.RawMessage(payload), Moves: moves}
	return svc, root, catalog, oldRel, newRel
}

func TestTrashRestoreUsesTheCurrentNamingScheme(t *testing.T) {
	svc, root, catalog, oldRel, newRel := trashFixture(t, false)
	settings := newMemSettings()
	_ = settings.Set(context.Background(), NamingSchemeSettingKey, "2")
	svc.namingSettings = settings

	if err := svc.RestoreTrash(context.Background(), "entry"); err != nil {
		t.Fatalf("RestoreTrash: %v", err)
	}
	if raw, err := os.ReadFile(filepath.Join(root, newRel)); err != nil || string(raw) != "audio" {
		t.Fatalf("audio not restored at %q: %v", newRel, err)
	}
	if raw, err := os.ReadFile(filepath.Join(root, storage.SidecarPathFor(newRel, ".lrc"))); err != nil || string(raw) != "lyrics" {
		t.Fatalf("lyrics not restored next to the audio: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, filepath.Dir(oldRel))); !os.IsNotExist(err) {
		t.Fatal("the old folder was recreated")
	}
	if catalog.retargets != 1 || catalog.restored == nil {
		t.Fatal("the new targets were not saved before the restore")
	}
	restored := string(catalog.restored.Payload)
	pathJSON, _ := json.Marshal(newRel)
	if !strings.Contains(restored, `"path":`+string(pathJSON)) {
		t.Fatalf("payload still points at the old path: %s", restored)
	}
	if !strings.Contains(restored, `"size_bytes":12345678901`) || !strings.Contains(restored, `"bitrate_kbps":160.5`) {
		t.Fatalf("payload numbers changed: %s", restored)
	}
}

func TestTrashRestoreKeepsTheOldNameWhileTheReleaseIsStillThere(t *testing.T) {
	// The library has not moved yet (the migration is switched off): the song
	// goes back beside its siblings, and the migration takes it along later.
	svc, root, catalog, oldRel, _ := trashFixture(t, true)
	svc.namingSettings = newMemSettings()

	if err := svc.RestoreTrash(context.Background(), "entry"); err != nil {
		t.Fatalf("RestoreTrash: %v", err)
	}
	if raw, err := os.ReadFile(filepath.Join(root, oldRel)); err != nil || string(raw) != "audio" {
		t.Fatalf("audio not restored at its old path: %v", err)
	}
	if catalog.retargets != 0 {
		t.Fatal("the entry was retargeted although the old folder exists")
	}
}

func TestTrashRestoreRecreatesAMissingFolderWithoutAMarker(t *testing.T) {
	// No marker yet, but the old folder is gone: the restore still uses the
	// new name rather than recreating a folder of the old scheme.
	svc, root, _, _, newRel := trashFixture(t, false)
	if err := svc.RestoreTrash(context.Background(), "entry"); err != nil {
		t.Fatalf("RestoreTrash: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, newRel)); err != nil {
		t.Fatalf("audio not restored at %q: %v", newRel, err)
	}
}
