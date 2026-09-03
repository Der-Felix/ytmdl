// Package config loads the backend configuration from an optional YAML file
// and from environment variables. MUSICDL_* is the public container contract;
// the older YTDM_* names remain supported for backwards compatibility.
// Environment variables always win over the YAML file.
package config

import (
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// Config is the fully resolved configuration of the backend.
type Config struct {
	Server    ServerConfig    `yaml:"server"`
	Database  DatabaseConfig  `yaml:"database"`
	Library   LibraryConfig   `yaml:"library"`
	Tools     ToolsConfig     `yaml:"tools"`
	Downloads DownloadsConfig `yaml:"downloads"`
	Matching  MatchingConfig  `yaml:"matching"`
	Providers ProvidersConfig `yaml:"providers"`

	Subscriptions SubscriptionsConfig `yaml:"subscriptions"`
	Logging       LoggingConfig       `yaml:"logging"`
	Update        UpdateConfig        `yaml:"update"`
}

// UpdateConfig tunes GitHub release update detection.
type UpdateConfig struct {
	Enabled       bool          `yaml:"enabled"`
	Repository    string        `yaml:"repository"`
	CheckInterval time.Duration `yaml:"check_interval"`
}

// ServerConfig describes the HTTP listener.
type ServerConfig struct {
	Address         string        `yaml:"address"`
	ReadTimeout     time.Duration `yaml:"read_timeout"`
	WriteTimeout    time.Duration `yaml:"write_timeout"`
	IdleTimeout     time.Duration `yaml:"idle_timeout"`
	ShutdownTimeout time.Duration `yaml:"shutdown_timeout"`
	MaxRequestBytes int64         `yaml:"max_request_bytes"`
	CookieSecure    bool          `yaml:"cookie_secure"`
	TrustedProxies  []string      `yaml:"trusted_proxies"`
}

// DatabaseConfig describes the PostgreSQL connection and its pool. The URL is
// the single source of truth for host, credentials and database name; the pool
// fields tune how many server connections the backend keeps open.
type DatabaseConfig struct {
	URL string `yaml:"url"`

	MaxConns        int           `yaml:"max_conns"`
	MinConns        int           `yaml:"min_conns"`
	MaxConnLifetime time.Duration `yaml:"max_conn_lifetime"`
	MaxConnIdleTime time.Duration `yaml:"max_conn_idle_time"`
	ConnectTimeout  time.Duration `yaml:"connect_timeout"`

	// StartupTimeout bounds the total time the backend waits for PostgreSQL to
	// accept connections while starting. Compose brings both containers up at
	// once, so the database is regularly a few seconds late.
	StartupTimeout time.Duration `yaml:"startup_timeout"`
	// StartupBackoff is the delay before the first startup retry. It doubles
	// with every further attempt, capped at five seconds.
	StartupBackoff time.Duration `yaml:"startup_backoff"`
}

// Redacted renders the connection URL without its password so that it can be
// logged. An unparsable URL is reduced to a placeholder rather than leaked.
func (d DatabaseConfig) Redacted() string {
	parsed, err := url.Parse(d.URL)
	if err != nil {
		return "postgres://<unparsable>"
	}
	if parsed.User != nil {
		if name := parsed.User.Username(); name != "" {
			parsed.User = url.User(name)
		} else {
			parsed.User = nil
		}
	}
	query := parsed.Query()
	for _, key := range []string{"password", "passfile"} {
		if query.Has(key) {
			query.Set(key, "xxxxx")
		}
	}
	parsed.RawQuery = query.Encode()
	return parsed.Redacted()
}

// LibraryConfig describes where finished audio files are stored.
type LibraryConfig struct {
	Path               string `yaml:"path"`
	StorageGuardID     string `yaml:"storage_guard_id"`
	MinFreeBytes       int64  `yaml:"min_free_bytes"`
	WriteCoverFile     bool   `yaml:"write_cover_file"`
	EmbedCover         bool   `yaml:"embed_cover"`
	LyricsEnabled      bool   `yaml:"lyrics_enabled"`
	LyricsWriteSidecar bool   `yaml:"lyrics_write_sidecar"`
}

// ToolsConfig locates the external programs the backend drives.
type ToolsConfig struct {
	YTDLPPath   string        `yaml:"ytdlp_path"`
	FFmpegPath  string        `yaml:"ffmpeg_path"`
	FFprobePath string        `yaml:"ffprobe_path"`
	CookieFile  string        `yaml:"cookie_file"`
	Timeout     time.Duration `yaml:"timeout"`
}

// DownloadsConfig controls the worker pool and download behaviour.
type DownloadsConfig struct {
	Concurrent          int           `yaml:"concurrent"`
	TrackTimeout        time.Duration `yaml:"track_timeout"`
	MaxRetries          int           `yaml:"max_retries"`
	MaxAttempts         int           `yaml:"max_attempts"`
	RetryBackoff        time.Duration `yaml:"retry_backoff"`
	AllowTranscode      bool          `yaml:"allow_transcode"`
	TempDir             string        `yaml:"temp_dir"`
	StagingDir          string        `yaml:"staging_dir"`
	StagingMinFreeBytes int64         `yaml:"staging_min_free_bytes"`
	StagingMaxBytes     int64         `yaml:"staging_max_bytes"`
	AllowOfflineStaging bool          `yaml:"allow_offline_staging"`
	SkipExisting        bool          `yaml:"skip_existing"`
}

// SubscriptionsConfig controls the periodic discography sync.
type SubscriptionsConfig struct {
	// Enabled switches the periodic sync off. The endpoints and the manual
	// "check now" keep working; only the scheduler stops.
	Enabled bool `yaml:"enabled"`

	// SyncInterval is how long a subscription waits between two runs.
	SyncInterval time.Duration `yaml:"sync_interval"`
	// CheckInterval is how often the scheduler looks for due subscriptions.
	// It is not a poll of the providers: a tick that finds nothing due costs
	// one indexed query.
	CheckInterval time.Duration `yaml:"check_interval"`
	// RetryInterval is when a failed run is attempted again.
	RetryInterval time.Duration `yaml:"retry_interval"`
	// SyncTimeout bounds one run.
	SyncTimeout time.Duration `yaml:"sync_timeout"`
	// BatchSize bounds how many subscriptions one tick works through.
	BatchSize int `yaml:"batch_size"`
}

// MatchingConfig controls the matching engine thresholds.
type MatchingConfig struct {
	MinScore            float64 `yaml:"min_score"`
	CandidateLimit      int     `yaml:"candidate_limit"`
	DurationToleranceMS int     `yaml:"duration_tolerance_ms"`
}

// ProvidersConfig selects and configures the metadata and media providers.
type ProvidersConfig struct {
	DefaultMetadata string        `yaml:"default_metadata"`
	DefaultMedia    string        `yaml:"default_media"`
	Deezer          DeezerConfig  `yaml:"deezer"`
	Spotify         SpotifyConfig `yaml:"spotify"`
	YTMusic         YTMusicConfig `yaml:"ytmusic"`
	YouTube         YouTubeConfig `yaml:"youtube"`
	Genius          GeniusConfig  `yaml:"genius"`
	HTTPTimeout     time.Duration `yaml:"http_timeout"`
}

// DeezerConfig holds the Deezer metadata provider settings.
//
// Deezer allows roughly 50 requests per five seconds per address and answers
// anything beyond that with a quota error instead of data. The pacing and
// retry settings below keep the client inside that ceiling; they apply to
// every consumer of the provider, not only to subscription syncs.
type DeezerConfig struct {
	Enabled    bool   `yaml:"enabled"`
	APIBaseURL string `yaml:"api_base_url"`

	// RequestsPerSecond is the sustained ceiling the client paces itself to.
	RequestsPerSecond float64 `yaml:"requests_per_second"`
	// Burst is how many requests may go out back to back before the pacing
	// takes hold, so that an interactive lookup is not slowed down by it.
	Burst int `yaml:"burst"`
	// MaxRetries bounds how often a rate limited or transiently failing
	// request is retried.
	MaxRetries int `yaml:"max_retries"`
	// RetryBackoff is the wait before the first retry; MaxRetryBackoff caps
	// the doubling and also caps a Retry-After the server sends.
	RetryBackoff    time.Duration `yaml:"retry_backoff"`
	MaxRetryBackoff time.Duration `yaml:"max_retry_backoff"`
}

// SpotifyConfig holds the Spotify metadata provider settings. Credentials are
// deliberately not read from the YAML file; they come from the environment.
type SpotifyConfig struct {
	Enabled      bool   `yaml:"enabled"`
	Market       string `yaml:"market"`
	APIBaseURL   string `yaml:"api_base_url"`
	AuthURL      string `yaml:"auth_url"`
	ClientID     string `yaml:"-"`
	ClientSecret string `yaml:"-"`
}

// YTMusicConfig holds the YouTube Music provider settings.
type YTMusicConfig struct {
	Enabled bool   `yaml:"enabled"`
	BaseURL string `yaml:"base_url"`
}

// YouTubeConfig holds the plain YouTube media provider settings.
type YouTubeConfig struct {
	Enabled bool `yaml:"enabled"`
}

// GeniusConfig holds the Genius lyrics fallback provider settings.
type GeniusConfig struct {
	Enabled     bool   `yaml:"enabled"`
	AccessToken string `yaml:"-"`
}

// LoggingConfig controls the structured logger.
type LoggingConfig struct {
	Level  string `yaml:"level"`
	Format string `yaml:"format"`
}

// Default returns the configuration used when neither a file nor environment
// variables provide a value.
func Default() Config {
	return Config{
		Server: ServerConfig{
			Address:         "0.0.0.0:8080",
			ReadTimeout:     30 * time.Second,
			WriteTimeout:    0, // streamed SSE responses must not be cut off
			IdleTimeout:     120 * time.Second,
			ShutdownTimeout: 30 * time.Second,
			MaxRequestBytes: 1 << 20,
			TrustedProxies:  []string{"127.0.0.1/32", "::1/128"},
		},
		Database: DatabaseConfig{
			URL:             "",
			MaxConns:        10,
			MinConns:        2,
			MaxConnLifetime: 30 * time.Minute,
			MaxConnIdleTime: 5 * time.Minute,
			ConnectTimeout:  10 * time.Second,
			StartupTimeout:  90 * time.Second,
			StartupBackoff:  time.Second,
		},
		Library: LibraryConfig{
			Path:               "./var/music",
			StorageGuardID:     "",
			MinFreeBytes:       0,
			WriteCoverFile:     true,
			EmbedCover:         true,
			LyricsEnabled:      true,
			LyricsWriteSidecar: true,
		},
		Tools: ToolsConfig{
			YTDLPPath:   "yt-dlp",
			FFmpegPath:  "ffmpeg",
			FFprobePath: "ffprobe",
			Timeout:     5 * time.Minute,
		},
		Downloads: DownloadsConfig{
			Concurrent:          2,
			TrackTimeout:        30 * time.Minute,
			MaxRetries:          2,
			MaxAttempts:         5,
			RetryBackoff:        15 * time.Second,
			AllowTranscode:      false,
			TempDir:             "",
			StagingDir:          "",
			StagingMinFreeBytes: 0,
			StagingMaxBytes:     0,
			AllowOfflineStaging: false,
			SkipExisting:        true,
		},
		Matching: MatchingConfig{
			MinScore:            70,
			CandidateLimit:      10,
			DurationToleranceMS: 4000,
		},
		Providers: ProvidersConfig{
			DefaultMetadata: "deezer",
			DefaultMedia:    "ytmusic",
			HTTPTimeout:     20 * time.Second,
			Deezer: DeezerConfig{
				Enabled:    true,
				APIBaseURL: "https://api.deezer.com",
				// Deezer's own ceiling is ten per second; eight leaves room
				// for the burst and the odd retry to fit underneath it.
				RequestsPerSecond: 8,
				Burst:             5,
				MaxRetries:        3,
				RetryBackoff:      500 * time.Millisecond,
				MaxRetryBackoff:   8 * time.Second,
			},
			Spotify: SpotifyConfig{
				Enabled:    true,
				Market:     "DE",
				APIBaseURL: "https://api.spotify.com/v1",
				AuthURL:    "https://accounts.spotify.com/api/token",
			},
			YTMusic: YTMusicConfig{Enabled: true, BaseURL: "https://music.youtube.com"},
			YouTube: YouTubeConfig{Enabled: true},
			Genius:  GeniusConfig{Enabled: false},
		},
		Subscriptions: SubscriptionsConfig{
			Enabled:       true,
			SyncInterval:  24 * time.Hour,
			CheckInterval: 15 * time.Minute,
			RetryInterval: time.Hour,
			SyncTimeout:   30 * time.Minute,
			BatchSize:     25,
		},
		Logging: LoggingConfig{Level: "info", Format: "json"},
		Update: UpdateConfig{
			Enabled:       true,
			Repository:    "Der-Felix/ytmdl",
			CheckInterval: 1 * time.Hour,
		},
	}
}

// Load reads the configuration. path may be empty, in which case only defaults
// and environment variables are used. A missing file at an explicitly given
// path is an error; a missing file at the default location is not.
func Load(path string) (Config, error) {
	cfg := Default()

	explicit := path != ""
	if !explicit {
		path = "config.yaml"
	}
	raw, err := os.ReadFile(path)
	switch {
	case err == nil:
		if err := yaml.Unmarshal(raw, &cfg); err != nil {
			return Config{}, fmt.Errorf("parse config %s: %w", path, err)
		}
	case errors.Is(err, os.ErrNotExist) && !explicit:
		// No config file at the conventional location: defaults plus env.
	default:
		return Config{}, fmt.Errorf("read config %s: %w", path, err)
	}

	if err := cfg.applyEnv(); err != nil {
		return Config{}, err
	}
	if err := cfg.normalise(); err != nil {
		return Config{}, err
	}
	if err := cfg.Validate(); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

// applyEnv overlays YTDM_* environment variables onto the configuration.
func (c *Config) applyEnv() error {
	var errs []error
	str := func(key string, dst *string) {
		if v, ok := lookup(key); ok {
			*dst = v
		}
	}
	num := func(key string, dst *int) {
		v, ok := lookup(key)
		if !ok {
			return
		}
		n, err := strconv.Atoi(v)
		if err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", key, err))
			return
		}
		*dst = n
	}
	num64 := func(key string, dst *int64) {
		v, ok := lookup(key)
		if !ok {
			return
		}
		n, err := strconv.ParseInt(v, 10, 64)
		if err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", key, err))
			return
		}
		*dst = n
	}
	flt := func(key string, dst *float64) {
		v, ok := lookup(key)
		if !ok {
			return
		}
		f, err := strconv.ParseFloat(v, 64)
		if err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", key, err))
			return
		}
		*dst = f
	}
	boolean := func(key string, dst *bool) {
		v, ok := lookup(key)
		if !ok {
			return
		}
		b, err := strconv.ParseBool(v)
		if err != nil {
			errs = append(errs, fmt.Errorf("parse %s=%q as bool: %w", key, v, err))
			return
		}
		*dst = b
	}
	strSlice := func(key string, dst *[]string) {
		if v, ok := lookup(key); ok {
			var list []string
			for _, item := range strings.Split(v, ",") {
				if s := strings.TrimSpace(item); s != "" {
					list = append(list, s)
				}
			}
			if len(list) > 0 {
				*dst = list
			}
		}
	}
	dur := func(key string, dst *time.Duration) {
		v, ok := lookup(key)
		if !ok {
			return
		}
		d, err := time.ParseDuration(v)
		if err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", key, err))
			return
		}
		*dst = d
	}

	// The seven short MUSICDL_* names below are the canonical container
	// interface. Skip their legacy counterpart when both are present so that a
	// stale YTDM_* value cannot make a valid MUSICDL_* configuration fail.
	if _, canonical := lookup("MUSICDL_LISTEN_ADDR"); !canonical {
		str("YTDM_SERVER_ADDRESS", &c.Server.Address)
	}
	dur("YTDM_SERVER_READ_TIMEOUT", &c.Server.ReadTimeout)
	dur("YTDM_SERVER_WRITE_TIMEOUT", &c.Server.WriteTimeout)
	dur("YTDM_SERVER_IDLE_TIMEOUT", &c.Server.IdleTimeout)
	dur("YTDM_SERVER_SHUTDOWN_TIMEOUT", &c.Server.ShutdownTimeout)
	num64("YTDM_SERVER_MAX_REQUEST_BYTES", &c.Server.MaxRequestBytes)

	if _, canonical := lookup("MUSICDL_LIBRARY"); !canonical {
		str("YTDM_LIBRARY_PATH", &c.Library.Path)
	}
	str("MUSICDL_STORAGE_GUARD_ID", &c.Library.StorageGuardID)
	str("YTDM_STORAGE_GUARD_ID", &c.Library.StorageGuardID)
	num64("MUSICDL_LIBRARY_MIN_FREE_BYTES", &c.Library.MinFreeBytes)
	num64("YTDM_LIBRARY_MIN_FREE_BYTES", &c.Library.MinFreeBytes)
	boolean("MUSICDL_LIBRARY_WRITE_COVER_FILE", &c.Library.WriteCoverFile)
	boolean("YTDM_LIBRARY_WRITE_COVER_FILE", &c.Library.WriteCoverFile)
	boolean("MUSICDL_LIBRARY_EMBED_COVER", &c.Library.EmbedCover)
	boolean("YTDM_LIBRARY_EMBED_COVER", &c.Library.EmbedCover)
	boolean("MUSICDL_LIBRARY_LYRICS_ENABLED", &c.Library.LyricsEnabled)
	boolean("YTDM_LIBRARY_LYRICS_ENABLED", &c.Library.LyricsEnabled)
	boolean("MUSICDL_LIBRARY_LYRICS_WRITE_SIDECAR", &c.Library.LyricsWriteSidecar)
	boolean("YTDM_LIBRARY_LYRICS_WRITE_SIDECAR", &c.Library.LyricsWriteSidecar)

	if _, canonical := lookup("MUSICDL_YTDLP"); !canonical {
		str("YTDM_YTDLP_PATH", &c.Tools.YTDLPPath)
	}
	if _, canonical := lookup("MUSICDL_FFMPEG"); !canonical {
		str("YTDM_FFMPEG_PATH", &c.Tools.FFmpegPath)
	}
	if _, canonical := lookup("MUSICDL_FFPROBE"); !canonical {
		str("YTDM_FFPROBE_PATH", &c.Tools.FFprobePath)
	}
	str("YTDM_COOKIEFILE", &c.Tools.CookieFile)
	dur("YTDM_TOOL_TIMEOUT", &c.Tools.Timeout)

	if _, canonical := lookup("MUSICDL_CONCURRENT_DOWNLOADS"); !canonical {
		num("YTDM_MAX_CONCURRENT_DOWNLOADS", &c.Downloads.Concurrent)
	}
	dur("YTDM_TRACK_TIMEOUT", &c.Downloads.TrackTimeout)
	num("YTDM_MAX_RETRIES", &c.Downloads.MaxRetries)
	num("YTDM_MAX_ATTEMPTS", &c.Downloads.MaxAttempts)
	num("MUSICDL_MAX_ATTEMPTS", &c.Downloads.MaxAttempts)
	dur("YTDM_RETRY_BACKOFF", &c.Downloads.RetryBackoff)
	boolean("YTDM_ALLOW_TRANSCODE", &c.Downloads.AllowTranscode)
	str("YTDM_TEMP_DIR", &c.Downloads.TempDir)
	str("MUSICDL_STAGING_DIR", &c.Downloads.StagingDir)
	str("YTDM_STAGING_DIR", &c.Downloads.StagingDir)
	num64("MUSICDL_STAGING_MIN_FREE_BYTES", &c.Downloads.StagingMinFreeBytes)
	num64("YTDM_STAGING_MIN_FREE_BYTES", &c.Downloads.StagingMinFreeBytes)
	num64("MUSICDL_STAGING_MAX_BYTES", &c.Downloads.StagingMaxBytes)
	num64("YTDM_STAGING_MAX_BYTES", &c.Downloads.StagingMaxBytes)
	boolean("MUSICDL_ALLOW_OFFLINE_STAGING", &c.Downloads.AllowOfflineStaging)
	boolean("YTDM_ALLOW_OFFLINE_STAGING", &c.Downloads.AllowOfflineStaging)
	boolean("YTDM_SKIP_EXISTING", &c.Downloads.SkipExisting)

	flt("YTDM_MATCH_MIN_SCORE", &c.Matching.MinScore)
	num("YTDM_MATCH_CANDIDATE_LIMIT", &c.Matching.CandidateLimit)
	num("YTDM_MATCH_DURATION_TOLERANCE_MS", &c.Matching.DurationToleranceMS)

	str("YTDM_DEFAULT_METADATA_PROVIDER", &c.Providers.DefaultMetadata)
	str("YTDM_DEFAULT_MEDIA_PROVIDER", &c.Providers.DefaultMedia)
	dur("YTDM_PROVIDER_HTTP_TIMEOUT", &c.Providers.HTTPTimeout)

	boolean("YTDM_DEEZER_ENABLED", &c.Providers.Deezer.Enabled)
	str("YTDM_DEEZER_API_BASE_URL", &c.Providers.Deezer.APIBaseURL)
	flt("MUSICDL_DEEZER_REQUESTS_PER_SECOND", &c.Providers.Deezer.RequestsPerSecond)
	num("MUSICDL_DEEZER_BURST", &c.Providers.Deezer.Burst)
	num("MUSICDL_DEEZER_MAX_RETRIES", &c.Providers.Deezer.MaxRetries)
	dur("MUSICDL_DEEZER_RETRY_BACKOFF", &c.Providers.Deezer.RetryBackoff)
	dur("MUSICDL_DEEZER_MAX_RETRY_BACKOFF", &c.Providers.Deezer.MaxRetryBackoff)

	boolean("YTDM_SPOTIFY_ENABLED", &c.Providers.Spotify.Enabled)
	str("YTDM_SPOTIFY_MARKET", &c.Providers.Spotify.Market)
	str("YTDM_SPOTIFY_API_BASE_URL", &c.Providers.Spotify.APIBaseURL)
	str("YTDM_SPOTIFY_AUTH_URL", &c.Providers.Spotify.AuthURL)
	str("YTDM_SPOTIFY_CLIENT_ID", &c.Providers.Spotify.ClientID)
	str("YTDM_SPOTIFY_CLIENT_SECRET", &c.Providers.Spotify.ClientSecret)

	boolean("YTDM_YTMUSIC_ENABLED", &c.Providers.YTMusic.Enabled)
	str("YTDM_YTMUSIC_BASE_URL", &c.Providers.YTMusic.BaseURL)
	boolean("YTDM_YOUTUBE_ENABLED", &c.Providers.YouTube.Enabled)
	boolean("MUSICDL_GENIUS_ENABLED", &c.Providers.Genius.Enabled)
	boolean("YTDM_GENIUS_ENABLED", &c.Providers.Genius.Enabled)
	str("GENIUS_ACCESS_TOKEN", &c.Providers.Genius.AccessToken)
	str("MUSICDL_GENIUS_ACCESS_TOKEN", &c.Providers.Genius.AccessToken)
	str("YTDM_GENIUS_ACCESS_TOKEN", &c.Providers.Genius.AccessToken)

	str("YTDM_LOG_LEVEL", &c.Logging.Level)
	str("YTDM_LOG_FORMAT", &c.Logging.Format)

	// Canonical, deliberately concise container settings. They are applied last
	// and therefore override the legacy names above.
	str("MUSICDL_LISTEN_ADDR", &c.Server.Address)
	str("MUSICDL_DATABASE_URL", &c.Database.URL)
	num("MUSICDL_DB_MAX_CONNS", &c.Database.MaxConns)
	num("MUSICDL_DB_MIN_CONNS", &c.Database.MinConns)
	dur("MUSICDL_DB_MAX_CONN_LIFETIME", &c.Database.MaxConnLifetime)
	dur("MUSICDL_DB_MAX_CONN_IDLE_TIME", &c.Database.MaxConnIdleTime)
	dur("MUSICDL_DB_CONNECT_TIMEOUT", &c.Database.ConnectTimeout)
	dur("MUSICDL_DB_STARTUP_TIMEOUT", &c.Database.StartupTimeout)
	dur("MUSICDL_DB_STARTUP_BACKOFF", &c.Database.StartupBackoff)
	str("MUSICDL_LIBRARY", &c.Library.Path)
	str("MUSICDL_STORAGE_GUARD_ID", &c.Library.StorageGuardID)
	num64("MUSICDL_LIBRARY_MIN_FREE_BYTES", &c.Library.MinFreeBytes)
	str("MUSICDL_STAGING_DIR", &c.Downloads.StagingDir)
	num64("MUSICDL_STAGING_MIN_FREE_BYTES", &c.Downloads.StagingMinFreeBytes)
	num64("MUSICDL_STAGING_MAX_BYTES", &c.Downloads.StagingMaxBytes)
	boolean("MUSICDL_ALLOW_OFFLINE_STAGING", &c.Downloads.AllowOfflineStaging)
	num("MUSICDL_MAX_ATTEMPTS", &c.Downloads.MaxAttempts)
	num("MUSICDL_CONCURRENT_DOWNLOADS", &c.Downloads.Concurrent)
	str("MUSICDL_YTDLP", &c.Tools.YTDLPPath)
	str("MUSICDL_FFMPEG", &c.Tools.FFmpegPath)
	str("MUSICDL_FFPROBE", &c.Tools.FFprobePath)
	boolean("MUSICDL_COOKIE_SECURE", &c.Server.CookieSecure)
	boolean("YTDM_COOKIE_SECURE", &c.Server.CookieSecure)
	strSlice("YTDM_TRUSTED_PROXIES", &c.Server.TrustedProxies)
	strSlice("MUSICDL_TRUSTED_PROXIES", &c.Server.TrustedProxies)

	boolean("MUSICDL_SUBSCRIPTIONS_ENABLED", &c.Subscriptions.Enabled)
	dur("MUSICDL_SUBSCRIPTION_SYNC_INTERVAL", &c.Subscriptions.SyncInterval)
	dur("MUSICDL_SUBSCRIPTION_CHECK_INTERVAL", &c.Subscriptions.CheckInterval)
	dur("MUSICDL_SUBSCRIPTION_RETRY_INTERVAL", &c.Subscriptions.RetryInterval)
	dur("MUSICDL_SUBSCRIPTION_SYNC_TIMEOUT", &c.Subscriptions.SyncTimeout)
	num("MUSICDL_SUBSCRIPTION_BATCH_SIZE", &c.Subscriptions.BatchSize)

	boolean("MUSICDL_UPDATE_CHECKS_ENABLED", &c.Update.Enabled)
	str("MUSICDL_UPDATE_REPOSITORY", &c.Update.Repository)
	dur("MUSICDL_UPDATE_CHECK_INTERVAL", &c.Update.CheckInterval)

	// The backend no longer runs on a local database file. Naming the removed
	// variables explicitly turns a silent fallback to the default URL into a
	// clear startup error.
	for _, removed := range []string{"MUSICDL_DATABASE", "YTDM_DATABASE_PATH"} {
		if _, set := lookup(removed); set {
			errs = append(errs, fmt.Errorf(
				"%s is no longer supported; the backend uses PostgreSQL, set MUSICDL_DATABASE_URL instead", removed))
		}
	}

	return errors.Join(errs...)
}

