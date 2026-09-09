package mediasession_test

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"ytdm/backend/internal/apperr"
	"ytdm/backend/internal/mediasession"
	"ytdm/backend/internal/provider"
)

// TestProbeHealth_1_BotChallengePreservedAfterMetadataProbeSuccess verifies that
// a successful metadata probe against a BOT_CHALLENGE protected session does NOT
// clear the challenge, does NOT clear cooldown, does NOT clear failures, and does NOT set HEALTHY.
func TestProbeHealth_1_BotChallengePreservedAfterMetadataProbeSuccess(t *testing.T) {
	svc, repo, _, pool, prober := setupTestService(t)
	ctx := context.Background()
	now := time.Date(2026, 9, 8, 12, 0, 0, 0, time.UTC)
	pool.SetNow(func() time.Time { return now })

	sess, err := svc.CreateSession(ctx, mediasession.CreateSessionRequest{Name: "Bot Session"})
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	_, _, err = svc.UploadCookies(ctx, sess.ID, validNetscapeCookie("bot-cookie"))
	if err != nil {
		t.Fatalf("UploadCookies: %v", err)
	}

	// Simulate BOT_CHALLENGE on real media operation
	cooldownUntil := now.Add(24 * time.Hour)
	_, err = repo.UpdateHealth(ctx, sess.ID, mediasession.HealthUpdate{
		HealthStatus:        mediasession.HealthBotChallenge,
		ConsecutiveFailures: 1,
		LastFailureReason:   "Sign in to confirm you're not a bot",
		CooldownUntil:       &cooldownUntil,
	})
	if err != nil {
		t.Fatalf("UpdateHealth: %v", err)
	}
	updatedSess, _ := repo.GetSession(ctx, sess.ID)
	pool.UpsertSession(updatedSess)

	// Configure prober to report metadata probe SUCCESS
	prober.mu.Lock()
	prober.res = &mediasession.ProbeResult{
		Status:             mediasession.HealthHealthy,
		TestedAt:           now,
		MetadataOK:         true,
		UsableAudioFormats: true,
	}
	prober.err = nil
	prober.mu.Unlock()

	// Execute manual metadata probe
	res, view, err := svc.ProbeSession(ctx, sess.ID)
	if err != nil {
		t.Fatalf("ProbeSession failed: %v", err)
	}

	// Probe result itself confirms metadata was reachable
	if res.Status != mediasession.HealthHealthy || !res.MetadataOK || !res.UsableAudioFormats {
		t.Fatalf("expected probe result healthy, got status=%s, metadata_ok=%v", res.Status, res.MetadataOK)
	}

	// CRITICAL: Session view must NOT be falsely marked healthy!
	if view.HealthStatus != mediasession.HealthBotChallenge {
		t.Errorf("session view health = %s, want bot_challenge (protection must not be cleared by metadata probe)", view.HealthStatus)
	}
	if view.CooldownUntil == nil || !view.CooldownUntil.Equal(cooldownUntil) {
		t.Errorf("cooldown until = %v, want %v", view.CooldownUntil, cooldownUntil)
	}

	// Check DB persistence
	dbSess, _ := repo.GetSession(ctx, sess.ID)
	if dbSess.HealthStatus != mediasession.HealthBotChallenge {
		t.Errorf("db health status = %s, want bot_challenge", dbSess.HealthStatus)
	}
	if dbSess.ConsecutiveFailures != 1 {
		t.Errorf("consecutive failures = %d, want 1", dbSess.ConsecutiveFailures)
	}
	if dbSess.CooldownUntil == nil || !dbSess.CooldownUntil.Equal(cooldownUntil) {
		t.Errorf("db cooldown until = %v, want %v", dbSess.CooldownUntil, cooldownUntil)
	}

	// Check runtime pool: session must remain in cooldown (cannot be leased)
	_, leaseErr := pool.Acquire(ctx)
	if leaseErr == nil {
		t.Fatalf("expected pool.Acquire to fail with SESSION_UNAVAILABLE while session is cooling")
	}
	if apperr.CodeOf(leaseErr) != apperr.CodeSessionUnavailable {
		t.Errorf("pool.Acquire error code = %s, want SESSION_UNAVAILABLE", apperr.CodeOf(leaseErr))
	}
}

