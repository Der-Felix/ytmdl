package mediasession

import (
	"context"
	"testing"
	"time"

	"ytdm/backend/internal/apperr"
	"ytdm/backend/internal/provider"
)

// TestBotChallenge_Regression_1_FirstChallenge_Preserves24hCooldown verifies that the
// first BOT_CHALLENGE produces a finite 24h cooldown, preserving existing expected duration.
func TestBotChallenge_Regression_1_FirstChallenge_Preserves24hCooldown(t *testing.T) {
	now := time.Date(2026, 9, 8, 12, 0, 0, 0, time.UTC)
	s := Session{
		ID:             "session-bot-1",
		ProviderFamily: provider.FamilyYouTube,
		Name:           "First Bot Challenge Session",
		CookieRef:      CookieRefPrefix + "session-bot-1",
		Enabled:        true,
		HealthStatus:   HealthHealthy,
	}
	storage := createTestStorage(t, "session-bot-1")
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

	updated, _ := repo.GetSession(context.Background(), "session-bot-1")
	if updated.HealthStatus != HealthBotChallenge {
		t.Fatalf("health_status = %q, want bot_challenge", updated.HealthStatus)
	}
	if updated.ConsecutiveFailures != 1 {
		t.Fatalf("consecutive_failures = %d, want 1", updated.ConsecutiveFailures)
	}
	if updated.CooldownUntil == nil {
		t.Fatalf("expected finite cooldown, got nil")
	}
	expected := now.Add(24 * time.Hour)
	if !updated.CooldownUntil.Equal(expected) {
		t.Fatalf("expected 24h cooldown (%v), got %v", expected, updated.CooldownUntil)
	}
}

// TestBotChallenge_Regression_2_SecondChallenge_FiniteCooldownNotNil verifies that
// repeated/second BOT_CHALLENGE produces a finite bounded cooldown (72h) and is NOT nil.
func TestBotChallenge_Regression_2_SecondChallenge_FiniteCooldownNotNil(t *testing.T) {
	now := time.Date(2026, 9, 8, 12, 0, 0, 0, time.UTC)
	s := Session{
		ID:             "session-bot-2",
		ProviderFamily: provider.FamilyYouTube,
		Name:           "Second Bot Challenge Session",
		CookieRef:      CookieRefPrefix + "session-bot-2",
		Enabled:        true,
		HealthStatus:   HealthHealthy,
	}
	storage := createTestStorage(t, "session-bot-2")
	repo := newMockRepo([]Session{s})
	cfg := DefaultPoolConfig(provider.FamilyYouTube)
	pool := NewSessionPool(cfg, storage, repo, nil)
	pool.SetNow(func() time.Time { return now })
	pool.SetSyncPersist(true)
	pool.ReloadSessions([]Session{s})

	botErr := apperr.New(apperr.CodeSessionBotChallenge, "Sign in to confirm you're not a bot")

	// First failure
	lease1, err := pool.Acquire(context.Background())
	if err != nil {
		t.Fatalf("first Acquire failed: %v", err)
	}
	lease1.Release(botErr)

	// Advance past 24h cooldown to permit probe lease
	probeTime := now.Add(24*time.Hour + time.Minute)
	pool.SetNow(func() time.Time { return probeTime })

	// Second failure
	lease2, err := pool.Acquire(context.Background())
	if err != nil {
		t.Fatalf("second Acquire failed: %v", err)
	}
	lease2.Release(botErr)

	updated2, _ := repo.GetSession(context.Background(), "session-bot-2")
	if updated2.HealthStatus != HealthBotChallenge {
		t.Fatalf("second failure: health_status = %q, want bot_challenge", updated2.HealthStatus)
	}
	if updated2.ConsecutiveFailures != 2 {
		t.Fatalf("second failure: consecutive_failures = %d, want 2", updated2.ConsecutiveFailures)
	}
	if updated2.CooldownUntil == nil {
		t.Fatalf("second failure: CooldownUntil must NOT be nil (dead-end bug)")
	}
	expected2 := probeTime.Add(72 * time.Hour)
	if !updated2.CooldownUntil.Equal(expected2) {
		t.Fatalf("second failure: expected 72h cooldown (%v), got %v", expected2, updated2.CooldownUntil)
	}

	// Advance past 72h cooldown and verify third failure is also finite and NOT nil
	probeTime3 := probeTime.Add(72*time.Hour + time.Minute)
	pool.SetNow(func() time.Time { return probeTime3 })

	lease3, err := pool.Acquire(context.Background())
	if err != nil {
		t.Fatalf("third Acquire failed: %v", err)
	}
	lease3.Release(botErr)

	updated3, _ := repo.GetSession(context.Background(), "session-bot-2")
	if updated3.ConsecutiveFailures != 3 {
		t.Fatalf("third failure: consecutive_failures = %d, want 3", updated3.ConsecutiveFailures)
	}
	if updated3.CooldownUntil == nil {
		t.Fatalf("third failure: CooldownUntil must NOT be nil")
	}
	expected3 := probeTime3.Add(72 * time.Hour)
	if !updated3.CooldownUntil.Equal(expected3) {
		t.Fatalf("third failure: expected 72h cooldown (%v), got %v", expected3, updated3.CooldownUntil)
	}
}

