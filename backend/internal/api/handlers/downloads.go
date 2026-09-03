package handlers

import (
	"net/http"
	"strings"

	"ytdm/backend/internal/api/response"
	"ytdm/backend/internal/apperr"
	"ytdm/backend/internal/jobs"
	"ytdm/backend/internal/music"
)

// artistDownloadRequest is the body of POST /downloads/artist.
type artistDownloadRequest struct {
	Provider      string               `json:"provider"`
	MediaProvider string               `json:"media_provider"`
	ArtistID      string               `json:"artist_id"`
	ReleaseFilter *music.ReleaseFilter `json:"release_filter"`
	SkipExisting  *bool                `json:"skip_existing"`
}

// releaseDownloadRequest is the body of POST /downloads/release.
type releaseDownloadRequest struct {
	Provider      string `json:"provider"`
	MediaProvider string `json:"media_provider"`
	ReleaseID     string `json:"release_id"`
	SkipExisting  *bool  `json:"skip_existing"`
}

// trackDownloadRequest is the body of POST /downloads/track.
type trackDownloadRequest struct {
	Provider      string `json:"provider"`
	MediaProvider string `json:"media_provider"`
	TrackID       string `json:"track_id"`
	ReleaseID     string `json:"release_id"`
	SkipExisting  *bool  `json:"skip_existing"`
}

// DownloadArtist answers POST /downloads/artist. It only creates the job; the
// catalogue is resolved and the audio fetched by the workers.
func (h *Handlers) DownloadArtist(w http.ResponseWriter, r *http.Request) {
	var body artistDownloadRequest
	if err := decodeJSON(r, &body); err != nil {
		response.Error(w, r, err)
		return
	}
	if strings.TrimSpace(body.ArtistID) == "" {
		response.Fail(w, r, apperr.CodeInvalidRequest, "artist_id is required.")
		return
	}
	if body.ReleaseFilter != nil && !body.ReleaseFilter.Any() {
		response.Fail(w, r, apperr.CodeInvalidRequest, "release_filter must select at least one release type.")
		return
	}

	h.enqueue(w, r, jobs.Request{
		Type:             jobs.TypeArtist,
		MetadataProvider: body.Provider,
		MediaProvider:    body.MediaProvider,
		TargetID:         body.ArtistID,
		Options: jobs.RequestOptions{
			ReleaseFilter: body.ReleaseFilter,
			SkipExisting:  body.SkipExisting,
		},
	})
}

// DownloadRelease answers POST /downloads/release.
func (h *Handlers) DownloadRelease(w http.ResponseWriter, r *http.Request) {
	var body releaseDownloadRequest
	if err := decodeJSON(r, &body); err != nil {
		response.Error(w, r, err)
		return
	}
	if strings.TrimSpace(body.ReleaseID) == "" {
		response.Fail(w, r, apperr.CodeInvalidRequest, "release_id is required.")
		return
	}

	h.enqueue(w, r, jobs.Request{
		Type:             jobs.TypeRelease,
		MetadataProvider: body.Provider,
		MediaProvider:    body.MediaProvider,
		TargetID:         body.ReleaseID,
		Options:          jobs.RequestOptions{SkipExisting: body.SkipExisting},
	})
}

// DownloadTrack answers POST /downloads/track.
func (h *Handlers) DownloadTrack(w http.ResponseWriter, r *http.Request) {
	var body trackDownloadRequest
	if err := decodeJSON(r, &body); err != nil {
		response.Error(w, r, err)
		return
	}
	if strings.TrimSpace(body.TrackID) == "" {
		response.Fail(w, r, apperr.CodeInvalidRequest, "track_id is required.")
		return
	}

	h.enqueue(w, r, jobs.Request{
		Type:             jobs.TypeTrack,
		MetadataProvider: body.Provider,
		MediaProvider:    body.MediaProvider,
		TargetID:         body.TrackID,
		ReleaseID:        body.ReleaseID,
		Options:          jobs.RequestOptions{SkipExisting: body.SkipExisting},
	})
}

// enqueue creates the job and answers with it.
func (h *Handlers) enqueue(w http.ResponseWriter, r *http.Request, req jobs.Request) {
	job, err := h.deps.Jobs.Enqueue(r.Context(), req)
	if err != nil {
		response.Error(w, r, err)
		return
	}
	w.Header().Set("Location", "/api/v1/jobs/"+job.ID)
	response.Data(w, http.StatusAccepted, job)
}
