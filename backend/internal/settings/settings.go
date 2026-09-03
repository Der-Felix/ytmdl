package settings

import (
	"context"
	"encoding/json"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"ytdm/backend/internal/apperr"
	"ytdm/backend/internal/config"
	"ytdm/backend/internal/jobs"
	"ytdm/backend/internal/matcher"
	"ytdm/backend/internal/music"
)

// Keys of the persisted settings.
const (
	KeySkipExisting              = "downloads.skip_existing"
	KeyMaxWorkers                = "downloads.max_workers"
	KeyRateLimit                 = "downloads.rate_limit"
	KeyScheduleEnabled           = "downloads.schedule_enabled"
	KeyScheduleStart             = "downloads.schedule_start"
	KeyScheduleEnd               = "downloads.schedule_end"
	KeyScheduleTimezone          = "downloads.schedule_timezone"
	KeyEmbedCover                = "library.embed_cover"
	KeyWriteCoverFile            = "library.write_cover_file"
	KeyLyricsEnabled             = "library.lyrics_enabled"
	KeyLyricsWriteSidecar        = "library.lyrics_write_sidecar"
	KeyLyricsGeniusEnabled       = "library.lyrics_genius_enabled"
	KeyMatchMinScore             = "matching.min_score"
	KeyQueuePaused               = "queue.paused"
	KeySubscriptionAutoDownload  = "subscriptions.default_auto_download"
	KeySubscriptionPriority      = "subscriptions.default_priority"
	KeySubscriptionReleaseFilter = "subscriptions.default_release_filter"
)

// Repository persists the settings.
type Repository interface {
	All(ctx context.Context) (map[string]string, error)
	SetMany(ctx context.Context, values map[string]string) error
}

// Settings is the effective configuration as the API reports it.
type Settings struct {
	SkipExisting          bool    `json:"skip_existing"`
	EmbedCover            bool    `json:"embed_cover"`
	WriteCoverFile        bool    `json:"write_cover_file"`
	LyricsEnabled         bool    `json:"lyrics_enabled"`
	LyricsWriteSidecar    bool    `json:"lyrics_write_sidecar"`
	LyricsGeniusEnabled   bool    `json:"lyrics_genius_enabled"`
	GeniusTokenConfigured bool    `json:"genius_token_configured"`
	MatchMinScore         float64 `json:"match_min_score"`

	ConcurrentDownloads int    `json:"concurrent_downloads"`
	MaxWorkers          int    `json:"max_workers"`
	RateLimit           string `json:"rate_limit"`
	ScheduleEnabled     bool   `json:"schedule_enabled"`
	ScheduleStart       string `json:"schedule_start"`
	ScheduleEnd         string `json:"schedule_end"`
	ScheduleTimezone    string `json:"schedule_timezone"`
	ServerTimezone      string `json:"server_timezone"`

	SubscriptionAutoDownload  bool                `json:"subscription_default_auto_download"`
	SubscriptionPriority      string              `json:"subscription_default_priority"`
	SubscriptionReleaseFilter music.ReleaseFilter `json:"subscription_default_release_filter"`

	LibraryPath         string `json:"library_path"`
	AllowTranscode      bool   `json:"allow_transcode"`
	DurationToleranceMS int    `json:"match_duration_tolerance_ms"`
	MetadataProvider    string `json:"default_metadata_provider"`
	MediaProvider       string `json:"default_media_provider"`
}

// Update carries the fields a PUT request wants to change. A nil field is left
// untouched.
type Update struct {
	SkipExisting        *bool    `json:"skip_existing"`
	EmbedCover          *bool    `json:"embed_cover"`
	WriteCoverFile      *bool    `json:"write_cover_file"`
	LyricsEnabled       *bool    `json:"lyrics_enabled"`
	LyricsWriteSidecar  *bool    `json:"lyrics_write_sidecar"`
	LyricsGeniusEnabled *bool    `json:"lyrics_genius_enabled"`
	MatchMinScore       *float64 `json:"match_min_score"`

	MaxWorkers       *int    `json:"max_workers"`
	RateLimit        *string `json:"rate_limit"`
	ScheduleEnabled  *bool   `json:"schedule_enabled"`
	ScheduleStart    *string `json:"schedule_start"`
	ScheduleEnd      *string `json:"schedule_end"`
	ScheduleTimezone *string `json:"schedule_timezone"`

	SubscriptionAutoDownload  *bool                `json:"subscription_default_auto_download"`
	SubscriptionPriority      *string              `json:"subscription_default_priority"`
	SubscriptionReleaseFilter *music.ReleaseFilter `json:"subscription_default_release_filter"`
}

