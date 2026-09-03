package handlers

import (
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"

	"ytdm/backend/internal/api/response"
	"ytdm/backend/internal/apperr"
	"ytdm/backend/internal/database/repository"
	"ytdm/backend/internal/library"
)

// LibraryArtists answers GET /library/artists.
func (h *Handlers) LibraryArtists(w http.ResponseWriter, r *http.Request) {
	limit, offset, err := pagination(r, 24, 120)
	if err != nil {
		response.Error(w, r, err)
		return
	}

	filter := repository.ArtistListFilter{
		Query:  queryString(r, "q"),
		Sort:   queryString(r, "sort"),
		Order:  queryString(r, "order"),
		Limit:  limit,
		Offset: offset,
	}

	artists, total, err := h.deps.Catalog.ListArtistsFiltered(r.Context(), filter)
	if err != nil {
		response.Error(w, r, err)
		return
	}
	response.List(w, artists, response.Meta{Count: len(artists), Total: total, Limit: limit, Offset: offset})
}

// LibraryArtistDetail answers GET /library/artists/{id}.
func (h *Handlers) LibraryArtistDetail(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if id == "" {
		response.Error(w, r, apperr.New(apperr.CodeInvalidRequest, "Artist ID is required."))
		return
	}

	detail, err := h.deps.Catalog.GetLibraryArtistDetail(r.Context(), id)
	if err != nil {
		response.Error(w, r, err)
		return
	}
	response.Data(w, http.StatusOK, detail)
}

// LibraryReleases answers GET /library/releases.
func (h *Handlers) LibraryReleases(w http.ResponseWriter, r *http.Request) {
	limit, offset, err := pagination(r, 24, 120)
	if err != nil {
		response.Error(w, r, err)
		return
	}

	var year int
	if rawYear := queryString(r, "year"); rawYear != "" {
		y, err := strconv.Atoi(rawYear)
		if err != nil || y < 0 {
			response.Error(w, r, apperr.Newf(apperr.CodeInvalidRequest, "Invalid year: %q.", rawYear))
			return
		}
		year = y
	}

	filter := repository.ReleaseListFilter{
		Query:       queryString(r, "q"),
		ArtistID:    queryString(r, "artist_id"),
		ReleaseType: queryString(r, "release_type"),
		Year:        year,
		Sort:        queryString(r, "sort"),
		Order:       queryString(r, "order"),
		Limit:       limit,
		Offset:      offset,
	}

	releases, total, err := h.deps.Catalog.ListReleasesFiltered(r.Context(), filter)
	if err != nil {
		response.Error(w, r, err)
		return
	}
	response.List(w, releases, response.Meta{Count: len(releases), Total: total, Limit: limit, Offset: offset})
}

// LibraryReleaseDetail answers GET /library/releases/{id}.
func (h *Handlers) LibraryReleaseDetail(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if id == "" {
		response.Error(w, r, apperr.New(apperr.CodeInvalidRequest, "Release ID is required."))
		return
	}

	detail, err := h.deps.Catalog.GetLibraryReleaseDetail(r.Context(), id)
	if err != nil {
		response.Error(w, r, err)
		return
	}
	response.Data(w, http.StatusOK, detail)
}

// LibraryTracks answers GET /library/tracks.
func (h *Handlers) LibraryTracks(w http.ResponseWriter, r *http.Request) {
	limit, offset, err := pagination(r, 50, 100)
	if err != nil {
		response.Error(w, r, err)
		return
	}

	filter := repository.TrackListFilter{
		Query:       queryString(r, "q"),
		ArtistID:    queryString(r, "artist_id"),
		ReleaseID:   queryString(r, "release_id"),
		LyricsState: queryString(r, "lyrics_state"),
		Sort:        queryString(r, "sort"),
		Order:       queryString(r, "order"),
		Limit:       limit,
		Offset:      offset,
	}

	tracks, total, err := h.deps.Catalog.ListTracksFiltered(r.Context(), filter)
	if err != nil {
		response.Error(w, r, err)
		return
	}
	response.List(w, tracks, response.Meta{Count: len(tracks), Total: total, Limit: limit, Offset: offset})
}

