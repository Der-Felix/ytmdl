package handlers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"ytdm/backend/internal/jobs"
	"ytdm/backend/internal/storage"
)

func TestStorageStatusEndpoints(t *testing.T) {
	libDir := t.TempDir()
	library, err := storage.NewLibrary(libDir)
	if err != nil {
		t.Fatalf("NewLibrary: %v", err)
	}

	guard := storage.NewStorageGuard(libDir, "", 1024*1024)
	library.SetGuard(guard)

	stagingDir := t.TempDir()
	stagingMgr, err := storage.NewStagingManager(stagingDir, 0, 0)
	if err != nil {
		t.Fatalf("NewStagingManager: %v", err)
	}

	manager := &jobs.Manager{}
	// Note: staging is injected via options in NewManager; test with mock/direct deps
	h := &Handlers{
		deps: Deps{
			Library: library,
			Jobs:    manager,
		},
		healthCache: make(map[string]checkResult),
	}

	// 1. GET /api/v1/storage/status
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/storage/status", nil)
	h.StorageStatus(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("StorageStatus status code = %d, want 200", rec.Code)
	}

	var res struct {
		Data StorageStatusResponse `json:"data"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&res); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if res.Data.Library.Path != libDir {
		t.Fatalf("library path = %q, want %q", res.Data.Library.Path, libDir)
	}
	if res.Data.Library.Status != string(storage.HealthHealthy) {
		t.Fatalf("library status = %q, want %q", res.Data.Library.Status, storage.HealthHealthy)
	}
	if res.Data.Library.GuardStatus != string(storage.GuardDisabled) {
		t.Fatalf("guard status = %q, want %q", res.Data.Library.GuardStatus, storage.GuardDisabled)
	}

	// 1b. Test with configured Secret Guard ID -> MUST NOT LEAK in JSON
	secretGuardID := "super-secret-guard-uuid-12345"
	guardWithSecret := storage.NewStorageGuard(libDir, secretGuardID, 1024*1024)
	library.SetGuard(guardWithSecret)

	recSecret := httptest.NewRecorder()
	reqSecret := httptest.NewRequest(http.MethodGet, "/api/v1/storage/status", nil)
	h.StorageStatus(recSecret, reqSecret)

	rawBody := recSecret.Body.String()
	if strings.Contains(rawBody, secretGuardID) {
		t.Fatalf("CRITICAL SECURITY LEAK: StorageStatus exposed secret guard ID %q in raw JSON: %s",
			secretGuardID, rawBody)
	}

	var resSecret struct {
		Data StorageStatusResponse `json:"data"`
	}
	if err := json.NewDecoder(recSecret.Body).Decode(&resSecret); err != nil {
		t.Fatalf("decode secret response: %v", err)
	}
	if !resSecret.Data.Library.GuardConfigured {
		t.Fatal("expected guard_configured=true")
	}
	if resSecret.Data.Library.GuardStatus != string(storage.GuardMissing) {
		t.Fatalf("expected guard_status=missing, got %q", resSecret.Data.Library.GuardStatus)
	}

	// 2. POST /api/v1/storage/probe
	recProbe := httptest.NewRecorder()
	reqProbe := httptest.NewRequest(http.MethodPost, "/api/v1/storage/probe", nil)
	h.StorageProbe(recProbe, reqProbe)

	if recProbe.Code != http.StatusOK {
		t.Fatalf("StorageProbe status code = %d, want 200", recProbe.Code)
	}

	// 3. POST /api/v1/storage/queue/pause and resume
	recPause := httptest.NewRecorder()
	reqPause := httptest.NewRequest(http.MethodPost, "/api/v1/storage/queue/pause", nil)
	h.StorageQueuePause(recPause, reqPause)

	if recPause.Code != http.StatusOK {
		t.Fatalf("StorageQueuePause status code = %d, want 200", recPause.Code)
	}
	if !manager.QueuePaused() {
		t.Fatal("expected queue to be paused")
	}

	recResume := httptest.NewRecorder()
	reqResume := httptest.NewRequest(http.MethodPost, "/api/v1/storage/queue/resume", nil)
	h.StorageQueueResume(recResume, reqResume)

	if recResume.Code != http.StatusOK {
		t.Fatalf("StorageQueueResume status code = %d, want 200", recResume.Code)
	}
	if manager.QueuePaused() {
		t.Fatal("expected queue to be unpaused")
	}

	_ = stagingMgr
}
