package mediasession_test

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"ytdm/backend/internal/mediasession"
	"ytdm/backend/internal/provider"
)

func TestHealthStatus_Validation(t *testing.T) {
	validStatuses := []mediasession.HealthStatus{
		mediasession.HealthUnknown,
		mediasession.HealthHealthy,
		mediasession.HealthCooldown,
		mediasession.HealthRateLimited,
		mediasession.HealthBotChallenge,
		mediasession.HealthAuthFailed,
	}

	for _, s := range validStatuses {
		if !s.Valid() {
			t.Errorf("expected %s to be valid", s)
		}
	}

	invalidStatuses := []mediasession.HealthStatus{
		"disabled",
		"active",
		"",
		"bogus",
	}

	for _, s := range invalidStatuses {
		if s.Valid() {
			t.Errorf("expected %s to be invalid", s)
		}
	}
}

func TestRuntimeSession_ConcurrencyLeases(t *testing.T) {
	s := mediasession.Session{
		ID:             "sess-1",
		ProviderFamily: provider.FamilyYouTube,
		Name:           "Session 1",
		Enabled:        true,
		HealthStatus:   mediasession.HealthHealthy,
	}

	rs := mediasession.NewRuntimeSession(s, 2)
	now := time.Now()

	// In-memory leases start strictly at 0
	if rs.CurrentLeases() != 0 {
		t.Fatalf("expected initial leases 0, got %d", rs.CurrentLeases())
	}

	// 1st lease
	if !rs.TryAcquire(now) {
		t.Fatal("expected 1st lease to succeed")
	}
	if rs.CurrentLeases() != 1 {
		t.Fatalf("expected leases 1, got %d", rs.CurrentLeases())
	}

	// 2nd lease
	if !rs.TryAcquire(now) {
		t.Fatal("expected 2nd lease to succeed")
	}
	if rs.CurrentLeases() != 2 {
		t.Fatalf("expected leases 2, got %d", rs.CurrentLeases())
	}

	// 3rd lease should fail (maxLeases = 2)
	if rs.TryAcquire(now) {
		t.Fatal("expected 3rd lease to fail due to capacity")
	}

	// Release 1
	rs.Release()
	if rs.CurrentLeases() != 1 {
		t.Fatalf("expected leases 1 after release, got %d", rs.CurrentLeases())
	}

	// Now can acquire again
	if !rs.TryAcquire(now) {
		t.Fatal("expected acquire after release to succeed")
	}
}

func TestRuntimeSession_HealthAndEnabledGate(t *testing.T) {
	now := time.Now()
	futureCooldown := now.Add(10 * time.Minute)
	pastCooldown := now.Add(-1 * time.Minute)

	tests := []struct {
		name        string
		enabled     bool
		status      mediasession.HealthStatus
		cooldown    *time.Time
		wantAcquire bool
	}{
		{
			name:        "healthy enabled",
			enabled:     true,
			status:      mediasession.HealthHealthy,
			wantAcquire: true,
		},
		{
			name:        "unknown enabled (new session usable)",
			enabled:     true,
			status:      mediasession.HealthUnknown,
			wantAcquire: true,
		},
		{
			name:        "disabled healthy",
			enabled:     false,
			status:      mediasession.HealthHealthy,
			wantAcquire: false,
		},
		{
			name:        "bot challenge enabled",
			enabled:     true,
			status:      mediasession.HealthBotChallenge,
			wantAcquire: false,
		},
		{
			name:        "auth failed enabled",
			enabled:     true,
			status:      mediasession.HealthAuthFailed,
			wantAcquire: false,
		},
		{
			name:        "active cooldown",
			enabled:     true,
			status:      mediasession.HealthCooldown,
			cooldown:    &futureCooldown,
			wantAcquire: false,
		},
		{
			name:        "expired cooldown",
			enabled:     true,
			status:      mediasession.HealthCooldown,
			cooldown:    &pastCooldown,
			wantAcquire: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			s := mediasession.Session{
				ID:            "test-sess",
				Enabled:       tc.enabled,
				HealthStatus:  tc.status,
				CooldownUntil: tc.cooldown,
			}
			rs := mediasession.NewRuntimeSession(s, 1)
			got := rs.TryAcquire(now)
			if got != tc.wantAcquire {
				t.Errorf("TryAcquire = %v, want %v", got, tc.wantAcquire)
			}
		})
	}
}

