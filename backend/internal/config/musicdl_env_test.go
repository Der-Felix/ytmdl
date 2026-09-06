package config

import (
	"os"
	"strings"
	"testing"
	"time"
)

type containerEnvValues struct {
	listenAddr          string
	databaseURL         string
	libraryPath         string
	concurrentDownloads int
	ytdlpPath           string
	ffmpegPath          string
	ffprobePath         string
}

func TestMusicDLEnvAliases(t *testing.T) {
	clearConfigEnv(t)

	t.Setenv("MUSICDL_LISTEN_ADDR", "127.0.0.1:9090")
	t.Setenv("MUSICDL_DATABASE_URL", "postgres://musicdl:secret@postgres:5432/musicdl?sslmode=disable")
	t.Setenv("MUSICDL_LIBRARY", "/container/music")
	t.Setenv("MUSICDL_CONCURRENT_DOWNLOADS", "7")
	t.Setenv("MUSICDL_YTDLP", "/container/bin/yt-dlp")
	t.Setenv("MUSICDL_FFMPEG", "/container/bin/ffmpeg")
	t.Setenv("MUSICDL_FFPROBE", "/container/bin/ffprobe")

	cfg := Default()
	if err := cfg.applyEnv(); err != nil {
		t.Fatalf("apply environment: %v", err)
	}

	assertContainerEnvValues(t, cfg, containerEnvValues{
		listenAddr:          "127.0.0.1:9090",
		databaseURL:         "postgres://musicdl:secret@postgres:5432/musicdl?sslmode=disable",
		libraryPath:         "/container/music",
		concurrentDownloads: 7,
		ytdlpPath:           "/container/bin/yt-dlp",
		ffmpegPath:          "/container/bin/ffmpeg",
		ffprobePath:         "/container/bin/ffprobe",
	})
}

func TestMusicDLEnvAliasesOverrideLegacyYTDMValues(t *testing.T) {
	clearConfigEnv(t)

	t.Setenv("YTDM_SERVER_ADDRESS", "legacy.invalid:1001")
	t.Setenv("YTDM_LIBRARY_PATH", "/legacy/music")
	// A canonical value must suppress parsing of the legacy alias as well as
	// overwrite it; stale legacy configuration may otherwise prevent startup.
	t.Setenv("YTDM_MAX_CONCURRENT_DOWNLOADS", "not-a-number")
	t.Setenv("YTDM_YTDLP_PATH", "/legacy/yt-dlp")
	t.Setenv("YTDM_FFMPEG_PATH", "/legacy/ffmpeg")
	t.Setenv("YTDM_FFPROBE_PATH", "/legacy/ffprobe")

	t.Setenv("MUSICDL_LISTEN_ADDR", "0.0.0.0:8080")
	t.Setenv("MUSICDL_DATABASE_URL", "postgres://musicdl:secret@postgres:5432/musicdl")
	t.Setenv("MUSICDL_LIBRARY", "/music")
	t.Setenv("MUSICDL_CONCURRENT_DOWNLOADS", "8")
	t.Setenv("MUSICDL_YTDLP", "/usr/bin/yt-dlp")
	t.Setenv("MUSICDL_FFMPEG", "/usr/bin/ffmpeg")
	t.Setenv("MUSICDL_FFPROBE", "/usr/bin/ffprobe")

	cfg := Default()
	if err := cfg.applyEnv(); err != nil {
		t.Fatalf("apply environment: %v", err)
	}

	assertContainerEnvValues(t, cfg, containerEnvValues{
		listenAddr:          "0.0.0.0:8080",
		databaseURL:         "postgres://musicdl:secret@postgres:5432/musicdl",
		libraryPath:         "/music",
		concurrentDownloads: 8,
		ytdlpPath:           "/usr/bin/yt-dlp",
		ffmpegPath:          "/usr/bin/ffmpeg",
		ffprobePath:         "/usr/bin/ffprobe",
	})
}

func assertContainerEnvValues(t *testing.T, cfg Config, want containerEnvValues) {
	t.Helper()

	got := containerEnvValues{
		listenAddr:          cfg.Server.Address,
		databaseURL:         cfg.Database.URL,
		libraryPath:         cfg.Library.Path,
		concurrentDownloads: cfg.Downloads.Concurrent,
		ytdlpPath:           cfg.Tools.YTDLPPath,
		ffmpegPath:          cfg.Tools.FFmpegPath,
		ffprobePath:         cfg.Tools.FFprobePath,
	}
	if got != want {
		t.Fatalf("container environment values = %+v, want %+v", got, want)
	}
}

