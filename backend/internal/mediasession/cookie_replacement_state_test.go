package mediasession_test

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"ytdm/backend/internal/apperr"
	"ytdm/backend/internal/mediasession"
)

// TestCookieReplacement_1_PreservesHistoricalTimestampsAndReasons verifies that
// replacing cookies resets ConsecutiveFailures and CooldownUntil, transitions to
// HealthUnknown, but PRESERVES historical LastSuccessAt, LastFailureAt, and LastFailureReason.
func TestCookieReplacement_1_PreservesHistoricalTimestampsAndReasons(t *testing.T) {
	svc, repo, _, _, prober := setupTestService(t)
	ctx := context.Background()
	t0 := time.Date(2026, 9, 8, 10, 0, 0, 0, time.UTC)
	tSuccess := time.Date(2026, 9, 8, 11, 0, 0, 0, time.UTC)
	tFailure := time.Date(2026, 9, 8, 12, 0, 0, 0, time.UTC)
	tCooldown := tFailure.Add(24 * time.Hour)

	sess, err := svc.CreateSession(ctx, mediasession.CreateSessionRequest{Name: "History Preservation Test"})
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	_, _, err = svc.UploadCookies(ctx, sess.ID, validNetscapeCookie("initial-cookie"))
	if err != nil {
		t.Fatalf("initial UploadCookies: %v", err)
	}

	// Establish historical success and failure records
	failureReason := "[session_bot_challenge] Sign in to confirm you are not a bot"
	_, err = repo.UpdateHealth(ctx, sess.ID, mediasession.HealthUpdate{
		HealthStatus:        mediasession.HealthBotChallenge,
		ConsecutiveFailures: 2,
		LastUsedAt:          &tFailure,
		LastSuccessAt:       &tSuccess,
		LastFailureAt:       &tFailure,
		LastFailureReason:   failureReason,
		CooldownUntil:       &tCooldown,
	})
	if err != nil {
		t.Fatalf("UpdateHealth: %v", err)
	}

	// Configure prober for successful candidate probe
	prober.mu.Lock()
	prober.res = &mediasession.ProbeResult{
		Status:             mediasession.HealthHealthy,
		TestedAt:           t0,
		MetadataOK:         true,
		UsableAudioFormats: true,
	}
	prober.err = nil
	prober.mu.Unlock()

	// Perform cookie replacement
	view, probeRes, err := svc.UploadCookies(ctx, sess.ID, validNetscapeCookie("replacement-cookie-xyz"))
	if err != nil {
		t.Fatalf("replacement UploadCookies: %v", err)
	}
	if probeRes == nil || probeRes.Status != mediasession.HealthHealthy {
		t.Fatalf("expected candidate probe healthy, got %v", probeRes)
	}

	// Verification 1: Health status must be HealthUnknown (Step 2)
	if view.HealthStatus != mediasession.HealthUnknown {
		t.Errorf("view.HealthStatus = %q, want %q", view.HealthStatus, mediasession.HealthUnknown)
	}

	// Verification 2: CooldownUntil cleared to nil
	if view.CooldownUntil != nil {
		t.Errorf("view.CooldownUntil = %v, want nil", view.CooldownUntil)
	}

	// Verification 4: Historical LastSuccessAt MUST be preserved (NOT NULLed)
	if view.LastSuccessAt == nil {
		t.Errorf("view.LastSuccessAt was NULLed on cookie replacement, want %v", tSuccess)
	} else if !view.LastSuccessAt.Equal(tSuccess) {
		t.Errorf("view.LastSuccessAt = %v, want %v", view.LastSuccessAt, tSuccess)
	}

	// Verification 5: Historical LastFailureAt MUST be preserved (NOT NULLed)
	if view.LastFailureAt == nil {
		t.Errorf("view.LastFailureAt was NULLed on cookie replacement, want %v", tFailure)
	} else if !view.LastFailureAt.Equal(tFailure) {
		t.Errorf("view.LastFailureAt = %v, want %v", view.LastFailureAt, tFailure)
	}

	// Verification 5: Historical LastFailureReason MUST be preserved (sanitized in public view)
	expectedPublicReason := "Session encountered bot challenge"
	if view.LastFailureReason != expectedPublicReason {
		t.Errorf("view.LastFailureReason = %q, want %q", view.LastFailureReason, expectedPublicReason)
	}

	// Verification 7: Database record matches view
	dbSess, err := repo.GetSession(ctx, sess.ID)
	if err != nil {
		t.Fatalf("repo.GetSession: %v", err)
	}
	if dbSess.LastSuccessAt == nil || !dbSess.LastSuccessAt.Equal(tSuccess) {
		t.Errorf("dbSess.LastSuccessAt = %v, want %v", dbSess.LastSuccessAt, tSuccess)
	}
	if dbSess.LastFailureAt == nil || !dbSess.LastFailureAt.Equal(tFailure) {
		t.Errorf("dbSess.LastFailureAt = %v, want %v", dbSess.LastFailureAt, tFailure)
	}
	if dbSess.LastFailureReason != failureReason {
		t.Errorf("dbSess.LastFailureReason = %q, want %q", dbSess.LastFailureReason, failureReason)
	}
	if dbSess.HealthStatus != mediasession.HealthUnknown {
		t.Errorf("dbSess.HealthStatus = %q, want unknown", dbSess.HealthStatus)
	}
	if dbSess.ConsecutiveFailures != 0 {
		t.Errorf("dbSess.ConsecutiveFailures = %d, want 0", dbSess.ConsecutiveFailures)
	}
	if dbSess.CooldownUntil != nil {
		t.Errorf("dbSess.CooldownUntil = %v, want nil", dbSess.CooldownUntil)
	}
}

