// Package release provides GitHub release discovery and manifest asset downloading.
package release

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"ytdm/backend/cmd/ytmdlctl/internal/manifest"
	"ytdm/backend/internal/update"
)

// Asset represents a release asset from GitHub.
type Asset struct {
	Name               string `json:"name"`
	BrowserDownloadURL string `json:"browser_download_url"`
}

// ReleaseInfo contains parsed GitHub release metadata.
type ReleaseInfo struct {
	TagName     string  `json:"tag_name"`
	Version     string  `json:"version"`
	Name        string  `json:"name"`
	Draft       bool    `json:"draft"`
	Prerelease  bool    `json:"prerelease"`
	PublishedAt string  `json:"published_at"`
	HTMLURL     string  `json:"html_url"`
	Body        string  `json:"body"`
	Assets      []Asset `json:"assets"`
}

// Client interacts with GitHub API and download endpoints.
type Client struct {
	baseURL    string
	repository string
	userAgent  string
	client     *http.Client
}

// NewClient creates a new release Client.
func NewClient(baseURL, repository, cliVersion string, baseClient *http.Client) *Client {
	if baseURL == "" {
		baseURL = "https://api.github.com"
	}
	if repository == "" {
		repository = "Der-Felix/ytmdl"
	}
	if cliVersion == "" {
		cliVersion = "dev"
	}
	ua := fmt.Sprintf("ytmdlctl/%s", cliVersion)

	parsedBase, _ := url.Parse(baseURL)
	testHost := ""
	if parsedBase != nil {
		testHost = parsedBase.Hostname()
	}

	// Build safe HTTP client with strict redirect policy
	httpClient := &http.Client{
		Timeout: 10 * time.Second,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 5 {
				return errors.New("too many redirects fetching release asset")
			}
			// Only allow HTTPS unless running against local test server
			if req.URL.Scheme != "https" && req.URL.Hostname() != "127.0.0.1" && req.URL.Hostname() != "localhost" {
				return fmt.Errorf("insecure redirect scheme %q; only https allowed", req.URL.Scheme)
			}
			if !isTrustedGitHubHost(req.URL.Hostname(), testHost) {
				return fmt.Errorf("redirect to untrusted host %q rejected", req.URL.Hostname())
			}
			return nil
		},
	}
	if baseClient != nil {
		httpClient.Transport = baseClient.Transport
		if baseClient.Timeout > 0 {
			httpClient.Timeout = baseClient.Timeout
		}
	}

	return &Client{
		baseURL:    strings.TrimRight(baseURL, "/"),
		repository: repository,
		userAgent:  ua,
		client:     httpClient,
	}
}

func isTrustedGitHubHost(host, testHost string) bool {
	host = strings.ToLower(host)
	if testHost != "" && (host == testHost || host == "127.0.0.1" || host == "localhost") {
		return true
	}
	if host == "github.com" || host == "api.github.com" {
		return true
	}
	if strings.HasSuffix(host, ".github.com") || strings.HasSuffix(host, ".githubusercontent.com") {
		return true
	}
	return false
}

// FetchLatest fetches the latest stable release from GitHub.
func (c *Client) FetchLatest(ctx context.Context) (*ReleaseInfo, error) {
	apiURL := fmt.Sprintf("%s/repos/%s/releases/latest", c.baseURL, c.repository)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, apiURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create release request: %w", err)
	}

	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	req.Header.Set("User-Agent", c.userAgent)

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("release request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 512*1024))
	if err != nil {
		return nil, fmt.Errorf("failed reading release response: %w", err)
	}

	switch resp.StatusCode {
	case http.StatusOK:
		var rel ReleaseInfo
		if err := json.Unmarshal(body, &rel); err != nil {
			return nil, fmt.Errorf("invalid release JSON from GitHub: %w", err)
		}

		if rel.Draft || rel.Prerelease {
			return nil, errors.New("latest release is draft or prerelease")
		}

		semver, err := update.ParseSemVer(rel.TagName)
		if err != nil {
			return nil, fmt.Errorf("latest release tag %q is not valid semver: %w", rel.TagName, err)
		}
		rel.Version = semver.String()

		return &rel, nil

	case http.StatusNotFound:
		return nil, errors.New("no public releases found on repository")

	case http.StatusForbidden, http.StatusTooManyRequests:
		return nil, errors.New("rate limited by GitHub")

	default:
		return nil, fmt.Errorf("GitHub release check received non-200 status %d", resp.StatusCode)
	}
}

