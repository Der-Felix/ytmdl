package mediasession

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"ytdm/backend/internal/apperr"
	"ytdm/backend/internal/provider"
)

// mockRepo provides an in-memory implementation of SessionRepository for pool testing.
type mockRepo struct {
	sessions map[string]Session
	updates  map[string]HealthUpdate
}

func newMockRepo(sessions []Session) *mockRepo {
	m := &mockRepo{
		sessions: make(map[string]Session),
		updates:  make(map[string]HealthUpdate),
	}
	for _, s := range sessions {
		m.sessions[s.ID] = s
	}
	return m
}

func (m *mockRepo) GetSession(ctx context.Context, id string) (*Session, error) {
	s, ok := m.sessions[id]
	if !ok {
		return nil, apperr.New(apperr.CodeSessionNotFound, "session not found")
	}
	return &s, nil
}

func (m *mockRepo) ListSessions(ctx context.Context, filter Filter) ([]Session, error) {
	var list []Session
	for _, s := range m.sessions {
		if filter.ProviderFamily != "" && string(s.ProviderFamily) != filter.ProviderFamily {
			continue
		}
		if filter.Enabled != nil && s.Enabled != *filter.Enabled {
			continue
		}
		if filter.HealthStatus != nil && s.HealthStatus != *filter.HealthStatus {
			continue
		}
		list = append(list, s)
	}
	return list, nil
}

func (m *mockRepo) UpdateHealth(ctx context.Context, id string, update HealthUpdate) (*Session, error) {
	s, ok := m.sessions[id]
	if !ok {
		return nil, apperr.New(apperr.CodeSessionNotFound, "session not found")
	}
	s.HealthStatus = update.HealthStatus
	s.ConsecutiveFailures = update.ConsecutiveFailures
	if update.LastUsedAt != nil {
		s.LastUsedAt = update.LastUsedAt
	}
	if update.LastSuccessAt != nil {
		s.LastSuccessAt = update.LastSuccessAt
	}
	if update.LastFailureAt != nil {
		s.LastFailureAt = update.LastFailureAt
	}
	s.LastFailureReason = update.LastFailureReason
	s.CooldownUntil = update.CooldownUntil
	m.sessions[id] = s
	m.updates[id] = update
	return &s, nil
}

func createTestStorage(t *testing.T, sessionIDs ...string) *CookieStorage {
	t.Helper()
	dir := t.TempDir()
	storage, err := NewCookieStorage(dir, nil)
	if err != nil {
		t.Fatalf("NewCookieStorage failed: %v", err)
	}
	for _, id := range sessionIDs {
		_, err := storage.Store(id, []byte("fake-cookie-content-"+id))
		if err != nil {
			t.Fatalf("Store failed for %s: %v", id, err)
		}
	}
	return storage
}

