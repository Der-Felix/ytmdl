package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"github.com/go-chi/chi/v5"
	"image"
	"image/color"
	"image/jpeg"
	_ "image/png"
	"io"
	"math"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
	"ytdm/backend/internal/api/middleware"
	"ytdm/backend/internal/api/response"
	"ytdm/backend/internal/apperr"
	"ytdm/backend/internal/database/repository"
)

func (h *Handlers) ListeningHistory(w http.ResponseWriter, r *http.Request) {
	u := middleware.UserFromContext(r.Context())
	data, err := h.deps.Catalog.ListeningHistory(r.Context(), u.ID, r.URL.Query().Get("sort"))
	if err != nil {
		response.Error(w, r, err)
		return
	}
	response.OK(w, r, data)
}
func (h *Handlers) RecordPlayback(w http.ResponseWriter, r *http.Request) {
	u := middleware.UserFromContext(r.Context())
	var req struct {
		EventID string `json:"event_id"`
		TrackID string `json:"track_id"`
	}
	if err := decodeJSON(r, &req); err != nil {
		response.Error(w, r, err)
		return
	}
	if err := h.deps.Catalog.RecordPlayback(r.Context(), u.ID, req.EventID, req.TrackID); err != nil {
		response.Error(w, r, err)
		return
	}
	response.NoContent(w)
}
func (h *Handlers) ClearListeningHistory(w http.ResponseWriter, r *http.Request) {
	u := middleware.UserFromContext(r.Context())
	if err := h.deps.Catalog.ClearListeningHistory(r.Context(), u.ID); err != nil {
		response.Error(w, r, err)
		return
	}
	response.NoContent(w)
}
func (h *Handlers) UpdateSelectedMetadata(w http.ResponseWriter, r *http.Request) {
	var req struct {
		IDs   []string                 `json:"track_ids"`
		Patch repository.MetadataPatch `json:"patch"`
	}
	if err := decodeJSON(r, &req); err != nil {
		response.Error(w, r, err)
		return
	}
	if err := h.deps.Catalog.UpdateTrackMetadata(r.Context(), req.IDs, req.Patch); err != nil {
		response.Error(w, r, err)
		return
	}
	response.NoContent(w)
}
func (h *Handlers) DuplicateGroups(w http.ResponseWriter, r *http.Request) {
	offset, _ := strconv.Atoi(r.URL.Query().Get("offset"))
	data, err := h.deps.Catalog.DuplicateGroups(r.Context(), offset)
	if err != nil {
		response.Error(w, r, err)
		return
	}
	response.OK(w, r, data)
}
func (h *Handlers) UploadLibraryArtwork(kind string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		r.Body = http.MaxBytesReader(w, r.Body, 9<<20)
		if err := r.ParseMultipartForm(9 << 20); err != nil {
			response.Error(w, r, apperr.New(apperr.CodeInvalidRequest, "Bitte ein JPEG- oder PNG-Bild bis 8 MiB auswählen."))
			return
		}
		defer r.MultipartForm.RemoveAll()
		f, _, err := r.FormFile("image")
		if err != nil {
			response.Error(w, r, apperr.New(apperr.CodeInvalidRequest, "Bild fehlt."))
			return
		}
		defer f.Close()
		data, err := io.ReadAll(io.LimitReader(f, (8<<20)+1))
		if err != nil || len(data) > 8<<20 {
			response.Error(w, r, apperr.New(apperr.CodeInvalidRequest, "Bild ist zu groß."))
			return
		}
		encoded, err := normalizeArtwork(data)
		if err != nil {
			response.Error(w, r, err)
			return
		}
		if err = h.deps.Catalog.SetCustomArtwork(r.Context(), kind, chi.URLParam(r, "id"), encoded); err != nil {
			response.Error(w, r, err)
			return
		}
		response.NoContent(w)
	}
}
func normalizeArtwork(data []byte) ([]byte, error) {
	invalid := apperr.New(apperr.CodeInvalidRequest, "Nur gültige JPEG/PNG-Bilder mit maximal 4096 × 4096 Pixeln sind erlaubt.")
	cfg, format, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil || (format != "jpeg" && format != "png") || cfg.Width < 1 || cfg.Height < 1 || cfg.Width > 4096 || cfg.Height > 4096 {
		return nil, invalid
	}
	src, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, invalid
	}
	width, height := cfg.Width, cfg.Height
	ratio := math.Min(1, 1600.0/float64(max(width, height)))
	width = max(1, int(float64(width)*ratio))
	height = max(1, int(float64(height)*ratio))
	dst := image.NewRGBA(image.Rect(0, 0, width, height))
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			r, g, b, a := src.At(x*cfg.Width/width, y*cfg.Height/height).RGBA() // Composite transparent pixels over neutral dark background.
			inv := uint32(65535) - a
			dst.SetRGBA(x, y, color.RGBA{R: uint8((r + inv*24/255) >> 8), G: uint8((g + inv*24/255) >> 8), B: uint8((b + inv*24/255) >> 8), A: 255})
		}
	}
	var out bytes.Buffer
	if err = jpeg.Encode(&out, dst, &jpeg.Options{Quality: 88}); err != nil {
		return nil, invalid
	}
	return out.Bytes(), nil
}
func (h *Handlers) DeleteLibraryArtwork(kind string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if err := h.deps.Catalog.SetCustomArtwork(r.Context(), kind, chi.URLParam(r, "id"), nil); err != nil {
			response.Error(w, r, err)
			return
		}
		response.NoContent(w)
	}
}

