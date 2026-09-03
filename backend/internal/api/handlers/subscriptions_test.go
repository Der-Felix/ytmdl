package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"ytdm/backend/internal/database/dbtest"
	"ytdm/backend/internal/database/repository"
	"ytdm/backend/internal/discography"
	"ytdm/backend/internal/jobs"
	"ytdm/backend/internal/music"
	"ytdm/backend/internal/provider"
	"ytdm/backend/internal/subscriptions"
)

// subscriptionProvider is a metadata provider with one artist and one album.
type subscriptionProvider struct{}

func (*subscriptionProvider) Name() string { return "deezer" }

func (*subscriptionProvider) SearchArtists(context.Context, string) ([]music.Artist, error) {
	return nil, nil
}

func (*subscriptionProvider) GetArtist(_ context.Context, id string) (*music.Artist, error) {
	return &music.Artist{Name: "Daft Punk", Provider: "deezer", SourceID: id}, nil
}

func (*subscriptionProvider) GetDiscography(context.Context, string) ([]music.Release, error) {
	return []music.Release{{
		Title: "Discovery", ReleaseType: music.ReleaseAlbum, Year: 2001,
		Provider: "deezer", SourceID: "302127",
	}}, nil
}

func (*subscriptionProvider) GetRelease(_ context.Context, id string) (*music.Release, error) {
	return &music.Release{Title: "Discovery", Provider: "deezer", SourceID: id}, nil
}

func (*subscriptionProvider) GetReleaseTracks(context.Context, string) ([]music.Track, error) {
	return []music.Track{{
		Title: "One More Time", Artists: []string{"Daft Punk"}, DurationMS: 320_000,
		SourceProvider: "deezer", SourceID: "3135556",
	}}, nil
}

// newSubscriptionTestHandler wires the real service against a real database,
// so the endpoints are exercised over the same code path production uses.
func newSubscriptionTestHandler(t *testing.T) *Handlers {
	t.Helper()

	db := dbtest.Open(t)
	quiet := slog.New(slog.NewTextHandler(io.Discard, nil))

	registry := provider.NewRegistry()
	registry.RegisterMetadata(&subscriptionProvider{})

	disco, err := discography.NewService(discography.Options{Registry: registry, Logger: quiet})
	if err != nil {
		t.Fatalf("discography service: %v", err)
	}

	service, err := subscriptions.New(subscriptions.Options{
		Store:         repository.NewSubscriptions(db),
		Catalog:       repository.NewCatalog(db),
		Files:         repository.NewFiles(db),
		Discography:   disco,
		Registry:      registry,
		Broker:        jobs.NewBroker(quiet),
		Logger:        quiet,
		SyncInterval:  24 * time.Hour,
		RetryInterval: time.Hour,
	})
	if err != nil {
		t.Fatalf("subscription service: %v", err)
	}
	if err := service.Start(context.Background()); err != nil {
		t.Fatalf("start service: %v", err)
	}
	t.Cleanup(service.Stop)

	return &Handlers{
		deps:        Deps{Subscriptions: service, Registry: registry, Logger: quiet},
		healthCache: make(map[string]checkResult),
	}
}

// call runs one handler with a JSON body and the given URL parameters.
func call(t *testing.T, handler http.HandlerFunc, method, target string, body any, params map[string]string) *httptest.ResponseRecorder {
	t.Helper()

	var reader io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("encode body: %v", err)
		}
		reader = bytes.NewReader(raw)
	}

	request := httptest.NewRequest(method, target, reader)
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	if len(params) > 0 {
		routeCtx := chi.NewRouteContext()
		for key, value := range params {
			routeCtx.URLParams.Add(key, value)
		}
		request = request.WithContext(
			context.WithValue(request.Context(), chi.RouteCtxKey, routeCtx))
	}

	recorder := httptest.NewRecorder()
	handler(recorder, request)
	return recorder
}