// GeniusController allows the settings service to control runtime state of the Genius lyrics provider.
type GeniusController interface {
	IsEnabled() bool
	SetEnabled(bool)
}

// Service reads, applies and persists the runtime settings.
type Service struct {
	repo    Repository
	manager *jobs.Manager
	matcher *matcher.Matcher
	static  Settings

	subAutoDownload  atomic.Bool
	subPriority      atomic.Pointer[string]
	subReleaseFilter atomic.Pointer[music.ReleaseFilter]

	geniusMu              sync.RWMutex
	geniusController      GeniusController
	geniusLyricsEnabled   atomic.Bool
	geniusTokenConfigured bool
}

// New builds the settings service.
func New(repo Repository, manager *jobs.Manager, engine *matcher.Matcher, cfg config.Config) (*Service, error) {
	if repo == nil || manager == nil || engine == nil {
		return nil, apperr.New(apperr.CodeInternal,
			"The settings service needs a repository, a job manager and a matcher.")
	}
	s := &Service{
		repo:    repo,
		manager: manager,
		matcher: engine,
		static: Settings{
			ConcurrentDownloads: manager.Concurrency(),
			LibraryPath:         cfg.Library.Path,
			AllowTranscode:      cfg.Downloads.AllowTranscode,
			DurationToleranceMS: cfg.Matching.DurationToleranceMS,
			MetadataProvider:    cfg.Providers.DefaultMetadata,
			MediaProvider:       cfg.Providers.DefaultMedia,
			ServerTimezone:      time.Local.String(),
		},
		geniusTokenConfigured: strings.TrimSpace(cfg.Providers.Genius.AccessToken) != "",
	}
	s.geniusLyricsEnabled.Store(cfg.Providers.Genius.Enabled)
	s.subAutoDownload.Store(false)
	defPri := string(jobs.PriorityLow)
	s.subPriority.Store(&defPri)
	defFilter := music.DefaultReleaseFilter()
	s.subReleaseFilter.Store(&defFilter)
	return s, nil
}

// SetGeniusController connects the Genius provider controller.
func (s *Service) SetGeniusController(c GeniusController) {
	s.geniusMu.Lock()
	defer s.geniusMu.Unlock()
	s.geniusController = c
	if c != nil {
		c.SetEnabled(s.geniusLyricsEnabled.Load())
	}
}