// validate checks that the database can actually be dialled with the given
// settings.
func (d DatabaseConfig) validate() []error {
	var errs []error
	if d.URL == "" {
		return []error{errors.New("database.url must not be empty; set MUSICDL_DATABASE_URL to a PostgreSQL connection URL")}
	}
	parsed, err := url.Parse(d.URL)
	if err != nil {
		return []error{fmt.Errorf("database.url is not a valid URL: %w", err)}
	}
	switch strings.ToLower(parsed.Scheme) {
	case "postgres", "postgresql":
	default:
		errs = append(errs, fmt.Errorf("database.url must use the postgres:// scheme, got %q", parsed.Scheme))
	}
	if parsed.Host == "" {
		errs = append(errs, errors.New("database.url must name a host"))
	}
	if d.MaxConns < 1 || d.MaxConns > 64 {
		errs = append(errs, fmt.Errorf("database.max_conns must be between 1 and 64, got %d", d.MaxConns))
	}
	if d.MinConns < 0 || d.MinConns > d.MaxConns {
		errs = append(errs, fmt.Errorf("database.min_conns must be between 0 and database.max_conns, got %d", d.MinConns))
	}
	if d.ConnectTimeout <= 0 {
		errs = append(errs, errors.New("database.connect_timeout must be greater than zero"))
	}
	if d.StartupTimeout <= 0 {
		errs = append(errs, errors.New("database.startup_timeout must be greater than zero"))
	}
	if d.StartupBackoff <= 0 {
		errs = append(errs, errors.New("database.startup_backoff must be greater than zero"))
	}
	if d.MaxConnLifetime < 0 || d.MaxConnIdleTime < 0 {
		errs = append(errs, errors.New("database.max_conn_lifetime and database.max_conn_idle_time must not be negative"))
	}
	return errs
}