// LibraryTrackDetail answers GET /library/tracks/{id}.
func (h *Handlers) LibraryTrackDetail(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if id == "" {
		response.Error(w, r, apperr.New(apperr.CodeInvalidRequest, "Track ID is required."))
		return
	}

	detail, err := h.deps.Catalog.GetLibraryTrackDetail(r.Context(), id)
	if err != nil {
		response.Error(w, r, err)
		return
	}
	response.Data(w, http.StatusOK, detail)
}

// LibrarySearch answers GET /library/search.
func (h *Handlers) LibrarySearch(w http.ResponseWriter, r *http.Request) {
	q := queryString(r, "q")
	limit := 5
	if rawLimit := queryString(r, "limit"); rawLimit != "" {
		if l, err := strconv.Atoi(rawLimit); err == nil && l > 0 {
			limit = min(l, 20)
		}
	}

	results, err := h.deps.Catalog.SearchLibrary(r.Context(), q, limit)
	if err != nil {
		response.Error(w, r, err)
		return
	}
	response.Data(w, http.StatusOK, results)
}

// LibraryStats answers GET /library/stats.
func (h *Handlers) LibraryStats(w http.ResponseWriter, r *http.Request) {
	if h.deps.LibraryService == nil {
		response.Data(w, http.StatusOK, map[string]any{"total_files": 0, "total_bytes": 0})
		return
	}
	stats, err := h.deps.LibraryService.Stats(r.Context())
	if err != nil {
		response.Error(w, r, err)
		return
	}
	response.Data(w, http.StatusOK, stats)
}

// StartLibraryScan answers POST /library/scan.
func (h *Handlers) StartLibraryScan(w http.ResponseWriter, r *http.Request) {
	if h.deps.LibraryService == nil {
		response.Data(w, http.StatusOK, map[string]any{"status": "completed"})
		return
	}
	scan, err := h.deps.LibraryService.Reconcile(r.Context())
	if err != nil {
		response.Error(w, r, err)
		return
	}
	response.Data(w, http.StatusOK, scan)
}

// GetLibraryScan answers GET /library/scan.
func (h *Handlers) GetLibraryScan(w http.ResponseWriter, r *http.Request) {
	if h.deps.LibraryService == nil {
		response.Data(w, http.StatusOK, map[string]any{"status": "completed", "issues": []any{}})
		return
	}
	limit, offset, err := pagination(r, 100, 500)
	if err != nil {
		response.Error(w, r, err)
		return
	}
	statusFilter := queryString(r, "status")
	scan, err := h.deps.LibraryService.GetScan(r.Context(), statusFilter, limit, offset)
	if err != nil {
		response.Error(w, r, err)
		return
	}
	response.Data(w, http.StatusOK, scan)
}

// RedownloadLibraryTrack answers POST /library/tracks/{id}/redownload.
func (h *Handlers) RedownloadLibraryTrack(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if h.deps.LibraryService == nil {
		response.Fail(w, r, apperr.CodeInternal, "Library service is unavailable.")
		return
	}
	job, err := h.deps.LibraryService.RedownloadTrack(r.Context(), id)
	if err != nil {
		response.Error(w, r, err)
		return
	}
	response.Data(w, http.StatusOK, job)
}

// RetagLibraryTrack answers POST /library/tracks/{id}/retag.
func (h *Handlers) RetagLibraryTrack(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if h.deps.LibraryService == nil {
		response.Fail(w, r, apperr.CodeInternal, "Library service is unavailable.")
		return
	}
	if err := h.deps.LibraryService.RetagTrack(r.Context(), id); err != nil {
		response.Error(w, r, err)
		return
	}
	response.Data(w, http.StatusOK, map[string]any{"ok": true})
}