// Load applies the persisted settings. Unreadable values are ignored so that a
// damaged row cannot keep the server from starting.
func (s *Service) Load(ctx context.Context) error {
	stored, err := s.repo.All(ctx)
	if err != nil {
		return err
	}

	if value, ok := parseBool(stored[KeySkipExisting]); ok {
		s.manager.SetSkipExisting(value)
	}
	if raw, ok := stored[KeyMaxWorkers]; ok {
		if w, err := strconv.Atoi(strings.TrimSpace(raw)); err == nil && w >= 1 && w <= 4 {
			s.manager.SetMaxWorkers(w)
		}
	}
	if raw, ok := stored[KeyRateLimit]; ok {
		s.manager.SetRateLimit(strings.TrimSpace(raw))
	}
	if value, ok := parseBool(stored[KeyScheduleEnabled]); ok {
		s.manager.SetScheduleEnabled(value)
	}
	if raw, ok := stored[KeyScheduleStart]; ok && isValidTime(raw) {
		s.manager.SetScheduleStart(strings.TrimSpace(raw))
	}
	if raw, ok := stored[KeyScheduleEnd]; ok && isValidTime(raw) {
		s.manager.SetScheduleEnd(strings.TrimSpace(raw))
	}
	if raw, ok := stored[KeyScheduleTimezone]; ok {
		s.manager.SetScheduleTimezone(strings.TrimSpace(raw))
	}
	if value, ok := parseBool(stored[KeySubscriptionAutoDownload]); ok {
		s.subAutoDownload.Store(value)
	}
	if raw, ok := stored[KeySubscriptionPriority]; ok {
		p := jobs.Priority(strings.TrimSpace(raw))
		if p.Valid() {
			s.subPriority.Store(&raw)
		}
	}
	if raw, ok := stored[KeySubscriptionReleaseFilter]; ok {
		var filter music.ReleaseFilter
		if err := json.Unmarshal([]byte(raw), &filter); err == nil {
			s.subReleaseFilter.Store(&filter)
		}
	}

	if value, ok := parseBool(stored[KeyEmbedCover]); ok {
		s.manager.SetEmbedCover(value)
	}
	if value, ok := parseBool(stored[KeyWriteCoverFile]); ok {
		s.manager.SetWriteCoverFile(value)
	}
	if value, ok := parseBool(stored[KeyLyricsEnabled]); ok {
		s.manager.SetLyricsEnabled(value)
	}
	if value, ok := parseBool(stored[KeyLyricsWriteSidecar]); ok {
		s.manager.SetLyricsWriteSidecar(value)
	}
	if value, ok := parseBool(stored[KeyLyricsGeniusEnabled]); ok {
		s.geniusLyricsEnabled.Store(value)
		s.geniusMu.RLock()
		if s.geniusController != nil {
			s.geniusController.SetEnabled(value)
		}
		s.geniusMu.RUnlock()
	}
	if value, ok := parseBool(stored[KeyQueuePaused]); ok {
		s.manager.SetQueuePaused(value)
	}
	if raw, ok := stored[KeyMatchMinScore]; ok {
		if value, err := strconv.ParseFloat(strings.TrimSpace(raw), 64); err == nil {
			s.matcher.SetMinScore(value)
		}
	}
	return nil
}

// GetSubscriptionDefaults returns default configuration for newly watched artists.
func (s *Service) GetSubscriptionDefaults() (bool, jobs.Priority, music.ReleaseFilter) {
	autoDownload := s.subAutoDownload.Load()
	priStr := s.subPriority.Load()
	pri := jobs.PriorityLow
	if priStr != nil {
		p := jobs.Priority(*priStr)
		if p.Valid() {
			pri = p
		}
	}
	filterPtr := s.subReleaseFilter.Load()
	filter := music.DefaultReleaseFilter()
	if filterPtr != nil {
		filter = *filterPtr
	}
	return autoDownload, pri, filter
}

// SetQueuePaused sets the queue pause state and persists it.
func (s *Service) SetQueuePaused(ctx context.Context, paused bool) error {
	s.manager.SetQueuePaused(paused)
	return s.repo.SetMany(ctx, map[string]string{
		KeyQueuePaused: strconv.FormatBool(paused),
	})
}

// Current returns the effective settings.
func (s *Service) Current() Settings {
	current := s.static
	current.SkipExisting = s.manager.SkipExisting()
	current.MaxWorkers = s.manager.MaxWorkers()
	current.RateLimit = s.manager.RateLimit()
	current.ScheduleEnabled = s.manager.ScheduleEnabled()
	current.ScheduleStart = s.manager.ScheduleStart()
	current.ScheduleEnd = s.manager.ScheduleEnd()
	current.ScheduleTimezone = s.manager.ScheduleTimezone()
	current.ServerTimezone = time.Local.String()

	current.SubscriptionAutoDownload = s.subAutoDownload.Load()
	if pri := s.subPriority.Load(); pri != nil {
		current.SubscriptionPriority = *pri
	} else {
		current.SubscriptionPriority = string(jobs.PriorityLow)
	}
	if filter := s.subReleaseFilter.Load(); filter != nil {
		current.SubscriptionReleaseFilter = *filter
	} else {
		current.SubscriptionReleaseFilter = music.DefaultReleaseFilter()
	}

	current.EmbedCover = s.manager.EmbedCover()
	current.WriteCoverFile = s.manager.WriteCoverFile()
	current.LyricsEnabled = s.manager.LyricsEnabled()
	current.LyricsWriteSidecar = s.manager.LyricsWriteSidecar()
	current.LyricsGeniusEnabled = s.geniusLyricsEnabled.Load()
	current.GeniusTokenConfigured = s.geniusTokenConfigured
	current.MatchMinScore = s.matcher.MinScore()
	return current
}

