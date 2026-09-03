package update

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestSemVerComparison(t *testing.T) {
	tests := []struct {
		a, b     string
		expected int
	}{
		{"0.14.1", "0.14.1", 0},
		{"v0.14.1", "0.14.1", 0},
		{"0.15.0", "0.14.1", 1},
		{"0.14.1", "0.15.0", -1},
		{"0.10.0", "0.9.0", 1},
		{"0.9.0", "0.10.0", -1},
		{"1.0.0", "0.99.99", 1},
		{"0.15.0-rc.1", "0.15.0", -1},
		{"0.15.0", "0.15.0-rc.1", 1},
	}

	for _, tc := range tests {
		a, errA := ParseSemVer(tc.a)
		b, errB := ParseSemVer(tc.b)
		if errA != nil || errB != nil {
			t.Fatalf("failed parsing %q or %q: %v / %v", tc.a, tc.b, errA, errB)
		}
		cmp := a.Compare(b)
		if cmp != tc.expected {
			t.Errorf("Compare(%q, %q) = %d, expected %d", tc.a, tc.b, cmp, tc.expected)
		}
	}
}

func TestUpdateCheckStates(t *testing.T) {
	tests := []struct {
		name           string
		currentVersion string
		handler        http.HandlerFunc
		expectedState  State
		expectedLatest string
	}{
		{
			name:           "newer release available",
			currentVersion: "0.14.1",
			handler: func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusOK)
				fmt.Fprintln(w, `{
					"tag_name": "v0.15.0",
					"name": "YTMDL v0.15.0",
					"draft": false,
					"prerelease": false,
					"published_at": "2026-09-03T12:00:00Z",
					"html_url": "https://github.com/Der-Felix/ytmdl/releases/tag/v0.15.0",
					"body": "First release"
				}`)
			},
			expectedState:  StateUpdateAvailable,
			expectedLatest: "0.15.0",
		},
		{
			name:           "current version is same as latest",
			currentVersion: "0.15.0",
			handler: func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusOK)
				fmt.Fprintln(w, `{
					"tag_name": "v0.15.0",
					"name": "YTMDL v0.15.0",
					"draft": false,
					"prerelease": false,
					"published_at": "2026-09-03T12:00:00Z",
					"html_url": "https://github.com/Der-Felix/ytmdl/releases/tag/v0.15.0",
					"body": "Latest release"
				}`)
			},
			expectedState:  StateUpToDate,
			expectedLatest: "0.15.0",
		},
		{
			name:           "current version is newer than latest",
			currentVersion: "0.16.0",
			handler: func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusOK)
				fmt.Fprintln(w, `{
					"tag_name": "v0.15.0",
					"name": "YTMDL v0.15.0",
					"draft": false,
					"prerelease": false,
					"published_at": "2026-09-03T12:00:00Z",
					"html_url": "https://github.com/Der-Felix/ytmdl/releases/tag/v0.15.0"
				}`)
			},
			expectedState:  StateUpToDate,
			expectedLatest: "0.15.0",
		},
		{
			name:           "404 no public release yet",
			currentVersion: "0.14.1",
			handler: func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusNotFound)
				fmt.Fprintln(w, `{"message": "Not Found"}`)
			},
			expectedState:  StateNoPublicRelease,
			expectedLatest: "",
		},
		{
			name:           "500 internal server error from GitHub",
			currentVersion: "0.14.1",
			handler: func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusInternalServerError)
			},
			expectedState:  StateUnavailable,
			expectedLatest: "",
		},
		{
			name:           "403 rate limited",
			currentVersion: "0.14.1",
			handler: func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusForbidden)
			},
			expectedState:  StateUnavailable,
			expectedLatest: "",
		},
		{
			name:           "invalid JSON body",
			currentVersion: "0.14.1",
			handler: func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusOK)
				fmt.Fprintln(w, `not json`)
			},
			expectedState:  StateInvalidRelease,
			expectedLatest: "",
		},
		{
			name:           "invalid semver tag name",
			currentVersion: "0.14.1",
			handler: func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusOK)
				fmt.Fprintln(w, `{
					"tag_name": "arbitrary-tag",
					"draft": false,
					"prerelease": false
				}`)
			},
			expectedState:  StateInvalidRelease,
			expectedLatest: "",
		},
		{
			name:           "draft release defensive rejection",
			currentVersion: "0.14.1",
			handler: func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusOK)
				fmt.Fprintln(w, `{
					"tag_name": "v0.15.0",
					"draft": true,
					"prerelease": false
				}`)
			},
			expectedState:  StateInvalidRelease,
			expectedLatest: "",
		},
		{
			name:           "prerelease defensive rejection",
			currentVersion: "0.14.1",
			handler: func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusOK)
				fmt.Fprintln(w, `{
					"tag_name": "v0.15.0-beta.1",
					"draft": false,
					"prerelease": true
				}`)
			},
			expectedState:  StateInvalidRelease,
			expectedLatest: "",
		},
		{
			name:           "development local version",
			currentVersion: "dev",
			handler: func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusOK)
				fmt.Fprintln(w, `{
					"tag_name": "v0.15.0",
					"draft": false,
					"prerelease": false
				}`)
			},
			expectedState:  StateDevelopment,
			expectedLatest: "0.15.0",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(tc.handler)
			defer server.Close()

			svc := NewService(Config{
				Enabled:       true,
				Repository:    "Der-Felix/ytmdl",
				CheckInterval: 1 * time.Hour,
				BaseURL:       server.URL,
			}, tc.currentVersion, server.Client(), nil)

			status, err := svc.GetStatus(context.Background(), true)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if status.State != tc.expectedState {
				t.Errorf("got state %q, expected %q", status.State, tc.expectedState)
			}
			if status.LatestVersion != tc.expectedLatest {
				t.Errorf("got latest version %q, expected %q", status.LatestVersion, tc.expectedLatest)
			}
		})
	}
}

