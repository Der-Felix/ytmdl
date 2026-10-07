package jobs

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"ytdm/backend/internal/apperr"
	"ytdm/backend/internal/downloader"
	"ytdm/backend/internal/music"
	"ytdm/backend/internal/provider"
)

func TestSkipExistingRequiresRequestedRuntime(t *testing.T) {
	for _, tc := range []struct {
		name     string
		duration int
		want     bool
	}{
		{"correct", 200000, true}, {"rounded", 201000, true},
		{"wrong recording", 863001, false}, {"preview", 30000, false}, {"unknown", 0, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			files := &fakeFiles{byTrack: []music.File{{Path: "song.opus", DurationMS: tc.duration}}}
			m, root := newPlaceManager(t, &fakeCatalog{known: &music.Track{ID: "known"}}, files)
			if err := os.WriteFile(filepath.Join(root, "song.opus"), []byte("existing"), 0600); err != nil {
				t.Fatal(err)
			}
			got, err := m.alreadyInLibrary(context.Background(), aWorkerTrack())
			if err != nil || got != tc.want {
				t.Fatalf("skip=%v, err=%v", got, err)
			}
		})
	}
}

func TestCorrectionPreservesOwnIncorrectRecording(t *testing.T) {
	rel := filepath.Join("Artist", "2001 - Album", "01 - Song.opus")
	files := &fakeFiles{byPath: map[string]*music.File{rel: {TrackID: "known", DurationMS: 863001}}}
	m, root := newPlaceManager(t, &fakeCatalog{known: &music.Track{ID: "known"}}, files)
	target := filepath.Join(root, rel)
	if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(target, []byte("original incorrect recording"), 0600); err != nil {
		t.Fatal(err)
	}
	file, err := m.place(context.Background(), aRelease(), aWorkerTrack(), aDownload(t, "verified correction"), nil, provider.MediaSource{Provider: "ytmusic", ID: "exact-source"})
	if err != nil {
		t.Fatal(err)
	}
	if file.Path == rel {
		t.Fatal("incorrect original was overwritten")
	}
	original, err := os.ReadFile(target)
	if err != nil || string(original) != "original incorrect recording" {
		t.Fatal("original changed")
	}
	corrected, err := os.ReadFile(filepath.Join(root, file.Path))
	if err != nil || string(corrected) != "verified correction" {
		t.Fatal("correction missing")
	}
}

type captureRequestedDuration struct{ duration int }

func (d *captureRequestedDuration) Download(_ context.Context, source provider.MediaSource, _ string, _ downloader.ProgressCallback) (*downloader.Result, error) {
	d.duration = source.DurationMS
	return nil, apperr.New(apperr.CodeInvalidAudio, "fixture stopped before publication")
}

func TestWorkerVerifiesAgainstRequestedRecordingNotSourceRuntime(t *testing.T) {
	// A flat search can omit runtime; verification still needs the requested one.
	prov := newMockFallbackMediaProvider("youtube", []provider.MediaCandidate{{ID: "candidate", Provider: "youtube", Title: "The Visitors", Artists: []string{"ABBA"}}})
	prov.resolveResults["candidate"] = &provider.MediaSource{Provider: "youtube", ID: "candidate", DurationMS: 863001}
	mgr, store := setupTestFallbackEnvironment(t, prov)
	d := &captureRequestedDuration{}
	mgr.downloader = d
	item := store.items["item-1"]
	(&worker{manager: mgr}).process(context.Background(), Job{ID: "job-1", MediaProvider: "youtube"}, item)
	if d.duration != item.Track.DurationMS {
		t.Fatalf("download expected %d, want requested %d", d.duration, item.Track.DurationMS)
	}
	if store.items["item-1"].Status == ItemCompleted {
		t.Fatal("invalid audio published")
	}
}
