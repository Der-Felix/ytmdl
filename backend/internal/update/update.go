package update

import (
	"context"
	"encoding/json"
	"errors"
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
	// StateNoChannelRelease means the chosen channel offers no release yet,
	// for example no qualified prerelease has been published.
	StateNoChannelRelease State = "no_channel_release"
	// StateAheadOfChannel means the installed version is newer than every
	// release the chosen channel offers - typically a prerelease after
	// switching back to the stable channel. Nothing is downgraded
	// automatically; returning is an explicit rollback or restore.
	StateAheadOfChannel State = "ahead_of_channel"
)

// Failure classifies why a check could not produce an answer, so that a
// network problem is never mistaken for "no update available".
type Failure string

const (
	FailureNetwork          Failure = "network_error"
	FailureRateLimited      Failure = "rate_limited"
	FailureUnexpectedStatus Failure = "unexpected_status"
	FailureInvalidResponse  Failure = "invalid_response"
	FailureConfiguration    Failure = "configuration"
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

	// Channel is the channel the check was made for.
	Channel Channel `json:"channel"`
	// CurrentPrerelease and LatestPrerelease mark prerelease versions.
	CurrentPrerelease bool `json:"current_prerelease,omitempty"`
	LatestPrerelease  bool `json:"latest_prerelease,omitempty"`
	// NewerStableVersion is set on the development channel when a stable
	// release is newer than both the installation and every prerelease.
	NewerStableVersion string `json:"newer_stable_version,omitempty"`
	// UpdateCommands are the host commands that perform the update through
	// ytmdlctl: a read-only preflight first, then the update itself.
	UpdateCommands []string `json:"update_commands,omitempty"`
	// Failure explains an unavailable or invalid check.
	Failure Failure `json:"failure,omitempty"`
}

// Config tunes the update detection service.
type Config struct {
	Enabled       bool
	Repository    string
	CheckInterval time.Duration
	BaseURL       string // optional override for testing, defaults to https://api.github.com
}

// SettingsStore persists the chosen channel. It matches the settings
// repository, so the channel lives next to all other runtime settings.
type SettingsStore interface {
	All(ctx context.Context) (map[string]string, error)
	SetMany(ctx context.Context, values map[string]string) error
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
	Assets      []struct {
		Name string `json:"name"`
	} `json:"assets"`
}

func (r gitHubRelease) candidate() ReleaseCandidate {
	names := make([]string, 0, len(r.Assets))
	for _, a := range r.Assets {
		names = append(names, a.Name)
	}
	return ReleaseCandidate{Tag: r.TagName, Draft: r.Draft, Prerelease: r.Prerelease, AssetNames: names}
}

// releaseListLimit bounds how many recent releases the development channel
// inspects. Prereleases are published from the newest development line, so a
// bounded page is enough.
const releaseListLimit = 30

// Service manages update checks, in-memory caching, and request de-duplication.
type Service struct {
	cfg            Config
	currentVersion string
	currentSemVer  *SemVer
	isDevVersion   bool
	client         *http.Client
	logger         *slog.Logger

	store SettingsStore

	mu          sync.RWMutex
	channel     Channel
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
		channel:        DefaultChannel,
	}
}

// UseSettings binds the service to the persistent settings and loads the
// stored channel. An unknown stored value falls back to the stable channel.
func (s *Service) UseSettings(ctx context.Context, store SettingsStore) error {
	s.mu.Lock()
	s.store = store
	s.mu.Unlock()
	if store == nil {
		return nil
	}
	values, err := store.All(ctx)
	if err != nil {
		return err
	}
	channel, perr := ParseChannel(values[SettingsKeyChannel])
	if perr != nil {
		s.logger.Warn("stored update channel is invalid; using the stable channel", "value", values[SettingsKeyChannel])
		channel = DefaultChannel
	}
	s.mu.Lock()
	s.channel = channel
	s.mu.Unlock()
	return nil
}

// Channel returns the channel the installation follows.
func (s *Service) Channel() Channel {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.channel
}

// SetChannel stores a new channel. It only records the choice and forgets the
// cached answer; it never installs anything, never restarts a container and
// never downgrades.
func (s *Service) SetChannel(ctx context.Context, channel Channel) error {
	if _, err := ParseChannel(string(channel)); err != nil || channel == "" {
		return fmt.Errorf("invalid update channel %q", channel)
	}
	s.mu.RLock()
	store := s.store
	s.mu.RUnlock()
	if store != nil {
		if err := store.SetMany(ctx, map[string]string{SettingsKeyChannel: string(channel)}); err != nil {
			return err
		}
	}
	s.mu.Lock()
	s.channel = channel
	s.cached = nil
	s.cachedUntil = time.Time{}
	s.mu.Unlock()
	s.logger.Info("update channel changed", "channel", string(channel))
	return nil
}