// clearConfigEnv prevents a developer's shell configuration from influencing
// these tests while preserving every value for the rest of the test process.
func clearConfigEnv(t *testing.T) {
	t.Helper()
	for _, entry := range os.Environ() {
		key, _, _ := strings.Cut(entry, "=")
		if strings.HasPrefix(key, "MUSICDL_") || strings.HasPrefix(key, "YTDM_") {
			t.Setenv(key, "")
		}
	}
}

// TestRemovedDatabasePathVariablesAreRejected pins that an installation that
// still carries the SQLite configuration fails loudly instead of silently
// starting against a different database.
func TestRemovedDatabasePathVariablesAreRejected(t *testing.T) {
	for _, key := range []string{"MUSICDL_DATABASE", "YTDM_DATABASE_PATH"} {
		t.Run(key, func(t *testing.T) {
			clearConfigEnv(t)
			t.Setenv(key, "/data/musicdl.db")
			t.Setenv("MUSICDL_DATABASE_URL", "postgres://musicdl@postgres:5432/musicdl")

			cfg := Default()
			err := cfg.applyEnv()
			if err == nil {
				t.Fatalf("%s was accepted, want an error naming MUSICDL_DATABASE_URL", key)
			}
			if !strings.Contains(err.Error(), "MUSICDL_DATABASE_URL") {
				t.Fatalf("error = %v, want it to name MUSICDL_DATABASE_URL", err)
			}
		})
	}
}

func TestDatabaseURLIsRedactedForLogging(t *testing.T) {
	cfg := DatabaseConfig{URL: "postgres://musicdl:sup3r-s3cret@postgres:5432/musicdl?sslmode=disable"}

	redacted := cfg.Redacted()
	if strings.Contains(redacted, "sup3r-s3cret") {
		t.Fatalf("redacted URL still contains the password: %s", redacted)
	}
	if !strings.Contains(redacted, "musicdl@postgres:5432") {
		t.Fatalf("redacted URL lost the connection target: %s", redacted)
	}
}

func TestDatabaseValidationRejectsUnusableSettings(t *testing.T) {
	tests := map[string]DatabaseConfig{
		"empty url":     {URL: ""},
		"wrong scheme":  {URL: "mysql://host:3306/db", MaxConns: 4, MinConns: 1, ConnectTimeout: time.Second, StartupTimeout: time.Second, StartupBackoff: time.Second},
		"no host":       {URL: "postgres:///db", MaxConns: 4, MinConns: 1, ConnectTimeout: time.Second, StartupTimeout: time.Second, StartupBackoff: time.Second},
		"min above max": {URL: "postgres://host:5432/db", MaxConns: 2, MinConns: 5, ConnectTimeout: time.Second, StartupTimeout: time.Second, StartupBackoff: time.Second},
		"no timeout":    {URL: "postgres://host:5432/db", MaxConns: 4, MinConns: 1, StartupTimeout: time.Second, StartupBackoff: time.Second},
	}
	for name, cfg := range tests {
		t.Run(name, func(t *testing.T) {
			if errs := cfg.validate(); len(errs) == 0 {
				t.Fatalf("%+v was accepted", cfg)
			}
		})
	}

	valid := Default().Database
	valid.URL = "postgres://musicdl:pw@postgres:5432/musicdl?sslmode=disable"
	if errs := valid.validate(); len(errs) != 0 {
		t.Fatalf("a valid configuration was rejected: %v", errs)
	}
}

func TestTrustedProxiesEnvAndValidation(t *testing.T) {
	clearConfigEnv(t)

	// Defaults: only loopback
	cfg := Default()
	if len(cfg.Server.TrustedProxies) != 2 || cfg.Server.TrustedProxies[0] != "127.0.0.1/32" {
		t.Fatalf("default trusted proxies = %v, want loopback", cfg.Server.TrustedProxies)
	}

	// Environment variable override
	t.Setenv("MUSICDL_TRUSTED_PROXIES", "127.0.0.1/32, ::1/128, 172.31.250.0/28, 10.0.0.5")
	if err := cfg.applyEnv(); err != nil {
		t.Fatalf("applyEnv failed: %v", err)
	}
	if len(cfg.Server.TrustedProxies) != 4 {
		t.Fatalf("got %d trusted proxies, want 4: %v", len(cfg.Server.TrustedProxies), cfg.Server.TrustedProxies)
	}

	// Validation fails on malformed CIDR / IP
	cfg.Server.TrustedProxies = []string{"invalid-cidr-or-ip"}
	if err := cfg.Validate(); err == nil {
		t.Fatal("expected error on invalid trusted proxy entry, got nil")
	}
}

