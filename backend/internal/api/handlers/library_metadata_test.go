package handlers

import (
	"bytes"
	"context"
	"github.com/go-chi/chi/v5"
	"image"
	"image/png"
	"net/http/httptest"
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"ytdm/backend/internal/database/dbtest"
	"ytdm/backend/internal/database/repository"
	"ytdm/backend/internal/music"
	"ytdm/backend/internal/storage"
)

func TestLocalArtworkConfinementAndFallback(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	var img bytes.Buffer
	if err := png.Encode(&img, image.NewRGBA(image.Rect(0, 0, 2, 2))); err != nil {
		t.Fatal(err)
	}
	write := func(path string, data []byte) {
		t.Helper()
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, data, 0644); err != nil {
			t.Fatal(err)
		}
	}
	write(filepath.Join(root, "album", "cover.jpg"), []byte("<svg>unsafe</svg>"))
	write(filepath.Join(root, "album", "cover.png"), img.Bytes())
	write(filepath.Join(outside, "cover.png"), img.Bytes())
	if err := os.Symlink(outside, filepath.Join(root, "escape")); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"escape/audio.opus", filepath.Join(outside, "audio.opus"), "../audio.opus"} {
		recorder := httptest.NewRecorder()
		if serveLocalArtwork(recorder, httptest.NewRequest("GET", "/", nil), root, []string{path}) {
			t.Fatal("escaped library")
		}
	}
	recorder := httptest.NewRecorder()
	if !serveLocalArtwork(recorder, httptest.NewRequest("GET", "/", nil), root, []string{"missing/song.opus", filepath.Join(root, "album", "song.opus")}) {
		t.Fatal("local cover unavailable")
	}
	if recorder.Code != 200 || recorder.Header().Get("Content-Type") != "image/png" || !bytes.Equal(recorder.Body.Bytes(), img.Bytes()) {
		t.Fatal("invalid artwork response")
	}
	// A named pipe must not block an artwork request.
	if err := syscall.Mkfifo(filepath.Join(root, "cover.jpg"), 0600); err != nil {
		t.Fatal(err)
	}
	if serveLocalArtwork(httptest.NewRecorder(), httptest.NewRequest("GET", "/", nil), root, []string{"song.opus"}) {
		t.Fatal("named pipe served")
	}
	// Oversized raster files are rejected even when their header is valid.
	file, err := os.Create(filepath.Join(root, "album", "cover.png"))
	if err != nil {
		t.Fatal(err)
	}
	file.Write(img.Bytes())
	file.Truncate((8 << 20) + 1)
	file.Close()
	if serveLocalArtwork(httptest.NewRecorder(), httptest.NewRequest("GET", "/", nil), root, []string{"album/song.opus"}) {
		t.Fatal("oversized artwork accepted")
	}
}

func TestArtworkHandlersUseCatalogOwnedDirectories(t *testing.T) {
	db := dbtest.Open(t)
	ctx := context.Background()
	rootPath := t.TempDir()
	lib, err := storage.NewLibrary(rootPath)
	if err != nil {
		t.Fatal(err)
	}
	c := repository.NewCatalog(db)
	a, err := c.UpsertArtist(ctx, music.Artist{Name: "Art Fixture", Provider: "ytmusic", SourceID: "artist:fixture"})
	if err != nil {
		t.Fatal(err)
	}
	release, err := c.UpsertRelease(ctx, music.Release{Title: "Album", Provider: "ytmusic", SourceID: "album"}, a.ID)
	if err != nil {
		t.Fatal(err)
	}
	track, err := c.UpsertTrack(ctx, music.Track{Title: "Song", DurationMS: 120000}, release.ID, a.ID, 1000)
	if err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(rootPath, "artist", "album")
	if err = os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	var img bytes.Buffer
	if err = png.Encode(&img, image.NewRGBA(image.Rect(0, 0, 2, 2))); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(dir, "cover.png"), img.Bytes(), 0644); err != nil {
		t.Fatal(err)
	}
	if _, err = db.ExecContext(ctx, `INSERT INTO files (id,track_id,path,size_bytes,created_at,updated_at) VALUES ('art-file',$1,'artist/album/song.opus',1,now(),now())`, track.ID); err != nil {
		t.Fatal(err)
	}
	h := &Handlers{deps: Deps{Catalog: c, Library: lib}}
	for _, entry := range []struct{ kind, id string }{{"artists", a.ID}, {"releases", release.ID}, {"tracks", track.ID}} {
		router := chi.NewRouter()
		router.Get("/{id}", h.LibraryArtwork(entry.kind))
		router.Head("/{id}", h.LibraryArtwork(entry.kind))
		for _, method := range []string{"GET", "HEAD"} {
			recorder := httptest.NewRecorder()
			router.ServeHTTP(recorder, httptest.NewRequest(method, "/"+entry.id, nil))
			if recorder.Code != 200 || recorder.Header().Get("Content-Type") != "image/png" {
				t.Fatalf("%s %s artwork unavailable", method, entry.kind)
			}
			if method == "GET" && !bytes.Equal(recorder.Body.Bytes(), img.Bytes()) {
				t.Fatal("wrong cover contents")
			}
			if method == "HEAD" && recorder.Body.Len() != 0 {
				t.Fatal("HEAD returned a body")
			}
		}
	}
}