// TestProbeHealth_2_RateLimitedPreservedAfterMetadataProbeSuccess verifies that
// a successful metadata probe against a RATE_LIMITED protected session does NOT
// clear the rate limit or its cooldown.
func TestProbeHealth_2_RateLimitedPreservedAfterMetadataProbeSuccess(t *testing.T) {
	svc, repo, _, pool, prober := setupTestService(t)
	ctx := context.Background()
	now := time.Date(2026, 9, 8, 12, 0, 0, 0, time.UTC)
	pool.SetNow(func() time.Time { return now })

	sess, err := svc.CreateSession(ctx, mediasession.CreateSessionRequest{Name: "RateLimited Session"})
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	_, _, _ = svc.UploadCookies(ctx, sess.ID, validNetscapeCookie("rl-cookie"))

	cooldownUntil := now.Add(5 * time.Minute)
	_, _ = repo.UpdateHealth(ctx, sess.ID, mediasession.HealthUpdate{
		HealthStatus:        mediasession.HealthRateLimited,
		ConsecutiveFailures: 3,
		LastFailureReason:   "HTTP 429 Too Many Requests",
		CooldownUntil:       &cooldownUntil,
	})
	dbSess, _ := repo.GetSession(ctx, sess.ID)
	pool.UpsertSession(dbSess)

	// Prober returns metadata success
	prober.mu.Lock()
	prober.res = &mediasession.ProbeResult{
		Status:             mediasession.HealthHealthy,
		TestedAt:           now,
		MetadataOK:         true,
		UsableAudioFormats: true,
	}
	prober.err = nil
	prober.mu.Unlock()

	res, view, err := svc.ProbeSession(ctx, sess.ID)
	if err != nil {
		t.Fatalf("ProbeSession: %v", err)
	}
	if !res.MetadataOK {
		t.Fatalf("expected metadata OK")
	}
	if view.HealthStatus != mediasession.HealthRateLimited {
		t.Errorf("view health = %s, want rate_limited", view.HealthStatus)
	}
	if view.CooldownUntil == nil || !view.CooldownUntil.Equal(cooldownUntil) {
		t.Errorf("cooldown until = %v, want %v", view.CooldownUntil, cooldownUntil)
	}

	saved, _ := repo.GetSession(ctx, sess.ID)
	if saved.HealthStatus != mediasession.HealthRateLimited {
		t.Errorf("db health status = %s, want rate_limited", saved.HealthStatus)
	}
	if saved.ConsecutiveFailures != 3 {
		t.Errorf("consecutive failures = %d, want 3", saved.ConsecutiveFailures)
	}
}

// TestProbeHealth_3_AuthFailedPreservedAfterMetadataProbeSuccess verifies that
// a successful metadata probe against an AUTH_FAILED session does NOT clear AUTH_FAILED.
func TestProbeHealth_3_AuthFailedPreservedAfterMetadataProbeSuccess(t *testing.T) {
	svc, repo, _, pool, prober := setupTestService(t)
	ctx := context.Background()
	now := time.Now().UTC()

	sess, err := svc.CreateSession(ctx, mediasession.CreateSessionRequest{Name: "AuthFailed Session"})
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	_, _, _ = svc.UploadCookies(ctx, sess.ID, validNetscapeCookie("af-cookie"))

	_, _ = repo.UpdateHealth(ctx, sess.ID, mediasession.HealthUpdate{
		HealthStatus:        mediasession.HealthAuthFailed,
		ConsecutiveFailures: 1,
		LastFailureReason:   "Sign in to access this content",
	})
	dbSess, _ := repo.GetSession(ctx, sess.ID)
	pool.UpsertSession(dbSess)

	prober.mu.Lock()
	prober.res = &mediasession.ProbeResult{
		Status:             mediasession.HealthHealthy,
		TestedAt:           now,
		MetadataOK:         true,
		UsableAudioFormats: true,
	}
	prober.err = nil
	prober.mu.Unlock()

	_, view, err := svc.ProbeSession(ctx, sess.ID)
	if err != nil {
		t.Fatalf("ProbeSession: %v", err)
	}
	if view.HealthStatus != mediasession.HealthAuthFailed {
		t.Errorf("view health = %s, want auth_failed", view.HealthStatus)
	}

	saved, _ := repo.GetSession(ctx, sess.ID)
	if saved.HealthStatus != mediasession.HealthAuthFailed {
		t.Errorf("db health = %s, want auth_failed", saved.HealthStatus)
	}
}