func TestLegacyAdapter_Compatibility(t *testing.T) {
	tmpDir := t.TempDir()
	cookiePath := filepath.Join(tmpDir, "cookies.txt")

	// 1. Unconfigured / non-existent
	unconfigured := mediasession.NewLegacyAdapter("")
	if unconfigured.IsConfigured() {
		t.Fatal("expected empty adapter not to be configured")
	}
	if unconfigured.SyntheticSession(provider.FamilyYouTube) != nil {
		t.Fatal("expected nil synthetic session for unconfigured adapter")
	}

	nonExistent := mediasession.NewLegacyAdapter(filepath.Join(tmpDir, "missing.txt"))
	if nonExistent.IsConfigured() {
		t.Fatal("expected missing file adapter not to be configured")
	}

	// 2. Configured and readable
	if err := os.WriteFile(cookiePath, []byte("# Netscape HTTP Cookie File\n.youtube.com\tTRUE\t/\tTRUE\t2147483647\tSID\tsecret123\n"), 0600); err != nil {
		t.Fatalf("write cookie file: %v", err)
	}

	adapter := mediasession.NewLegacyAdapter(cookiePath)
	if !adapter.IsConfigured() {
		t.Fatal("expected adapter to be configured for existing file")
	}

	synthetic := adapter.SyntheticSession(provider.FamilyYouTube)
	if synthetic == nil {
		t.Fatal("expected non-nil synthetic session")
	}

	// Invariants check:
	// - Initial health must be UNKNOWN
	if synthetic.HealthStatus != mediasession.HealthUnknown {
		t.Fatalf("expected initial health UNKNOWN, got %s", synthetic.HealthStatus)
	}
	// - Enabled must be true
	if !synthetic.Enabled {
		t.Fatal("expected synthetic session to be enabled")
	}
	// - Internal ID must NOT be magic public UUID contract
	if synthetic.ID != mediasession.LegacySessionID {
		t.Fatalf("expected synthetic ID %s, got %s", mediasession.LegacySessionID, synthetic.ID)
	}
	// - CookieRef must be opaque, NEVER exposing raw filesystem path
	if synthetic.CookieRef == cookiePath || synthetic.CookieRef != mediasession.LegacyCookieRef {
		t.Fatalf("cookie ref must be opaque token %q, got %q", mediasession.LegacyCookieRef, synthetic.CookieRef)
	}

	// 3. Fallback resolution logic:
	// Case A: No DB sessions -> fallback to legacy
	resolved := mediasession.ResolveActiveSessions(nil, adapter, provider.FamilyYouTube)
	if len(resolved) != 1 || resolved[0].ID != mediasession.LegacySessionID {
		t.Fatalf("expected fallback to legacy session, got %+v", resolved)
	}

	// Case B: DB has applicable enabled session -> DB takes precedence, legacy NOT used
	dbSession := mediasession.Session{
		ID:             "db-sess-1",
		ProviderFamily: provider.FamilyYouTube,
		Name:           "DB Session",
		CookieRef:      "managed://db/1",
		Enabled:        true,
		HealthStatus:   mediasession.HealthHealthy,
	}
	resolvedWithDB := mediasession.ResolveActiveSessions([]mediasession.Session{dbSession}, adapter, provider.FamilyYouTube)
	if len(resolvedWithDB) != 1 || resolvedWithDB[0].ID != "db-sess-1" {
		t.Fatalf("expected DB session to take precedence, got %+v", resolvedWithDB)
	}

	// Case C: DB has session for different provider family -> fallback for YouTube
	otherFamilySession := mediasession.Session{
		ID:             "db-sess-other",
		ProviderFamily: provider.Family("soundcloud"),
		Enabled:        true,
	}
	resolvedDifferentFamily := mediasession.ResolveActiveSessions([]mediasession.Session{otherFamilySession}, adapter, provider.FamilyYouTube)
	if len(resolvedDifferentFamily) != 1 || resolvedDifferentFamily[0].ID != mediasession.LegacySessionID {
		t.Fatalf("expected fallback for YouTube when DB only has other family, got %+v", resolvedDifferentFamily)
	}
}