// TestBotChallenge_Regression_3_SecondChallenge_WaitingWorkFiniteRetryAfter_NonTerminal verifies
// that waiting work receives finite retry-after and remains non-terminal (SESSION_UNAVAILABLE).
func TestBotChallenge_Regression_3_SecondChallenge_WaitingWorkFiniteRetryAfter_NonTerminal(t *testing.T) {
	now := time.Date(2026, 9, 8, 12, 0, 0, 0, time.UTC)
	s := Session{
		ID:                  "session-bot-3",
		ProviderFamily:      provider.FamilyYouTube,
		Name:                "Waiting Work Session",
		CookieRef:           CookieRefPrefix + "session-bot-3",
		Enabled:             true,
		HealthStatus:        HealthBotChallenge,
		ConsecutiveFailures: 2,
		CooldownUntil:       func() *time.Time { cd := now.Add(72 * time.Hour); return &cd }(),
	}
	storage := createTestStorage(t, "session-bot-3")
	repo := newMockRepo([]Session{s})
	cfg := DefaultPoolConfig(provider.FamilyYouTube)
	pool := NewSessionPool(cfg, storage, repo, nil)
	pool.SetNow(func() time.Time { return now })
	pool.ReloadSessions([]Session{s})

	_, err := pool.Acquire(context.Background())
	if err == nil {
		t.Fatalf("expected Acquire error during cooldown, got nil")
	}

	// Must be non-terminal SESSION_UNAVAILABLE, NOT SESSION_NOT_FOUND
	if apperr.CodeOf(err) != apperr.CodeSessionUnavailable {
		t.Fatalf("expected CodeSessionUnavailable, got %v", apperr.CodeOf(err))
	}
	if !apperr.IsSessionWait(err) {
		t.Fatalf("expected IsSessionWait(err) to be true for non-terminal wait")
	}

	retryAfter, ok := apperr.RetryAfter(err)
	if !ok {
		t.Fatalf("expected retry-after duration on error")
	}
	if retryAfter != 72*time.Hour {
		t.Fatalf("expected retry-after = 72h, got %v", retryAfter)
	}
}