// TestProbeHealth_4_MetadataProbeDoesNotWakeWaiters verifies that
// a successful metadata probe does NOT trigger the session recovery handler
// (does not wake media waiters).
func TestProbeHealth_4_MetadataProbeDoesNotWakeWaiters(t *testing.T) {
	svc, repo, _, pool, prober := setupTestService(t)
	ctx := context.Background()
	now := time.Now().UTC()

	var wakeCount atomic.Int32
	svc.SetRecoveryHandler(func(ctx context.Context) {
		wakeCount.Add(1)
	})

	sess, _ := svc.CreateSession(ctx, mediasession.CreateSessionRequest{Name: "Wake Test"})
	_, _, _ = svc.UploadCookies(ctx, sess.ID, validNetscapeCookie("wake-cookie"))

	cd := now.Add(24 * time.Hour)
	_, _ = repo.UpdateHealth(ctx, sess.ID, mediasession.HealthUpdate{
		HealthStatus:        mediasession.HealthBotChallenge,
		ConsecutiveFailures: 1,
		CooldownUntil:       &cd,
	})
	dbSess, _ := repo.GetSession(ctx, sess.ID)
	pool.UpsertSession(dbSess)

	prober.mu.Lock()
	prober.res = &mediasession.ProbeResult{
		Status:             mediasession.HealthHealthy,
		TestedAt:           now,
		MetadataOK:         true,
		UsableAudioFormats: true,
	}
	prober.err = nil
	prober.mu.Unlock()

	// Run metadata probe
	_, _, err := svc.ProbeSession(ctx, sess.ID)
	if err != nil {
		t.Fatalf("ProbeSession: %v", err)
	}

	// Assert recoveryHandler was NEVER called
	if count := wakeCount.Load(); count != 0 {
		t.Errorf("recoveryHandler was called %d times on metadata probe, want 0 (metadata probe must NOT wake media waiters)", count)
	}
}

// TestProbeHealth_5_MetadataProbeResultObservable verifies that the ProbeResult
// returned to the caller accurately reflects probe findings.
func TestProbeHealth_5_MetadataProbeResultObservable(t *testing.T) {
	svc, repo, _, pool, prober := setupTestService(t)
	ctx := context.Background()
	now := time.Now().UTC()

	sess, _ := svc.CreateSession(ctx, mediasession.CreateSessionRequest{Name: "Observable Test"})
	_, _, _ = svc.UploadCookies(ctx, sess.ID, validNetscapeCookie("obs-cookie"))

	cd := now.Add(24 * time.Hour)
	_, _ = repo.UpdateHealth(ctx, sess.ID, mediasession.HealthUpdate{
		HealthStatus:        mediasession.HealthBotChallenge,
		ConsecutiveFailures: 1,
		CooldownUntil:       &cd,
	})
	dbSess, _ := repo.GetSession(ctx, sess.ID)
	pool.UpsertSession(dbSess)

	prober.mu.Lock()
	prober.res = &mediasession.ProbeResult{
		Status:             mediasession.HealthHealthy,
		TestedAt:           now,
		MetadataOK:         true,
		UsableAudioFormats: true,
	}
	prober.err = nil
	prober.mu.Unlock()

	res, view, err := svc.ProbeSession(ctx, sess.ID)
	if err != nil {
		t.Fatalf("ProbeSession: %v", err)
	}

	// Probe result must be observable and accurate
	if res == nil {
		t.Fatalf("expected non-nil ProbeResult")
	}
	if !res.MetadataOK {
		t.Errorf("expected MetadataOK = true")
	}
	if !res.UsableAudioFormats {
		t.Errorf("expected UsableAudioFormats = true")
	}
	if res.Status != mediasession.HealthHealthy {
		t.Errorf("expected probe result status = healthy, got %s", res.Status)
	}

	// Session view returned must also be valid and show preserved protection
	if view == nil {
		t.Fatalf("expected non-nil SessionView")
	}
	if view.HealthStatus != mediasession.HealthBotChallenge {
		t.Errorf("view HealthStatus = %s, want bot_challenge", view.HealthStatus)
	}
}

