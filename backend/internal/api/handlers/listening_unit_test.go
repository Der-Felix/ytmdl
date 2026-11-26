package handlers

import (
	"encoding/json"
	"net/http/httptest"
	"slices"
	"testing"
	"time"

	"ytdm/backend/internal/apperr"
)

// The health answer names the listening capabilities, so a client can tell a
// server with the new endpoints from an older one without probing them.
func TestHealthReportsListeningFeatures(t *testing.T) {
	h := newHealthTestHandler(&healthPinger{}, &healthChecker{}, &healthProvider{})
	recorder := requestHealth(t, h, "/api/v1/health?scope=essential")
	var body struct {
		Data struct {
			Features []string `json:"features"`
		} `json:"data"`
	}
	if err := json.NewDecoder(recorder.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"mixes.groups", "radio.station", "history.stats", "search.lyrics", "listening.preferences"} {
		if !slices.Contains(body.Data.Features, want) {
			t.Fatalf("feature %q missing from %v", want, body.Data.Features)
		}
	}
}

func TestRequestLocation(t *testing.T) {
	t.Setenv("TZ", "")
	loc, err := requestLocation(httptest.NewRequest("GET", "/x?tz=Europe/Berlin", nil))
	if err != nil || loc.String() != "Europe/Berlin" {
		t.Fatal(loc, err)
	}
	if loc, err = requestLocation(httptest.NewRequest("GET", "/x", nil)); err != nil || loc != time.UTC {
		t.Fatal("default is not UTC", loc, err)
	}
	for _, bad := range []string{"Mars/Olympus", "Local"} {
		if _, err := requestLocation(httptest.NewRequest("GET", "/x?tz="+bad, nil)); apperr.CodeOf(err) != apperr.CodeInvalidRequest {
			t.Fatalf("tz %q accepted: %v", bad, err)
		}
	}
	t.Setenv("TZ", "America/New_York")
	if loc, err = requestLocation(httptest.NewRequest("GET", "/x", nil)); err != nil || loc.String() != "America/New_York" {
		t.Fatal("TZ fallback ignored", loc, err)
	}
	t.Setenv("TZ", "Nowhere/Nothing")
	if loc, err = requestLocation(httptest.NewRequest("GET", "/x", nil)); err != nil || loc != time.UTC {
		t.Fatal("broken TZ must fall back to UTC", loc, err)
	}
}

func TestRadioCursorAndStationCache(t *testing.T) {
	if token, offset, ok := parseRadioCursor("abc.25"); !ok || token != "abc" || offset != 25 {
		t.Fatal(token, offset, ok)
	}
	for _, bad := range []string{"", "abc", ".3", "abc.-1", "abc.x", "abc.501"} {
		if _, _, ok := parseRadioCursor(bad); ok {
			t.Fatalf("cursor %q accepted", bad)
		}
	}
	var s radioStations
	token := s.put(&radioStation{user: "u1", ids: []string{"a"}, expires: time.Now().Add(time.Hour)})
	if s.get(token, "u1") == nil {
		t.Fatal("station lost")
	}
	if s.get(token, "u2") != nil {
		t.Fatal("another user's station served")
	}
	expired := s.put(&radioStation{user: "u1", expires: time.Now().Add(-time.Second)})
	if s.get(expired, "u1") != nil {
		t.Fatal("expired station served")
	}
	for range radioStationMax + 5 {
		s.put(&radioStation{user: "u1", expires: time.Now().Add(time.Hour)})
	}
	if len(s.stations) > radioStationMax {
		t.Fatalf("cache grew to %d", len(s.stations))
	}
}
