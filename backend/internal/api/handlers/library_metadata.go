package handlers

import (
	"bytes"
	"crypto/sha256"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"syscall"

	"github.com/go-chi/chi/v5"
	"ytdm/backend/internal/api/response"
	"ytdm/backend/internal/apperr"
)

func (h *Handlers) LibraryGenres(w http.ResponseWriter, r *http.Request) {
	genres, err := h.deps.Catalog.ListGenres(r.Context())
	if err != nil {
		response.Error(w, r, err)
		return
	}
	response.Data(w, http.StatusOK, genres)
}

func (h *Handlers) UpdateArtistGenres(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Genres *[]string `json:"genres"`
	}
	if err := decodeJSON(r, &body); err != nil {
		response.Error(w, r, err)
		return
	}
	if body.Genres == nil {
		response.Error(w, r, apperr.New(apperr.CodeInvalidRequest, "Genres müssen als Liste angegeben werden."))
		return
	}
	genres, err := h.deps.Catalog.SetArtistGenres(r.Context(), chi.URLParam(r, "id"), *body.Genres)
	if err != nil {
		response.Error(w, r, err)
		return
	}
	response.Data(w, http.StatusOK, genres)
}

func (h *Handlers) LibraryArtwork(kind string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := chi.URLParam(r, "id")
		customKind, customID := kind, id
		if kind == "tracks" {
			detail, err := h.deps.Catalog.GetLibraryTrackDetail(r.Context(), id)
			if err != nil {
				response.Error(w, r, err)
				return
			}
			customKind, customID = "releases", detail.Track.ReleaseID
		}
		data, updated, err := h.deps.Catalog.CustomArtwork(r.Context(), customKind, customID)
		if err != nil {
			response.Error(w, r, err)
			return
		}
		if len(data) > 0 {
			w.Header().Set("ETag", fmt.Sprintf(`"%x"`, sha256.Sum256(data)))
			w.Header().Set("Content-Type", "image/jpeg")
			w.Header().Set("X-Content-Type-Options", "nosniff")
			w.Header().Set("Cache-Control", "private, no-cache")
			http.ServeContent(w, r, "cover.jpg", updated, bytes.NewReader(data))
			return
		}
		paths, err := h.deps.Catalog.ArtworkFilePaths(r.Context(), kind, chi.URLParam(r, "id"))
		if err != nil {
			response.Error(w, r, err)
			return
		}
		if h.deps.Library == nil {
			response.Error(w, r, apperr.New(apperr.CodeFileNotFound, "Kein lokales Cover vorhanden."))
			return
		}
		if serveLocalArtwork(w, r, h.deps.Library.Root(), paths) {
			return
		}
		response.Error(w, r, apperr.New(apperr.CodeFileNotFound, "Kein lokales Cover vorhanden."))
	}
}

// OpenRoot confines symlinks as well as .. components, including during concurrent renames.
// Only bounded raster images beside catalog-owned audio files may be served.
func serveLocalArtwork(w http.ResponseWriter, r *http.Request, rootPath string, audioPaths []string) bool {
	root, err := os.OpenRoot(rootPath)
	if err != nil {
		return false
	}
	defer root.Close()
	seen := map[string]bool{}
	for _, audio := range audioPaths {
		if filepath.IsAbs(audio) {
			audio, err = filepath.Rel(rootPath, audio)
			if err != nil {
				continue
			}
		}
		dir := filepath.Dir(audio)
		if seen[dir] {
			continue
		}
		seen[dir] = true
		for _, name := range []string{"cover.jpg", "cover.jpeg", "cover.png", "cover.webp"} {
			f, err := root.OpenFile(filepath.Join(dir, name), os.O_RDONLY|syscall.O_NONBLOCK, 0)
			if err != nil {
				continue
			}
			info, err := f.Stat()
			if err != nil || !info.Mode().IsRegular() || info.Size() <= 0 || info.Size() > 8<<20 {
				f.Close()
				continue
			}
			header := make([]byte, 512)
			n, err := f.Read(header)
			if err != nil && err != io.EOF {
				f.Close()
				continue
			}
			mime := http.DetectContentType(header[:n])
			if mime != "image/jpeg" && mime != "image/png" && mime != "image/webp" {
				f.Close()
				continue
			}
			if _, err := f.Seek(0, io.SeekStart); err != nil {
				f.Close()
				continue
			}
			w.Header().Set("Content-Type", mime)
			w.Header().Set("X-Content-Type-Options", "nosniff")
			w.Header().Set("Cache-Control", "private, no-cache")
			http.ServeContent(w, r, name, info.ModTime(), io.NewSectionReader(f, 0, info.Size()))
			f.Close()
			return true
		}
	}
	return false
}
