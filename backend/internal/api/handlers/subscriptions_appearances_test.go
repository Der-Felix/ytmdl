package handlers

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"ytdm/backend/internal/subscriptions"
)

// rawField reads one top-level field of the response's data object, so that
// a test sees what a client sees rather than what a Go struct defaults to.
func rawField(t *testing.T, body string, field string) json.RawMessage {
	t.Helper()
	var envelope struct {
		Data map[string]json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal([]byte(body), &envelope); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	value, ok := envelope.Data[field]
	if !ok {
		t.Fatalf("the response has no %q field: %s", field, body)
	}
	return value
}

func TestSubscribeIncludeAppearancesDefaultsToTrue(t *testing.T) {
	h := newSubscriptionTestHandler(t)

	recorder := call(t, h.Subscribe, http.MethodPost, "/subscriptions", map[string]any{
		"provider": "deezer", "artist_source_id": "27", "artist_name": "Daft Punk",
	}, nil)
	if recorder.Code != http.StatusCreated {
		t.Fatalf("status %d, body %s", recorder.Code, recorder.Body)
	}
	if got := string(rawField(t, recorder.Body.String(), "include_appearances")); got != "true" {
		t.Fatalf("include_appearances = %s, want true", got)
	}
}

func TestSubscribeAndUpdateIncludeAppearances(t *testing.T) {
	h := newSubscriptionTestHandler(t)

	recorder := call(t, h.Subscribe, http.MethodPost, "/subscriptions", map[string]any{
		"provider": "deezer", "artist_source_id": "4492087", "artist_name": "Bebe Rexha",
		"include_appearances": false,
	}, nil)
	if recorder.Code != http.StatusCreated {
		t.Fatalf("status %d, body %s", recorder.Code, recorder.Body)
	}
	sub := decodeData[subscriptions.Subscription](t, recorder)
	if sub.IncludeAppearances {
		t.Fatal("include_appearances=false was not stored on create")
	}

	// A PATCH that does not name the switch leaves it alone.
	recorder = call(t, h.UpdateSubscription, http.MethodPatch, "/subscriptions/"+sub.ID,
		map[string]any{"auto_download": true}, map[string]string{"id": sub.ID})
	if recorder.Code != http.StatusOK {
		t.Fatalf("status %d, body %s", recorder.Code, recorder.Body)
	}
	if got := string(rawField(t, recorder.Body.String(), "include_appearances")); got != "false" {
		t.Fatalf("an unrelated update changed include_appearances to %s", got)
	}

	// The switch alone is a valid change.
	recorder = call(t, h.UpdateSubscription, http.MethodPatch, "/subscriptions/"+sub.ID,
		map[string]any{"include_appearances": true}, map[string]string{"id": sub.ID})
	if recorder.Code != http.StatusOK {
		t.Fatalf("status %d, body %s", recorder.Code, recorder.Body)
	}
	updated := decodeData[subscriptions.Subscription](t, recorder)
	if !updated.IncludeAppearances || !updated.AutoDownload {
		t.Fatalf("the update did not apply as asked: %+v", updated)
	}

	list := decodeData[[]subscriptions.Subscription](t,
		call(t, h.ListSubscriptions, http.MethodGet, "/subscriptions", nil, nil))
	if len(list) != 1 || !list[0].IncludeAppearances {
		t.Fatalf("the list does not carry the switch: %+v", list)
	}
}

func TestImportReadsIncludeAppearancesWithAndWithoutTheField(t *testing.T) {
	h := newSubscriptionTestHandler(t)

	// One item from a file written before the field existed, one that turns
	// appearances off.
	body := `{"format":"` + subscriptions.ExportFormatName + `","version":1,"exported_at":"2026-01-01T00:00:00Z",
		"subscriptions":[
			{"artist_name":"Old","provider":"deezer","artist_source_id":"1","enabled":true,"auto_download":false,
			 "release_filter":{"albums":true,"singles":true,"eps":true},"download_priority":"low"},
			{"artist_name":"New","provider":"deezer","artist_source_id":"2","enabled":true,"auto_download":false,
			 "release_filter":{"albums":true,"singles":true,"eps":true},"download_priority":"low",
			 "include_appearances":false}]}`
	var payload subscriptions.ExportPayload
	if err := json.Unmarshal([]byte(body), &payload); err != nil {
		t.Fatalf("decode fixture: %v", err)
	}

	recorder := call(t, h.ApplyImportSubscriptions, http.MethodPost, "/subscriptions/import", payload, nil)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status %d, body %s", recorder.Code, recorder.Body)
	}

	list := decodeData[[]subscriptions.Subscription](t,
		call(t, h.ListSubscriptions, http.MethodGet, "/subscriptions", nil, nil))
	got := map[string]bool{}
	for _, sub := range list {
		got[sub.ArtistSourceID] = sub.IncludeAppearances
	}
	if !got["1"] {
		t.Fatal("a file without include_appearances must import as true")
	}
	if got["2"] {
		t.Fatal("include_appearances=false was not imported")
	}

	// The export always writes the field.
	export := call(t, h.ExportSubscriptions, http.MethodGet, "/subscriptions/export", nil, nil)
	if !strings.Contains(export.Body.String(), `"include_appearances":false`) ||
		!strings.Contains(export.Body.String(), `"include_appearances":true`) {
		t.Fatalf("the export does not carry the switch: %s", export.Body)
	}
}