// Bounded read-only analysis of an open catalog file; no media is rewritten.
func (h *Handlers) AnalyzeTrackLoudness(w http.ResponseWriter, r *http.Request) {
	h.loudnessMu.Lock()
	if h.loudnessBusy {
		h.loudnessMu.Unlock()
		response.Error(w, r, apperr.New(apperr.CodeInvalidRequest, "Eine Lautstärkeanalyse läuft bereits. Bitte gleich erneut versuchen."))
		return
	}
	h.loudnessBusy = true
	h.loudnessMu.Unlock()
	defer func() { h.loudnessMu.Lock(); h.loudnessBusy = false; h.loudnessMu.Unlock() }()
	detail, err := h.deps.Catalog.GetLibraryTrackDetail(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		response.Error(w, r, err)
		return
	}
	if detail.File == nil || h.deps.Library == nil {
		response.Error(w, r, apperr.New(apperr.CodeFileNotFound, "Audiodatei fehlt."))
		return
	}
	root, err := os.OpenRoot(h.deps.Library.Root())
	if err != nil {
		response.Error(w, r, apperr.New(apperr.CodeFileNotFound, "Bibliothek nicht erreichbar."))
		return
	}
	defer root.Close()
	path := detail.File.Path
	if filepath.IsAbs(path) {
		path, err = filepath.Rel(h.deps.Library.Root(), path)
		if err != nil {
			response.Error(w, r, apperr.New(apperr.CodeFileNotFound, "Audiodatei nicht erreichbar."))
			return
		}
	}
	f, err := root.OpenFile(path, os.O_RDONLY|syscall.O_NONBLOCK, 0)
	if err != nil {
		response.Error(w, r, apperr.New(apperr.CodeFileNotFound, "Audiodatei nicht erreichbar."))
		return
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil || !st.Mode().IsRegular() || st.Size() > 512<<20 || detail.Track.DurationMS > 3600000 {
		response.Error(w, r, apperr.New(apperr.CodeInvalidRequest, "Analyse unterstützt reguläre Audiodateien bis 512 MiB und eine Stunde."))
		return
	}
	cached, err := h.deps.Catalog.Loudness(r.Context(), detail.Track.ID, detail.File.ID, st.Size(), st.ModTime().UnixNano())
	if err != nil {
		response.Error(w, r, err)
		return
	}
	if cached != nil {
		response.OK(w, r, cached)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Minute)
	defer cancel()
	bin := h.deps.FFmpegPath
	if bin == "" {
		bin = "ffmpeg"
	}
	cmd := exec.CommandContext(ctx, bin, "-nostdin", "-hide_banner", "-protocol_whitelist", "file,pipe", "-format_whitelist", "mp3,flac,ogg,wav,mov,matroska,webm,aac", "-i", "/dev/fd/3", "-map", "0:a:0", "-vn", "-af", "loudnorm=I=-16:TP=-1.5:LRA=11:print_format=json", "-f", "null", "-")
	cmd.ExtraFiles = []*os.File{f}
	var output tailBuffer
	cmd.Stderr = &output
	if err = cmd.Run(); err != nil {
		response.Error(w, r, apperr.New(apperr.CodeInvalidRequest, "Die Audiodatei konnte innerhalb des Analyselimits nicht gemessen werden."))
		return
	}
	l, err := parseLoudness(output.data)
	if err != nil {
		response.Error(w, r, err)
		return
	}
	after, err := f.Stat()
	if err != nil || after.Size() != st.Size() || !after.ModTime().Equal(st.ModTime()) {
		response.Error(w, r, apperr.New(apperr.CodeInvalidRequest, "Die Datei wurde während der Analyse verändert."))
		return
	}
	if err = h.deps.Catalog.SetLoudness(r.Context(), detail.Track.ID, detail.File.ID, st.Size(), st.ModTime().UnixNano(), l); err != nil {
		response.Error(w, r, err)
		return
	}
	response.OK(w, r, l)
}

type tailBuffer struct{ data []byte }

func (b *tailBuffer) Write(p []byte) (int, error) {
	b.data = append(b.data, p...)
	if len(b.data) > 65536 {
		b.data = b.data[len(b.data)-65536:]
	}
	return len(p), nil
}
func parseLoudness(data []byte) (repository.Loudness, error) {
	invalid := apperr.New(apperr.CodeInvalidRequest, "Keine gültige Lautstärkemessung erhalten.")
	start := bytes.LastIndexByte(data, '{')
	end := bytes.LastIndexByte(data, '}')
	if start < 0 || end < start {
		return repository.Loudness{}, invalid
	}
	var v map[string]string
	if json.Unmarshal(bytes.TrimSpace(data[start:end+1]), &v) != nil {
		return repository.Loudness{}, invalid
	}
	i, e := strconv.ParseFloat(strings.TrimSpace(v["input_i"]), 64)
	tp, e2 := strconv.ParseFloat(strings.TrimSpace(v["input_tp"]), 64)
	if e != nil || e2 != nil || math.IsNaN(i) || math.IsNaN(tp) {
		return repository.Loudness{}, invalid
	}
	if math.IsInf(i, -1) && math.IsInf(tp, -1) {
		return repository.Loudness{GainDB: 0, IntegratedLUFS: -99, TruePeakDB: -99}, nil
	}
	if math.IsInf(i, 0) || math.IsInf(tp, 0) {
		return repository.Loudness{}, invalid
	}
	gain := math.Max(-24, math.Min(6, math.Min(-16-i, -1.5-tp)))
	return repository.Loudness{GainDB: gain, IntegratedLUFS: i, TruePeakDB: tp}, nil
}