func decodeData[T any](t *testing.T, recorder *httptest.ResponseRecorder) T {
	t.Helper()
	var body struct {
		Data T `json:"data"`
	}
	if err := json.NewDecoder(recorder.Body).Decode(&body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	return body.Data
}

func decodeErrorCode(t *testing.T, recorder *httptest.ResponseRecorder) string {
	t.Helper()
	var body struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if err := json.NewDecoder(recorder.Body).Decode(&body); err != nil {
		t.Fatalf("decode error response: %v", err)
	}
	return body.Error.Code
}

func subscribeVia(t *testing.T, h *Handlers) subscriptions.Subscription {
	t.Helper()
	recorder := call(t, h.Subscribe, http.MethodPost, "/subscriptions", map[string]any{
		"provider":         "deezer",
		"artist_source_id": "27",
		"artist_name":      "Daft Punk",
	}, nil)
	if recorder.Code != http.StatusCreated {
		t.Fatalf("subscribe: status %d, body %s", recorder.Code, recorder.Body)
	}
	return decodeData[subscriptions.Subscription](t, recorder)
}

/* -------------------------------------------------------------------- tests */

func TestSubscribeEndpoint(t *testing.T) {
	h := newSubscriptionTestHandler(t)

	recorder := call(t, h.Subscribe, http.MethodPost, "/subscriptions", map[string]any{
		"provider":         "deezer",
		"artist_source_id": "27",
		"artist_name":      "Daft Punk",
		"auto_download":    true,
	}, nil)

	if recorder.Code != http.StatusCreated {
		t.Fatalf("status %d, body %s", recorder.Code, recorder.Body)
	}
	sub := decodeData[subscriptions.Subscription](t, recorder)
	if sub.ID == "" {
		t.Fatal("the answer carries no id")
	}
	if !sub.AutoDownload {
		t.Fatal("auto download was requested but not stored")
	}
	if location := recorder.Header().Get("Location"); location != "/api/v1/subscriptions/"+sub.ID {
		t.Fatalf("the Location header is wrong: %q", location)
	}
}

func TestSubscribeTwiceReturnsTheSameSubscription(t *testing.T) {
	h := newSubscriptionTestHandler(t)

	first := subscribeVia(t, h)
	second := subscribeVia(t, h)
	if first.ID != second.ID {
		t.Fatalf("subscribing twice produced two subscriptions: %q and %q", first.ID, second.ID)
	}

	list := decodeData[[]subscriptions.Subscription](t,
		call(t, h.ListSubscriptions, http.MethodGet, "/subscriptions", nil, nil))
	if len(list) != 1 {
		t.Fatalf("expected one subscription, got %d", len(list))
	}
}

func TestSubscribeRejectsAMissingArtist(t *testing.T) {
	h := newSubscriptionTestHandler(t)

	recorder := call(t, h.Subscribe, http.MethodPost, "/subscriptions",
		map[string]any{"provider": "deezer"}, nil)

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status %d, body %s", recorder.Code, recorder.Body)
	}
	if code := decodeErrorCode(t, recorder); code != "INVALID_REQUEST" {
		t.Fatalf("error code %q", code)
	}
}

func TestSubscribeRejectsAnUnknownProvider(t *testing.T) {
	h := newSubscriptionTestHandler(t)

	recorder := call(t, h.Subscribe, http.MethodPost, "/subscriptions", map[string]any{
		"provider":         "not-a-provider",
		"artist_source_id": "27",
		"artist_name":      "Daft Punk",
	}, nil)

	if recorder.Code != http.StatusNotFound {
		t.Fatalf("status %d, body %s", recorder.Code, recorder.Body)
	}
	if code := decodeErrorCode(t, recorder); code != "PROVIDER_NOT_FOUND" {
		t.Fatalf("error code %q", code)
	}
}

// The artist page asks "is this artist watched?" through the list filter.
func TestListSubscriptionsFiltersByArtist(t *testing.T) {
	h := newSubscriptionTestHandler(t)
	sub := subscribeVia(t, h)

	match := decodeData[[]subscriptions.Subscription](t, call(t, h.ListSubscriptions,
		http.MethodGet, "/subscriptions?provider=deezer&artist_source_id=27", nil, nil))
	if len(match) != 1 || match[0].ID != sub.ID {
		t.Fatalf("the watched artist was not found: %+v", match)
	}

	none := decodeData[[]subscriptions.Subscription](t, call(t, h.ListSubscriptions,
		http.MethodGet, "/subscriptions?provider=deezer&artist_source_id=999", nil, nil))
	if len(none) != 0 {
		t.Fatalf("an unwatched artist matched: %+v", none)
	}
}