func (s *Service) baseStatus(channel Channel, now time.Time) Status {
	return Status{
		CurrentVersion:    s.currentVersion,
		Channel:           channel,
		CurrentPrerelease: s.currentSemVer != nil && s.currentSemVer.PreRelease != "",
		CheckedAt:         now,
	}
}

// GetStatus returns the current update status. If force is true, cached status is bypassed.
func (s *Service) GetStatus(ctx context.Context, force bool) (Status, error) {
	channel := s.Channel()
	if !s.cfg.Enabled {
		st := s.baseStatus(channel, time.Now())
		st.State = StateDisabled
		return st, nil
	}

	if !repoRegex.MatchString(s.cfg.Repository) {
		s.logger.Warn("invalid update repository configuration", "repository", s.cfg.Repository)
		st := s.baseStatus(channel, time.Now())
		st.State = StateUnavailable
		st.Failure = FailureConfiguration
		return st, nil
	}

	if !force {
		s.mu.RLock()
		if s.cached != nil && s.cached.Channel == channel && time.Now().Before(s.cachedUntil) {
			result := *s.cached
			result.Cached = true
			s.mu.RUnlock()
			return result, nil
		}
		s.mu.RUnlock()
	}

	// Deduplicate concurrent requests per channel
	res, err, _ := s.flight.Do("check:"+string(channel), func() (any, error) {
		return s.performCheck(ctx, channel)
	})
	if err != nil {
		st := s.baseStatus(channel, time.Now())
		st.State = StateUnavailable
		st.Failure = FailureNetwork
		return st, nil
	}

	return res.(Status), nil
}

// fetchError is a classified failure of a GitHub request.
type fetchError struct {
	failure Failure
	status  int
	err     error
}

func (e *fetchError) Error() string {
	if e.err != nil {
		return fmt.Sprintf("%s: %v", e.failure, e.err)
	}
	return fmt.Sprintf("%s (HTTP %d)", e.failure, e.status)
}

var errNotFound = errors.New("not found")

// get performs one read-only GitHub API request.
func (s *Service) get(ctx context.Context, path string, limit int64) ([]byte, error) {
	url := fmt.Sprintf("%s/repos/%s/%s", strings.TrimRight(s.cfg.BaseURL, "/"), s.cfg.Repository, path)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, &fetchError{failure: FailureConfiguration, err: err}
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	req.Header.Set("User-Agent", fmt.Sprintf("YTMDL/%s", s.currentVersion))

	resp, err := s.client.Do(req)
	if err != nil {
		return nil, &fetchError{failure: FailureNetwork, err: err}
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, limit))
	if err != nil {
		return nil, &fetchError{failure: FailureNetwork, err: err}
	}
	switch resp.StatusCode {
	case http.StatusOK:
		return body, nil
	case http.StatusNotFound:
		return nil, errNotFound
	case http.StatusForbidden, http.StatusTooManyRequests:
		return nil, &fetchError{failure: FailureRateLimited, status: resp.StatusCode}
	default:
		return nil, &fetchError{failure: FailureUnexpectedStatus, status: resp.StatusCode}
	}
}

func (s *Service) unavailable(channel Channel, now time.Time, err error) Status {
	st := s.baseStatus(channel, now)
	st.State = StateUnavailable
	st.Failure = FailureNetwork
	var fe *fetchError
	if errors.As(err, &fe) {
		st.Failure = fe.failure
	}
	s.logger.Info("update check failed", "channel", string(channel), "failure", string(st.Failure), "error", err)
	return s.cacheResult(st, 15*time.Minute)
}

func (s *Service) invalid(channel Channel, now time.Time, reason string) Status {
	s.logger.Info("update check received an invalid release", "channel", string(channel), "reason", reason)
	st := s.baseStatus(channel, now)
	st.State = StateInvalidRelease
	st.Failure = FailureInvalidResponse
	return s.cacheResult(st, 15*time.Minute)
}

