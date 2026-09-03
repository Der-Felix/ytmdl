package handlers

import (
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"ytdm/backend/internal/api/response"
	"ytdm/backend/internal/apperr"
	"ytdm/backend/internal/jobs"
	"ytdm/backend/internal/music"
	"ytdm/backend/internal/subscriptions"
)

// subscribeRequest is the body of POST /subscriptions.
//
// The artist is named by the provider it was found on. Nothing here converts
// an artist onto a different provider: a Spotify artist stays a Spotify
// subscription and is synced through the Spotify metadata provider.
type subscribeRequest struct {
	Provider         string               `json:"provider"`
	ArtistSourceID   string               `json:"artist_source_id"`
	ArtistName       string               `json:"artist_name"`
	ArtistImageURL   string               `json:"artist_image_url"`
	AutoDownload     *bool                `json:"auto_download"`
	ReleaseFilter    *music.ReleaseFilter `json:"release_filter"`
	DownloadPriority *jobs.Priority       `json:"download_priority"`
}

// updateSubscriptionRequest is the body of PATCH /subscriptions/{id}. An
// omitted field is left untouched.
type updateSubscriptionRequest struct {
	Enabled          *bool                `json:"enabled"`
	AutoDownload     *bool                `json:"auto_download"`
	ReleaseFilter    *music.ReleaseFilter `json:"release_filter"`
	DownloadPriority *jobs.Priority       `json:"download_priority"`
}

// ListSubscriptions answers GET /subscriptions.
func (h *Handlers) ListSubscriptions(w http.ResponseWriter, r *http.Request) {
	if h.deps.Subscriptions == nil {
		response.Fail(w, r, apperr.CodeInternal, "Subscriptions are not available.")
		return
	}
	limit, offset, err := pagination(r, 100, 500)
	if err != nil {
		response.Error(w, r, err)
		return
	}

	list, err := h.deps.Subscriptions.List(r.Context(), subscriptions.ListFilter{
		Provider:       queryString(r, "provider"),
		ArtistSourceID: queryString(r, "artist_source_id"),
		Limit:          limit,
		Offset:         offset,
	})
	if err != nil {
		response.Error(w, r, err)
		return
	}
	response.List(w, list, response.Meta{Count: len(list), Limit: limit, Offset: offset})
}

// Subscribe answers POST /subscriptions. Subscribing to an artist that is
// already watched returns the existing subscription rather than a conflict, so
// a repeated request is harmless.
func (h *Handlers) Subscribe(w http.ResponseWriter, r *http.Request) {
	if h.deps.Subscriptions == nil {
		response.Fail(w, r, apperr.CodeInternal, "Subscriptions are not available.")
		return
	}
	var body subscribeRequest
	if err := decodeJSON(r, &body); err != nil {
		response.Error(w, r, err)
		return
	}
	if strings.TrimSpace(body.ArtistSourceID) == "" {
		response.Fail(w, r, apperr.CodeInvalidRequest, "artist_source_id is required.")
		return
	}

	var (
		defaultAutoDownload bool
		defaultPriority     jobs.Priority
		defaultFilter       music.ReleaseFilter
	)
	if h.deps.Settings != nil {
		defaultAutoDownload, defaultPriority, defaultFilter = h.deps.Settings.GetSubscriptionDefaults()
	} else {
		defaultAutoDownload = false
		defaultPriority = jobs.PriorityLow
		defaultFilter = music.DefaultReleaseFilter()
	}

	req := subscriptions.NewSubscription{
		Provider:         body.Provider,
		ArtistSourceID:   body.ArtistSourceID,
		ArtistName:       body.ArtistName,
		ArtistImageURL:   body.ArtistImageURL,
		AutoDownload:     defaultAutoDownload,
		ReleaseFilter:    &defaultFilter,
		DownloadPriority: &defaultPriority,
	}
	if body.AutoDownload != nil {
		req.AutoDownload = *body.AutoDownload
	}
	if body.ReleaseFilter != nil && body.ReleaseFilter.Any() {
		req.ReleaseFilter = body.ReleaseFilter
	}
	if body.DownloadPriority != nil && body.DownloadPriority.Valid() {
		req.DownloadPriority = body.DownloadPriority
	}

	sub, err := h.deps.Subscriptions.Create(r.Context(), req)
	if err != nil {
		response.Error(w, r, err)
		return
	}
	w.Header().Set("Location", "/api/v1/subscriptions/"+sub.ID)
	response.Data(w, http.StatusCreated, sub)
}