func TestGetSubscriptionEndpoint(t *testing.T) {
	h := newSubscriptionTestHandler(t)
	sub := subscribeVia(t, h)

	recorder := call(t, h.GetSubscription, http.MethodGet, "/subscriptions/"+sub.ID,
		nil, map[string]string{"id": sub.ID})
	if recorder.Code != http.StatusOK {
		t.Fatalf("status %d, body %s", recorder.Code, recorder.Body)
	}

	payload := decodeData[struct {
		Subscription subscriptions.Subscription `json:"subscription"`
		LastResult   *subscriptions.SyncResult  `json:"last_result"`
	}](t, recorder)

	if payload.Subscription.ID != sub.ID {
		t.Fatalf("the wrong subscription was returned: %+v", payload.Subscription)
	}
	if payload.LastResult != nil {
		t.Fatalf("a subscription that never ran must carry no report: %+v", payload.LastResult)
	}
}

func TestGetSubscriptionNotFound(t *testing.T) {
	h := newSubscriptionTestHandler(t)

	recorder := call(t, h.GetSubscription, http.MethodGet, "/subscriptions/missing",
		nil, map[string]string{"id": "missing"})

	if recorder.Code != http.StatusNotFound {
		t.Fatalf("status %d, body %s", recorder.Code, recorder.Body)
	}
	if code := decodeErrorCode(t, recorder); code != "SUBSCRIPTION_NOT_FOUND" {
		t.Fatalf("error code %q", code)
	}
}

func TestUpdateSubscriptionEndpoint(t *testing.T) {
	h := newSubscriptionTestHandler(t)
	sub := subscribeVia(t, h)

	recorder := call(t, h.UpdateSubscription, http.MethodPatch, "/subscriptions/"+sub.ID,
		map[string]any{"auto_download": true}, map[string]string{"id": sub.ID})
	if recorder.Code != http.StatusOK {
		t.Fatalf("status %d, body %s", recorder.Code, recorder.Body)
	}
	updated := decodeData[subscriptions.Subscription](t, recorder)
	if !updated.AutoDownload {
		t.Fatal("auto download was not switched on")
	}
	if !updated.Enabled {
		t.Fatal("a field the request did not name must keep its value")
	}

	recorder = call(t, h.UpdateSubscription, http.MethodPatch, "/subscriptions/"+sub.ID,
		map[string]any{"enabled": false}, map[string]string{"id": sub.ID})
	updated = decodeData[subscriptions.Subscription](t, recorder)
	if updated.Enabled {
		t.Fatal("the subscription was not disabled")
	}
	if !updated.AutoDownload {
		t.Fatal("disabling reset auto download")
	}
}

func TestUpdateSubscriptionRejectsAnEmptyChange(t *testing.T) {
	h := newSubscriptionTestHandler(t)
	sub := subscribeVia(t, h)

	recorder := call(t, h.UpdateSubscription, http.MethodPatch, "/subscriptions/"+sub.ID,
		map[string]any{}, map[string]string{"id": sub.ID})

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status %d, body %s", recorder.Code, recorder.Body)
	}
}

func TestUpdateSubscriptionRejectsAnUnknownField(t *testing.T) {
	h := newSubscriptionTestHandler(t)
	sub := subscribeVia(t, h)

	recorder := call(t, h.UpdateSubscription, http.MethodPatch, "/subscriptions/"+sub.ID,
		map[string]any{"auto_downlod": true}, map[string]string{"id": sub.ID})

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("a typo in a client must be reported, got status %d", recorder.Code)
	}
}

func TestDeleteSubscriptionEndpoint(t *testing.T) {
	h := newSubscriptionTestHandler(t)
	sub := subscribeVia(t, h)

	recorder := call(t, h.DeleteSubscription, http.MethodDelete, "/subscriptions/"+sub.ID,
		nil, map[string]string{"id": sub.ID})
	if recorder.Code != http.StatusNoContent {
		t.Fatalf("status %d, body %s", recorder.Code, recorder.Body)
	}
	if recorder.Body.Len() != 0 {
		t.Fatalf("a 204 must carry no body: %s", recorder.Body)
	}

	recorder = call(t, h.GetSubscription, http.MethodGet, "/subscriptions/"+sub.ID,
		nil, map[string]string{"id": sub.ID})
	if recorder.Code != http.StatusNotFound {
		t.Fatalf("the subscription is still readable: status %d", recorder.Code)
	}
}

