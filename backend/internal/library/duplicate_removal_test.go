package library

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"ytdm/backend/internal/music"
)

type failingDuplicateFiles struct {
	*mockFiles
	failID string
}

func (f *failingDuplicateFiles) Delete(ctx context.Context, id string) error {
	if id == f.failID {
		return errors.New("fixture DB failure")
	}
	return f.mockFiles.Delete(ctx, id)
}

func duplicateRemovalFixture(t *testing.T) (*Service, string, *mockCatalog, *mockFiles, *mockJobs) {
	t.Helper()
	s, root, c, f, j, _, _ := setupTestService(t)
	for _, id := range []string{"keep", "lose1", "lose2"} {
		c.tracks[id] = music.Track{ID: id, SourceID: "source-" + id}
		path := filepath.Join(root, id+".opus")
		if err := os.WriteFile(path, []byte("fixture audio"), 0600); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, id+".lrc"), []byte("fixture lyrics"), 0600); err != nil {
			t.Fatal(err)
		}
		f.files[path] = music.File{ID: "file-" + id, TrackID: id, Path: path}
	}
	return s, root, c, f, j
}
func TestDuplicateRemovalPreservesWinnerAndReturnsCompletedIDs(t *testing.T) {
	s, root, c, f, _ := duplicateRemovalFixture(t)
	ctx := context.Background()
	validated := false
	s.files = &failingDuplicateFiles{mockFiles: f, failID: "file-lose2"}
	deleted, failed, err := s.RemoveDuplicateTracks(ctx, "keep", []string{"lose1", "lose2"}, func() error { validated = true; return nil })
	if !validated || err == nil || failed != "lose2" || len(deleted) != 1 || deleted[0] != "lose1" {
		t.Fatalf("partial result %v %s %v", deleted, failed, err)
	}
	for _, name := range []string{"keep.opus", "keep.lrc"} {
		if _, err := os.Stat(filepath.Join(root, name)); err != nil {
			t.Fatal("winner touched", err)
		}
	}
	if _, ok := c.tracks["keep"]; !ok {
		t.Fatal("winner removed")
	}
	if _, ok := c.tracks["lose1"]; ok {
		t.Fatal("completed track retained")
	}
	if _, ok := c.tracks["lose2"]; !ok {
		t.Fatal("failed track claimed removed")
	}
}
func TestDuplicateRemovalPreflightsEntireSet(t *testing.T) {
	for _, scenario := range []string{"winner", "repeat", "missing", "job", "release-job", "symlink", "outside", "directory", "alias", "stale", "locked"} {
		t.Run(scenario, func(t *testing.T) {
			s, root, c, f, j := duplicateRemovalFixture(t)
			ids := []string{"lose1", "lose2"}
			var validation func() error
			path := filepath.Join(root, "lose2.opus")
			switch scenario {
			case "winner":
				ids = append(ids, "keep")
			case "repeat":
				ids = append(ids, "lose1")
			case "missing":
				delete(c.tracks, "lose2")
			case "job":
				j.unfinishedMap["source-lose2"] = true
			case "release-job":
				tr := c.tracks["lose2"]
				tr.ReleaseID = "release"
				c.tracks["lose2"] = tr
				c.releases["release"] = music.Release{ID: "release", SourceID: "release-source"}
				j.unfinishedMap["release-source"] = true
			case "symlink":
				outside := filepath.Join(t.TempDir(), "outside")
				if err := os.WriteFile(outside, []byte("untouched"), 0600); err != nil {
					t.Fatal(err)
				}
				if err := os.Remove(path); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(outside, path); err != nil {
					t.Fatal(err)
				}
			case "outside":
				tr := f.files[path]
				tr.Path = filepath.Join(t.TempDir(), "outside")
				f.files[path] = tr
			case "directory":
				if err := os.Remove(path); err != nil {
					t.Fatal(err)
				}
				if err := os.Mkdir(path, 0700); err != nil {
					t.Fatal(err)
				}
			case "alias":
				tr := f.files[path]
				tr.Path = filepath.Join(root, "keep.opus")
				f.files[path] = tr
			case "stale":
				validation = func() error { return errors.New("changed snapshot") }
			case "locked":
				unlock, ok := s.locks.TryLock("track:keep")
				if !ok {
					t.Fatal("fixture lock")
				}
				defer unlock()
			}
			deleted, failed, err := s.RemoveDuplicateTracks(context.Background(), "keep", ids, validation)
			if err == nil || len(deleted) != 0 || failed != "" {
				t.Fatalf("preflight mutated: %v %s %v", deleted, failed, err)
			}
			for _, name := range []string{"keep.opus", "keep.lrc", "lose1.opus", "lose1.lrc"} {
				if _, err := os.Stat(filepath.Join(root, name)); err != nil {
					t.Fatal("file changed before full validation", err)
				}
			}
			if _, ok := c.tracks["lose1"]; !ok {
				t.Fatal("early track deleted")
			}
		})
	}
}
func TestDuplicateRemovalSuccessAndUnselectedVersion(t *testing.T) {
	s, root, c, _, _ := duplicateRemovalFixture(t)
	deleted, failed, err := s.RemoveDuplicateTracks(context.Background(), "keep", []string{"lose1"}, nil)
	if err != nil || failed != "" || len(deleted) != 1 {
		t.Fatal(deleted, failed, err)
	}
	for _, name := range []string{"keep.opus", "keep.lrc", "lose2.opus", "lose2.lrc"} {
		if _, err := os.Stat(filepath.Join(root, name)); err != nil {
			t.Fatal("unselected file removed", err)
		}
	}
	if _, err = os.Stat(filepath.Join(root, "lose1.opus")); !os.IsNotExist(err) {
		t.Fatal("selected file retained")
	}
	if _, ok := c.tracks["lose2"]; !ok {
		t.Fatal("unselected track removed")
	}
}

func TestDuplicateRemovalPreservesSharedUnselectedLyrics(t *testing.T) {
	s, root, _, f, _ := duplicateRemovalFixture(t)
	old := filepath.Join(root, "lose2.opus")
	path := filepath.Join(root, "lose1.flac")
	if err := os.Rename(old, path); err != nil {
		t.Fatal(err)
	}
	tr := f.files[old]
	tr.Path = path
	delete(f.files, old)
	f.files[path] = tr
	_, _, err := s.RemoveDuplicateTracks(context.Background(), "keep", []string{"lose1"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = os.Stat(filepath.Join(root, "lose1.lrc")); err != nil {
		t.Fatal("shared lyrics removed", err)
	}
	if _, err = os.Stat(path); err != nil {
		t.Fatal("unselected audio removed", err)
	}
}
