package update

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"time"

	"golang.org/x/sync/singleflight"
)

var repoRegex = regexp.MustCompile(`^[a-zA-Z0-9_.-]+/[a-zA-Z0-9_.-]+$`)

// State represents the outcome of an update check.
type State string

const (
	StateUpToDate        State = "up_to_date"
	StateUpdateAvailable State = "update_available"
	StateNoPublicRelease State = "no_public_release"
	StateDisabled        State = "disabled"
	StateUnavailable     State = "unavailable"
	StateInvalidRelease  State = "invalid_release"
	StateDevelopment     State = "development_version"
)

// Status represents the public DTO returned to the client and UI.
type Status struct {
	CurrentVersion string    `json:"current_version"`
	LatestVersion  string    `json:"latest_version,omitempty"`
	State          State     `json:"state"`
	ReleaseName    string    `json:"release_name,omitempty"`
	PublishedAt    string    `json:"published_at,omitempty"`
	ReleaseURL     string    `json:"release_url,omitempty"`
	ReleaseNotes   string    `json:"release_notes,omitempty"`
	CheckedAt      time.Time `json:"checked_at"`
	Cached         bool      `json:"cached"`
}

// Config tunes the update detection service.
type Config struct {
	Enabled       bool
	Repository    string
	CheckInterval time.Duration
	BaseURL       string // optional override for testing, defaults to https://api.github.com
}

// gitHubRelease matches the relevant subset of GitHub's release response.
type gitHubRelease struct {
	TagName     string `json:"tag_name"`
	Name        string `json:"name"`
	Draft       bool   `json:"draft"`
	Prerelease  bool   `json:"prerelease"`
	PublishedAt string `json:"published_at"`
	HTMLURL     string `json:"html_url"`
	Body        string `json:"body"`
}

// Service manages update checks, in-memory caching, and request de-duplication.
type Service struct {
	cfg            Config
	currentVersion string
	currentSemVer  *SemVer
	isDevVersion   bool
	client         *http.Client
	logger         *slog.Logger

	mu          sync.RWMutex
	cached      *Status
	cachedUntil time.Time
	flight      singleflight.Group
}

// NewService builds an update detection service.
func NewService(cfg Config, currentVersion string, client *http.Client, logger *slog.Logger) *Service {
	if client == nil {
		client = &http.Client{Timeout: 10 * time.Second}
	}
	if logger == nil {
		logger = slog.Default()
	}
	if cfg.Repository == "" {
		cfg.Repository = "Der-Felix/ytmdl"
	}
	if cfg.CheckInterval <= 0 {
		cfg.CheckInterval = 1 * time.Hour
	} else if cfg.CheckInterval < 5*time.Minute {
		cfg.CheckInterval = 5 * time.Minute
	}
	if cfg.BaseURL == "" {
		cfg.BaseURL = "https://api.github.com"
	}

	cleanCurrent := strings.TrimSpace(currentVersion)
	if cleanCurrent == "" {
		cleanCurrent = "dev"
	}

	parsed, err := ParseSemVer(cleanCurrent)
	isDev := err != nil || cleanCurrent == "dev"

	var semverPtr *SemVer
	if !isDev {
		semverPtr = &parsed
	}

	return &Service{
		cfg:            cfg,
		currentVersion: cleanCurrent,
		currentSemVer:  semverPtr,
		isDevVersion:   isDev,
		client:         client,
		logger:         logger,
	}
}

// GetStatus returns the current update status. If force is true, cached status is bypassed.
func (s *Service) GetStatus(ctx context.Context, force bool) (Status, error) {
	if !s.cfg.Enabled {
		return Status{
			CurrentVersion: s.currentVersion,
			State:          StateDisabled,
			CheckedAt:      time.Now(),
			Cached:         false,
		}, nil
	}

	if !repoRegex.MatchString(s.cfg.Repository) {
		s.logger.Warn("invalid update repository configuration", "repository", s.cfg.Repository)
		return Status{
			CurrentVersion: s.currentVersion,
			State:          StateUnavailable,
			CheckedAt:      time.Now(),
			Cached:         false,
		}, nil
	}

	if !force {
		s.mu.RLock()
		if s.cached != nil && time.Now().Before(s.cachedUntil) {
			result := *s.cached
			result.Cached = true
			s.mu.RUnlock()
			return result, nil
		}
		s.mu.RUnlock()
	}

	// Deduplicate concurrent requests
	res, err, _ := s.flight.Do("check", func() (any, error) {
		return s.performCheck(ctx)
	})
	if err != nil {
		return Status{
			CurrentVersion: s.currentVersion,
			State:          StateUnavailable,
			CheckedAt:      time.Now(),
			Cached:         false,
		}, nil
	}

	status := res.(Status)
	return status, nil
}

