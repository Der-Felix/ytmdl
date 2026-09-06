package mediasession

import (
	"context"
	"strings"
	"time"

	"ytdm/backend/internal/apperr"
	"ytdm/backend/internal/ytdlp"
)

// ProbeResult contains sanitized outcomes from probing a media session.
// Raw stderr, cookie contents, cookie paths, and auth tokens are NEVER included.
type ProbeResult struct {
	Status             HealthStatus `json:"status"`
	TestedAt           time.Time    `json:"tested_at"`
	MetadataOK         bool         `json:"metadata_ok"`
	UsableAudioFormats bool         `json:"usable_audio_formats"`
	FailureCategory    string       `json:"failure_category,omitempty"`
	CooldownUntil      *time.Time   `json:"cooldown_until,omitempty"`
}

// Prober defines the capability to test a media session with credentials.
type Prober interface {
	Probe(ctx context.Context, sessionID string, cookiePath string) (*ProbeResult, error)
}

// YTDLPProber validates media sessions by executing lightweight yt-dlp metadata queries.
type YTDLPProber struct {
	client *ytdlp.Client
	target string
}

// NewYTDLPProber creates a Prober backed by yt-dlp.
func NewYTDLPProber(client *ytdlp.Client, target string) *YTDLPProber {
	if strings.TrimSpace(target) == "" {
		target = "ytsearch1:Rick Astley Never Gonna Give You Up"
	}
	return &YTDLPProber{
		client: client,
		target: target,
	}
}

// Probe runs a lightweight query against the target to verify credentials.
func (p *YTDLPProber) Probe(ctx context.Context, sessionID string, cookiePath string) (*ProbeResult, error) {
	now := time.Now().UTC()
	c := p.client
	if c == nil {
		return nil, apperr.New(apperr.CodeToolUnavailable, "yt-dlp is not available")
	}
	if cookiePath != "" {
		c = c.WithCookieFile(cookiePath)
	}

	infos, err := c.Query(ctx, p.target, "--no-playlist", "--no-warnings")
	if err != nil {
		code := apperr.CodeOf(err)
		status := HealthUnknown
		var cooldownUntil *time.Time

		switch code {
		case apperr.CodeSessionRateLimited:
			status = HealthRateLimited
			cd := 1 * time.Minute
			until := now.Add(cd)
			cooldownUntil = &until
		case apperr.CodeSessionBotChallenge:
			status = HealthBotChallenge
			until := now.Add(24 * time.Hour)
			cooldownUntil = &until
		case apperr.CodeSessionAuthFailed:
			status = HealthAuthFailed
		case apperr.CodeProviderRateLimited:
			status = HealthRateLimited
			cd := 1 * time.Minute
			until := now.Add(cd)
			cooldownUntil = &until
		default:
			status = HealthUnknown
		}

		return &ProbeResult{
			Status:             status,
			TestedAt:           now,
			MetadataOK:         false,
			UsableAudioFormats: false,
			FailureCategory:    string(code),
			CooldownUntil:      cooldownUntil,
		}, err
	}

	if len(infos) == 0 || infos[0].ID == "" {
		return &ProbeResult{
			Status:             HealthUnknown,
			TestedAt:           now,
			MetadataOK:         false,
			UsableAudioFormats: false,
			FailureCategory:    "EMPTY_METADATA",
		}, apperr.New(apperr.CodeProviderUnavailable, "no metadata returned by provider")
	}

	hasAudio := false
	for _, f := range infos[0].Formats {
		if f.HasAudio() {
			hasAudio = true
			break
		}
	}

	if !hasAudio {
		return &ProbeResult{
			Status:             HealthUnknown,
			TestedAt:           now,
			MetadataOK:         true,
			UsableAudioFormats: false,
			FailureCategory:    "NO_USABLE_AUDIO_FORMATS",
		}, apperr.New(apperr.CodeMediaVerifyFailed, "no usable audio formats found")
	}

	return &ProbeResult{
		Status:             HealthHealthy,
		TestedAt:           now,
		MetadataOK:         true,
		UsableAudioFormats: true,
	}, nil
}