// Apply validates an update, applies it and persists it.
func (s *Service) Apply(ctx context.Context, update Update) (Settings, error) {
	values := make(map[string]string, 12)

	if update.MaxWorkers != nil {
		w := *update.MaxWorkers
		if w < 1 || w > 4 {
			return Settings{}, apperr.Newf(apperr.CodeInvalidRequest, "max_workers must be between 1 and 4, got %d.", w)
		}
		s.manager.SetMaxWorkers(w)
		values[KeyMaxWorkers] = strconv.Itoa(w)
	}
	if update.RateLimit != nil {
		rl := strings.TrimSpace(*update.RateLimit)
		if !isValidRateLimit(rl) {
			return Settings{}, apperr.Newf(apperr.CodeInvalidRequest, "Invalid rate_limit %q. Must be a number with optional K, M, or G suffix (e.g. 5M) or empty for unlimited.", *update.RateLimit)
		}
		s.manager.SetRateLimit(rl)
		values[KeyRateLimit] = rl
	}

	// Validate schedule window consistency
	effectiveSchedEnabled := s.manager.ScheduleEnabled()
	if update.ScheduleEnabled != nil {
		effectiveSchedEnabled = *update.ScheduleEnabled
	}
	effectiveStart := s.manager.ScheduleStart()
	if update.ScheduleStart != nil {
		st := strings.TrimSpace(*update.ScheduleStart)
		if !isValidTime(st) {
			return Settings{}, apperr.Newf(apperr.CodeInvalidRequest, "schedule_start must be in HH:MM format (00:00 - 23:59), got %q.", st)
		}
		effectiveStart = st
	}
	effectiveEnd := s.manager.ScheduleEnd()
	if update.ScheduleEnd != nil {
		et := strings.TrimSpace(*update.ScheduleEnd)
		if !isValidTime(et) {
			return Settings{}, apperr.Newf(apperr.CodeInvalidRequest, "schedule_end must be in HH:MM format (00:00 - 23:59), got %q.", et)
		}
		effectiveEnd = et
	}
	if effectiveSchedEnabled && effectiveStart == effectiveEnd {
		return Settings{}, apperr.New(apperr.CodeInvalidRequest, "schedule_start and schedule_end cannot be equal when schedule is enabled.")
	}

	if update.ScheduleEnabled != nil {
		s.manager.SetScheduleEnabled(*update.ScheduleEnabled)
		values[KeyScheduleEnabled] = strconv.FormatBool(*update.ScheduleEnabled)
	}
	if update.ScheduleStart != nil {
		st := strings.TrimSpace(*update.ScheduleStart)
		s.manager.SetScheduleStart(st)
		values[KeyScheduleStart] = st
	}
	if update.ScheduleEnd != nil {
		et := strings.TrimSpace(*update.ScheduleEnd)
		s.manager.SetScheduleEnd(et)
		values[KeyScheduleEnd] = et
	}
	if update.ScheduleTimezone != nil {
		tz := strings.TrimSpace(*update.ScheduleTimezone)
		if tz != "" {
			if _, err := time.LoadLocation(tz); err != nil {
				return Settings{}, apperr.Newf(apperr.CodeInvalidRequest, "Invalid timezone %q.", tz)
			}
		}
		s.manager.SetScheduleTimezone(tz)
		values[KeyScheduleTimezone] = tz
	}

	if update.SubscriptionAutoDownload != nil {
		s.subAutoDownload.Store(*update.SubscriptionAutoDownload)
		values[KeySubscriptionAutoDownload] = strconv.FormatBool(*update.SubscriptionAutoDownload)
	}
	if update.SubscriptionPriority != nil {
		p := jobs.Priority(strings.TrimSpace(*update.SubscriptionPriority))
		if !p.Valid() {
			return Settings{}, apperr.Newf(apperr.CodeInvalidRequest, "Invalid subscription priority %q.", *update.SubscriptionPriority)
		}
		priStr := string(p)
		s.subPriority.Store(&priStr)
		values[KeySubscriptionPriority] = priStr
	}
	if update.SubscriptionReleaseFilter != nil {
		if !update.SubscriptionReleaseFilter.Any() {
			return Settings{}, apperr.New(apperr.CodeInvalidRequest, "At least one release type must be enabled in release filter.")
		}
		s.subReleaseFilter.Store(update.SubscriptionReleaseFilter)
		b, err := json.Marshal(update.SubscriptionReleaseFilter)
		if err != nil {
			return Settings{}, apperr.Wrap(apperr.CodeInternal, "failed to marshal release filter", err)
		}
		values[KeySubscriptionReleaseFilter] = string(b)
	}

	if update.MatchMinScore != nil {
		if !s.matcher.SetMinScore(*update.MatchMinScore) {
			return Settings{}, apperr.Newf(apperr.CodeInvalidRequest,
				"match_min_score must be greater than 0 and at most 100, got %v.", *update.MatchMinScore)
		}
		values[KeyMatchMinScore] = strconv.FormatFloat(*update.MatchMinScore, 'f', -1, 64)
	}
	if update.SkipExisting != nil {
		s.manager.SetSkipExisting(*update.SkipExisting)
		values[KeySkipExisting] = strconv.FormatBool(*update.SkipExisting)
	}
	if update.EmbedCover != nil {
		s.manager.SetEmbedCover(*update.EmbedCover)
		values[KeyEmbedCover] = strconv.FormatBool(*update.EmbedCover)
	}
	if update.WriteCoverFile != nil {
		s.manager.SetWriteCoverFile(*update.WriteCoverFile)
		values[KeyWriteCoverFile] = strconv.FormatBool(*update.WriteCoverFile)
	}
	if update.LyricsEnabled != nil {
		s.manager.SetLyricsEnabled(*update.LyricsEnabled)
		values[KeyLyricsEnabled] = strconv.FormatBool(*update.LyricsEnabled)
	}
	if update.LyricsWriteSidecar != nil {
		s.manager.SetLyricsWriteSidecar(*update.LyricsWriteSidecar)
		values[KeyLyricsWriteSidecar] = strconv.FormatBool(*update.LyricsWriteSidecar)
	}
	if update.LyricsGeniusEnabled != nil {
		val := *update.LyricsGeniusEnabled
		s.geniusLyricsEnabled.Store(val)
		s.geniusMu.RLock()
		if s.geniusController != nil {
			s.geniusController.SetEnabled(val)
		}
		s.geniusMu.RUnlock()
		values[KeyLyricsGeniusEnabled] = strconv.FormatBool(val)
	}

	if len(values) == 0 {
		return Settings{}, apperr.New(apperr.CodeInvalidRequest, "The request changes no setting.")
	}
	if err := s.repo.SetMany(ctx, values); err != nil {
		return Settings{}, err
	}
	return s.Current(), nil
}

func isValidRateLimit(val string) bool {
	s := strings.TrimSpace(val)
	if s == "" || s == "0" {
		return true
	}
	// Optional unit suffix: K, M, G, k, m, g
	last := s[len(s)-1]
	digits := s
	if last == 'K' || last == 'k' || last == 'M' || last == 'm' || last == 'G' || last == 'g' {
		digits = s[:len(s)-1]
	}
	if digits == "" {
		return false
	}
	for _, c := range digits {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}

func isValidTime(val string) bool {
	parts := strings.Split(strings.TrimSpace(val), ":")
	if len(parts) != 2 {
		return false
	}
	h, err := strconv.Atoi(parts[0])
	if err != nil || h < 0 || h > 23 {
		return false
	}
	m, err := strconv.Atoi(parts[1])
	if err != nil || m < 0 || m > 59 {
		return false
	}
	return true
}

func parseBool(raw string) (bool, bool) {
	value, err := strconv.ParseBool(strings.TrimSpace(raw))
	if err != nil {
		return false, false
	}
	return value, true
}
