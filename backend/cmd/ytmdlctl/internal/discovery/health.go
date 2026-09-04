package discovery

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// BackendHealth holds the parsed health status from /api/v1/health.
type BackendHealth struct {
	Status          string // "ok", "degraded", "unavailable"
	Version         string
	UptimeSeconds   int
	DatabaseHealthy bool
}

// CheckVersionMismatch returns true if running version differs from configured version.
func (h *BackendHealth) CheckVersionMismatch(configuredVersion string) bool {
	if h == nil || h.Version == "" || configuredVersion == "" {
		return false
	}
	cleanRunning := strings.TrimPrefix(strings.TrimSpace(h.Version), "v")
	cleanConfigured := strings.TrimPrefix(strings.TrimSpace(configuredVersion), "v")
	return cleanRunning != cleanConfigured
}

// HealthClient communicates with the backend HTTP API.
type HealthClient struct {
	baseURL    string
	cliVersion string
	client     *http.Client
}

// NewHealthClient creates a HealthClient with strict redirect rules and timeout.
func NewHealthClient(baseURL, cliVersion string, client *http.Client) *HealthClient {
	if cliVersion == "" {
		cliVersion = "dev"
	}

	httpClient := &http.Client{
		Timeout: 5 * time.Second,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 3 {
				return errors.New("too many redirects during health check")
			}
			// Reject cross-origin redirects
			if req.URL.Host != via[0].URL.Host {
				return fmt.Errorf("cross-origin redirect from %s to %s rejected for health check", via[0].URL.Host, req.URL.Host)
			}
			return nil
		},
	}
	if client != nil {
		httpClient.Transport = client.Transport
		if client.Timeout > 0 {
			httpClient.Timeout = client.Timeout
		}
	}

	return &HealthClient{
		baseURL:    strings.TrimRight(baseURL, "/"),
		cliVersion: cliVersion,
		client:     httpClient,
	}
}

type rawHealthResponse struct {
	Data struct {
		Status        string `json:"status"`
		Version       string `json:"version"`
		UptimeSeconds int    `json:"uptime_seconds"`
		Checks        map[string]struct {
			OK bool `json:"ok"`
		} `json:"checks"`
	} `json:"data"`
}

// Check queries /api/v1/health.
func (c *HealthClient) Check(ctx context.Context) (*BackendHealth, error) {
	url := c.baseURL + "/api/v1/health"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create health request: %w", err)
	}
	req.Header.Set("User-Agent", fmt.Sprintf("ytmdlctl/%s", c.cliVersion))

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("health request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 64*1024))
	if err != nil {
		return nil, fmt.Errorf("failed reading health body: %w", err)
	}

	var raw rawHealthResponse
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, fmt.Errorf("invalid health response JSON: %w", err)
	}

	dbOK := false
	if dbCheck, exists := raw.Data.Checks["database"]; exists {
		dbOK = dbCheck.OK
	}

	return &BackendHealth{
		Status:          raw.Data.Status,
		Version:         raw.Data.Version,
		UptimeSeconds:   raw.Data.UptimeSeconds,
		DatabaseHealthy: dbOK,
	}, nil
}