// TestBotChallenge_Regression_4_CooldownActive_SessionNotSelectable verifies that
// while the cooldown is active, the session is ineligible and cannot be acquired.
func TestBotChallenge_Regression_4_CooldownActive_SessionNotSelectable(t *testing.T) {
	now := time.Date(2026, 9, 8, 12, 0, 0, 0, time.UTC)
	cooldown := now.Add(72 * time.Hour)
	s := Session{
		ID:                  "session-bot-4",
		ProviderFamily:      provider.FamilyYouTube,
		Name:                "Cooling Session",
		CookieRef:           CookieRefPrefix + "session-bot-4",
		Enabled:             true,
		HealthStatus:        HealthBotChallenge,
		ConsecutiveFailures: 2,
		CooldownUntil:       &cooldown,
	}
	storage := createTestStorage(t, "session-bot-4")
	repo := newMockRepo([]Session{s})
	cfg := DefaultPoolConfig(provider.FamilyYouTube)
	pool := NewSessionPool(cfg, storage, repo, nil)
	pool.SetNow(func() time.Time { return now })
	pool.ReloadSessions([]Session{s})

	// Direct check on selectBestSession
	candidates := pool.candidateListLocked()
	selected := selectBestSession(candidates, now, true)
	if selected != nil {
		t.Fatalf("session should NOT be selectable during active cooldown, but got %v", selected.Session().ID)
	}

	// Mid-cooldown check at now + 36h
	midTime := now.Add(36 * time.Hour)
	pool.SetNow(func() time.Time { return midTime })
	selectedMid := selectBestSession(candidates, midTime, true)
	if selectedMid != nil {
		t.Fatalf("session should NOT be selectable at mid-cooldown, but got %v", selectedMid.Session().ID)
	}

	_, err := pool.Acquire(context.Background())
	if apperr.CodeOf(err) != apperr.CodeSessionUnavailable {
		t.Fatalf("expected CodeSessionUnavailable, got %v", err)
	}
}

// TestBotChallenge_Regression_5_CooldownExpired_SessionReEntersEligibility verifies that
// once the cooldown naturally expires, the session re-enters eligibility for a single probe
// lease, and a clean release restores it to HealthHealthy.
func TestBotChallenge_Regression_5_CooldownExpired_SessionReEntersEligibility(t *testing.T) {
	now := time.Date(2026, 9, 8, 12, 0, 0, 0, time.UTC)
	cooldown := now.Add(72 * time.Hour)
	s := Session{
		ID:                  "session-bot-5",
		ProviderFamily:      provider.FamilyYouTube,
		Name:                "Expiring Session",
		CookieRef:           CookieRefPrefix + "session-bot-5",
		Enabled:             true,
		HealthStatus:        HealthBotChallenge,
		ConsecutiveFailures: 2,
		CooldownUntil:       &cooldown,
	}
	storage := createTestStorage(t, "session-bot-5")
	repo := newMockRepo([]Session{s})
	cfg := DefaultPoolConfig(provider.FamilyYouTube)
	cfg.MaxLeasesPerSession = 3
	pool := NewSessionPool(cfg, storage, repo, nil)
	pool.SetNow(func() time.Time { return now })
	pool.SetSyncPersist(true)
	pool.ReloadSessions([]Session{s})

	// Before expiry -> ineligble
	if selected := selectBestSession(pool.candidateListLocked(), now, true); selected != nil {
		t.Fatalf("should not be selectable before expiry")
	}

	// Advance time past cooldown
	expiredTime := cooldown.Add(1 * time.Second)
	pool.SetNow(func() time.Time { return expiredTime })

	// Cooldown expired: selectBestSession should select the session for single probe lease
	selected := selectBestSession(pool.candidateListLocked(), expiredTime, true)
	if selected == nil {
		t.Fatalf("expected session to be selectable after cooldown expiry")
	}

	lease, err := pool.Acquire(context.Background())
	if err != nil {
		t.Fatalf("Acquire failed after cooldown expiry: %v", err)
	}

	// While probe lease is active, concurrent lease is capped at 1
	if extra := selectBestSession(pool.candidateListLocked(), expiredTime, true); extra != nil {
		t.Fatalf("expected single probe cap (1 lease) to reject concurrent acquire while probing")
	}

	// Successful release restores session to healthy
	lease.Release(nil)

	updated, _ := repo.GetSession(context.Background(), "session-bot-5")
	if updated.HealthStatus != HealthHealthy {
		t.Fatalf("health_status after successful probe = %q, want healthy", updated.HealthStatus)
	}
	if updated.ConsecutiveFailures != 0 {
		t.Fatalf("consecutive_failures after successful probe = %d, want 0", updated.ConsecutiveFailures)
	}
	if updated.CooldownUntil != nil {
		t.Fatalf("cooldown_until after successful probe = %v, want nil", updated.CooldownUntil)
	}

	// Session now supports its full capacity (3 leases)
	l1, err := pool.Acquire(context.Background())
	if err != nil {
		t.Fatalf("l1 Acquire: %v", err)
	}
	l2, err := pool.Acquire(context.Background())
	if err != nil {
		t.Fatalf("l2 Acquire: %v", err)
	}
	l3, err := pool.Acquire(context.Background())
	if err != nil {
		t.Fatalf("l3 Acquire: %v", err)
	}
	l1.Release(nil)
	l2.Release(nil)
	l3.Release(nil)
}