// DeleteLibraryTrack answers DELETE /library/tracks/{id}.
func (h *Handlers) DeleteLibraryTrack(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if h.deps.LibraryService == nil {
		response.Fail(w, r, apperr.CodeInternal, "Library service is unavailable.")
		return
	}
	if err := h.deps.LibraryService.DeleteTrack(r.Context(), id); err != nil {
		response.Error(w, r, err)
		return
	}
	response.Data(w, http.StatusOK, map[string]any{"ok": true})
}

// DeleteLibraryRelease answers DELETE /library/releases/{id}.
func (h *Handlers) DeleteLibraryRelease(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if h.deps.LibraryService == nil {
		response.Fail(w, r, apperr.CodeInternal, "Library service is unavailable.")
		return
	}
	if err := h.deps.LibraryService.DeleteRelease(r.Context(), id); err != nil {
		response.Error(w, r, err)
		return
	}
	response.Data(w, http.StatusOK, map[string]any{"ok": true})
}

// DeleteLibraryOrphanIssue answers DELETE /library/scan/issues/{id}.
func (h *Handlers) DeleteLibraryOrphanIssue(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if h.deps.LibraryService == nil {
		response.Fail(w, r, apperr.CodeInternal, "Library service is unavailable.")
		return
	}
	if err := h.deps.LibraryService.DeleteOrphanIssue(r.Context(), id); err != nil {
		response.Error(w, r, err)
		return
	}
	response.Data(w, http.StatusOK, map[string]any{"ok": true})
}

// TrackLyrics answers GET /library/tracks/{id}/lyrics.
func (h *Handlers) TrackLyrics(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if h.deps.LibraryService == nil {
		response.Fail(w, r, apperr.CodeInternal, "Library service is unavailable.")
		return
	}
	lyrics, err := h.deps.LibraryService.TrackLyrics(r.Context(), id)
	if err != nil {
		response.Error(w, r, err)
		return
	}
	response.Data(w, http.StatusOK, lyrics)
}

// RefreshTrackLyrics answers POST /library/tracks/{id}/lyrics/refresh.
func (h *Handlers) RefreshTrackLyrics(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if h.deps.LibraryService == nil {
		response.Fail(w, r, apperr.CodeInternal, "Library service is unavailable.")
		return
	}
	lyrics, err := h.deps.LibraryService.RefreshTrackLyrics(r.Context(), id)
	if err != nil {
		response.Error(w, r, err)
		return
	}
	response.Data(w, http.StatusOK, lyrics)
}

// DeleteTrackLyrics answers DELETE /library/tracks/{id}/lyrics.
func (h *Handlers) DeleteTrackLyrics(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if h.deps.LibraryService == nil {
		response.Fail(w, r, apperr.CodeInternal, "Library service is unavailable.")
		return
	}
	if err := h.deps.LibraryService.DeleteTrackLyrics(r.Context(), id); err != nil {
		response.Error(w, r, err)
		return
	}
	response.Data(w, http.StatusOK, map[string]any{"ok": true})
}

// BackfillLyrics answers POST /library/lyrics/backfill.
func (h *Handlers) BackfillLyrics(w http.ResponseWriter, r *http.Request) {
	if h.deps.LibraryService == nil {
		response.Fail(w, r, apperr.CodeInternal, "Library service is unavailable.")
		return
	}
	result, err := h.deps.LibraryService.StartBackfillLyrics()
	if err != nil {
		response.Error(w, r, err)
		return
	}
	response.Data(w, http.StatusAccepted, result)
}

// BackfillLyricsStatus answers GET /library/lyrics/backfill.
func (h *Handlers) BackfillLyricsStatus(w http.ResponseWriter, r *http.Request) {
	if h.deps.LibraryService == nil {
		response.Fail(w, r, apperr.CodeInternal, "Library service is unavailable.")
		return
	}
	status := h.deps.LibraryService.BackfillStatusOf()
	if status == nil {
		response.Data(w, http.StatusOK, map[string]any{"status": "idle"})
		return
	}
	response.Data(w, http.StatusOK, status)
}