// GetSubscription answers GET /subscriptions/{id}, including the report of the
// most recent run when this process still has one.
func (h *Handlers) GetSubscription(w http.ResponseWriter, r *http.Request) {
	if h.deps.Subscriptions == nil {
		response.Fail(w, r, apperr.CodeInternal, "Subscriptions are not available.")
		return
	}
	id := chi.URLParam(r, "id")

	sub, err := h.deps.Subscriptions.Get(r.Context(), id)
	if err != nil {
		response.Error(w, r, err)
		return
	}

	payload := map[string]any{"subscription": sub}
	if result := h.deps.Subscriptions.LastResult(id); result != nil {
		payload["last_result"] = result
	}
	response.Data(w, http.StatusOK, payload)
}

// UpdateSubscription answers PATCH /subscriptions/{id}.
func (h *Handlers) UpdateSubscription(w http.ResponseWriter, r *http.Request) {
	if h.deps.Subscriptions == nil {
		response.Fail(w, r, apperr.CodeInternal, "Subscriptions are not available.")
		return
	}
	var body updateSubscriptionRequest
	if err := decodeJSON(r, &body); err != nil {
		response.Error(w, r, err)
		return
	}

	sub, err := h.deps.Subscriptions.Update(r.Context(), chi.URLParam(r, "id"),
		subscriptions.Update{
			Enabled:          body.Enabled,
			AutoDownload:     body.AutoDownload,
			ReleaseFilter:    body.ReleaseFilter,
			DownloadPriority: body.DownloadPriority,
		})
	if err != nil {
		response.Error(w, r, err)
		return
	}
	response.Data(w, http.StatusOK, sub)
}

// DeleteSubscription answers DELETE /subscriptions/{id}. Only the subscription
// goes: everything that was already downloaded stays in the library.
func (h *Handlers) DeleteSubscription(w http.ResponseWriter, r *http.Request) {
	if h.deps.Subscriptions == nil {
		response.Fail(w, r, apperr.CodeInternal, "Subscriptions are not available.")
		return
	}
	if err := h.deps.Subscriptions.Delete(r.Context(), chi.URLParam(r, "id")); err != nil {
		response.Error(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// SyncSubscription answers POST /subscriptions/{id}/sync.
//
// The run happens in the background and the request returns at once. Walking a
// discography takes longer than a request may be held open, and the download
// endpoints already work the same way: the answer is the accepted order, the
// progress arrives on the event stream.
func (h *Handlers) SyncSubscription(w http.ResponseWriter, r *http.Request) {
	if h.deps.Subscriptions == nil {
		response.Fail(w, r, apperr.CodeInternal, "Subscriptions are not available.")
		return
	}
	sub, err := h.deps.Subscriptions.StartSync(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		response.Error(w, r, err)
		return
	}
	response.Data(w, http.StatusAccepted, sub)
}

// ExportSubscriptions answers GET /subscriptions/export with a versioned JSON document.
func (h *Handlers) ExportSubscriptions(w http.ResponseWriter, r *http.Request) {
	if h.deps.Subscriptions == nil {
		response.Fail(w, r, apperr.CodeInternal, "Subscriptions are not available.")
		return
	}

	payload, err := h.deps.Subscriptions.Export(r.Context())
	if err != nil {
		response.Error(w, r, err)
		return
	}

	filename := fmt.Sprintf("ytmdl-subscriptions-%s.json", time.Now().UTC().Format("2006-01-02"))
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", filename))
	response.Data(w, http.StatusOK, payload)
}

// PreviewImportSubscriptions answers POST /subscriptions/import/preview.
func (h *Handlers) PreviewImportSubscriptions(w http.ResponseWriter, r *http.Request) {
	if h.deps.Subscriptions == nil {
		response.Fail(w, r, apperr.CodeInternal, "Subscriptions are not available.")
		return
	}

	var payload subscriptions.ExportPayload
	if err := decodeJSON(r, &payload); err != nil {
		response.Error(w, r, err)
		return
	}

	preview, err := h.deps.Subscriptions.PreviewImport(r.Context(), payload)
	if err != nil {
		response.Error(w, r, err)
		return
	}
	response.Data(w, http.StatusOK, preview)
}

// ApplyImportSubscriptions answers POST /subscriptions/import/apply.
func (h *Handlers) ApplyImportSubscriptions(w http.ResponseWriter, r *http.Request) {
	if h.deps.Subscriptions == nil {
		response.Fail(w, r, apperr.CodeInternal, "Subscriptions are not available.")
		return
	}

	var payload subscriptions.ExportPayload
	if err := decodeJSON(r, &payload); err != nil {
		response.Error(w, r, err)
		return
	}

	result, err := h.deps.Subscriptions.ApplyImport(r.Context(), payload)
	if err != nil {
		response.Error(w, r, err)
		return
	}
	response.Data(w, http.StatusOK, result)
}
