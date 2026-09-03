package handlers

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"ytdm/backend/internal/update"
)

func TestSystemUpdateEndpoints(t *testing.T) {
	fakeGitHub := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		fmt.Fprintln(w, `{
			"tag_name": "v0.15.0",
			"name": "YTMDL v0.15.0",
			"draft": false,
			"prerelease": false,
			"published_at": "2026-09-03T12:00:00Z",
			"html_url": "https://github.com/Der-Felix/ytmdl/releases/tag/v0.15.0",
			"body": "Release notes here"
		}`)
	}))
	defer fakeGitHub.Close()

	updateSvc := update.NewService(update.Config{
		Enabled:       true,
		Repository:    "Der-Felix/ytmdl",
		CheckInterval: 1 * time.Hour,
		BaseURL:       fakeGitHub.URL,
	}, "0.14.1", fakeGitHub.Client(), nil)

	h := &Handlers{
		deps: Deps{
			Updates: updateSvc,
		},
		healthCache: make(map[string]checkResult),
	}

	// 1. GET /api/v1/system/update
	req := httptest.NewRequest(http.MethodGet, "/api/v1/system/update", nil)
	w := httptest.NewRecorder()
	h.GetUpdateStatus(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp struct {
		Data update.Status `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to parse response JSON: %v", err)
	}

	if resp.Data.State != update.StateUpdateAvailable {
		t.Errorf("expected state update_available, got %s", resp.Data.State)
	}
	if resp.Data.LatestVersion != "0.15.0" {
		t.Errorf("expected latest version 0.15.0, got %s", resp.Data.LatestVersion)
	}
	if resp.Data.CurrentVersion != "0.14.1" {
		t.Errorf("expected current version 0.14.1, got %s", resp.Data.CurrentVersion)
	}

	// 2. POST /api/v1/system/update/check (forced refresh)
	reqCheck := httptest.NewRequest(http.MethodPost, "/api/v1/system/update/check", nil)
	wCheck := httptest.NewRecorder()
	h.CheckUpdate(wCheck, reqCheck)

	if wCheck.Code != http.StatusOK {
		t.Fatalf("expected status 200 on check, got %d: %s", wCheck.Code, wCheck.Body.String())
	}

	var respCheck struct {
		Data update.Status `json:"data"`
	}
	if err := json.Unmarshal(wCheck.Body.Bytes(), &respCheck); err != nil {
		t.Fatalf("failed to parse check response JSON: %v", err)
	}
	if respCheck.Data.State != update.StateUpdateAvailable {
		t.Errorf("expected state update_available, got %s", respCheck.Data.State)
	}
}

func TestSystemUpdateDisabled(t *testing.T) {
	updateSvc := update.NewService(update.Config{
		Enabled: false,
	}, "0.14.1", nil, nil)

	h := &Handlers{
		deps: Deps{
			Updates: updateSvc,
		},
		healthCache: make(map[string]checkResult),
	}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/system/update", nil)
	w := httptest.NewRecorder()
	h.GetUpdateStatus(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", w.Code)
	}

	var resp struct {
		Data update.Status `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed parsing response: %v", err)
	}
	if resp.Data.State != update.StateDisabled {
		t.Errorf("expected state disabled, got %s", resp.Data.State)
	}
}
