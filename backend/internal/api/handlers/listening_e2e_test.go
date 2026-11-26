package handlers_test

import (
	"encoding/json"
	"net/http"
	"testing"
)

type stationPage struct {
	Data struct {
		Title string `json:"title"`
		Seed  struct {
			Kind string `json:"kind"`
			ID   string `json:"id"`
			Name string `json:"name"`
		} `json:"seed"`
		Tracks []struct {
			ID string `json:"id"`
		} `json:"tracks"`
		NextCursor *string `json:"next_cursor"`
		Remaining  int     `json:"remaining"`
	} `json:"data"`
}

// stationIDs walks a station batch by batch and returns every track id.
func stationIDs(t *testing.T, c *testClient, query string) (string, []string) {
	t.Helper()
	path := "/api/v1/library/radio/station?" + query
	ids, title := []string{}, ""
	for range 10 {
		res, raw, err := c.do(http.MethodGet, path, nil, false)
		var page stationPage
		if err != nil || res.StatusCode != http.StatusOK || json.Unmarshal(raw, &page) != nil {
			t.Fatal("station", res, err, string(raw))
		}
		title = page.Data.Title
		for _, tr := range page.Data.Tracks {
			ids = append(ids, tr.ID)
		}
		if page.Data.NextCursor == nil {
			return title, ids
		}
		path = "/api/v1/library/radio/station?" + query + "&cursor=" + *page.Data.NextCursor
	}
	t.Fatal("station never ended")
	return "", nil
}