// TestCookieReplacement_2_DoesNotClearPlatformFailureImmediately verifies that
// replacing cookies while a family/platform cooldown is active does NOT clear platformFailure.
func TestCookieReplacement_2_DoesNotClearPlatformFailureImmediately(t *testing.T) {
	svc, _, _, pool, prober := setupTestService(t)
	ctx := context.Background()
	now := time.Date(2026, 9, 8, 12, 0, 0, 0, time.UTC)
	pool.SetNow(func() time.Time { return now })

	sess, err := svc.CreateSession(ctx, mediasession.CreateSessionRequest{Name: "Platform Cooldown Test"})
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	_, _, err = svc.UploadCookies(ctx, sess.ID, validNetscapeCookie("initial-plat-cookie"))
	if err != nil {
		t.Fatalf("UploadCookies: %v", err)
	}

	// Active platform failure
	pool.RecordPlatformFailure(apperr.New(apperr.CodeProviderRateLimited, "IP blocked"), 10*time.Minute)
	if !pool.IsPlatformCooling() {
		t.Fatal("expected pool to be cooling")
	}

	prober.mu.Lock()
	prober.res = &mediasession.ProbeResult{
		Status:             mediasession.HealthHealthy,
		TestedAt:           now,
		MetadataOK:         true,
		UsableAudioFormats: true,
	}
	prober.err = nil
	prober.mu.Unlock()

	// Replace cookies with candidate
	_, _, err = svc.UploadCookies(ctx, sess.ID, validNetscapeCookie("replaced-plat-cookie"))
	if err != nil {
		t.Fatalf("UploadCookies replacement: %v", err)
	}

	// Platform failure MUST NOT be cleared by cookie replacement alone!
	if !pool.IsPlatformCooling() {
		t.Error("cookie replacement prematurely cleared platformFailure; must remain cooling until real media success or cooldown expiry")
	}
	if _, ok := pool.LastPlatformFailure(); !ok {
		t.Error("LastPlatformFailure was cleared by cookie replacement")
	}
}