func (s *Service) performCheck(ctx context.Context, channel Channel) (Status, error) {
	now := time.Now()

	var (
		target      *gitHubRelease
		newerStable string
	)

	switch channel {
	case ChannelDevelopment:
		body, err := s.get(ctx, fmt.Sprintf("releases?per_page=%d", releaseListLimit), 4*1024*1024)
		if errors.Is(err, errNotFound) {
			return s.noRelease(channel, now, StateNoPublicRelease), nil
		}
		if err != nil {
			return s.unavailable(channel, now, err), nil
		}
		var releases []gitHubRelease
		if err := json.Unmarshal(body, &releases); err != nil {
			return s.invalid(channel, now, "release list is not valid JSON"), nil
		}
		candidates := make([]ReleaseCandidate, len(releases))
		for i := range releases {
			candidates[i] = releases[i].candidate()
		}
		if idx := SelectLatest(candidates, ChannelDevelopment); idx >= 0 {
			target = &releases[idx]
		}
		if idx := SelectLatest(candidates, ChannelStable); idx >= 0 {
			stable, _ := ParseSemVer(releases[idx].TagName)
			newer := true
			if target != nil {
				pre, _ := ParseSemVer(target.TagName)
				newer = stable.Compare(pre) > 0
			}
			if newer && (s.currentSemVer == nil || stable.Compare(*s.currentSemVer) > 0) {
				newerStable = stable.String()
			}
		}
		if target == nil {
			st := s.noRelease(channel, now, StateNoChannelRelease)
			st.NewerStableVersion = newerStable
			return s.cacheResult(st, s.cfg.CheckInterval), nil
		}

	default:
		body, err := s.get(ctx, "releases/latest", 512*1024)
		if errors.Is(err, errNotFound) {
			// Normal case when 0 public releases exist on GitHub
			return s.noRelease(channel, now, StateNoPublicRelease), nil
		}
		if err != nil {
			return s.unavailable(channel, now, err), nil
		}
		var release gitHubRelease
		if err := json.Unmarshal(body, &release); err != nil {
			return s.invalid(channel, now, "latest release is not valid JSON"), nil
		}
		// Defensive check: the latest release must be a regular, published
		// stable release with a matching version.
		if _, ok := release.candidate().Eligible(ChannelStable); !ok {
			return s.invalid(channel, now, "latest release is a draft, a prerelease or not SemVer"), nil
		}
		target = &release
	}

	latest, err := ParseSemVer(target.TagName)
	if err != nil {
		return s.invalid(channel, now, "release tag is not valid SemVer"), nil
	}

	releaseURL := strings.TrimSpace(target.HTMLURL)
	if !strings.HasPrefix(releaseURL, "https://github.com/") {
		releaseURL = ""
	}

	state := StateUpToDate
	switch {
	case s.isDevVersion:
		state = StateDevelopment
	case s.currentSemVer != nil:
		switch cmp := s.currentSemVer.Compare(latest); {
		case cmp < 0:
			state = StateUpdateAvailable
		case cmp > 0:
			state = StateAheadOfChannel
		}
	}

	status := s.baseStatus(channel, now)
	status.LatestVersion = latest.String()
	status.LatestPrerelease = latest.PreRelease != ""
	status.State = state
	status.ReleaseName = target.Name
	status.PublishedAt = target.PublishedAt
	status.ReleaseURL = releaseURL
	status.ReleaseNotes = target.Body
	status.NewerStableVersion = newerStable
	if state == StateUpdateAvailable {
		status.UpdateCommands = UpdateCommands(channel, latest.String())
	}

	s.logger.Info("update check completed",
		"channel", string(channel),
		"current", s.currentVersion,
		"latest", latest.String(),
		"state", state)

	return s.cacheResult(status, s.cfg.CheckInterval), nil
}

func (s *Service) noRelease(channel Channel, now time.Time, state State) Status {
	s.logger.Info("no release offered on the update channel", "channel", string(channel), "repo", s.cfg.Repository)
	st := s.baseStatus(channel, now)
	st.State = state
	return s.cacheResult(st, s.cfg.CheckInterval)
}

// UpdateCommands are the host commands that install version from channel:
// the read-only preflight, then the transactional update. The version is
// always explicit, so the host installs exactly what the check reported.
func UpdateCommands(channel Channel, version string) []string {
	base := fmt.Sprintf("ytmdlctl update --channel %s --target %s", channel, version)
	return []string{base + " --dry-run", base}
}

func (s *Service) cacheResult(status Status, ttl time.Duration) Status {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cached = &status
	s.cachedUntil = time.Now().Add(ttl)
	return status
}