func TestListeningPreferencesStationStatsAndSearchOverHTTP(t *testing.T) {
	_, admin, user, ids := setupPlaylistsE2ETest(t)

	// A station in batches of one: every track once, then no cursor.
	title, got := stationIDs(t, user, "artist=art_e2e&limit=1&nonce=n")
	seen := map[string]bool{}
	for _, id := range got {
		if seen[id] {
			t.Fatal("station repeated", id, got)
		}
		seen[id] = true
	}
	if title != "Radio: Daft Punk" || len(got) != 3 {
		t.Fatalf("station %q %v", title, got)
	}
	if res, _, _ := user.do(http.MethodGet, "/api/v1/library/radio/station?cursor=broken", nil, false); res.StatusCode != http.StatusBadRequest {
		t.Fatal("broken cursor accepted", res.StatusCode)
	}
	if res, _, _ := user.do(http.MethodGet, "/api/v1/library/radio?seed="+ids[0]+"&artist=art_e2e", nil, false); res.StatusCode != http.StatusBadRequest {
		t.Fatal("two seeds accepted", res.StatusCode)
	}
	if res, _, _ := user.do(http.MethodGet, "/api/v1/library/radio/station?artist=missing", nil, false); res.StatusCode != http.StatusNotFound {
		t.Fatal("unknown artist", res.StatusCode)
	}

	// Preferences: CSRF, idempotency, privacy.
	body := map[string]any{"kind": "track", "target": ids[0]}
	if res, _, _ := user.do(http.MethodPut, "/api/v1/listening-preferences", body, false); res.StatusCode != http.StatusForbidden {
		t.Fatal("preference without CSRF accepted", res.StatusCode)
	}
	var pref struct {
		Data struct {
			ID, Kind, Target, Name string
		}
	}
	res, raw, err := user.do(http.MethodPut, "/api/v1/listening-preferences", body, true)
	if err != nil || res.StatusCode != http.StatusOK || json.Unmarshal(raw, &pref) != nil || pref.Data.ID == "" || pref.Data.Name != "One More Time" {
		t.Fatal("add preference", res, err, string(raw))
	}
	first := pref.Data.ID
	if res, raw, _ = user.do(http.MethodPut, "/api/v1/listening-preferences", body, true); json.Unmarshal(raw, &pref) != nil || pref.Data.ID != first {
		t.Fatal("preference not idempotent", string(raw))
	}
	for _, bad := range []struct {
		body   map[string]any
		status int
	}{
		{map[string]any{"kind": "mood", "target": "x"}, http.StatusBadRequest},
		{map[string]any{"kind": "track", "target": "missing"}, http.StatusNotFound},
		{map[string]any{"kind": "artist", "target": "missing"}, http.StatusNotFound},
	} {
		if res, _, _ = user.do(http.MethodPut, "/api/v1/listening-preferences", bad.body, true); res.StatusCode != bad.status {
			t.Fatalf("%v: %d", bad.body, res.StatusCode)
		}
	}
	var list struct{ Data []map[string]any }
	if _, raw, _ = admin.do(http.MethodGet, "/api/v1/listening-preferences", nil, false); json.Unmarshal(raw, &list) != nil || list.Data == nil || len(list.Data) != 0 {
		t.Fatal("preferences not private or not an array", string(raw))
	}
	if res, _, _ = admin.do(http.MethodDelete, "/api/v1/listening-preferences/"+first, nil, true); res.StatusCode != http.StatusNotFound {
		t.Fatal("another user deleted a preference", res.StatusCode)
	}

	// The excluded track leaves the radio, also the plain list.
	if _, got = stationIDs(t, user, "artist=art_e2e&limit=2&nonce=m"); len(got) != 2 || got[0] == ids[0] || got[1] == ids[0] {
		t.Fatalf("excluded track in a station: %v", got)
	}
	var radio struct{ Data []struct{ ID string } }
	if res, raw, err = user.do(http.MethodGet, "/api/v1/library/radio?artist=art_e2e&limit=10", nil, false); err != nil || res.StatusCode != 200 || json.Unmarshal(raw, &radio) != nil || len(radio.Data) != 2 {
		t.Fatal("radio list", res, err, string(raw))
	}
	for _, tr := range radio.Data {
		if tr.ID == ids[0] {
			t.Fatal("excluded track in the radio list")
		}
	}
	if res, _, _ = user.do(http.MethodDelete, "/api/v1/listening-preferences?kind=track&target="+ids[0], nil, true); res.StatusCode != http.StatusNoContent {
		t.Fatal("remove by target", res.StatusCode)
	}
	if _, raw, _ = user.do(http.MethodGet, "/api/v1/listening-preferences", nil, false); json.Unmarshal(raw, &list) != nil || len(list.Data) != 0 {
		t.Fatal("preference not removed", string(raw))
	}

	// Statistics.
	event := map[string]any{"track_id": ids[1], "event_id": "event-stats-0000000001"}
	if res, _, _ = user.do(http.MethodPost, "/api/v1/history", event, true); res.StatusCode != http.StatusNoContent {
		t.Fatal("history write", res.StatusCode)
	}
	var stats struct {
		Data struct {
			Period   string `json:"period"`
			TimeZone string `json:"time_zone"`
			Totals   struct {
				Plays int `json:"plays"`
			} `json:"totals"`
			TopTracks []struct {
				Track struct{ ID string } `json:"track"`
				Plays int                 `json:"plays"`
			} `json:"top_tracks"`
			Activity struct {
				Points []map[string]any `json:"points"`
			} `json:"activity"`
		}
	}
	res, raw, err = user.do(http.MethodGet, "/api/v1/history/stats?period=week&tz=Europe/Berlin", nil, false)
	if err != nil || res.StatusCode != 200 || json.Unmarshal(raw, &stats) != nil || stats.Data.Period != "week" || stats.Data.TimeZone != "Europe/Berlin" ||
		stats.Data.Totals.Plays != 1 || len(stats.Data.TopTracks) != 1 || stats.Data.TopTracks[0].Track.ID != ids[1] || len(stats.Data.Activity.Points) != 8 {
		t.Fatal("stats", res, err, string(raw))
	}
	if _, raw, _ = admin.do(http.MethodGet, "/api/v1/history/stats", nil, false); json.Unmarshal(raw, &stats) != nil || stats.Data.Period != "month" || stats.Data.Totals.Plays != 0 {
		t.Fatal("stats not private", string(raw))
	}
	for _, q := range []string{"period=decade", "tz=Mars/Base"} {
		if res, _, _ = user.do(http.MethodGet, "/api/v1/history/stats?"+q, nil, false); res.StatusCode != http.StatusBadRequest {
			t.Fatalf("%s: %d", q, res.StatusCode)
		}
	}
	if res, _, _ = user.do(http.MethodGet, "/api/v1/library/mixes?tz=Mars/Base", nil, false); res.StatusCode != http.StatusBadRequest {
		t.Fatal("mixes with an unknown time zone", res.StatusCode)
	}
	if res, _, _ = user.do(http.MethodGet, "/api/v1/library/mixes?tz=Europe/Berlin", nil, false); res.StatusCode != http.StatusOK {
		t.Fatal("mixes with a time zone", res.StatusCode)
	}

	// The search answer always carries the lyrics group.
	var search struct {
		Data struct {
			Tracks []map[string]any `json:"tracks"`
			Lyrics []map[string]any `json:"lyrics"`
		}
	}
	if res, raw, err = user.do(http.MethodGet, "/api/v1/library/search?q=Digital", nil, false); err != nil || res.StatusCode != 200 ||
		json.Unmarshal(raw, &search) != nil || len(search.Data.Tracks) != 1 || search.Data.Lyrics == nil {
		t.Fatal("search", res, err, string(raw))
	}
}