// TestCookieReplacement_3_NoWaiterWakeStormWhilePlatformCooling verifies that
// cookie replacement does not invoke the recovery handler (waking waiters) while
// the platform cooldown is actively cooling.
func TestCookieReplacement_3_NoWaiterWakeStormWhilePlatformCooling(t *testing.T) {
	svc, _, _, pool, prober := setupTestService(t)
	ctx := context.Background()
	now := time.Date(2026, 9, 8, 12, 0, 0, 0, time.UTC)
	pool.SetNow(func() time.Time { return now })

	var wakeCount atomic.Int32
	svc.SetRecoveryHandler(func(ctx context.Context) {
		wakeCount.Add(1)
	})

	sess, _ := svc.CreateSession(ctx, mediasession.CreateSessionRequest{Name: "Wake Storm Test"})
	_, _, _ = svc.UploadCookies(ctx, sess.ID, validNetscapeCookie("wake-init-cookie"))

	// Platform failure active
	pool.RecordPlatformFailure(apperr.New(apperr.CodeProviderRateLimited, "IP blocked"), 10*time.Minute)

	prober.mu.Lock()
	prober.res = &mediasession.ProbeResult{
		Status:             mediasession.HealthHealthy,
		TestedAt:           now,
		MetadataOK:         true,
		UsableAudioFormats: true,
	}
	prober.err = nil
	prober.mu.Unlock()

	// Replace cookies
	_, _, err := svc.UploadCookies(ctx, sess.ID, validNetscapeCookie("wake-replace-cookie"))
	if err != nil {
		t.Fatalf("UploadCookies: %v", err)
	}

	// Must NOT wake waiters while platform is cooling
	if count := wakeCount.Load(); count != 0 {
		t.Errorf("recovery handler was called %d times while platform cooling, want 0 (wake storm prevented)", count)
	}
}

// TestCookieReplacement_4_ControlledRevalidationLifecycle verifies that:
// 1. When platform cooldown expires, the HealthUnknown session is eligible for controlled revalidation (capLimit=1).
// 2. A second concurrent lease is rejected/queued.
// 3. Real media success clears platformFailure via F5, transitions to HealthHealthy, and wakes waiters.
func TestCookieReplacement_4_ControlledRevalidationLifecycle(t *testing.T) {
	svc, repo, _, pool, prober := setupTestService(t)
	ctx := context.Background()
	now := time.Date(2026, 9, 8, 12, 0, 0, 0, time.UTC)
	pool.SetNow(func() time.Time { return now })

	var wakeCount atomic.Int32
	svc.SetRecoveryHandler(func(ctx context.Context) {
		wakeCount.Add(1)
	})

	sess, _ := svc.CreateSession(ctx, mediasession.CreateSessionRequest{Name: "Revalidation Test"})
	_, _, _ = svc.UploadCookies(ctx, sess.ID, validNetscapeCookie("reval-init-cookie"))

	// Platform failure: 5 minutes cooldown
	pool.RecordPlatformFailure(apperr.New(apperr.CodeProviderRateLimited, "IP blocked"), 5*time.Minute)

	prober.mu.Lock()
	prober.res = &mediasession.ProbeResult{
		Status:             mediasession.HealthHealthy,
		TestedAt:           now,
		MetadataOK:         true,
		UsableAudioFormats: true,
	}
	prober.err = nil
	prober.mu.Unlock()

	// Replace cookies
	view, _, err := svc.UploadCookies(ctx, sess.ID, validNetscapeCookie("reval-replace-cookie"))
	if err != nil {
		t.Fatalf("UploadCookies: %v", err)
	}
	if view.HealthStatus != mediasession.HealthUnknown {
		t.Fatalf("expected HealthUnknown, got %v", view.HealthStatus)
	}

	// While cooling, Acquire returns SESSION_UNAVAILABLE
	_, err = pool.Acquire(ctx)
	if err == nil || apperr.CodeOf(err) != apperr.CodeSessionUnavailable {
		t.Fatalf("expected CodeSessionUnavailable during platform cooldown, got %v", err)
	}

	// Advance time past platform cooldown (5m + 1s)
	tAfterCooling := now.Add(5*time.Minute + time.Second)
	pool.SetNow(func() time.Time { return tAfterCooling })

	// Reset wake counter to verify real media recovery wake
	wakeCount.Store(0)

	// Controlled revalidation: single lease acquired
	l1, err := pool.Acquire(ctx)
	if err != nil {
		t.Fatalf("Acquire for controlled revalidation failed: %v", err)
	}

	// Second concurrent lease blocked by capLimit=1
	ctxShort, cancel := context.WithTimeout(ctx, 30*time.Millisecond)
	defer cancel()
	_, err2 := pool.Acquire(ctxShort)
	if err2 == nil {
		t.Fatal("expected second lease under HealthUnknown to be blocked, but succeeded")
	}

	// Real media success occurs!
	l1.Release(nil)

	// Session is now HealthHealthy
	dbSess, _ := repo.GetSession(ctx, sess.ID)
	if dbSess.HealthStatus != mediasession.HealthHealthy {
		t.Errorf("dbSess.HealthStatus = %v, want HealthHealthy", dbSess.HealthStatus)
	}

	// Platform failure cleared via F5
	if pool.IsPlatformCooling() {
		t.Error("pool is still cooling after real media success")
	}
	if _, ok := pool.LastPlatformFailure(); ok {
		t.Error("LastPlatformFailure should be cleared after real media success")
	}

	// Waiters woken via F5
	if count := wakeCount.Load(); count != 1 {
		t.Errorf("recovery handler called %d times after real media success, want 1", count)
	}
}