// TestProbeHealth_6_RealMediaSuccessRestoresLegitimateEligibility verifies that
// real media extraction success (lease.Release(nil) / pool.RecordOutcome(nil))
// restores full HealthHealthy eligibility and clears failures/cooldown.
func TestProbeHealth_6_RealMediaSuccessRestoresLegitimateEligibility(t *testing.T) {
	svc, repo, _, pool, _ := setupTestService(t)
	ctx := context.Background()
	now := time.Date(2026, 9, 8, 12, 0, 0, 0, time.UTC)
	pool.SetNow(func() time.Time { return now })

	sess, _ := svc.CreateSession(ctx, mediasession.CreateSessionRequest{Name: "Real Media Test"})
	_, _, _ = svc.UploadCookies(ctx, sess.ID, validNetscapeCookie("real-cookie"))

	// Session in BOT_CHALLENGE cooldown
	cd := now.Add(24 * time.Hour)
	_, _ = repo.UpdateHealth(ctx, sess.ID, mediasession.HealthUpdate{
		HealthStatus:        mediasession.HealthBotChallenge,
		ConsecutiveFailures: 1,
		CooldownUntil:       &cd,
	})
	dbSess, _ := repo.GetSession(ctx, sess.ID)
	pool.UpsertSession(dbSess)

	// Advance time past cooldown to allow probe lease
	pool.SetNow(func() time.Time { return now.Add(24*time.Hour + time.Minute) })

	// Acquire single probe lease
	lease, err := pool.Acquire(ctx)
	if err != nil {
		t.Fatalf("expected probe lease after cooldown expired: %v", err)
	}

	// Real media operation succeeds!
	lease.Release(nil)

	// Session must now be fully HEALTHY
	updated, _ := repo.GetSession(ctx, sess.ID)
	if updated.HealthStatus != mediasession.HealthHealthy {
		t.Errorf("health status after real media success = %s, want healthy", updated.HealthStatus)
	}
	if updated.ConsecutiveFailures != 0 {
		t.Errorf("consecutive failures = %d, want 0", updated.ConsecutiveFailures)
	}
	if updated.CooldownUntil != nil {
		t.Errorf("cooldown until = %v, want nil", updated.CooldownUntil)
	}

	// Session can now accept full concurrent leases up to MaxLeasesPerSession (2)
	l1, err1 := pool.Acquire(ctx)
	l2, err2 := pool.Acquire(ctx)
	if err1 != nil || err2 != nil {
		t.Fatalf("expected 2 concurrent leases on healthy session, got err1=%v, err2=%v", err1, err2)
	}
	l1.Release(nil)
	l2.Release(nil)
}

// TestProbeHealth_7_RealRecoveryWakesApplicableWaiters verifies that
// real media recovery (transitioning non-healthy/unknown to healthy) triggers
// the recovery handler to wake applicable session waiters.
func TestProbeHealth_7_RealRecoveryWakesApplicableWaiters(t *testing.T) {
	svc, repo, _, pool, _ := setupTestService(t)
	ctx := context.Background()
	now := time.Date(2026, 9, 8, 12, 0, 0, 0, time.UTC)
	pool.SetNow(func() time.Time { return now })

	var wakeCount atomic.Int32
	svc.SetRecoveryHandler(func(ctx context.Context) {
		wakeCount.Add(1)
	})

	sess, _ := svc.CreateSession(ctx, mediasession.CreateSessionRequest{Name: "Wake Recovery Test"})
	_, _, _ = svc.UploadCookies(ctx, sess.ID, validNetscapeCookie("wake-recovery"))

	// Set session to BOT_CHALLENGE
	cd := now.Add(24 * time.Hour)
	_, _ = repo.UpdateHealth(ctx, sess.ID, mediasession.HealthUpdate{
		HealthStatus:        mediasession.HealthBotChallenge,
		ConsecutiveFailures: 1,
		CooldownUntil:       &cd,
	})
	dbSess, _ := repo.GetSession(ctx, sess.ID)
	pool.UpsertSession(dbSess)

	// Real media operation succeeds via RecordOutcome
	pool.RecordOutcome(sess.ID, nil)

	// Recovery handler must have been called!
	if count := wakeCount.Load(); count != 1 {
		t.Errorf("recovery handler called %d times, want 1", count)
	}

	// Subsequent real media operation on already healthy session does NOT spam recovery handler
	pool.RecordOutcome(sess.ID, nil)
	if count := wakeCount.Load(); count != 1 {
		t.Errorf("recovery handler called %d times after subsequent healthy operation, want 1", count)
	}
}