func TestStorageAndStagingEnvConfig(t *testing.T) {
	clearConfigEnv(t)

	t.Setenv("MUSICDL_STORAGE_GUARD_ID", "guard-uuid-1234")
	t.Setenv("MUSICDL_LIBRARY_MIN_FREE_BYTES", "10737418240")
	t.Setenv("MUSICDL_STAGING_DIR", "/data/staging")
	t.Setenv("MUSICDL_STAGING_MIN_FREE_BYTES", "5368709120")
	t.Setenv("MUSICDL_STAGING_MAX_BYTES", "53687091200")
	t.Setenv("MUSICDL_ALLOW_OFFLINE_STAGING", "true")
	t.Setenv("MUSICDL_MAX_ATTEMPTS", "7")

	cfg := Default()
	if err := cfg.applyEnv(); err != nil {
		t.Fatalf("apply environment: %v", err)
	}

	if cfg.Library.StorageGuardID != "guard-uuid-1234" {
		t.Errorf("got StorageGuardID = %q, want guard-uuid-1234", cfg.Library.StorageGuardID)
	}
	if cfg.Library.MinFreeBytes != 10737418240 {
		t.Errorf("got Library.MinFreeBytes = %d, want 10737418240", cfg.Library.MinFreeBytes)
	}
	if cfg.Downloads.StagingDir != "/data/staging" {
		t.Errorf("got Downloads.StagingDir = %q, want /data/staging", cfg.Downloads.StagingDir)
	}
	if cfg.Downloads.StagingMinFreeBytes != 5368709120 {
		t.Errorf("got Downloads.StagingMinFreeBytes = %d, want 5368709120", cfg.Downloads.StagingMinFreeBytes)
	}
	if cfg.Downloads.StagingMaxBytes != 53687091200 {
		t.Errorf("got Downloads.StagingMaxBytes = %d, want 53687091200", cfg.Downloads.StagingMaxBytes)
	}
	if !cfg.Downloads.AllowOfflineStaging {
		t.Errorf("got Downloads.AllowOfflineStaging = false, want true")
	}
	if cfg.Downloads.MaxAttempts != 7 {
		t.Errorf("got Downloads.MaxAttempts = %d, want 7", cfg.Downloads.MaxAttempts)
	}
}

func TestPlayerClientsAndPacingConfig(t *testing.T) {
	clearConfigEnv(t)

	// Valid configuration via MUSICDL_*
	t.Setenv("MUSICDL_YTDLP_PLAYER_CLIENTS", "android,web")
	t.Setenv("MUSICDL_YOUTUBE_REQUESTS_PER_SECOND", "1.5")
	t.Setenv("MUSICDL_YOUTUBE_BURST", "4")

	cfg := Default()
	cfg.Database.URL = "postgres://test:test@localhost:5432/test"
	if err := cfg.applyEnv(); err != nil {
		t.Fatalf("apply environment: %v", err)
	}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("expected valid config, got error: %v", err)
	}

	if cfg.Tools.PlayerClients != "android,web" {
		t.Errorf("got PlayerClients = %q, want android,web", cfg.Tools.PlayerClients)
	}
	if cfg.Providers.YouTube.RequestsPerSecond != 1.5 {
		t.Errorf("got YouTube RequestsPerSecond = %v, want 1.5", cfg.Providers.YouTube.RequestsPerSecond)
	}
	if cfg.Providers.YouTube.Burst != 4 {
		t.Errorf("got YouTube Burst = %d, want 4", cfg.Providers.YouTube.Burst)
	}

	// Malformed player clients should fail validation
	cfg.Tools.PlayerClients = "android; rm -rf /"
	if err := cfg.Validate(); err == nil {
		t.Fatal("expected validation error for malformed player clients, got nil")
	}
}