// TestCookieReplacement_5_FailedRevalidation_RestoresProtection verifies that
// if the controlled revalidation fails on real media acquisition (e.g. BOT_CHALLENGE),
// proper protection state is restored and the backlog is NOT released.
func TestCookieReplacement_5_FailedRevalidation_RestoresProtection(t *testing.T) {
	svc, repo, _, pool, prober := setupTestService(t)
	ctx := context.Background()
	now := time.Date(2026, 9, 8, 12, 0, 0, 0, time.UTC)
	pool.SetNow(func() time.Time { return now })

	sess, _ := svc.CreateSession(ctx, mediasession.CreateSessionRequest{Name: "Failed Reval Test"})
	_, _, _ = svc.UploadCookies(ctx, sess.ID, validNetscapeCookie("fail-reval-cookie"))

	prober.mu.Lock()
	prober.res = &mediasession.ProbeResult{
		Status:             mediasession.HealthHealthy,
		TestedAt:           now,
		MetadataOK:         true,
		UsableAudioFormats: true,
	}
	prober.err = nil
	prober.mu.Unlock()

	// Replace cookies -> HealthUnknown
	view, _, err := svc.UploadCookies(ctx, sess.ID, validNetscapeCookie("fail-replace-cookie"))
	if err != nil {
		t.Fatalf("UploadCookies: %v", err)
	}
	if view.HealthStatus != mediasession.HealthUnknown {
		t.Fatalf("expected HealthUnknown, got %v", view.HealthStatus)
	}

	// Controlled revalidation runs
	l1, err := pool.Acquire(ctx)
	if err != nil {
		t.Fatalf("Acquire: %v", err)
	}

	// Real media operation encounters BOT_CHALLENGE
	botErr := apperr.New(apperr.CodeSessionBotChallenge, "Sign in to confirm you are not a bot")
	l1.Release(botErr)

	// Session transitions to HealthBotChallenge with 24h cooldown
	dbSess, _ := repo.GetSession(ctx, sess.ID)
	if dbSess.HealthStatus != mediasession.HealthBotChallenge {
		t.Errorf("dbSess.HealthStatus = %v, want HealthBotChallenge", dbSess.HealthStatus)
	}
	if dbSess.CooldownUntil == nil {
		t.Fatal("expected CooldownUntil set, got nil")
	}
	expected24h := now.Add(24 * time.Hour)
	if !dbSess.CooldownUntil.Equal(expected24h) {
		t.Errorf("CooldownUntil = %v, want %v", dbSess.CooldownUntil, expected24h)
	}

	// Backlog is blocked by cooldown
	_, err = pool.Acquire(ctx)
	if err == nil || apperr.CodeOf(err) != apperr.CodeSessionUnavailable {
		t.Errorf("expected CodeSessionUnavailable while session in bot challenge cooldown, got %v", err)
	}
}