// FetchTag fetches a specific release by tag from GitHub.
func (c *Client) FetchTag(ctx context.Context, tag string) (*ReleaseInfo, error) {
	tag = strings.TrimSpace(tag)
	if !strings.HasPrefix(tag, "v") {
		tag = "v" + tag
	}
	apiURL := fmt.Sprintf("%s/repos/%s/releases/tags/%s", c.baseURL, c.repository, tag)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, apiURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create release request: %w", err)
	}

	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	req.Header.Set("User-Agent", c.userAgent)

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("release request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 512*1024))
	if err != nil {
		return nil, fmt.Errorf("failed reading release response: %w", err)
	}

	switch resp.StatusCode {
	case http.StatusOK:
		var rel ReleaseInfo
		if err := json.Unmarshal(body, &rel); err != nil {
			return nil, fmt.Errorf("invalid release JSON from GitHub: %w", err)
		}
		if rel.Draft || rel.Prerelease {
			return nil, errors.New("requested release is draft or prerelease")
		}
		semver, err := update.ParseSemVer(rel.TagName)
		if err != nil {
			return nil, fmt.Errorf("release tag %q is not valid semver: %w", rel.TagName, err)
		}
		rel.Version = semver.String()
		return &rel, nil
	case http.StatusNotFound:
		return nil, fmt.Errorf("release %q not found on repository", tag)
	case http.StatusForbidden, http.StatusTooManyRequests:
		return nil, errors.New("rate limited by GitHub")
	default:
		return nil, fmt.Errorf("GitHub release check received status %d", resp.StatusCode)
	}
}

// DownloadManifest locates and downloads release-manifest.json for the given release.
func (c *Client) DownloadManifest(ctx context.Context, rel *ReleaseInfo) (*manifest.Manifest, error) {
	var downloadURL string
	for _, a := range rel.Assets {
		if a.Name == "release-manifest.json" {
			downloadURL = a.BrowserDownloadURL
			break
		}
	}

	if downloadURL == "" {
		return nil, errors.New("release does not contain release-manifest.json asset")
	}

	parsed, err := url.Parse(downloadURL)
	if err != nil {
		return nil, fmt.Errorf("invalid manifest asset URL: %w", err)
	}

	parsedBase, _ := url.Parse(c.baseURL)
	testHost := ""
	if parsedBase != nil {
		testHost = parsedBase.Hostname()
	}

	// Validate scheme
	if parsed.Scheme != "https" && parsed.Hostname() != "127.0.0.1" && parsed.Hostname() != "localhost" {
		return nil, fmt.Errorf("manifest asset URL must use HTTPS (got %q)", parsed.Scheme)
	}

	// Validate initial host
	if !isTrustedGitHubHost(parsed.Hostname(), testHost) {
		return nil, fmt.Errorf("untrusted manifest asset host %q", parsed.Hostname())
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, downloadURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create manifest download request: %w", err)
	}
	req.Header.Set("User-Agent", c.userAgent)

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed downloading manifest: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("manifest download returned status %d", resp.StatusCode)
	}

	data, err := io.ReadAll(io.LimitReader(resp.Body, manifest.MaxManifestBytes+1))
	if err != nil {
		return nil, fmt.Errorf("failed reading manifest body: %w", err)
	}

	if len(data) > manifest.MaxManifestBytes {
		return nil, fmt.Errorf("manifest asset exceeds size limit of %d bytes", manifest.MaxManifestBytes)
	}

	m, err := manifest.Decode(data)
	if err != nil {
		return nil, fmt.Errorf("failed decoding manifest: %w", err)
	}

	if err := m.Validate(rel.TagName); err != nil {
		return nil, fmt.Errorf("manifest validation failed: %w", err)
	}

	return m, nil
}
