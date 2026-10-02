package handlers

import (
	"context"
	"crypto/sha256"
	"github.com/go-chi/chi/v5"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"ytdm/backend/internal/database/dbtest"
	"ytdm/backend/internal/database/repository"
	"ytdm/backend/internal/music"
	"ytdm/backend/internal/storage"
)

func TestLoudnessAnalysisReadOnlyCacheAndConfinement(t *testing.T) {
	binary, err := exec.LookPath("ffmpeg")
	if err != nil {
		t.Skip("FFmpeg not installed")
	}
	db := dbtest.Open(t)
	ctx := context.Background()
	catalog := repository.NewCatalog(db)
	files := repository.NewFiles(db)
	root := t.TempDir()
	lib, err := storage.NewLibrary(root)
	if err != nil {
		t.Fatal(err)
	}
	artist, err := catalog.UpsertArtist(ctx, music.Artist{Name: "Tone", Provider: "fixture", SourceID: "tone"})
	if err != nil {
		t.Fatal(err)
	}
	track, err := catalog.UpsertTrack(ctx, music.Track{Title: "Tone", Artists: []string{"Tone"}, DurationMS: 3000}, "", artist.ID, 0)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, "tone.wav")
	cmd := exec.Command(binary, "-nostdin", "-v", "error", "-f", "lavfi", "-i", "sine=frequency=440:duration=3", "-y", path)
	if err = cmd.Run(); err != nil {
		t.Fatal("fixture generation failed")
	}
	data, _ := os.ReadFile(path)
	before := sha256.Sum256(data)
	st, _ := os.Stat(path)
	_, err = files.Upsert(ctx, music.File{TrackID: track.ID, Path: "tone.wav", SizeBytes: int64(len(data)), Codec: "pcm_s16le"})
	if err != nil {
		t.Fatal(err)
	}
	h := &Handlers{deps: Deps{Catalog: catalog, Files: files, Library: lib, FFmpegPath: binary}}
	router := chi.NewRouter()
	router.Post("/tracks/{id}/loudness", h.AnalyzeTrackLoudness)
	request := func() int {
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/tracks/"+track.ID+"/loudness", nil))
		if rec.Code != 200 {
			t.Log(rec.Body.String())
		}
		return rec.Code
	}
	if status := request(); status != 200 {
		t.Fatal("analysis failed", status)
	}
	after, _ := os.ReadFile(path)
	updated, _ := os.Stat(path)
	if sha256.Sum256(after) != before || !st.ModTime().Equal(updated.ModTime()) {
		t.Fatal("analysis changed media")
	}
	h.deps.FFmpegPath = "missing-fixture-binary"
	if request() != 200 {
		t.Fatal("cache not reused")
	}
	if err = os.Remove(path); err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(t.TempDir(), "outside.wav")
	if err = os.WriteFile(outside, data, 0600); err != nil {
		t.Fatal(err)
	}
	if err = os.Symlink(outside, path); err != nil {
		t.Fatal(err)
	}
	if status := request(); status != 404 {
		t.Fatal("outside-root source accepted", status)
	}
}