func (s *Service) performCheck(ctx context.Context) (Status, error) {
	now := time.Now()

	url := fmt.Sprintf("%s/repos/%s/releases/latest", strings.TrimRight(s.cfg.BaseURL, "/"), s.cfg.Repository)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		s.logger.Warn("failed to create update check request", "error", err)
		return s.cacheResult(Status{
			CurrentVersion: s.currentVersion,
			State:          StateUnavailable,
			CheckedAt:      now,
		}, 15*time.Minute), nil
	}

	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	req.Header.Set("User-Agent", fmt.Sprintf("YTMDL/%s", s.currentVersion))

	resp, err := s.client.Do(req)
	if err != nil {
		s.logger.Info("update check request failed", "error", err)
		return s.cacheResult(Status{
			CurrentVersion: s.currentVersion,
			State:          StateUnavailable,
			CheckedAt:      now,
		}, 15*time.Minute), nil
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 512*1024))
	if err != nil {
		s.logger.Info("failed reading update check response", "error", err)
		return s.cacheResult(Status{
			CurrentVersion: s.currentVersion,
			State:          StateUnavailable,
			CheckedAt:      now,
		}, 15*time.Minute), nil
	}

	switch resp.StatusCode {
	case http.StatusOK:
		var release gitHubRelease
		if err := json.Unmarshal(body, &release); err != nil {
			s.logger.Info("invalid release JSON from GitHub", "error", err)
			return s.cacheResult(Status{
				CurrentVersion: s.currentVersion,
				State:          StateInvalidRelease,
				CheckedAt:      now,
			}, 15*time.Minute), nil
		}

		// Defensive check: latest stable must not be draft or prerelease
		if release.Draft || release.Prerelease {
			s.logger.Info("latest release is draft or prerelease", "tag", release.TagName)
			return s.cacheResult(Status{
				CurrentVersion: s.currentVersion,
				State:          StateInvalidRelease,
				CheckedAt:      now,
			}, 15*time.Minute), nil
		}

		latestSemVer, err := ParseSemVer(release.TagName)
		if err != nil {
			s.logger.Info("latest release tag is not valid semver", "tag", release.TagName)
			return s.cacheResult(Status{
				CurrentVersion: s.currentVersion,
				State:          StateInvalidRelease,
				CheckedAt:      now,
			}, 15*time.Minute), nil
		}

		releaseURL := strings.TrimSpace(release.HTMLURL)
		if !strings.HasPrefix(releaseURL, "https://github.com/") {
			releaseURL = ""
		}

		state := StateUpToDate
		if s.isDevVersion {
			state = StateDevelopment
		} else if s.currentSemVer != nil {
			cmp := s.currentSemVer.Compare(latestSemVer)
			if cmp < 0 {
				state = StateUpdateAvailable
			} else {
				state = StateUpToDate
			}
		}

		status := Status{
			CurrentVersion: s.currentVersion,
			LatestVersion:  latestSemVer.String(),
			State:          state,
			ReleaseName:    release.Name,
			PublishedAt:    release.PublishedAt,
			ReleaseURL:     releaseURL,
			ReleaseNotes:   release.Body,
			CheckedAt:      now,
			Cached:         false,
		}

		s.logger.Info("update check completed",
			"current", s.currentVersion,
			"latest", latestSemVer.String(),
			"state", state)

		return s.cacheResult(status, s.cfg.CheckInterval), nil

	case http.StatusNotFound:
		// Normal case when 0 public releases exist on GitHub
		s.logger.Info("no public releases found on repository", "repo", s.cfg.Repository)
		status := Status{
			CurrentVersion: s.currentVersion,
			State:          StateNoPublicRelease,
			CheckedAt:      now,
			Cached:         false,
		}
		return s.cacheResult(status, s.cfg.CheckInterval), nil

	case http.StatusForbidden, http.StatusTooManyRequests:
		s.logger.Info("update check rate limited by GitHub", "status", resp.StatusCode)
		status := Status{
			CurrentVersion: s.currentVersion,
			State:          StateUnavailable,
			CheckedAt:      now,
			Cached:         false,
		}
		return s.cacheResult(status, 15*time.Minute), nil

	default:
		s.logger.Info("update check received non-200 status", "status", resp.StatusCode)
		status := Status{
			CurrentVersion: s.currentVersion,
			State:          StateUnavailable,
			CheckedAt:      now,
			Cached:         false,
		}
		return s.cacheResult(status, 15*time.Minute), nil
	}
}

func (s *Service) cacheResult(status Status, ttl time.Duration) Status {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cached = &status
	s.cachedUntil = time.Now().Add(ttl)
	return status
}