func TestSessionPool_SelectionHierarchy(t *testing.T) {
	now := time.Date(2026, 9, 6, 12, 0, 0, 0, time.UTC)
	t1 := now.Add(-10 * time.Minute)
	t2 := now.Add(-5 * time.Minute)

	sessions := []Session{
		{
			ID:             "session-a",
			ProviderFamily: provider.FamilyYouTube,
			Name:           "Session A (used 10m ago)",
			CookieRef:      CookieRefPrefix + "session-a",
			Enabled:        true,
			HealthStatus:   HealthHealthy,
			LastUsedAt:     &t1,
		},
		{
			ID:             "session-b",
			ProviderFamily: provider.FamilyYouTube,
			Name:           "Session B (used 5m ago)",
			CookieRef:      CookieRefPrefix + "session-b",
			Enabled:        true,
			HealthStatus:   HealthHealthy,
			LastUsedAt:     &t2,
		},
		{
			ID:             "session-c",
			ProviderFamily: provider.FamilyYouTube,
			Name:           "Session C (never used)",
			CookieRef:      CookieRefPrefix + "session-c",
			Enabled:        true,
			HealthStatus:   HealthHealthy,
			LastUsedAt:     nil, // oldest / never used
		},
		{
			ID:             "session-disabled",
			ProviderFamily: provider.FamilyYouTube,
			Name:           "Disabled Session",
			CookieRef:      CookieRefPrefix + "session-disabled",
			Enabled:        false,
			HealthStatus:   HealthHealthy,
		},
		{
			ID:             "session-cooling",
			ProviderFamily: provider.FamilyYouTube,
			Name:           "Cooling Session",
			CookieRef:      CookieRefPrefix + "session-cooling",
			Enabled:        true,
			HealthStatus:   HealthRateLimited,
			CooldownUntil:  func() *time.Time { cd := now.Add(10 * time.Minute); return &cd }(),
		},
		{
			ID:             "session-auth-failed",
			ProviderFamily: provider.FamilyYouTube,
			Name:           "Auth Failed Session",
			CookieRef:      CookieRefPrefix + "session-auth-failed",
			Enabled:        true,
			HealthStatus:   HealthAuthFailed,
		},
	}

	storage := createTestStorage(t, "session-a", "session-b", "session-c", "session-disabled", "session-cooling", "session-auth-failed")
	repo := newMockRepo(sessions)

	cfg := DefaultPoolConfig(provider.FamilyYouTube)
	cfg.MaxLeasesPerSession = 2
	pool := NewSessionPool(cfg, storage, repo, nil)
	pool.SetNow(func() time.Time { return now })
	pool.SetSyncPersist(true)
	pool.ReloadSessions(sessions)

	ctx := context.Background()

	// 1. Least-Loaded + LRU: session-c has never been used, so it must be selected first
	lease1, err := pool.Acquire(ctx)
	if err != nil {
		t.Fatalf("Acquire 1 failed: %v", err)
	}
	if lease1.SessionID() != "session-c" {
		t.Errorf("Acquire 1 selected %q, want session-c (never used)", lease1.SessionID())
	}

	// 2. Now session-c has 1 lease. session-a and session-b have 0 leases.
	// Between session-a (-10m) and session-b (-5m), session-a is older (LRU tie-break).
	lease2, err := pool.Acquire(ctx)
	if err != nil {
		t.Fatalf("Acquire 2 failed: %v", err)
	}
	if lease2.SessionID() != "session-a" {
		t.Errorf("Acquire 2 selected %q, want session-a (LRU tie-break)", lease2.SessionID())
	}

	// 3. Next: session-b has 0 leases, while session-a and session-c have 1 lease.
	// Least-loaded selects session-b.
	lease3, err := pool.Acquire(ctx)
	if err != nil {
		t.Fatalf("Acquire 3 failed: %v", err)
	}
	if lease3.SessionID() != "session-b" {
		t.Errorf("Acquire 3 selected %q, want session-b (lowest load)", lease3.SessionID())
	}

	// 4. Release lease2 (session-a)
	lease2.Release(nil)

	// Now session-a has 0 leases again. Next acquire should select session-a!
	lease4, err := pool.Acquire(ctx)
	if err != nil {
		t.Fatalf("Acquire 4 failed: %v", err)
	}
	if lease4.SessionID() != "session-a" {
		t.Errorf("Acquire 4 selected %q, want session-a (0 leases)", lease4.SessionID())
	}

	lease1.Release(nil)
	lease3.Release(nil)
	lease4.Release(nil)
}

func TestSessionPool_DeterministicTieBreakByID(t *testing.T) {
	now := time.Date(2026, 9, 6, 12, 0, 0, 0, time.UTC)

	sessions := []Session{
		{
			ID:             "session-z",
			ProviderFamily: provider.FamilyYouTube,
			Name:           "Session Z",
			CookieRef:      CookieRefPrefix + "session-z",
			Enabled:        true,
			HealthStatus:   HealthHealthy,
			LastUsedAt:     &now,
		},
		{
			ID:             "session-m",
			ProviderFamily: provider.FamilyYouTube,
			Name:           "Session M",
			CookieRef:      CookieRefPrefix + "session-m",
			Enabled:        true,
			HealthStatus:   HealthHealthy,
			LastUsedAt:     &now,
		},
	}

	storage := createTestStorage(t, "session-z", "session-m")
	repo := newMockRepo(sessions)

	cfg := DefaultPoolConfig(provider.FamilyYouTube)
	pool := NewSessionPool(cfg, storage, repo, nil)
	pool.SetNow(func() time.Time { return now })
	pool.ReloadSessions(sessions)

	lease, err := pool.Acquire(context.Background())
	if err != nil {
		t.Fatalf("Acquire failed: %v", err)
	}
	defer lease.Release(nil)

	// Tie-break by stable ID: "session-m" < "session-z"
	if lease.SessionID() != "session-m" {
		t.Errorf("got %q, want session-m (deterministic ID tie-break)", lease.SessionID())
	}
}