// PreviewBackfillLyrics answers GET /library/lyrics/backfill/preview.
func (h *Handlers) PreviewBackfillLyrics(w http.ResponseWriter, r *http.Request) {
	if h.deps.LibraryService == nil {
		response.Fail(w, r, apperr.CodeInternal, "Library service is unavailable.")
		return
	}
	stats, err := h.deps.LibraryService.PreviewBackfillLyrics(r.Context())
	if err != nil {
		response.Error(w, r, err)
		return
	}
	response.Data(w, http.StatusOK, stats)
}

// CompatibilityReport answers GET /library/compatibility.
func (h *Handlers) CompatibilityReport(w http.ResponseWriter, r *http.Request) {
	if h.deps.LibraryService == nil {
		response.Fail(w, r, apperr.CodeInternal, "Library service is unavailable.")
		return
	}
	report, err := h.deps.LibraryService.CompatibilityReport(r.Context())
	if err != nil {
		response.Error(w, r, err)
		return
	}
	response.Data(w, http.StatusOK, report)
}

// Reorganize answers POST /library/reorganize.
func (h *Handlers) Reorganize(w http.ResponseWriter, r *http.Request) {
	if h.deps.LibraryService == nil {
		response.Fail(w, r, apperr.CodeInternal, "Library service is unavailable.")
		return
	}
	var req library.ReorganizeRequest
	if err := decodeJSON(r, &req); err != nil {
		response.Error(w, r, err)
		return
	}
	result, err := h.deps.LibraryService.Reorganize(r.Context(), req)
	if err != nil {
		response.Error(w, r, err)
		return
	}
	response.Data(w, http.StatusOK, result)
}

// AudioMIMEType returns the verified content-type for standard audio extensions.
func AudioMIMEType(ext string) string {
	switch strings.ToLower(strings.TrimSpace(ext)) {
	case ".opus", ".ogg":
		return "audio/ogg"
	case ".m4a":
		return "audio/mp4"
	case ".mp3":
		return "audio/mpeg"
	case ".flac":
		return "audio/flac"
	case ".wav":
		return "audio/wav"
	default:
		return "application/octet-stream"
	}
}

// StreamFile answers GET /library/files/{id}/stream.
func (h *Handlers) StreamFile(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if h.deps.LibraryService == nil {
		response.Fail(w, r, apperr.CodeInternal, "Library service is unavailable.")
		return
	}
	absPath, file, err := h.deps.LibraryService.StreamFile(r.Context(), id)
	if err != nil {
		response.Error(w, r, err)
		return
	}
	serveAudioFile(w, r, absPath, file.Path)
}

// StreamTrack answers GET /library/tracks/{id}/stream.
func (h *Handlers) StreamTrack(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if h.deps.LibraryService == nil {
		response.Fail(w, r, apperr.CodeInternal, "Library service is unavailable.")
		return
	}
	absPath, file, err := h.deps.LibraryService.StreamTrack(r.Context(), id)
	if err != nil {
		response.Error(w, r, err)
		return
	}
	serveAudioFile(w, r, absPath, file.Path)
}

func serveAudioFile(w http.ResponseWriter, r *http.Request, absPath, relPath string) {
	f, err := os.Open(absPath)
	if err != nil {
		if os.IsNotExist(err) {
			response.Fail(w, r, apperr.CodeFileNotFound, "Audio file is missing from storage.")
			return
		}
		response.Fail(w, r, apperr.CodeInternal, "Failed to open audio file.")
		return
	}
	defer f.Close()

	stat, err := f.Stat()
	if err != nil || stat.IsDir() {
		response.Fail(w, r, apperr.CodeFileNotFound, "Audio file is inaccessible.")
		return
	}

	ext := filepath.Ext(relPath)
	mimeType := AudioMIMEType(ext)

	w.Header().Set("Content-Type", mimeType)
	w.Header().Set("Accept-Ranges", "bytes")
	http.ServeContent(w, r, filepath.Base(relPath), stat.ModTime(), f)
}