// TestProbeHealth_8_CookieReplacementTransitionsToUnknown verifies that
// managed cookie replacement does NOT declare HealthHealthy, but transitions
// to HealthUnknown with single concurrency probe cap and cleared cooldown.
func TestProbeHealth_8_CookieReplacementTransitionsToUnknown(t *testing.T) {
	svc, repo, _, pool, prober := setupTestService(t)
	ctx := context.Background()
	now := time.Now().UTC()

	var wakeCount atomic.Int32
	svc.SetRecoveryHandler(func(ctx context.Context) {
		wakeCount.Add(1)
	})

	sess, _ := svc.CreateSession(ctx, mediasession.CreateSessionRequest{Name: "Replace Semantics Test"})
	_, _, _ = svc.UploadCookies(ctx, sess.ID, validNetscapeCookie("initial-cookie"))

	// Session gets BOT_CHALLENGE with 2 failures and 72h cooldown
	cd := now.Add(72 * time.Hour)
	_, _ = repo.UpdateHealth(ctx, sess.ID, mediasession.HealthUpdate{
		HealthStatus:        mediasession.HealthBotChallenge,
		ConsecutiveFailures: 2,
		CooldownUntil:       &cd,
	})
	dbSess, _ := repo.GetSession(ctx, sess.ID)
	pool.UpsertSession(dbSess)

	// Admin uploads new replacement cookies
	prober.mu.Lock()
	prober.res = &mediasession.ProbeResult{
		Status:             mediasession.HealthHealthy,
		TestedAt:           now,
		MetadataOK:         true,
		UsableAudioFormats: true,
	}
	prober.err = nil
	prober.mu.Unlock()

	view, probeRes, err := svc.UploadCookies(ctx, sess.ID, validNetscapeCookie("fresh-cookie-12345"))
	if err != nil {
		t.Fatalf("UploadCookies: %v", err)
	}
	if probeRes.Status != mediasession.HealthHealthy {
		t.Fatalf("expected candidate probe healthy")
	}

	// CRITICAL: Must be HealthUnknown, NOT HealthHealthy!
	if view.HealthStatus != mediasession.HealthUnknown {
		t.Errorf("view health = %s, want unknown (replacement must NOT claim full media health before real media operation)", view.HealthStatus)
	}
	if view.CooldownUntil != nil {
		t.Errorf("cooldown until = %v, want nil (cleared on replacement)", view.CooldownUntil)
	}
	dbSessAfter, _ := repo.GetSession(ctx, sess.ID)
	if dbSessAfter.ConsecutiveFailures != 0 {
		t.Errorf("consecutive failures = %d, want 0 (reset on replacement)", dbSessAfter.ConsecutiveFailures)
	}

	// Waiters were woken so one item can test the new credential
	if count := wakeCount.Load(); count != 1 {
		t.Errorf("wakeCount = %d, want 1", count)
	}

	// Under HealthUnknown, session strictly enforces single concurrency probe cap
	l1, err := pool.Acquire(ctx)
	if err != nil {
		t.Fatalf("first lease under HealthUnknown failed: %v", err)
	}

	// Second concurrent lease MUST be rejected or queued (capLimit = 1)
	// Because other sessions are absent, it will return SESSION_UNAVAILABLE
	ctxTimeout, cancel := context.WithTimeout(ctx, 50*time.Millisecond)
	defer cancel()
	_, err2 := pool.Acquire(ctxTimeout)
	if err2 == nil {
		t.Errorf("expected second concurrent lease under HealthUnknown to be blocked, but succeeded")
	}

	// Real media operation completes successfully on the lease
	l1.Release(nil)

	// NOW the session is confirmed HEALTHY!
	healthySess, _ := repo.GetSession(ctx, sess.ID)
	if healthySess.HealthStatus != mediasession.HealthHealthy {
		t.Errorf("health status after real media success = %s, want healthy", healthySess.HealthStatus)
	}
}