func TestSessionPool_UnknownSessionControlledProbe(t *testing.T) {
	now := time.Date(2026, 9, 6, 12, 0, 0, 0, time.UTC)

	sessions := []Session{
		{
			ID:             "session-unknown",
			ProviderFamily: provider.FamilyYouTube,
			Name:           "Unknown Session",
			CookieRef:      CookieRefPrefix + "session-unknown",
			Enabled:        true,
			HealthStatus:   HealthUnknown,
		},
	}

	storage := createTestStorage(t, "session-unknown")
	repo := newMockRepo(sessions)

	cfg := DefaultPoolConfig(provider.FamilyYouTube)
	cfg.MaxLeasesPerSession = 5 // maxLeases is 5, but UNKNOWN must be capped at 1!
	pool := NewSessionPool(cfg, storage, repo, nil)
	pool.SetNow(func() time.Time { return now })
	pool.SetSyncPersist(true)
	pool.ReloadSessions(sessions)

	ctx := context.Background()

	// 1st lease on UNKNOWN succeeds (probe)
	lease1, err := pool.Acquire(ctx)
	if err != nil {
		t.Fatalf("1st acquire on UNKNOWN failed: %v", err)
	}

	// 2nd concurrent lease must fail or block because UNKNOWN is capped at 1 concurrency
	ctxTimeout, cancel := context.WithTimeout(ctx, 50*time.Millisecond)
	defer cancel()

	_, err = pool.Acquire(ctxTimeout)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("expected DeadlineExceeded for 2nd concurrent lease on UNKNOWN, got %v", err)
	}

	// Confirmed successful operation transitions UNKNOWN to HEALTHY
	lease1.Release(nil)

	updated, _ := repo.GetSession(ctx, "session-unknown")
	if updated.HealthStatus != HealthHealthy {
		t.Errorf("health_status after success = %q, want healthy", updated.HealthStatus)
	}
	if updated.ConsecutiveFailures != 0 {
		t.Errorf("consecutive_failures = %d, want 0", updated.ConsecutiveFailures)
	}
	if updated.LastSuccessAt == nil {
		t.Error("last_success_at should be set after confirmed success")
	}
}

func TestSessionPool_ExpiredCooldownEligibility(t *testing.T) {
	now := time.Date(2026, 9, 6, 12, 0, 0, 0, time.UTC)
	pastCooldown := now.Add(-1 * time.Minute)

	sessions := []Session{
		{
			ID:                  "session-cooldown-expired",
			ProviderFamily:      provider.FamilyYouTube,
			Name:                "Expired Cooldown Session",
			CookieRef:           CookieRefPrefix + "session-cooldown-expired",
			Enabled:             true,
			HealthStatus:        HealthRateLimited,
			ConsecutiveFailures: 1,
			CooldownUntil:       &pastCooldown,
		},
	}

	storage := createTestStorage(t, "session-cooldown-expired")
	repo := newMockRepo(sessions)

	cfg := DefaultPoolConfig(provider.FamilyYouTube)
	pool := NewSessionPool(cfg, storage, repo, nil)
	pool.SetNow(func() time.Time { return now })
	pool.SetSyncPersist(true)
	pool.ReloadSessions(sessions)

	// Since cooldown expired, session becomes eligible for a probe lease
	lease, err := pool.Acquire(context.Background())
	if err != nil {
		t.Fatalf("Acquire failed: %v", err)
	}

	// Confirmed success clears rate limit and restores to HEALTHY
	lease.Release(nil)

	updated, _ := repo.GetSession(context.Background(), "session-cooldown-expired")
	if updated.HealthStatus != HealthHealthy {
		t.Errorf("health_status = %q, want healthy", updated.HealthStatus)
	}
	if updated.CooldownUntil != nil {
		t.Errorf("cooldown_until should be nil after recovery, got %v", updated.CooldownUntil)
	}
}

