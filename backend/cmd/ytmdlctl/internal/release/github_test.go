package release_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"ytdm/backend/cmd/ytmdlctl/internal/manifest"
	"ytdm/backend/cmd/ytmdlctl/internal/release"
)

const sampleManifest = `{
  "manifest_version": 1,
  "release_version": "0.16.0",
  "release_tag": "v0.16.0",
  "target_schema": 8,
  "rollback_classification": "schema_neutral",
  "min_upgrade_from": "0.15.0",
  "images": {
    "backend": {
      "repository": "ghcr.io/der-felix/ytmdl-backend",
      "tag": "0.16.0",
      "digest": "sha256:1111111111111111111111111111111111111111111111111111111111111111"
    },
    "frontend": {
      "repository": "ghcr.io/der-felix/ytmdl-frontend",
      "tag": "0.16.0",
      "digest": "sha256:2222222222222222222222222222222222222222222222222222222222222222"
    }
  },
  "required_env": ["POSTGRES_PASSWORD"]
}`

func TestFetchLatestReleaseWithManifest(t *testing.T) {
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("User-Agent") != "ytmdlctl/test-v1" {
			t.Errorf("User-Agent = %q, want ytmdlctl/test-v1", r.Header.Get("User-Agent"))
		}

		switch r.URL.Path {
		case "/repos/Der-Felix/ytmdl/releases/latest":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{
				"tag_name": "v0.16.0",
				"name": "YTMDL v0.16.0",
				"draft": false,
				"prerelease": false,
				"published_at": "2026-09-03T18:00:00Z",
				"html_url": "https://github.com/Der-Felix/ytmdl/releases/tag/v0.16.0",
				"body": "Release notes for v0.16.0",
				"assets": [
					{
						"name": "release-manifest.json",
						"browser_download_url": "` + server.URL + `/download/manifest.json"
					}
				]
			}`))
		case "/download/manifest.json":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(sampleManifest))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	client := release.NewClient(server.URL, "Der-Felix/ytmdl", "test-v1", nil)
	ctx := context.Background()

	rel, err := client.FetchLatest(ctx)
	if err != nil {
		t.Fatalf("FetchLatest failed: %v", err)
	}

	if rel.TagName != "v0.16.0" || rel.Version != "0.16.0" {
		t.Errorf("got TagName %s, Version %s", rel.TagName, rel.Version)
	}

	// Download manifest
	m, err := client.DownloadManifest(ctx, rel)
	if err != nil {
		t.Fatalf("DownloadManifest failed: %v", err)
	}

	if m.ReleaseVersion != "0.16.0" {
		t.Errorf("m.ReleaseVersion = %s, want 0.16.0", m.ReleaseVersion)
	}
	if m.RollbackClassification != manifest.RollbackSchemaNeutral {
		t.Errorf("m.RollbackClassification = %s, want schema_neutral", m.RollbackClassification)
	}
}

func TestFetchLatestReleaseErrors(t *testing.T) {
	tests := []struct {
		name        string
		status      int
		body        string
		expectedErr string
	}{
		{
			name:        "not found 404",
			status:      http.StatusNotFound,
			body:        `{"message": "Not Found"}`,
			expectedErr: "no public releases found",
		},
		{
			name:        "rate limited 403",
			status:      http.StatusForbidden,
			body:        `{"message": "rate limit exceeded"}`,
			expectedErr: "rate limited by GitHub",
		},
		{
			name:   "draft release ignored",
			status: http.StatusOK,
			body: `{
				"tag_name": "v0.16.0",
				"draft": true,
				"prerelease": false
			}`,
			expectedErr: "latest release is draft or prerelease",
		},
		{
			name:   "prerelease ignored",
			status: http.StatusOK,
			body: `{
				"tag_name": "v0.16.0-rc1",
				"draft": false,
				"prerelease": true
			}`,
			expectedErr: "latest release is draft or prerelease",
		},
		{
			name:        "invalid JSON",
			status:      http.StatusOK,
			body:        `{invalid json`,
			expectedErr: "invalid release JSON",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(tc.body))
			}))
			defer server.Close()

			client := release.NewClient(server.URL, "Der-Felix/ytmdl", "dev", nil)
			_, err := client.FetchLatest(context.Background())
			if err == nil {
				t.Fatalf("expected error containing %q, got nil", tc.expectedErr)
			}
			if !strings.Contains(err.Error(), tc.expectedErr) {
				t.Errorf("error %q does not contain %q", err.Error(), tc.expectedErr)
			}
		})
	}
}

func TestManifestRedirectToUntrustedHostRejected(t *testing.T) {
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/redirect":
			// Redirect to untrusted external host
			http.Redirect(w, r, "https://malicious-attacker.com/manifest.json", http.StatusFound)
		}
	}))
	defer server.Close()

	client := release.NewClient(server.URL, "Der-Felix/ytmdl", "dev", nil)
	rel := &release.ReleaseInfo{
		TagName: "v0.16.0",
		Assets: []release.Asset{
			{Name: "release-manifest.json", BrowserDownloadURL: server.URL + "/redirect"},
		},
	}

	_, err := client.DownloadManifest(context.Background(), rel)
	if err == nil || !strings.Contains(err.Error(), "untrusted host") {
		t.Errorf("expected untrusted host error on redirect, got: %v", err)
	}
}

func TestManifestOversizedRejected(t *testing.T) {
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Output 70 KiB of data (> 64 KiB limit)
		w.Header().Set("Content-Type", "application/json")
		bigData := strings.Repeat(" ", 70*1024)
		_, _ = fmt.Fprintf(w, `{"manifest_version": 1, "padding": "%s"}`, bigData)
	}))
	defer server.Close()

	client := release.NewClient(server.URL, "Der-Felix/ytmdl", "dev", nil)
	rel := &release.ReleaseInfo{
		TagName: "v0.16.0",
		Assets: []release.Asset{
			{Name: "release-manifest.json", BrowserDownloadURL: server.URL + "/manifest.json"},
		},
	}

	_, err := client.DownloadManifest(context.Background(), rel)
	if err == nil || !strings.Contains(err.Error(), "exceeds size limit") {
		t.Errorf("expected size limit error, got: %v", err)
	}
}
