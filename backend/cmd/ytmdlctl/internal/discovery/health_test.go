package discovery_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"ytdm/backend/cmd/ytmdlctl/internal/discovery"
)

func TestCheckBackendHealthSuccess(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("User-Agent") != "ytmdlctl/test-v1" {
			t.Errorf("User-Agent = %q, want ytmdlctl/test-v1", r.Header.Get("User-Agent"))
		}
		if r.URL.Path != "/api/v1/health" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{
			"data": {
				"status": "ok",
				"version": "0.15.0",
				"uptime_seconds": 120,
				"checks": {
					"database": {"ok": true}
				}
			}
		}`))
	}))
	defer server.Close()

	ctx := context.Background()
	client := discovery.NewHealthClient(server.URL, "test-v1", nil)

	h, err := client.Check(ctx)
	if err != nil {
		t.Fatalf("Check failed: %v", err)
	}

	if h.Status != "ok" {
		t.Errorf("Status = %q, want ok", h.Status)
	}
	if h.Version != "0.15.0" {
		t.Errorf("Version = %q, want 0.15.0", h.Version)
	}
	if !h.DatabaseHealthy {
		t.Errorf("DatabaseHealthy = false, want true")
	}
}

func TestCheckBackendHealthVersionMismatch(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"data":{"status":"ok","version":"0.15.0","checks":{"database":{"ok":true}}}}`))
	}))
	defer server.Close()

	ctx := context.Background()
	client := discovery.NewHealthClient(server.URL, "dev", nil)

	h, err := client.Check(ctx)
	if err != nil {
		t.Fatalf("Check failed: %v", err)
	}

	// Compare with configured version "0.16.0"
	mismatch := h.CheckVersionMismatch("0.16.0")
	if !mismatch {
		t.Error("expected version mismatch between running 0.15.0 and configured 0.16.0")
	}

	// Compare with configured version "0.15.0"
	mismatch = h.CheckVersionMismatch("0.15.0")
	if mismatch {
		t.Error("did not expect version mismatch for identical version 0.15.0")
	}
}

func TestCheckBackendHealthServiceUnavailable(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = w.Write([]byte(`{"data":{"status":"unavailable","version":"0.15.0","checks":{"database":{"ok":false}}}}`))
	}))
	defer server.Close()

	ctx := context.Background()
	client := discovery.NewHealthClient(server.URL, "dev", nil)

	h, err := client.Check(ctx)
	if err != nil {
		t.Fatalf("Check returned err: %v", err)
	}

	if h.Status != "unavailable" {
		t.Errorf("Status = %q, want unavailable", h.Status)
	}
	if h.DatabaseHealthy {
		t.Error("expected DatabaseHealthy = false")
	}
}

func TestHealthCrossOriginRedirectRejected(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "https://external-malicious.com/api/v1/health", http.StatusFound)
	}))
	defer server.Close()

	client := discovery.NewHealthClient(server.URL, "dev", nil)
	_, err := client.Check(context.Background())
	if err == nil || !strings.Contains(err.Error(), "cross-origin redirect") {
		t.Errorf("expected cross-origin redirect error, got: %v", err)
	}
}