func TestDisabledUpdateCheck(t *testing.T) {
	svc := NewService(Config{
		Enabled: false,
	}, "0.14.1", nil, nil)

	status, err := svc.GetStatus(context.Background(), true)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if status.State != StateDisabled {
		t.Errorf("expected state disabled, got %s", status.State)
	}
}

func TestInvalidRepositoryAndSSRFProtection(t *testing.T) {
	maliciousRepos := []string{
		"http://evil.com/repo",
		"https://evil.example/repo",
		"evil.example:8080/repo",
		"evil/repo?x=1",
		"evil/repo#fragment",
		"../evil/repo",
		"evil//repo",
		"invalid/repo/extra",
		"single",
		"invalid repo/name",
		"evil@attacker.com/repo",
		"/absolute/path",
	}

	for _, repo := range maliciousRepos {
		svc := NewService(Config{
			Enabled:    true,
			Repository: repo,
		}, "0.14.1", nil, nil)

		status, err := svc.GetStatus(context.Background(), true)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if status.State != StateUnavailable {
			t.Errorf("repo %q: expected state unavailable, got %s", repo, status.State)
		}
	}
}

func TestFixedGitHubHostGuarantee(t *testing.T) {
	// Verify that the default Service constructor strictly targets api.github.com
	svc := NewService(Config{
		Enabled:    true,
		Repository: "Der-Felix/ytmdl",
	}, "0.14.1", nil, nil)

	if svc.cfg.BaseURL != "https://api.github.com" {
		t.Fatalf("expected BaseURL to be https://api.github.com, got %q", svc.cfg.BaseURL)
	}
}

func TestCachingAndSingleflight(t *testing.T) {
	var callCount int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&callCount, 1)
		time.Sleep(50 * time.Millisecond) // simulate latency
		w.WriteHeader(http.StatusOK)
		fmt.Fprintln(w, `{
			"tag_name": "v0.15.0",
			"name": "YTMDL v0.15.0",
			"draft": false,
			"prerelease": false
		}`)
	}))
	defer server.Close()

	svc := NewService(Config{
		Enabled:       true,
		Repository:    "Der-Felix/ytmdl",
		CheckInterval: 1 * time.Hour,
		BaseURL:       server.URL,
	}, "0.14.1", server.Client(), nil)

	// First call should hit the server
	status1, err := svc.GetStatus(context.Background(), false)
	if err != nil || status1.Cached {
		t.Fatalf("call 1: err=%v, cached=%v", err, status1.Cached)
	}
	if atomic.LoadInt32(&callCount) != 1 {
		t.Fatalf("expected 1 call, got %d", atomic.LoadInt32(&callCount))
	}

	// Second non-forced call should hit cache
	status2, err := svc.GetStatus(context.Background(), false)
	if err != nil || !status2.Cached {
		t.Fatalf("call 2: err=%v, cached=%v", err, status2.Cached)
	}
	if atomic.LoadInt32(&callCount) != 1 {
		t.Fatalf("expected still 1 call, got %d", atomic.LoadInt32(&callCount))
	}

	// Force call should bypass cache
	status3, err := svc.GetStatus(context.Background(), true)
	if err != nil || status3.Cached {
		t.Fatalf("call 3: err=%v, cached=%v", err, status3.Cached)
	}
	if atomic.LoadInt32(&callCount) != 2 {
		t.Fatalf("expected 2 calls, got %d", atomic.LoadInt32(&callCount))
	}

	// Concurrency test: 5 concurrent callers with force should collapse into single flight
	var wg sync.WaitGroup
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _ = svc.GetStatus(context.Background(), true)
		}()
	}
	wg.Wait()

	// Call count should increase by at most 1 or 2 due to singleflight
	calls := atomic.LoadInt32(&callCount)
	if calls > 4 {
		t.Errorf("concurrency test: expected <= 4 calls due to singleflight, got %d", calls)
	}
}