func TestSessionPool_FailureContainment_CandidateVsSessionVsProvider(t *testing.T) {
	t.Run("CandidateError_DoesNotPenalizeSession", func(t *testing.T) {
		now := time.Date(2026, 9, 6, 12, 0, 0, 0, time.UTC)
		s := Session{
			ID:             "session-candidate",
			ProviderFamily: provider.FamilyYouTube,
			Name:           "Candidate Test Session",
			CookieRef:      CookieRefPrefix + "session-candidate",
			Enabled:        true,
			HealthStatus:   HealthHealthy,
		}
		storage := createTestStorage(t, "session-candidate")
		repo := newMockRepo([]Session{s})
		cfg := DefaultPoolConfig(provider.FamilyYouTube)
		pool := NewSessionPool(cfg, storage, repo, nil)
		pool.SetNow(func() time.Time { return now })
		pool.SetSyncPersist(true)
		pool.ReloadSessions([]Session{s})

		lease, err := pool.Acquire(context.Background())
		if err != nil {
			t.Fatalf("Acquire failed: %v", err)
		}
		candidateErr := apperr.New(apperr.CodeTrackNotFound, "video unavailable: 404 not found")
		lease.Release(candidateErr)

		s1, _ := repo.GetSession(context.Background(), "session-candidate")
		if s1.HealthStatus != HealthHealthy {
			t.Errorf("candidate error changed health_status to %q, want healthy", s1.HealthStatus)
		}
		if s1.ConsecutiveFailures != 0 {
			t.Errorf("candidate error set consecutive_failures to %d, want 0", s1.ConsecutiveFailures)
		}
	})

	t.Run("ProviderSystemicError_DoesNotPenalizeSession", func(t *testing.T) {
		now := time.Date(2026, 9, 6, 12, 0, 0, 0, time.UTC)
		s := Session{
			ID:             "session-provider",
			ProviderFamily: provider.FamilyYouTube,
			Name:           "Provider Test Session",
			CookieRef:      CookieRefPrefix + "session-provider",
			Enabled:        true,
			HealthStatus:   HealthHealthy,
		}
		storage := createTestStorage(t, "session-provider")
		repo := newMockRepo([]Session{s})
		cfg := DefaultPoolConfig(provider.FamilyYouTube)
		pool := NewSessionPool(cfg, storage, repo, nil)
		pool.SetNow(func() time.Time { return now })
		pool.SetSyncPersist(true)
		pool.ReloadSessions([]Session{s})

		lease, err := pool.Acquire(context.Background())
		if err != nil {
			t.Fatalf("Acquire failed: %v", err)
		}
		providerErr := apperr.New(apperr.CodeProviderRateLimited, "HTTP 429: Too many requests on IP")
		lease.Release(providerErr)

		s1, _ := repo.GetSession(context.Background(), "session-provider")
		if s1.HealthStatus != HealthHealthy {
			t.Errorf("provider error changed health_status to %q, want healthy", s1.HealthStatus)
		}
		fail, ok := pool.LastPlatformFailure()
		if !ok || fail.Err != providerErr {
			t.Errorf("expected platform failure recorded on pool, got %v", fail)
		}
	})

	t.Run("SessionRateLimited_AppliesProgressiveCooldown", func(t *testing.T) {
		now := time.Date(2026, 9, 6, 12, 0, 0, 0, time.UTC)
		s := Session{
			ID:             "session-ratelimit",
			ProviderFamily: provider.FamilyYouTube,
			Name:           "Rate Limit Test Session",
			CookieRef:      CookieRefPrefix + "session-ratelimit",
			Enabled:        true,
			HealthStatus:   HealthHealthy,
		}
		storage := createTestStorage(t, "session-ratelimit")
		repo := newMockRepo([]Session{s})
		cfg := DefaultPoolConfig(provider.FamilyYouTube)
		pool := NewSessionPool(cfg, storage, repo, nil)
		pool.SetNow(func() time.Time { return now })
		pool.SetSyncPersist(true)
		pool.ReloadSessions([]Session{s})

		lease, err := pool.Acquire(context.Background())
		if err != nil {
			t.Fatalf("Acquire failed: %v", err)
		}
		rateErr := apperr.New(apperr.CodeSessionRateLimited, "session rate-limited")
		lease.Release(rateErr)

		s1, _ := repo.GetSession(context.Background(), "session-ratelimit")
		if s1.HealthStatus != HealthRateLimited {
			t.Errorf("health_status = %q, want rate_limited", s1.HealthStatus)
		}
		if s1.ConsecutiveFailures != 1 {
			t.Errorf("consecutive_failures = %d, want 1", s1.ConsecutiveFailures)
		}
		if s1.CooldownUntil == nil || !s1.CooldownUntil.Equal(now.Add(1*time.Minute)) {
			t.Errorf("expected 1m cooldown, got %v", s1.CooldownUntil)
		}
		if s1.LastFailureReason == "" {
			t.Error("last_failure_reason should be populated")
		}
	})

	t.Run("SessionBotChallenge_Applies24hCooldown", func(t *testing.T) {
		now := time.Date(2026, 9, 6, 12, 0, 0, 0, time.UTC)
		s := Session{
			ID:             "session-bot",
			ProviderFamily: provider.FamilyYouTube,
			Name:           "Bot Challenge Test Session",
			CookieRef:      CookieRefPrefix + "session-bot",
			Enabled:        true,
			HealthStatus:   HealthHealthy,
		}
		storage := createTestStorage(t, "session-bot")
		repo := newMockRepo([]Session{s})
		cfg := DefaultPoolConfig(provider.FamilyYouTube)
		pool := NewSessionPool(cfg, storage, repo, nil)
		pool.SetNow(func() time.Time { return now })
		pool.SetSyncPersist(true)
		pool.ReloadSessions([]Session{s})

		lease, err := pool.Acquire(context.Background())
		if err != nil {
			t.Fatalf("Acquire failed: %v", err)
		}
		botErr := apperr.New(apperr.CodeSessionBotChallenge, "Sign in to confirm you're not a bot")
		lease.Release(botErr)

		s1, _ := repo.GetSession(context.Background(), "session-bot")
		if s1.HealthStatus != HealthBotChallenge {
			t.Errorf("health_status = %q, want bot_challenge", s1.HealthStatus)
		}
		if s1.ConsecutiveFailures != 1 {
			t.Errorf("consecutive_failures = %d, want 1", s1.ConsecutiveFailures)
		}
		if s1.CooldownUntil == nil || !s1.CooldownUntil.Equal(now.Add(24*time.Hour)) {
			t.Errorf("expected 24h bot challenge cooldown, got %v", s1.CooldownUntil)
		}
	})

	t.Run("SessionAuthFailed_IndefiniteExclusion", func(t *testing.T) {
		now := time.Date(2026, 9, 6, 12, 0, 0, 0, time.UTC)
		s := Session{
			ID:             "session-auth",
			ProviderFamily: provider.FamilyYouTube,
			Name:           "Auth Failed Test Session",
			CookieRef:      CookieRefPrefix + "session-auth",
			Enabled:        true,
			HealthStatus:   HealthHealthy,
		}
		storage := createTestStorage(t, "session-auth")
		repo := newMockRepo([]Session{s})
		cfg := DefaultPoolConfig(provider.FamilyYouTube)
		pool := NewSessionPool(cfg, storage, repo, nil)
		pool.SetNow(func() time.Time { return now })
		pool.SetSyncPersist(true)
		pool.ReloadSessions([]Session{s})

		lease, err := pool.Acquire(context.Background())
		if err != nil {
			t.Fatalf("Acquire failed: %v", err)
		}
		authErr := apperr.New(apperr.CodeSessionAuthFailed, "Sign in to confirm your subscription: login required")
		lease.Release(authErr)

		s1, _ := repo.GetSession(context.Background(), "session-auth")
		if s1.HealthStatus != HealthAuthFailed {
			t.Errorf("health_status = %q, want auth_failed", s1.HealthStatus)
		}
		if s1.CooldownUntil != nil {
			t.Errorf("auth_failed should have nil cooldown (indefinite exclusion), got %v", s1.CooldownUntil)
		}

		// Subsequent acquire must fail immediately with CodeSessionNotFound
		_, err = pool.Acquire(context.Background())
		if err == nil {
			t.Fatal("expected Acquire to fail when session is auth_failed, got nil")
		}
	})
}

func containsSanitizedCode(reason, code string) bool {
	return fmt.Sprintf("[%s]", code) != "" && (errors.New(reason) != nil)
}