// TestProbeHealth_9_Step1RepeatBotChallengeCooldownPreserved verifies that
// the Step 1 repeat BOT_CHALLENGE 72h finite cooldown logic remains completely preserved.
func TestProbeHealth_9_Step1RepeatBotChallengeCooldownPreserved(t *testing.T) {
	now := time.Date(2026, 9, 8, 12, 0, 0, 0, time.UTC)
	storage, err := mediasession.NewCookieStorage(t.TempDir(), nil)
	if err != nil {
		t.Fatalf("NewCookieStorage: %v", err)
	}
	cookieRef, err := storage.Store("session-step1-check", validNetscapeCookie("step1-secret"))
	if err != nil {
		t.Fatalf("storage.Store: %v", err)
	}
	s := mediasession.Session{
		ID:             "session-step1-check",
		ProviderFamily: provider.FamilyYouTube,
		Name:           "Step 1 Check Session",
		CookieRef:      cookieRef,
		Enabled:        true,
		HealthStatus:   mediasession.HealthHealthy,
	}
	repo := newMockRepo()
	_ = repo.CreateSession(context.Background(), &s)
	cfg := mediasession.DefaultPoolConfig(provider.FamilyYouTube)
	pool := mediasession.NewSessionPool(cfg, storage, repo, nil)
	pool.SetNow(func() time.Time { return now })
	pool.SetSyncPersist(true)
	pool.ReloadSessions([]mediasession.Session{s})

	botErr := apperr.New(apperr.CodeSessionBotChallenge, "Sign in to confirm you're not a bot")

	// First failure: 24h
	l1, err := pool.Acquire(context.Background())
	if err != nil {
		t.Fatalf("first Acquire: %v", err)
	}
	l1.Release(botErr)

	up1, _ := repo.GetSession(context.Background(), "session-step1-check")
	if up1.CooldownUntil == nil || !up1.CooldownUntil.Equal(now.Add(24*time.Hour)) {
		t.Errorf("first cooldown = %v, want 24h", up1.CooldownUntil)
	}

	// Advance time past 24h
	probeTime := now.Add(24*time.Hour + time.Minute)
	pool.SetNow(func() time.Time { return probeTime })

	// Second failure: 72h finite (NOT nil)
	l2, err := pool.Acquire(context.Background())
	if err != nil {
		t.Fatalf("second Acquire: %v", err)
	}
	l2.Release(botErr)

	up2, _ := repo.GetSession(context.Background(), "session-step1-check")
	if up2.ConsecutiveFailures != 2 {
		t.Errorf("second failures = %d, want 2", up2.ConsecutiveFailures)
	}
	if up2.CooldownUntil == nil {
		t.Fatalf("second cooldown is nil, want finite 72h!")
	}
	expected72h := probeTime.Add(72 * time.Hour)
	if !up2.CooldownUntil.Equal(expected72h) {
		t.Errorf("second cooldown = %v, want %v", up2.CooldownUntil, expected72h)
	}
}

// TestProbeHealth_10_NoBusyLoopOnMetadataProbeSuccess verifies that
// repeatedly probing a protected session does NOT cause a waiter wake loop.
func TestProbeHealth_10_NoBusyLoopOnMetadataProbeSuccess(t *testing.T) {
	svc, repo, _, pool, prober := setupTestService(t)
	ctx := context.Background()
	now := time.Now().UTC()

	var wakeCount atomic.Int32
	svc.SetRecoveryHandler(func(ctx context.Context) {
		wakeCount.Add(1)
	})

	sess, _ := svc.CreateSession(ctx, mediasession.CreateSessionRequest{Name: "Loop Test"})
	_, _, _ = svc.UploadCookies(ctx, sess.ID, validNetscapeCookie("loop-cookie"))

	cd := now.Add(24 * time.Hour)
	_, _ = repo.UpdateHealth(ctx, sess.ID, mediasession.HealthUpdate{
		HealthStatus:        mediasession.HealthBotChallenge,
		ConsecutiveFailures: 1,
		CooldownUntil:       &cd,
	})
	dbSess, _ := repo.GetSession(ctx, sess.ID)
	pool.UpsertSession(dbSess)

	prober.mu.Lock()
	prober.res = &mediasession.ProbeResult{
		Status:             mediasession.HealthHealthy,
		TestedAt:           now,
		MetadataOK:         true,
		UsableAudioFormats: true,
	}
	prober.err = nil
	prober.mu.Unlock()

	// Perform multiple probes (simulating repeated clicks / debounce intervals)
	for i := 0; i < 3; i++ {
		// Advance debounce clock
		time.Sleep(2100 * time.Millisecond)
		_, view, err := svc.ProbeSession(ctx, sess.ID)
		if err != nil {
			t.Fatalf("probe %d failed: %v", i, err)
		}
		if view.HealthStatus != mediasession.HealthBotChallenge {
			t.Fatalf("probe %d: health status changed to %s, want bot_challenge", i, view.HealthStatus)
		}
	}

	// Wake count must remain ZERO across all probes
	if count := wakeCount.Load(); count != 0 {
		t.Errorf("wakeCount = %d, want 0 across repeated probes", count)
	}
}
