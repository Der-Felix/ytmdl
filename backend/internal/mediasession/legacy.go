package mediasession

import (
	"os"
	"strings"
	"time"

	"ytdm/backend/internal/provider"
)

// LegacySessionID is an implementation-internal identifier for the unpersisted
// legacy cookie file session. It is explicitly NOT a public UUID contract.
const LegacySessionID = "legacy:default_cookiefile"

// LegacyCookieRef is the opaque reference for the legacy cookie file.
const LegacyCookieRef = "managed://legacy/default"

// LegacyAdapter manages runtime compatibility with the existing YTDM_COOKIEFILE.
// It never copies cookie secrets or exposes the raw filesystem path in public representations.
type LegacyAdapter struct {
	cookieFilePath string
}

// NewLegacyAdapter creates a legacy adapter for the given cookie file path.
func NewLegacyAdapter(cookieFilePath string) *LegacyAdapter {
	return &LegacyAdapter{
		cookieFilePath: strings.TrimSpace(cookieFilePath),
	}
}

// IsConfigured reports whether a legacy cookie file is specified and exists/is readable on disk.
func (a *LegacyAdapter) IsConfigured() bool {
	if a == nil || a.cookieFilePath == "" {
		return false
	}
	info, err := os.Stat(a.cookieFilePath)
	return err == nil && !info.IsDir() && info.Size() > 0
}

// SyntheticSession creates an in-memory session representation for the legacy cookie file.
// Invariants:
// 1. Initial health is UNKNOWN (never assumed HEALTHY before validation)
// 2. Secret contents are NEVER copied into database or memory
// 3. Raw filesystem path is NOT exposed in CookieRef
// 4. Returns nil if the file is unconfigured or unreadable
func (a *LegacyAdapter) SyntheticSession(family provider.Family) *Session {
	if !a.IsConfigured() {
		return nil
	}
	now := time.Now().UTC()
	return &Session{
		ID:             LegacySessionID,
		ProviderFamily: family,
		Name:           "Legacy Environment Cookie",
		CookieRef:      LegacyCookieRef,
		Enabled:        true,
		HealthStatus:   HealthUnknown,
		CreatedAt:      now,
		UpdatedAt:      now,
	}
}

// CookiePath returns the raw filesystem path for internal worker use only.
// It must never be exposed over public APIs or logs.
func (a *LegacyAdapter) CookiePath() string {
	if a == nil {
		return ""
	}
	return a.cookieFilePath
}

// ResolveActiveSessions returns active managed sessions for the specified provider family.
// If the media_sessions list has no applicable enabled sessions, it falls back to
// the synthetic legacy session if configured and readable.
func ResolveActiveSessions(sessions []Session, legacy *LegacyAdapter, family provider.Family) []Session {
	var active []Session
	for _, s := range sessions {
		if s.ProviderFamily == family && s.Enabled {
			active = append(active, s)
		}
	}
	if len(active) > 0 {
		return active
	}
	if legacy != nil && legacy.IsConfigured() {
		syn := legacy.SyntheticSession(family)
		if syn != nil {
			return []Session{*syn}
		}
	}
	return nil
}