// validate rejects Deezer settings that would defeat the rate limiting.
func (d DeezerConfig) validate() []error {
	if !d.Enabled {
		return nil
	}
	var errs []error
	// Deezer's documented ceiling is 50 requests per 5 seconds. Anything at or
	// above that is not a faster sync, it is a sync that loses releases.
	if d.RequestsPerSecond <= 0 || d.RequestsPerSecond > 10 {
		errs = append(errs, fmt.Errorf(
			"providers.deezer.requests_per_second must be between 0 and 10 (Deezer allows about 10/s), got %v",
			d.RequestsPerSecond))
	}
	if d.Burst < 1 || d.Burst > 50 {
		errs = append(errs, fmt.Errorf(
			"providers.deezer.burst must be between 1 and 50, got %d", d.Burst))
	}
	if d.MaxRetries < 0 || d.MaxRetries > 10 {
		errs = append(errs, fmt.Errorf(
			"providers.deezer.max_retries must be between 0 and 10, got %d", d.MaxRetries))
	}
	if d.RetryBackoff <= 0 {
		errs = append(errs, errors.New("providers.deezer.retry_backoff must be greater than zero"))
	}
	if d.MaxRetryBackoff < d.RetryBackoff {
		errs = append(errs, fmt.Errorf(
			"providers.deezer.max_retry_backoff (%s) must not be below retry_backoff (%s)",
			d.MaxRetryBackoff, d.RetryBackoff))
	}
	// One request must never be able to outlive the sync it belongs to.
	if d.MaxRetryBackoff > time.Minute {
		errs = append(errs, fmt.Errorf(
			"providers.deezer.max_retry_backoff must not exceed 1m, got %s", d.MaxRetryBackoff))
	}
	return errs
}

