package handlers

import (
	"github.com/go-chi/chi/v5"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"ytdm/backend/internal/api/response"
	"ytdm/backend/internal/apperr"
	"ytdm/backend/internal/fingerprint"
)

func (h *Handlers) AudioAnalysisStatus(w http.ResponseWriter, r *http.Request) {
	v, err := h.deps.Catalog.AudioAnalysisStatus(r.Context())
	if err != nil {
		response.Error(w, r, err)
		return
	}
	response.OK(w, r, v)
}
func (h *Handlers) ResetAudioAnalysis(w http.ResponseWriter, r *http.Request) {
	h.fingerprintMu.Lock()
	defer h.fingerprintMu.Unlock()
	if h.fingerprintBusy {
		response.Error(w, r, apperr.New(apperr.CodeConflict, "Eine Audioanalyse läuft noch."))
		return
	}
	if err := h.deps.Catalog.ResetAudioAnalysis(r.Context()); err != nil {
		response.Error(w, r, err)
		return
	}
	response.NoContent(w)
}
func (h *Handlers) AnalyzeAudioFingerprint(w http.ResponseWriter, r *http.Request) {
	h.fingerprintMu.Lock()
	if h.fingerprintBusy {
		h.fingerprintMu.Unlock()
		response.Error(w, r, apperr.New(apperr.CodeConflict, "Eine Audioanalyse läuft bereits. Bitte später erneut starten."))
		return
	}
	h.fingerprintBusy = true
	h.fingerprintMu.Unlock()
	defer func() { h.fingerprintMu.Lock(); h.fingerprintBusy = false; h.fingerprintMu.Unlock() }()
	calculator, err := exec.LookPath("fpcalc")
	if err != nil || h.deps.Library == nil {
		response.Error(w, r, apperr.New(apperr.CodeInvalidRequest, "Audioerkennung ist auf diesem Server nicht verfügbar. Das Backend-Image muss Chromaprint enthalten."))
		return
	}
	id := chi.URLParam(r, "id")
	file, err := h.deps.Catalog.FingerprintFile(r.Context(), id)
	if err != nil {
		response.Error(w, r, err)
		return
	}
	root, err := os.OpenRoot(h.deps.Library.Root())
	if err != nil {
		response.Error(w, r, apperr.New(apperr.CodeFileNotFound, "Bibliothek nicht erreichbar."))
		return
	}
	defer root.Close()
	path := file.Path
	if filepath.IsAbs(path) {
		path, err = filepath.Rel(h.deps.Library.Root(), path)
	}
	if err != nil || !filepath.IsLocal(path) {
		response.Error(w, r, apperr.New(apperr.CodeFileNotFound, "Audiodatei nicht erreichbar."))
		return
	}
	f, err := root.OpenFile(path, os.O_RDONLY|syscall.O_NONBLOCK, 0)
	if err != nil {
		response.Error(w, r, apperr.New(apperr.CodeFileNotFound, "Audiodatei nicht erreichbar."))
		return
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil || !st.Mode().IsRegular() || st.Size() > 512<<20 {
		response.Error(w, r, apperr.New(apperr.CodeInvalidRequest, "Analyse unterstützt reguläre Audiodateien bis 512 MiB."))
		return
	}
	ffmpeg := h.deps.FFmpegPath
	if ffmpeg == "" {
		ffmpeg = "ffmpeg"
	}
	measurement, err := fingerprint.Measure(r.Context(), f, ffmpeg, calculator)
	if r.Context().Err() != nil {
		return
	}
	state := "ready"
	if err != nil {
		state = "failed"
	} else if !fingerprint.Usable(measurement.Words) {
		state = "inconclusive"
	}
	after, e := f.Stat()
	if e != nil || after.Size() != st.Size() || !after.ModTime().Equal(st.ModTime()) {
		response.Error(w, r, apperr.New(apperr.CodeConflict, "Die Datei wurde während der Analyse geändert."))
		return
	}
	if err = h.deps.Catalog.SaveAudioFingerprint(r.Context(), id, file.ID, file.UpdatedAt, st.Size(), st.ModTime().UnixNano(), measurement.Words, state); err != nil {
		response.Error(w, r, err)
		return
	}
	response.OK(w, r, map[string]string{"state": state})
}
