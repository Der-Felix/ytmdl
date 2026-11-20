package handlers

import (
	"encoding/json"
	"net/http"
	"strings"
	"sync/atomic"
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

// expectAppearances checks the stored choice and the effective value of a
// subscription response as raw JSON.
func expectAppearances(t *testing.T, label, body, own, effective string) {
	t.Helper()
	if got := string(rawField(t, body, "include_appearances")); got != own {
		t.Fatalf("%s: include_appearances = %s, want %s", label, got, own)
	}
	if got := string(rawField(t, body, "effective_include_appearances")); got != effective {
		t.Fatalf("%s: effective_include_appearances = %s, want %s", label, got, effective)
	}
}

func TestSubscribeFollowsTheSettingByDefault(t *testing.T) {
	for _, global := range []bool{true, false} {
		global := global
		h := newSubscriptionTestHandlerWithAppearances(t, func() bool { return global })

		recorder := call(t, h.Subscribe, http.MethodPost, "/subscriptions", map[string]any{
			"provider": "deezer", "artist_source_id": "27", "artist_name": "Daft Punk",
		}, nil)
		if recorder.Code != http.StatusCreated {
			t.Fatalf("status %d, body %s", recorder.Code, recorder.Body)
		}
		want := "true"
		if !global {
			want = "false"
		}
		expectAppearances(t, "create", recorder.Body.String(), "null", want)
	}
}

func TestPatchIncludeAppearancesTriState(t *testing.T) {
	var global atomic.Bool
	global.Store(true)
	h := newSubscriptionTestHandlerWithAppearances(t, global.Load)

	recorder := call(t, h.Subscribe, http.MethodPost, "/subscriptions", map[string]any{
		"provider": "deezer", "artist_source_id": "4492087", "artist_name": "Bebe Rexha",
		"include_appearances": false,
	}, nil)
	if recorder.Code != http.StatusCreated {
		t.Fatalf("status %d, body %s", recorder.Code, recorder.Body)
	}
	expectAppearances(t, "create with false", recorder.Body.String(), "false", "false")
	sub := decodeData[subscriptions.Subscription](t, recorder)
	params := map[string]string{"id": sub.ID}

	steps := []struct {
		label          string
		body           map[string]any
		own, effective string
	}{
		{"absent", map[string]any{"auto_download": true}, "false", "false"},
		{"true", map[string]any{"include_appearances": true}, "true", "true"},
		{"null", map[string]any{"include_appearances": nil}, "null", "true"},
		{"false", map[string]any{"include_appearances": false}, "false", "false"},
	}
	for _, step := range steps {
		recorder = call(t, h.UpdateSubscription, http.MethodPatch, "/subscriptions/"+sub.ID, step.body, params)
		if recorder.Code != http.StatusOK {
			t.Fatalf("%s: status %d, body %s", step.label, recorder.Code, recorder.Body)
		}
		expectAppearances(t, step.label, recorder.Body.String(), step.own, step.effective)
	}
	if got := string(rawField(t, recorder.Body.String(), "auto_download")); got != "true" {
		t.Fatalf("auto_download = %s; the unrelated update was lost", got)
	}

	// A wrong type is rejected rather than read as null.
	recorder = call(t, h.UpdateSubscription, http.MethodPatch, "/subscriptions/"+sub.ID,
		map[string]any{"include_appearances": "yes"}, params)
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("a string must be rejected, got %d: %s", recorder.Code, recorder.Body)
	}

	// Back to following the setting, then the setting changes: the listing
	// reports the new effective value without the subscription being touched.
	call(t, h.UpdateSubscription, http.MethodPatch, "/subscriptions/"+sub.ID,
		map[string]any{"include_appearances": nil}, params)
	global.Store(false)
	list := call(t, h.ListSubscriptions, http.MethodGet, "/subscriptions", nil, nil)
	var envelope struct {
		Data []map[string]json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(list.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("decode list: %v", err)
	}
	if len(envelope.Data) != 1 ||
		string(envelope.Data[0]["include_appearances"]) != "null" ||
		string(envelope.Data[0]["effective_include_appearances"]) != "false" {
		t.Fatalf("the list does not follow the setting: %s", list.Body)
	}
}

func TestImportAndExportIncludeAppearances(t *testing.T) {
	h := newSubscriptionTestHandler(t)

	// One item from a file written before the field existed, one with an
	// explicit null, one that turns appearances off.
	body := `{"format":"` + subscriptions.ExportFormatName + `","version":1,"exported_at":"2026-01-01T00:00:00Z",
		"subscriptions":[
			{"artist_name":"Old","provider":"deezer","artist_source_id":"1","enabled":true,"auto_download":false,
			 "release_filter":{"albums":true,"singles":true,"eps":true},"download_priority":"low"},
			{"artist_name":"Null","provider":"deezer","artist_source_id":"3","enabled":true,"auto_download":false,
			 "release_filter":{"albums":true,"singles":true,"eps":true},"download_priority":"low",
			 "include_appearances":null},
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
	got := map[string]*bool{}
	for _, sub := range list {
		got[sub.ArtistSourceID] = sub.IncludeAppearances
		if sub.EffectiveIncludeAppearances != (sub.ArtistSourceID != "2") {
			t.Fatalf("%s: effective value %v", sub.ArtistSourceID, sub.EffectiveIncludeAppearances)
		}
	}
	if got["1"] != nil || got["3"] != nil {
		t.Fatal("absent and null must import as following the setting")
	}
	if got["2"] == nil || *got["2"] {
		t.Fatal("include_appearances=false was not imported")
	}

	// The export writes the field only for the subscription that has a choice.
	export := call(t, h.ExportSubscriptions, http.MethodGet, "/subscriptions/export", nil, nil)
	if n := strings.Count(export.Body.String(), `"include_appearances"`); n != 1 ||
		!strings.Contains(export.Body.String(), `"include_appearances":false`) {
		t.Fatalf("the export must carry exactly the one own choice (found %d): %s", n, export.Body)
	}
}
