package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"ytdm/backend/internal/music"
	"ytdm/backend/internal/provider"
)

type healthPinger struct {
	err   error
	calls int
}

func (p *healthPinger) PingContext(context.Context) error {
	p.calls++
	return p.err
}

type healthChecker struct {
	err   error
	calls int
}

func (c *healthChecker) Available(context.Context) error {
	c.calls++
	return c.err
}

// healthProvider exposes an availability probe that would represent an
// external provider request. The health endpoint must never invoke it.
type healthProvider struct {
	availabilityCalls int
}

func (*healthProvider) Name() string { return "health-test-provider" }

func (*healthProvider) SearchArtists(context.Context, string) ([]music.Artist, error) {
	panic("health endpoint called provider SearchArtists")
}

func (*healthProvider) GetArtist(context.Context, string) (*music.Artist, error) {
	panic("health endpoint called provider GetArtist")
}

func (*healthProvider) GetDiscography(context.Context, string) ([]music.Release, error) {
	panic("health endpoint called provider GetDiscography")
}

func (*healthProvider) GetRelease(context.Context, string) (*music.Release, error) {
	panic("health endpoint called provider GetRelease")
}

func (*healthProvider) GetReleaseTracks(context.Context, string) ([]music.Track, error) {
	panic("health endpoint called provider GetReleaseTracks")
}

func (p *healthProvider) Available(context.Context) error {
	p.availabilityCalls++
	return nil
}

type healthResponse struct {
	Data struct {
		Status string                 `json:"status"`
		Checks map[string]checkResult `json:"checks"`
	} `json:"data"`
}

func TestHealthDatabaseFailureReturnsServiceUnavailable(t *testing.T) {
	database := &healthPinger{err: errors.New("database is unavailable")}
	tool := &healthChecker{}
	providerProbe := &healthProvider{}
	h := newHealthTestHandler(database, tool, providerProbe)

	response := requestHealth(t, h, "/api/v1/health")

	if response.Code != http.StatusServiceUnavailable {
		t.Fatalf("status code = %d, want %d", response.Code, http.StatusServiceUnavailable)
	}
	body := decodeHealthResponse(t, response)
	if body.Data.Status != "unavailable" {
		t.Errorf("health status = %q, want %q", body.Data.Status, "unavailable")
	}
	if result := body.Data.Checks["database"]; result.OK {
		t.Errorf("database check = %+v, want a failed check", result)
	}
	assertProviderWasNotProbed(t, providerProbe)
}

func TestHealthToolFailureIsDegradedButStillHealthy(t *testing.T) {
	database := &healthPinger{}
	tool := &healthChecker{err: errors.New("tool is unavailable")}
	providerProbe := &healthProvider{}
	h := newHealthTestHandler(database, tool, providerProbe)

	response := requestHealth(t, h, "/api/v1/health")

	if response.Code != http.StatusOK {
		t.Fatalf("status code = %d, want %d", response.Code, http.StatusOK)
	}
	body := decodeHealthResponse(t, response)
	if body.Data.Status != "degraded" {
		t.Errorf("health status = %q, want %q", body.Data.Status, "degraded")
	}
	if result := body.Data.Checks["ffmpeg"]; result.OK {
		t.Errorf("ffmpeg check = %+v, want a failed check", result)
	}
	assertProviderWasNotProbed(t, providerProbe)
}

func TestHealthEssentialScopeSkipsExternalChecks(t *testing.T) {
	database := &healthPinger{}
	tool := &healthChecker{err: errors.New("must not be observed")}
	providerProbe := &healthProvider{}
	h := newHealthTestHandler(database, tool, providerProbe)

	response := requestHealth(t, h, "/api/v1/health?scope=essential")

	if response.Code != http.StatusOK {
		t.Fatalf("status code = %d, want %d", response.Code, http.StatusOK)
	}
	body := decodeHealthResponse(t, response)
	if body.Data.Status != "ok" {
		t.Errorf("health status = %q, want %q", body.Data.Status, "ok")
	}
	if database.calls != 1 {
		t.Errorf("database ping calls = %d, want 1", database.calls)
	}
	if tool.calls != 0 {
		t.Errorf("tool availability calls = %d, want 0", tool.calls)
	}
	if _, exists := body.Data.Checks["ffmpeg"]; exists {
		t.Error("essential checks unexpectedly contain ffmpeg")
	}
	assertProviderWasNotProbed(t, providerProbe)
}

func newHealthTestHandler(database Pinger, tool Checker, providerProbe *healthProvider) *Handlers {
	registry := provider.NewRegistry()
	registry.RegisterMetadata(providerProbe)
	return &Handlers{
		deps: Deps{
			Database:  database,
			Registry:  registry,
			Tools:     map[string]Checker{"ffmpeg": tool},
			Version:   "test",
			StartedAt: time.Now(),
		},
		healthCache: make(map[string]checkResult),
	}
}

func requestHealth(t *testing.T, h *Handlers, target string) *httptest.ResponseRecorder {
	t.Helper()
	recorder := httptest.NewRecorder()
	h.Health(recorder, httptest.NewRequest(http.MethodGet, target, nil))
	return recorder
}

func decodeHealthResponse(t *testing.T, recorder *httptest.ResponseRecorder) healthResponse {
	t.Helper()
	var body healthResponse
	if err := json.NewDecoder(recorder.Body).Decode(&body); err != nil {
		t.Fatalf("decode health response: %v", err)
	}
	return body
}

func assertProviderWasNotProbed(t *testing.T, providerProbe *healthProvider) {
	t.Helper()
	if providerProbe.availabilityCalls != 0 {
		t.Errorf("provider availability calls = %d, want 0", providerProbe.availabilityCalls)
	}
}