// minCheckInterval keeps the scheduler off a tight loop. Anything below a
// minute is a configuration mistake, not a faster sync: the subscriptions
// themselves are due once a day.
const minCheckInterval = time.Minute

// validate rejects a subscription configuration the scheduler cannot run with.
func (s SubscriptionsConfig) validate() []error {
	var errs []error
	if s.SyncInterval < time.Minute {
		errs = append(errs, fmt.Errorf(
			"subscriptions.sync_interval must be at least 1m, got %s", s.SyncInterval))
	}
	if s.CheckInterval < minCheckInterval {
		errs = append(errs, fmt.Errorf(
			"subscriptions.check_interval must be at least %s, got %s", minCheckInterval, s.CheckInterval))
	}
	if s.CheckInterval > s.SyncInterval {
		errs = append(errs, fmt.Errorf(
			"subscriptions.check_interval (%s) must not exceed subscriptions.sync_interval (%s); "+
				"a subscription would be checked for less often than it is due",
			s.CheckInterval, s.SyncInterval))
	}
	if s.RetryInterval < time.Minute {
		errs = append(errs, fmt.Errorf(
			"subscriptions.retry_interval must be at least 1m, got %s", s.RetryInterval))
	}
	if s.SyncTimeout <= 0 {
		errs = append(errs, errors.New("subscriptions.sync_timeout must be greater than zero"))
	}
	if s.BatchSize < 1 || s.BatchSize > 200 {
		errs = append(errs, fmt.Errorf(
			"subscriptions.batch_size must be between 1 and 200, got %d", s.BatchSize))
	}
	return errs
}