func TestDeleteSubscriptionNotFound(t *testing.T) {
	h := newSubscriptionTestHandler(t)

	recorder := call(t, h.DeleteSubscription, http.MethodDelete, "/subscriptions/missing",
		nil, map[string]string{"id": "missing"})

	if recorder.Code != http.StatusNotFound {
		t.Fatalf("status %d, body %s", recorder.Code, recorder.Body)
	}
}

// The sync endpoint accepts the order and returns at once; walking a
// discography takes longer than a request may be held open.
func TestSyncSubscriptionIsAccepted(t *testing.T) {
	h := newSubscriptionTestHandler(t)
	sub := subscribeVia(t, h)

	recorder := call(t, h.SyncSubscription, http.MethodPost, "/subscriptions/"+sub.ID+"/sync",
		nil, map[string]string{"id": sub.ID})
	if recorder.Code != http.StatusAccepted {
		t.Fatalf("status %d, body %s", recorder.Code, recorder.Body)
	}
	if started := decodeData[subscriptions.Subscription](t, recorder); !started.Syncing {
		t.Fatal("the answer does not report the run as started")
	}

	// The run finishes in the background and leaves its report behind.
	deadline := time.Now().Add(10 * time.Second)
	for {
		got := call(t, h.GetSubscription, http.MethodGet, "/subscriptions/"+sub.ID,
			nil, map[string]string{"id": sub.ID})
		payload := decodeData[struct {
			Subscription subscriptions.Subscription `json:"subscription"`
			LastResult   *subscriptions.SyncResult  `json:"last_result"`
		}](t, got)

		if payload.LastResult != nil {
			if payload.LastResult.NewTracks != 1 {
				t.Fatalf("the report is wrong: %+v", payload.LastResult)
			}
			if payload.Subscription.LastSyncStatus != subscriptions.StatusSuccess {
				t.Fatalf("the run was not recorded as successful: %q",
					payload.Subscription.LastSyncStatus)
			}
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("the background run never finished")
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func TestSyncSubscriptionNotFound(t *testing.T) {
	h := newSubscriptionTestHandler(t)

	recorder := call(t, h.SyncSubscription, http.MethodPost, "/subscriptions/missing/sync",
		nil, map[string]string{"id": "missing"})

	if recorder.Code != http.StatusNotFound {
		t.Fatalf("status %d, body %s", recorder.Code, recorder.Body)
	}
}

func TestExportSubscriptionsEndpoint(t *testing.T) {
	h := newSubscriptionTestHandler(t)
	sub := subscribeVia(t, h)

	recorder := call(t, h.ExportSubscriptions, http.MethodGet, "/subscriptions/export", nil, nil)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status %d, body %s", recorder.Code, recorder.Body)
	}

	cd := recorder.Header().Get("Content-Disposition")
	if !strings.Contains(cd, "attachment") || !strings.Contains(cd, "ytmdl-subscriptions-") {
		t.Fatalf("unexpected Content-Disposition: %q", cd)
	}

	export := decodeData[subscriptions.ExportPayload](t, recorder)
	if export.Format != subscriptions.ExportFormatName {
		t.Fatalf("expected format %q, got %q", subscriptions.ExportFormatName, export.Format)
	}
	if export.Version != subscriptions.ExportFormatVersion {
		t.Fatalf("expected version %d, got %d", subscriptions.ExportFormatVersion, export.Version)
	}
	if len(export.Subscriptions) != 1 {
		t.Fatalf("expected 1 subscription, got %d", len(export.Subscriptions))
	}
	exportedItem := export.Subscriptions[0]
	if exportedItem.ArtistName != sub.ArtistName || exportedItem.ArtistSourceID != sub.ArtistSourceID || exportedItem.Provider != sub.Provider {
		t.Fatalf("exported item does not match: %+v", exportedItem)
	}

	// Verify no secrets or internal IDs in raw JSON
	rawJSON := recorder.Body.String()
	if strings.Contains(rawJSON, sub.ID) {
		t.Fatalf("exported payload must not contain internal database subscription ID %q: %s", sub.ID, rawJSON)
	}
	if strings.Contains(rawJSON, "password") || strings.Contains(rawJSON, "secret") || strings.Contains(rawJSON, "token") {
		t.Fatalf("exported payload contains sensitive keys: %s", rawJSON)
	}
}

func TestImportPreviewEndpoint(t *testing.T) {
	h := newSubscriptionTestHandler(t)
	existingSub := subscribeVia(t, h)

	// Valid payload with 1 new, 1 would_update, 1 unchanged, 1 duplicate, 1 invalid
	payload := subscriptions.ExportPayload{
		Format:     subscriptions.ExportFormatName,
		Version:    subscriptions.ExportFormatVersion,
		ExportedAt: time.Now().UTC(),
		Subscriptions: []subscriptions.ExportSubscription{
			// Existing sub with change (would_update)
			{
				ArtistName:       existingSub.ArtistName,
				Provider:         existingSub.Provider,
				ArtistSourceID:   existingSub.ArtistSourceID,
				Enabled:          false, // Changed from true
				AutoDownload:     true,  // Changed from false
				ReleaseFilter:    music.DefaultReleaseFilter(),
				DownloadPriority: jobs.PriorityHigh,
			},
			// New sub
			{
				ArtistName:       "New Artist",
				Provider:         "deezer",
				ArtistSourceID:   "99999",
				Enabled:          true,
				AutoDownload:     false,
				ReleaseFilter:    music.DefaultReleaseFilter(),
				DownloadPriority: jobs.PriorityLow,
			},
			// Duplicate of new sub within same file
			{
				ArtistName:       "New Artist Duplicate",
				Provider:         "deezer",
				ArtistSourceID:   "99999",
				Enabled:          true,
				AutoDownload:     false,
				ReleaseFilter:    music.DefaultReleaseFilter(),
				DownloadPriority: jobs.PriorityLow,
			},
			// Invalid item (missing source ID)
			{
				ArtistName: "Invalid Artist",
				Provider:   "deezer",
			},
		},
	}

	// Preview request
	recorder := call(t, h.PreviewImportSubscriptions, http.MethodPost, "/subscriptions/import/preview", payload, nil)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status %d, body %s", recorder.Code, recorder.Body)
	}

	preview := decodeData[subscriptions.ImportPreview](t, recorder)
	if preview.Total != 4 {
		t.Fatalf("expected total 4, got %d", preview.Total)
	}
	if preview.New != 1 {
		t.Fatalf("expected 1 new, got %d", preview.New)
	}
	if preview.WouldUpdate != 1 {
		t.Fatalf("expected 1 would_update, got %d", preview.WouldUpdate)
	}
	if preview.Duplicates != 1 {
		t.Fatalf("expected 1 duplicate, got %d", preview.Duplicates)
	}
	if preview.Invalid != 1 {
		t.Fatalf("expected 1 invalid, got %d", preview.Invalid)
	}

	// Verify preview caused NO database mutations
	listAfter := decodeData[[]subscriptions.Subscription](t,
		call(t, h.ListSubscriptions, http.MethodGet, "/subscriptions", nil, nil))
	if len(listAfter) != 1 {
		t.Fatalf("preview must NOT mutate the database; expected 1 subscription, got %d", len(listAfter))
	}
	if listAfter[0].Enabled != true {
		t.Fatal("preview must NOT modify existing subscription flags")
	}
}

func TestImportApplyAndIdempotency(t *testing.T) {
	h := newSubscriptionTestHandler(t)
	existingSub := subscribeVia(t, h)

	payload := subscriptions.ExportPayload{
		Format:     subscriptions.ExportFormatName,
		Version:    subscriptions.ExportFormatVersion,
		ExportedAt: time.Now().UTC(),
		Subscriptions: []subscriptions.ExportSubscription{
			// Update existing
			{
				ArtistName:       existingSub.ArtistName,
				Provider:         existingSub.Provider,
				ArtistSourceID:   existingSub.ArtistSourceID,
				Enabled:          false,
				AutoDownload:     true,
				ReleaseFilter:    music.DefaultReleaseFilter(),
				DownloadPriority: jobs.PriorityHigh,
			},
			// New subscription
			{
				ArtistName:       "Justice",
				Provider:         "deezer",
				ArtistSourceID:   "55555",
				Enabled:          true,
				AutoDownload:     true,
				ReleaseFilter:    music.DefaultReleaseFilter(),
				DownloadPriority: jobs.PriorityNormal,
			},
		},
	}

	// Apply import
	recorder := call(t, h.ApplyImportSubscriptions, http.MethodPost, "/subscriptions/import/apply", payload, nil)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status %d, body %s", recorder.Code, recorder.Body)
	}

	result := decodeData[subscriptions.ImportResult](t, recorder)
	if result.Created != 1 {
		t.Fatalf("expected 1 created, got %d", result.Created)
	}
	if result.Updated != 1 {
		t.Fatalf("expected 1 updated, got %d", result.Updated)
	}
	if result.Failed != 0 {
		t.Fatalf("expected 0 failed, got %d", result.Failed)
	}

	// Verify database state
	list := decodeData[[]subscriptions.Subscription](t,
		call(t, h.ListSubscriptions, http.MethodGet, "/subscriptions", nil, nil))
	if len(list) != 2 {
		t.Fatalf("expected 2 subscriptions in DB, got %d", len(list))
	}

	// Check updated subscription values
	var updatedFound bool
	for _, s := range list {
		if s.ID == existingSub.ID {
			updatedFound = true
			if s.Enabled != false {
				t.Fatalf("expected enabled=false, got %v", s.Enabled)
			}
			if s.AutoDownload != true {
				t.Fatalf("expected auto_download=true, got %v", s.AutoDownload)
			}
			if s.DownloadPriority != jobs.PriorityHigh {
				t.Fatalf("expected priority high, got %v", s.DownloadPriority)
			}
		}
	}
	if !updatedFound {
		t.Fatal("updated subscription was not found in DB")
	}

	// Test idempotency: re-exporting and re-importing the same payload
	exportRecorder := call(t, h.ExportSubscriptions, http.MethodGet, "/subscriptions/export", nil, nil)
	reExport := decodeData[subscriptions.ExportPayload](t, exportRecorder)

	previewRecorder := call(t, h.PreviewImportSubscriptions, http.MethodPost, "/subscriptions/import/preview", reExport, nil)
	rePreview := decodeData[subscriptions.ImportPreview](t, previewRecorder)
	if rePreview.New != 0 {
		t.Fatalf("idempotency check: expected 0 new, got %d", rePreview.New)
	}
	if rePreview.WouldUpdate != 0 {
		t.Fatalf("idempotency check: expected 0 would_update, got %d", rePreview.WouldUpdate)
	}
	if rePreview.Unchanged != 2 {
		t.Fatalf("idempotency check: expected 2 unchanged, got %d", rePreview.Unchanged)
	}

	// Re-applying produces 0 mutations
	reApplyRecorder := call(t, h.ApplyImportSubscriptions, http.MethodPost, "/subscriptions/import/apply", reExport, nil)
	reApply := decodeData[subscriptions.ImportResult](t, reApplyRecorder)
	if reApply.Created != 0 || reApply.Updated != 0 || reApply.Unchanged != 2 {
		t.Fatalf("idempotency re-apply failed: created=%d, updated=%d, unchanged=%d", reApply.Created, reApply.Updated, reApply.Unchanged)
	}
}

func TestImportInvalidFormatAndVersion(t *testing.T) {
	h := newSubscriptionTestHandler(t)

	// Wrong format
	badFormat := subscriptions.ExportPayload{
		Format:  "invalid-format",
		Version: 1,
	}
	recorder := call(t, h.PreviewImportSubscriptions, http.MethodPost, "/subscriptions/import/preview", badFormat, nil)
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("expected status 400 for bad format, got %d", recorder.Code)
	}

	// Wrong version
	badVersion := subscriptions.ExportPayload{
		Format:  subscriptions.ExportFormatName,
		Version: 99,
	}
	recorder = call(t, h.PreviewImportSubscriptions, http.MethodPost, "/subscriptions/import/preview", badVersion, nil)
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("expected status 400 for bad version, got %d", recorder.Code)
	}
}