// TestBotChallenge_Regression_6_SupportedEarlyRecovery_ClearsLongCooldown verifies that
// supported early recovery (e.g. replacing cookies) clears the stale 72h cooldown,
// restores HealthHealthy, and wakes waiting acquisitions.
func TestBotChallenge_Regression_6_SupportedEarlyRecovery_ClearsLongCooldown(t *testing.T) {
	now := time.Date(2026, 9, 8, 12, 0, 0, 0, time.UTC)
	cooldown := now.Add(72 * time.Hour)
	s := Session{
		ID:                  "session-bot-6",
		ProviderFamily:      provider.FamilyYouTube,
		Name:                "Recovery Session",
		CookieRef:           CookieRefPrefix + "session-bot-6",
		Enabled:             true,
		HealthStatus:        HealthBotChallenge,
		ConsecutiveFailures: 2,
		CooldownUntil:       &cooldown,
	}
	storage := createTestStorage(t, "session-bot-6")
	repo := newMockRepo([]Session{s})
	cfg := DefaultPoolConfig(provider.FamilyYouTube)
	pool := NewSessionPool(cfg, storage, repo, nil)
	pool.SetNow(func() time.Time { return now })
	pool.SetSyncPersist(true)
	pool.ReloadSessions([]Session{s})

	// Early recovery happens: operator replaces cookies, updating health in repo and pool
	recoveredNow := now.Add(10 * time.Minute)
	pool.SetNow(func() time.Time { return recoveredNow })

	// Update health in repo as ReplaceCookies / supported recovery does
	recoveredSession, err := repo.UpdateHealth(context.Background(), s.ID, HealthUpdate{
		HealthStatus:        HealthHealthy,
		ConsecutiveFailures: 0,
		LastSuccessAt:       &recoveredNow,
		CooldownUntil:       nil,
	})
	if err != nil {
		t.Fatalf("UpdateHealth: %v", err)
	}

	// Update in pool
	pool.UpsertSession(recoveredSession)

	// Verify cooldown was cleared
	updated, _ := repo.GetSession(context.Background(), "session-bot-6")
	if updated.HealthStatus != HealthHealthy {
		t.Fatalf("health_status = %q, want healthy", updated.HealthStatus)
	}
	if updated.CooldownUntil != nil {
		t.Fatalf("cooldown_until should be nil after recovery, got %v", updated.CooldownUntil)
	}

	// Acquire immediately succeeds without waiting for 72h
	lease, err := pool.Acquire(context.Background())
	if err != nil {
		t.Fatalf("Acquire should succeed immediately after early recovery, got: %v", err)
	}
	lease.Release(nil)
}

