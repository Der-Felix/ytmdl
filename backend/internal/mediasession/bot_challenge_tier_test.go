package mediasession

import (
	"context"
	"testing"
	"time"

	"ytdm/backend/internal/apperr"
	"ytdm/backend/internal/provider"
)

// TestBotChallengeTier_1_FreshSession_FirstChallenge_24h verifies that a fresh or healthy
// session encountering its first BOT_CHALLENGE receives a 24h cooldown.
func TestBotChallengeTier_1_FreshSession_FirstChallenge_24h(t *testing.T) {
	now := time.Date(2026, 9, 8, 12, 0, 0, 0, time.UTC)
	s := Session{
		ID:             "session-fresh-bot",
		ProviderFamily: provider.FamilyYouTube,
		Name:           "Fresh Bot Session",
		CookieRef:      CookieRefPrefix + "session-fresh-bot",
		Enabled:        true,
		HealthStatus:   HealthHealthy,
	}
	storage := createTestStorage(t, "session-fresh-bot")
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

	updated, _ := repo.GetSession(context.Background(), "session-fresh-bot")
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

// TestBotChallengeTier_2_RateLimitedFirst_ThenBotChallenge_24h verifies that if a session
// has a prior RATE_LIMITED failure (ConsecutiveFailures = 1), its first subsequent BOT_CHALLENGE
// receives a 24h cooldown, NOT the repeated 72h tier.
func TestBotChallengeTier_2_RateLimitedFirst_ThenBotChallenge_24h(t *testing.T) {
	now := time.Date(2026, 9, 8, 12, 0, 0, 0, time.UTC)
	s := Session{
		ID:             "session-rate-then-bot",
		ProviderFamily: provider.FamilyYouTube,
		Name:           "Rate Then Bot Session",
		CookieRef:      CookieRefPrefix + "session-rate-then-bot",
		Enabled:        true,
		HealthStatus:   HealthHealthy,
	}
	storage := createTestStorage(t, "session-rate-then-bot")
	repo := newMockRepo([]Session{s})
	cfg := DefaultPoolConfig(provider.FamilyYouTube)
	pool := NewSessionPool(cfg, storage, repo, nil)
	pool.SetNow(func() time.Time { return now })
	pool.SetSyncPersist(true)
	pool.ReloadSessions([]Session{s})

	// First failure: RATE_LIMITED
	l1, err := pool.Acquire(context.Background())
	if err != nil {
		t.Fatalf("first Acquire failed: %v", err)
	}
	rateErr := apperr.New(apperr.CodeSessionRateLimited, "rate limited by provider")
	l1.Release(rateErr)

	up1, _ := repo.GetSession(context.Background(), "session-rate-then-bot")
	if up1.HealthStatus != HealthRateLimited {
		t.Fatalf("up1 health_status = %q, want rate_limited", up1.HealthStatus)
	}
	if up1.ConsecutiveFailures != 1 {
		t.Fatalf("up1 consecutive_failures = %d, want 1", up1.ConsecutiveFailures)
	}

	// Advance past rate limit cooldown (1 minute) so probe lease is permitted
	t2 := now.Add(2 * time.Minute)
	pool.SetNow(func() time.Time { return t2 })

	// Second failure: FIRST EVER BOT_CHALLENGE
	l2, err := pool.Acquire(context.Background())
	if err != nil {
		t.Fatalf("second Acquire failed: %v", err)
	}
	botErr := apperr.New(apperr.CodeSessionBotChallenge, "Sign in to confirm you're not a bot")
	l2.Release(botErr)

	up2, _ := repo.GetSession(context.Background(), "session-rate-then-bot")
	if up2.HealthStatus != HealthBotChallenge {
		t.Fatalf("up2 health_status = %q, want bot_challenge", up2.HealthStatus)
	}
	// Generic ConsecutiveFailures counter must accurately reflect total session failures (2)
	if up2.ConsecutiveFailures != 2 {
		t.Fatalf("up2 consecutive_failures = %d, want 2", up2.ConsecutiveFailures)
	}
	if up2.CooldownUntil == nil {
		t.Fatalf("up2 expected finite cooldown, got nil")
	}

	// Crucial check: First bot challenge MUST receive 24h, NOT 72h!
	expected24h := t2.Add(24 * time.Hour)
	wrong72h := t2.Add(72 * time.Hour)
	if up2.CooldownUntil.Equal(wrong72h) {
		t.Fatalf("BUG CONFIRMED: received 72h repeat cooldown after RATE_LIMITED, want 24h initial cooldown")
	}
	if !up2.CooldownUntil.Equal(expected24h) {
		t.Fatalf("expected 24h cooldown (%v), got %v", expected24h, up2.CooldownUntil)
	}
}

// TestBotChallengeTier_3_AuthFailedFirst_ThenBotChallenge_24h verifies that if a session
// has a prior AUTH_FAILED failure, its first BOT_CHALLENGE receives a 24h cooldown.
func TestBotChallengeTier_3_AuthFailedFirst_ThenBotChallenge_24h(t *testing.T) {
	now := time.Date(2026, 9, 8, 12, 0, 0, 0, time.UTC)
	s := Session{
		ID:             "session-auth-then-bot",
		ProviderFamily: provider.FamilyYouTube,
		Name:           "Auth Then Bot Session",
		CookieRef:      CookieRefPrefix + "session-auth-then-bot",
		Enabled:        true,
		HealthStatus:   HealthHealthy,
	}
	storage := createTestStorage(t, "session-auth-then-bot")
	repo := newMockRepo([]Session{s})
	cfg := DefaultPoolConfig(provider.FamilyYouTube)
	pool := NewSessionPool(cfg, storage, repo, nil)
	pool.SetNow(func() time.Time { return now })
	pool.SetSyncPersist(true)
	pool.ReloadSessions([]Session{s})

	// First failure: AUTH_FAILED
	l1, err := pool.Acquire(context.Background())
	if err != nil {
		t.Fatalf("first Acquire failed: %v", err)
	}
	authErr := apperr.New(apperr.CodeSessionAuthFailed, "session authentication invalid")
	l1.Release(authErr)

	up1, _ := repo.GetSession(context.Background(), "session-auth-then-bot")
	if up1.HealthStatus != HealthAuthFailed {
		t.Fatalf("up1 health_status = %q, want auth_failed", up1.HealthStatus)
	}
	if up1.ConsecutiveFailures != 1 {
		t.Fatalf("up1 consecutive_failures = %d, want 1", up1.ConsecutiveFailures)
	}

	// Directly record subsequent BOT_CHALLENGE failure
	t2 := now.Add(5 * time.Minute)
	botErr := apperr.New(apperr.CodeSessionBotChallenge, "Sign in to confirm you're not a bot")
	pool.RecordFailure(context.Background(), s.ID, botErr, t2)

	up2, _ := repo.GetSession(context.Background(), "session-auth-then-bot")
	if up2.HealthStatus != HealthBotChallenge {
		t.Fatalf("up2 health_status = %q, want bot_challenge", up2.HealthStatus)
	}
	if up2.ConsecutiveFailures != 2 {
		t.Fatalf("up2 consecutive_failures = %d, want 2", up2.ConsecutiveFailures)
	}
	if up2.CooldownUntil == nil {
		t.Fatalf("up2 expected finite cooldown, got nil")
	}

	expected24h := t2.Add(24 * time.Hour)
	wrong72h := t2.Add(72 * time.Hour)
	if up2.CooldownUntil.Equal(wrong72h) {
		t.Fatalf("BUG CONFIRMED: received 72h repeat cooldown after AUTH_FAILED, want 24h initial cooldown")
	}
	if !up2.CooldownUntil.Equal(expected24h) {
		t.Fatalf("expected 24h cooldown (%v), got %v", expected24h, up2.CooldownUntil)
	}
}

// TestBotChallengeTier_4_GenericFailureFirst_ThenBotChallenge_24h verifies that if a session
// has a prior generic failure incrementing ConsecutiveFailures, its first BOT_CHALLENGE
// receives a 24h cooldown.
func TestBotChallengeTier_4_GenericFailureFirst_ThenBotChallenge_24h(t *testing.T) {
	now := time.Date(2026, 9, 8, 12, 0, 0, 0, time.UTC)
	s := Session{
		ID:             "session-generic-then-bot",
		ProviderFamily: provider.FamilyYouTube,
		Name:           "Generic Then Bot Session",
		CookieRef:      CookieRefPrefix + "session-generic-then-bot",
		Enabled:        true,
		HealthStatus:   HealthHealthy,
	}
	storage := createTestStorage(t, "session-generic-then-bot")
	repo := newMockRepo([]Session{s})
	cfg := DefaultPoolConfig(provider.FamilyYouTube)
	pool := NewSessionPool(cfg, storage, repo, nil)
	pool.SetNow(func() time.Time { return now })
	pool.SetSyncPersist(true)
	pool.ReloadSessions([]Session{s})

	// First failure: generic session rate limit or other session-scoped failure
	l1, err := pool.Acquire(context.Background())
	if err != nil {
		t.Fatalf("first Acquire failed: %v", err)
	}
	genErr := apperr.New(apperr.CodeSessionRateLimited, "temporary generic session error")
	l1.Release(genErr)

	// Advance time past 1m cooldown
	t2 := now.Add(2 * time.Minute)
	pool.SetNow(func() time.Time { return t2 })

	// Second failure: BOT_CHALLENGE
	l2, err := pool.Acquire(context.Background())
	if err != nil {
		t.Fatalf("second Acquire failed: %v", err)
	}
	botErr := apperr.New(apperr.CodeSessionBotChallenge, "Sign in to confirm you're not a bot")
	l2.Release(botErr)

	up2, _ := repo.GetSession(context.Background(), "session-generic-then-bot")
	if up2.HealthStatus != HealthBotChallenge {
		t.Fatalf("up2 health_status = %q, want bot_challenge", up2.HealthStatus)
	}
	if up2.ConsecutiveFailures != 2 {
		t.Fatalf("up2 consecutive_failures = %d, want 2", up2.ConsecutiveFailures)
	}

	expected24h := t2.Add(24 * time.Hour)
	wrong72h := t2.Add(72 * time.Hour)
	if up2.CooldownUntil.Equal(wrong72h) {
		t.Fatalf("BUG CONFIRMED: received 72h repeat cooldown after generic failure, want 24h initial cooldown")
	}
	if !up2.CooldownUntil.Equal(expected24h) {
		t.Fatalf("expected 24h cooldown (%v), got %v", expected24h, up2.CooldownUntil)
	}
}

// TestBotChallengeTier_5_GenuineRepeatedBotChallenge_72h verifies that genuine repeated
// BOT_CHALLENGE (without healthy recovery in between) receives the 72h tier.
func TestBotChallengeTier_5_GenuineRepeatedBotChallenge_72h(t *testing.T) {
	now := time.Date(2026, 9, 8, 12, 0, 0, 0, time.UTC)
	s := Session{
		ID:             "session-repeat-bot",
		ProviderFamily: provider.FamilyYouTube,
		Name:           "Repeat Bot Session",
		CookieRef:      CookieRefPrefix + "session-repeat-bot",
		Enabled:        true,
		HealthStatus:   HealthHealthy,
	}
	storage := createTestStorage(t, "session-repeat-bot")
	repo := newMockRepo([]Session{s})
	cfg := DefaultPoolConfig(provider.FamilyYouTube)
	pool := NewSessionPool(cfg, storage, repo, nil)
	pool.SetNow(func() time.Time { return now })
	pool.SetSyncPersist(true)
	pool.ReloadSessions([]Session{s})

	botErr := apperr.New(apperr.CodeSessionBotChallenge, "Sign in to confirm you're not a bot")

	// First challenge: 24h
	l1, err := pool.Acquire(context.Background())
	if err != nil {
		t.Fatalf("first Acquire: %v", err)
	}
	l1.Release(botErr)

	up1, _ := repo.GetSession(context.Background(), "session-repeat-bot")
	expected24h := now.Add(24 * time.Hour)
	if !up1.CooldownUntil.Equal(expected24h) {
		t.Fatalf("first challenge cooldown = %v, want %v", up1.CooldownUntil, expected24h)
	}

	// Advance past 24h cooldown to permit probe lease
	t2 := now.Add(24*time.Hour + time.Minute)
	pool.SetNow(func() time.Time { return t2 })

	// Second challenge: repeated, must receive 72h
	l2, err := pool.Acquire(context.Background())
	if err != nil {
		t.Fatalf("second Acquire: %v", err)
	}
	l2.Release(botErr)

	up2, _ := repo.GetSession(context.Background(), "session-repeat-bot")
	if up2.HealthStatus != HealthBotChallenge {
		t.Fatalf("second failure health_status = %q, want bot_challenge", up2.HealthStatus)
	}
	if up2.ConsecutiveFailures != 2 {
		t.Fatalf("second failure consecutive_failures = %d, want 2", up2.ConsecutiveFailures)
	}
	if up2.CooldownUntil == nil {
		t.Fatalf("second failure cooldown is nil, want finite 72h")
	}
	expected72h := t2.Add(72 * time.Hour)
	if !up2.CooldownUntil.Equal(expected72h) {
		t.Fatalf("second challenge cooldown = %v, want 72h (%v)", up2.CooldownUntil, expected72h)
	}

	// Advance past 72h cooldown to permit third probe lease
	t3 := t2.Add(72*time.Hour + time.Minute)
	pool.SetNow(func() time.Time { return t3 })

	// Third challenge: also 72h, never nil
	l3, err := pool.Acquire(context.Background())
	if err != nil {
		t.Fatalf("third Acquire: %v", err)
	}
	l3.Release(botErr)

	up3, _ := repo.GetSession(context.Background(), "session-repeat-bot")
	if up3.CooldownUntil == nil {
		t.Fatalf("third failure cooldown is nil, want finite 72h")
	}
	expectedThird72h := t3.Add(72 * time.Hour)
	if !up3.CooldownUntil.Equal(expectedThird72h) {
		t.Fatalf("third challenge cooldown = %v, want 72h (%v)", up3.CooldownUntil, expectedThird72h)
	}
}

// TestBotChallengeTier_6_RateLimitProgressiveCooldown_Unchanged verifies that
// calculateRateLimitCooldown continues to use generic ConsecutiveFailures progressive tiers.
func TestBotChallengeTier_6_RateLimitProgressiveCooldown_Unchanged(t *testing.T) {
	now := time.Date(2026, 9, 8, 12, 0, 0, 0, time.UTC)
	s := Session{
		ID:             "session-progressive-rate",
		ProviderFamily: provider.FamilyYouTube,
		Name:           "Progressive Rate Session",
		CookieRef:      CookieRefPrefix + "session-progressive-rate",
		Enabled:        true,
		HealthStatus:   HealthHealthy,
	}
	storage := createTestStorage(t, "session-progressive-rate")
	repo := newMockRepo([]Session{s})
	cfg := DefaultPoolConfig(provider.FamilyYouTube)
	pool := NewSessionPool(cfg, storage, repo, nil)
	pool.SetNow(func() time.Time { return now })
	pool.SetSyncPersist(true)
	pool.ReloadSessions([]Session{s})

	expectedTiers := []time.Duration{
		1 * time.Minute,  // 1 failure
		2 * time.Minute,  // 2 failures
		5 * time.Minute,  // 3 failures
		15 * time.Minute, // 4 failures
		30 * time.Minute, // 5 failures
		1 * time.Hour,    // 6 failures
	}

	currTime := now
	rateErr := apperr.New(apperr.CodeSessionRateLimited, "rate limited")

	for i, expectedDur := range expectedTiers {
		pool.SetNow(func() time.Time { return currTime })
		lease, err := pool.Acquire(context.Background())
		if err != nil {
			t.Fatalf("failure %d Acquire: %v", i+1, err)
		}
		lease.Release(rateErr)

		up, _ := repo.GetSession(context.Background(), "session-progressive-rate")
		if up.ConsecutiveFailures != i+1 {
			t.Fatalf("failure %d: consecutive_failures = %d, want %d", i+1, up.ConsecutiveFailures, i+1)
		}
		expectedCooldown := currTime.Add(expectedDur)
		if up.CooldownUntil == nil || !up.CooldownUntil.Equal(expectedCooldown) {
			t.Fatalf("failure %d: cooldown = %v, want %v", i+1, up.CooldownUntil, expectedCooldown)
		}

		// Advance past cooldown to permit next probe lease
		currTime = expectedCooldown.Add(time.Second)
	}
}

// TestBotChallengeTier_7_DBAndPoolStateAgree verifies that pool in-memory state
// and repository database state are in complete agreement after bot challenge transitions.
func TestBotChallengeTier_7_DBAndPoolStateAgree(t *testing.T) {
	now := time.Date(2026, 9, 8, 12, 0, 0, 0, time.UTC)
	s := Session{
		ID:             "session-db-agree",
		ProviderFamily: provider.FamilyYouTube,
		Name:           "DB Agreement Session",
		CookieRef:      CookieRefPrefix + "session-db-agree",
		Enabled:        true,
		HealthStatus:   HealthHealthy,
	}
	storage := createTestStorage(t, "session-db-agree")
	repo := newMockRepo([]Session{s})
	cfg := DefaultPoolConfig(provider.FamilyYouTube)
	pool := NewSessionPool(cfg, storage, repo, nil)
	pool.SetNow(func() time.Time { return now })
	pool.SetSyncPersist(true)
	pool.ReloadSessions([]Session{s})

	// Step 1: Rate limit failure
	l1, _ := pool.Acquire(context.Background())
	l1.Release(apperr.New(apperr.CodeSessionRateLimited, "rate limit"))

	mem1 := pool.GetSession(s.ID).Session()
	db1, _ := repo.GetSession(context.Background(), s.ID)

	if mem1.HealthStatus != db1.HealthStatus || mem1.HealthStatus != HealthRateLimited {
		t.Fatalf("step 1 status mismatch: mem=%v, db=%v", mem1.HealthStatus, db1.HealthStatus)
	}
	if mem1.ConsecutiveFailures != db1.ConsecutiveFailures || mem1.ConsecutiveFailures != 1 {
		t.Fatalf("step 1 failures mismatch: mem=%d, db=%d", mem1.ConsecutiveFailures, db1.ConsecutiveFailures)
	}

	// Step 2: First bot challenge after rate limit
	t2 := now.Add(2 * time.Minute)
	pool.SetNow(func() time.Time { return t2 })

	l2, _ := pool.Acquire(context.Background())
	l2.Release(apperr.New(apperr.CodeSessionBotChallenge, "bot challenge"))

	mem2 := pool.GetSession(s.ID).Session()
	db2, _ := repo.GetSession(context.Background(), s.ID)

	if mem2.HealthStatus != db2.HealthStatus || mem2.HealthStatus != HealthBotChallenge {
		t.Fatalf("step 2 status mismatch: mem=%v, db=%v", mem2.HealthStatus, db2.HealthStatus)
	}
	if mem2.ConsecutiveFailures != db2.ConsecutiveFailures || mem2.ConsecutiveFailures != 2 {
		t.Fatalf("step 2 failures mismatch: mem=%d, db=%d", mem2.ConsecutiveFailures, db2.ConsecutiveFailures)
	}
	expected24h := t2.Add(24 * time.Hour)
	if !mem2.CooldownUntil.Equal(expected24h) || !db2.CooldownUntil.Equal(expected24h) {
		t.Fatalf("step 2 cooldown mismatch or wrong tier: mem=%v, db=%v, want %v", mem2.CooldownUntil, db2.CooldownUntil, expected24h)
	}

	// Step 3: Reload sessions from DB to simulate server restart
	newPool := NewSessionPool(cfg, storage, repo, nil)
	newPool.SetNow(func() time.Time { return t2.Add(24*time.Hour + time.Minute) })
	newPool.SetSyncPersist(true)
	dbSessions, _ := repo.ListSessions(context.Background(), Filter{})
	newPool.ReloadSessions(dbSessions)

	// Step 4: Second bot challenge after restart -> must get 72h
	t3 := t2.Add(24*time.Hour + time.Minute)
	l3, err := newPool.Acquire(context.Background())
	if err != nil {
		t.Fatalf("step 4 acquire after reload failed: %v", err)
	}
	l3.Release(apperr.New(apperr.CodeSessionBotChallenge, "repeat bot challenge"))

	mem3 := newPool.GetSession(s.ID).Session()
	db3, _ := repo.GetSession(context.Background(), s.ID)

	if mem3.HealthStatus != db3.HealthStatus || mem3.HealthStatus != HealthBotChallenge {
		t.Fatalf("step 4 status mismatch: mem=%v, db=%v", mem3.HealthStatus, db3.HealthStatus)
	}
	if mem3.ConsecutiveFailures != db3.ConsecutiveFailures || mem3.ConsecutiveFailures != 3 {
		t.Fatalf("step 4 failures mismatch: mem=%d, db=%d", mem3.ConsecutiveFailures, db3.ConsecutiveFailures)
	}
	expected72h := t3.Add(72 * time.Hour)
	if !mem3.CooldownUntil.Equal(expected72h) || !db3.CooldownUntil.Equal(expected72h) {
		t.Fatalf("step 4 cooldown mismatch or wrong tier: mem=%v, db=%v, want %v", mem3.CooldownUntil, db3.CooldownUntil, expected72h)
	}
}