func lookup(key string) (string, bool) {
	v, ok := os.LookupEnv(key)
	if !ok {
		return "", false
	}
	v = strings.TrimSpace(v)
	if v == "" {
		return "", false
	}
	return v, true
}

// normalise expands the configured paths to absolute, cleaned paths so that
// every later path check operates on a stable prefix.
func (c *Config) normalise() error {
	c.Logging.Level = strings.ToLower(strings.TrimSpace(c.Logging.Level))
	c.Logging.Format = strings.ToLower(strings.TrimSpace(c.Logging.Format))
	c.Providers.DefaultMetadata = strings.ToLower(strings.TrimSpace(c.Providers.DefaultMetadata))
	c.Providers.DefaultMedia = strings.ToLower(strings.TrimSpace(c.Providers.DefaultMedia))

	c.Database.URL = strings.TrimSpace(c.Database.URL)

	for _, p := range []*string{&c.Library.Path, &c.Downloads.TempDir} {
		if *p == "" {
			continue
		}
		abs, err := filepath.Abs(*p)
		if err != nil {
			return fmt.Errorf("resolve path %q: %w", *p, err)
		}
		*p = abs
	}
	return nil
}

// Validate rejects configurations the backend cannot run with.
func (c *Config) Validate() error {
	var errs []error
	if c.Server.Address == "" {
		errs = append(errs, errors.New("server.address must not be empty"))
	}
	if c.Server.ShutdownTimeout <= 0 {
		errs = append(errs, errors.New("server.shutdown_timeout must be greater than zero"))
	}
	errs = append(errs, c.Database.validate()...)
	if c.Library.Path == "" {
		errs = append(errs, errors.New("library.path must not be empty"))
	}
	if c.Downloads.Concurrent < 1 || c.Downloads.Concurrent > 16 {
		errs = append(errs, fmt.Errorf("downloads.concurrent must be between 1 and 16, got %d", c.Downloads.Concurrent))
	}
	if c.Downloads.MaxRetries < 0 || c.Downloads.MaxRetries > 10 {
		errs = append(errs, fmt.Errorf("downloads.max_retries must be between 0 and 10, got %d", c.Downloads.MaxRetries))
	}
	if c.Matching.MinScore < 0 || c.Matching.MinScore > 100 {
		errs = append(errs, fmt.Errorf("matching.min_score must be between 0 and 100, got %v", c.Matching.MinScore))
	}
	if c.Matching.CandidateLimit < 1 || c.Matching.CandidateLimit > 50 {
		errs = append(errs, fmt.Errorf("matching.candidate_limit must be between 1 and 50, got %d", c.Matching.CandidateLimit))
	}
	if c.Matching.DurationToleranceMS < 0 {
		errs = append(errs, errors.New("matching.duration_tolerance_ms must not be negative"))
	}
	switch c.Logging.Level {
	case "debug", "info", "warn", "error":
	default:
		errs = append(errs, fmt.Errorf("logging.level must be debug, info, warn or error, got %q", c.Logging.Level))
	}
	switch c.Logging.Format {
	case "json", "text":
	default:
		errs = append(errs, fmt.Errorf("logging.format must be json or text, got %q", c.Logging.Format))
	}
	if c.Providers.Spotify.Enabled && (c.Providers.Spotify.ClientID == "" || c.Providers.Spotify.ClientSecret == "") {
		// Not fatal: the provider registers itself as unavailable instead of
		// preventing the server from starting.
		c.Providers.Spotify.Enabled = false
	}
	errs = append(errs, c.Subscriptions.validate()...)
	errs = append(errs, c.Providers.Deezer.validate()...)
	if c.Server.MaxRequestBytes < 1024 {
		errs = append(errs, fmt.Errorf("server.max_request_bytes must be at least 1024, got %d", c.Server.MaxRequestBytes))
	}
	for _, proxy := range c.Server.TrustedProxies {
		if _, _, err := net.ParseCIDR(proxy); err != nil {
			if ip := net.ParseIP(proxy); ip == nil {
				errs = append(errs, fmt.Errorf("server.trusted_proxies contains invalid IP or CIDR %q", proxy))
			}
		}
	}
	if c.Update.Enabled {
		if c.Update.Repository == "" {
			c.Update.Repository = "Der-Felix/ytmdl"
		}
		if !updateRepoRegex.MatchString(c.Update.Repository) {
			errs = append(errs, fmt.Errorf("update.repository must be in format owner/repo, got %q", c.Update.Repository))
		}
		if c.Update.CheckInterval < 5*time.Minute {
			c.Update.CheckInterval = 5 * time.Minute
		}
	}
	return errors.Join(errs...)
}

var updateRepoRegex = regexp.MustCompile(`^[a-zA-Z0-9_.-]+/[a-zA-Z0-9_.-]+$`)