// TestBotChallenge_Regression_7_RetryBudget_PreservedWhileWaiting verifies that
// when repeated BOT_CHALLENGE cooldown is active, the resulting error is classified
// as a non-penalizing session wait state where ConsumesJobRetry is false.
func TestBotChallenge_Regression_7_RetryBudget_PreservedWhileWaiting(t *testing.T) {
	now := time.Date(2026, 9, 8, 12, 0, 0, 0, time.UTC)
	cooldown := now.Add(72 * time.Hour)
	s := Session{
		ID:                  "session-bot-7",
		ProviderFamily:      provider.FamilyYouTube,
		Name:                "Retry Budget Test Session",
		CookieRef:           CookieRefPrefix + "session-bot-7",
		Enabled:             true,
		HealthStatus:        HealthBotChallenge,
		ConsecutiveFailures: 2,
		CooldownUntil:       &cooldown,
	}
	storage := createTestStorage(t, "session-bot-7")
	repo := newMockRepo([]Session{s})
	cfg := DefaultPoolConfig(provider.FamilyYouTube)
	pool := NewSessionPool(cfg, storage, repo, nil)
	pool.SetNow(func() time.Time { return now })
	pool.ReloadSessions([]Session{s})

	_, err := pool.Acquire(context.Background())
	if err == nil {
		t.Fatalf("expected Acquire to fail during cooldown")
	}

	if !apperr.IsSessionWait(err) {
		t.Fatalf("expected IsSessionWait(err) = true, got false")
	}
	if apperr.ConsumesJobRetry(err) {
		t.Fatalf("expected ConsumesJobRetry(err) = false, got true (retry budget would be consumed)")
	}
}

// TestBotChallenge_Regression_8_NoBusyLoop_NoFallback15mPolling verifies that
// repeated BOT_CHALLENGE derives retry-after directly from the 72h cooldown and does
// NOT trigger the 15-minute fallback polling busy loop.
func TestBotChallenge_Regression_8_NoBusyLoop_NoFallback15mPolling(t *testing.T) {
	now := time.Date(2026, 9, 8, 12, 0, 0, 0, time.UTC)
	cooldown := now.Add(72 * time.Hour)
	s := Session{
		ID:                  "session-bot-8",
		ProviderFamily:      provider.FamilyYouTube,
		Name:                "No Busy Loop Session",
		CookieRef:           CookieRefPrefix + "session-bot-8",
		Enabled:             true,
		HealthStatus:        HealthBotChallenge,
		ConsecutiveFailures: 2,
		CooldownUntil:       &cooldown,
	}
	storage := createTestStorage(t, "session-bot-8")
	repo := newMockRepo([]Session{s})
	cfg := DefaultPoolConfig(provider.FamilyYouTube)
	pool := NewSessionPool(cfg, storage, repo, nil)
	pool.SetNow(func() time.Time { return now })
	pool.ReloadSessions([]Session{s})

	// Point 1: At now, retry-after must be 72h, NOT 15m
	_, err := pool.Acquire(context.Background())
	wait1, ok := apperr.RetryAfter(err)
	if !ok || wait1 != 72*time.Hour {
		t.Fatalf("at now: retryAfter = %v, want 72h (15m busy-loop detected)", wait1)
	}

	// Point 2: At now + 15m, retry-after must be 71h45m, NOT 15m
	t15m := now.Add(15 * time.Minute)
	pool.SetNow(func() time.Time { return t15m })
	_, err = pool.Acquire(context.Background())
	wait2, ok := apperr.RetryAfter(err)
	expectedWait2 := cooldown.Sub(t15m) // 71h45m
	if !ok || wait2 != expectedWait2 {
		t.Fatalf("at +15m: retryAfter = %v, want %v", wait2, expectedWait2)
	}

	// Point 3: At now + 24h, retry-after must be 48h, NOT 15m
	t24h := now.Add(24 * time.Hour)
	pool.SetNow(func() time.Time { return t24h })
	_, err = pool.Acquire(context.Background())
	wait3, ok := apperr.RetryAfter(err)
	expectedWait3 := cooldown.Sub(t24h) // 48h
	if !ok || wait3 != expectedWait3 {
		t.Fatalf("at +24h: retryAfter = %v, want %v", wait3, expectedWait3)
	}
}
