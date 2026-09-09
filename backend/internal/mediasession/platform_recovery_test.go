package mediasession_test

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"ytdm/backend/internal/apperr"
	"ytdm/backend/internal/mediasession"
)

// TestPlatformRecovery_1_PlatformFailureClearedOnRealMediaSuccess verifies that
// when family/platform cooldown is active and a session is already HealthHealthy,
// genuine successful media work (RecordOutcome, RecordSuccess, lease release)
// clears the active platform failure.
func TestPlatformRecovery_1_PlatformFailureClearedOnRealMediaSuccess(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 9, 8, 14, 0, 0, 0, time.UTC)

	t.Run("RecordOutcome clears platform failure on healthy session", func(t *testing.T) {
		svc, _, _, pool, _ := setupTestService(t)
		pool.SetNow(func() time.Time { return now })

		sess, err := svc.CreateSession(ctx, mediasession.CreateSessionRequest{Name: "Healthy Sess 1"})
		if err != nil {
			t.Fatalf("CreateSession: %v", err)
		}
		_, _, err = svc.UploadCookies(ctx, sess.ID, validNetscapeCookie("cookie-1"))
		if err != nil {
			t.Fatalf("UploadCookies: %v", err)
		}

		// Ensure session is HealthHealthy
		pool.RecordSuccess(ctx, sess.ID, now)

		// Set active platform-wide failure
		pool.RecordPlatformFailure(apperr.New(apperr.CodeProviderRateLimited, "IP blocked"), 10*time.Minute)
		if !pool.IsPlatformCooling() {
			t.Fatal("expected pool to be cooling after RecordPlatformFailure")
		}

		// Real media outcome: success (err == nil)
		pool.RecordOutcome(sess.ID, nil)

		if pool.IsPlatformCooling() {
			t.Error("pool is still cooling after real media success via RecordOutcome")
		}
		if _, ok := pool.LastPlatformFailure(); ok {
			t.Error("LastPlatformFailure should be cleared after real media success")
		}
		avail := pool.Availability()
		if avail.State != mediasession.PoolStateEligible {
			t.Errorf("expected pool state Eligible, got %v (%s)", avail.State, avail.Reason)
		}
	})

	t.Run("RecordSuccess clears platform failure on healthy session", func(t *testing.T) {
		svc, _, _, pool, _ := setupTestService(t)
		pool.SetNow(func() time.Time { return now })

		sess, err := svc.CreateSession(ctx, mediasession.CreateSessionRequest{Name: "Healthy Sess 2"})
		if err != nil {
			t.Fatalf("CreateSession: %v", err)
		}
		_, _, err = svc.UploadCookies(ctx, sess.ID, validNetscapeCookie("cookie-2"))
		if err != nil {
			t.Fatalf("UploadCookies: %v", err)
		}

		// Ensure session is HealthHealthy
		pool.RecordSuccess(ctx, sess.ID, now)

		// Set active platform failure
		pool.RecordPlatformFailure(apperr.New(apperr.CodeProviderRateLimited, "IP blocked"), 10*time.Minute)
		if !pool.IsPlatformCooling() {
			t.Fatal("expected pool to be cooling")
		}

		// Real media success via RecordSuccess
		pool.RecordSuccess(ctx, sess.ID, now)

		if pool.IsPlatformCooling() {
			t.Error("pool is still cooling after real media success via RecordSuccess")
		}
		if _, ok := pool.LastPlatformFailure(); ok {
			t.Error("LastPlatformFailure should be cleared after real media success")
		}
	})

	t.Run("Lease release nil clears platform failure on healthy session", func(t *testing.T) {
		svc, _, _, pool, _ := setupTestService(t)
		pool.SetNow(func() time.Time { return now })

		sess, err := svc.CreateSession(ctx, mediasession.CreateSessionRequest{Name: "Healthy Sess 3"})
		if err != nil {
			t.Fatalf("CreateSession: %v", err)
		}
		_, _, err = svc.UploadCookies(ctx, sess.ID, validNetscapeCookie("cookie-3"))
		if err != nil {
			t.Fatalf("UploadCookies: %v", err)
		}

		// Ensure session is HealthHealthy
		pool.RecordSuccess(ctx, sess.ID, now)

		// Acquire lease before platform failure occurs
		lease, err := pool.Acquire(ctx)
		if err != nil {
			t.Fatalf("Acquire: %v", err)
		}

		// Platform failure occurs while lease is in flight
		pool.RecordPlatformFailure(apperr.New(apperr.CodeProviderRateLimited, "IP blocked"), 10*time.Minute)
		if !pool.IsPlatformCooling() {
			t.Fatal("expected pool to be cooling")
		}

		// Lease completes successfully
		lease.Release(nil)

		if pool.IsPlatformCooling() {
			t.Error("pool is still cooling after lease release with nil error")
		}
		if _, ok := pool.LastPlatformFailure(); ok {
			t.Error("LastPlatformFailure should be cleared after lease release with nil error")
		}
	})
}

// TestPlatformRecovery_2_RecoveryHandlerFiredOnRealMediaSuccess verifies that
// genuine media success on an already HealthHealthy session fires the recovery callback
// if a platform failure was active.
func TestPlatformRecovery_2_RecoveryHandlerFiredOnRealMediaSuccess(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 9, 8, 14, 0, 0, 0, time.UTC)

	t.Run("RecordSuccess fires recoveryHandler when platform failure was active", func(t *testing.T) {
		svc, _, _, pool, _ := setupTestService(t)
		pool.SetNow(func() time.Time { return now })

		sess, err := svc.CreateSession(ctx, mediasession.CreateSessionRequest{Name: "Recovery Sess 1"})
		if err != nil {
			t.Fatalf("CreateSession: %v", err)
		}
		_, _, err = svc.UploadCookies(ctx, sess.ID, validNetscapeCookie("cookie-rec-1"))
		if err != nil {
			t.Fatalf("UploadCookies: %v", err)
		}

		// Make sure session is HealthHealthy first
		pool.RecordSuccess(ctx, sess.ID, now)

		var recoveryCalled atomic.Int32
		pool.SetRecoveryHandler(func(ctx context.Context) {
			recoveryCalled.Add(1)
		})

		// Mark platform failure
		pool.RecordPlatformFailure(apperr.New(apperr.CodeProviderRateLimited, "IP rate limit"), 15*time.Minute)

		// Media success on already healthy session
		pool.RecordSuccess(ctx, sess.ID, now)

		if count := recoveryCalled.Load(); count != 1 {
			t.Errorf("recoveryHandler was called %d times, want 1", count)
		}
	})

	t.Run("RecordOutcome fires recoveryHandler when platform failure was active", func(t *testing.T) {
		svc, _, _, pool, _ := setupTestService(t)
		pool.SetNow(func() time.Time { return now })

		sess, err := svc.CreateSession(ctx, mediasession.CreateSessionRequest{Name: "Recovery Sess 2"})
		if err != nil {
			t.Fatalf("CreateSession: %v", err)
		}
		_, _, err = svc.UploadCookies(ctx, sess.ID, validNetscapeCookie("cookie-rec-2"))
		if err != nil {
			t.Fatalf("UploadCookies: %v", err)
		}

		pool.RecordSuccess(ctx, sess.ID, now)

		var recoveryCalled atomic.Int32
		pool.SetRecoveryHandler(func(ctx context.Context) {
			recoveryCalled.Add(1)
		})

		pool.RecordPlatformFailure(apperr.New(apperr.CodeProviderRateLimited, "IP rate limit"), 15*time.Minute)

		pool.RecordOutcome(sess.ID, nil)

		if count := recoveryCalled.Load(); count != 1 {
			t.Errorf("recoveryHandler was called %d times, want 1", count)
		}
	})
}

// TestPlatformRecovery_3_WaitingSessionUnavailableWorkWoken verifies that
// waiting work blocked by SESSION_UNAVAILABLE is unblocked via the recovery callback.
func TestPlatformRecovery_3_WaitingSessionUnavailableWorkWoken(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 9, 8, 14, 0, 0, 0, time.UTC)

	svc, _, _, pool, _ := setupTestService(t)
	pool.SetNow(func() time.Time { return now })

	sess, err := svc.CreateSession(ctx, mediasession.CreateSessionRequest{Name: "Wait Sess"})
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	_, _, err = svc.UploadCookies(ctx, sess.ID, validNetscapeCookie("cookie-wait"))
	if err != nil {
		t.Fatalf("UploadCookies: %v", err)
	}

	pool.RecordSuccess(ctx, sess.ID, now)

	// Platform failure active
	pool.RecordPlatformFailure(apperr.New(apperr.CodeProviderRateLimited, "rate limited"), 10*time.Minute)

	// Attempting lease acquisition returns SESSION_UNAVAILABLE
	_, err = pool.Acquire(ctx)
	if err == nil {
		t.Fatal("expected error from Acquire during platform cooldown, got nil")
	}
	if apperr.CodeOf(err) != apperr.CodeSessionUnavailable {
		t.Fatalf("expected CodeSessionUnavailable, got %v", apperr.CodeOf(err))
	}

	// Channel to simulate waiting work being woken
	wokenCh := make(chan struct{}, 1)
	pool.SetRecoveryHandler(func(ctx context.Context) {
		select {
		case wokenCh <- struct{}{}:
		default:
		}
	})

	// Real media success occurs
	pool.RecordSuccess(ctx, sess.ID, now)

	// Verify the recovery callback woke the waiter
	select {
	case <-wokenCh:
		// Successfully woken
	case <-time.After(1 * time.Second):
		t.Fatal("timed out waiting for recoveryHandler to wake SESSION_UNAVAILABLE work")
	}

	// Verify lease acquisition now succeeds
	lease, err := pool.Acquire(ctx)
	if err != nil {
		t.Fatalf("Acquire after recovery failed: %v", err)
	}
	lease.Release(nil)
}

// TestPlatformRecovery_4_NoWakeStormOnSteadyStateHealthySuccess verifies that
// when a session is already HealthHealthy and NO platform failure is active,
// routine media successes do NOT fire the recovery callback.
func TestPlatformRecovery_4_NoWakeStormOnSteadyStateHealthySuccess(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 9, 8, 14, 0, 0, 0, time.UTC)

	svc, _, _, pool, _ := setupTestService(t)
	pool.SetNow(func() time.Time { return now })

	sess, err := svc.CreateSession(ctx, mediasession.CreateSessionRequest{Name: "Steady Sess"})
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	_, _, err = svc.UploadCookies(ctx, sess.ID, validNetscapeCookie("cookie-steady"))
	if err != nil {
		t.Fatalf("UploadCookies: %v", err)
	}

	// Make session healthy
	pool.RecordSuccess(ctx, sess.ID, now)

	var recoveryCalled atomic.Int32
	pool.SetRecoveryHandler(func(ctx context.Context) {
		recoveryCalled.Add(1)
	})

	// Perform multiple normal successes
	for i := 0; i < 5; i++ {
		pool.RecordSuccess(ctx, sess.ID, now)
		pool.RecordOutcome(sess.ID, nil)

		lease, err := pool.Acquire(ctx)
		if err != nil {
			t.Fatalf("Acquire: %v", err)
		}
		lease.Release(nil)
	}

	if count := recoveryCalled.Load(); count != 0 {
		t.Errorf("recoveryHandler was called %d times during steady-state healthy success, want 0 (wake storm prevented)", count)
	}
}

// TestPlatformRecovery_5_ProtectedSessionRealMediaSuccessRecoversSessionAndFiresCallback verifies
// that if a session was protected (e.g. HealthRateLimited or HealthBotChallenge) and real media
// success occurs, the session recovers to HealthHealthy, platform failure is cleared, and callback fires.
func TestPlatformRecovery_5_ProtectedSessionRealMediaSuccessRecoversSessionAndFiresCallback(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 9, 8, 14, 0, 0, 0, time.UTC)

	svc, repo, _, pool, _ := setupTestService(t)
	pool.SetNow(func() time.Time { return now })

	sess, err := svc.CreateSession(ctx, mediasession.CreateSessionRequest{Name: "Protected Sess"})
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	_, _, err = svc.UploadCookies(ctx, sess.ID, validNetscapeCookie("cookie-prot"))
	if err != nil {
		t.Fatalf("UploadCookies: %v", err)
	}

	// Set session to HealthRateLimited
	cd := now.Add(30 * time.Minute)
	_, _ = repo.UpdateHealth(ctx, sess.ID, mediasession.HealthUpdate{
		HealthStatus:        mediasession.HealthRateLimited,
		ConsecutiveFailures: 2,
		CooldownUntil:       &cd,
	})
	dbSess, _ := repo.GetSession(ctx, sess.ID)
	pool.UpsertSession(dbSess)

	// Also mark platform failure
	pool.RecordPlatformFailure(apperr.New(apperr.CodeProviderRateLimited, "platform rate limit"), 15*time.Minute)

	var recoveryCalled atomic.Int32
	pool.SetRecoveryHandler(func(ctx context.Context) {
		recoveryCalled.Add(1)
	})

	// Genuine media success
	pool.RecordSuccess(ctx, sess.ID, now)

	if count := recoveryCalled.Load(); count != 1 {
		t.Errorf("recoveryHandler was called %d times, want 1", count)
	}

	if pool.IsPlatformCooling() {
		t.Error("pool is still cooling after real media success")
	}

	updated, _ := repo.GetSession(ctx, sess.ID)
	if updated.HealthStatus != mediasession.HealthHealthy {
		t.Errorf("session health status = %v, want %v", updated.HealthStatus, mediasession.HealthHealthy)
	}
	if updated.ConsecutiveFailures != 0 {
		t.Errorf("consecutive failures = %d, want 0", updated.ConsecutiveFailures)
	}
	if updated.CooldownUntil != nil {
		t.Errorf("cooldown until = %v, want nil", updated.CooldownUntil)
	}
}

// TestPlatformRecovery_6_MetadataProbeSuccessDoesNotClearPlatformFailureOrWakeWaiters verifies
// that a successful metadata-only probe does NOT clear platformFailure and does NOT wake waiters.
func TestPlatformRecovery_6_MetadataProbeSuccessDoesNotClearPlatformFailureOrWakeWaiters(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 9, 8, 14, 0, 0, 0, time.UTC)

	svc, _, _, pool, prober := setupTestService(t)
	pool.SetNow(func() time.Time { return now })

	sess, err := svc.CreateSession(ctx, mediasession.CreateSessionRequest{Name: "Probe Sess"})
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	_, _, err = svc.UploadCookies(ctx, sess.ID, validNetscapeCookie("cookie-probe"))
	if err != nil {
		t.Fatalf("UploadCookies: %v", err)
	}

	// Healthy session
	pool.RecordSuccess(ctx, sess.ID, now)

	// Platform failure active
	pool.RecordPlatformFailure(apperr.New(apperr.CodeProviderRateLimited, "provider rate limit"), 30*time.Minute)

	var wakeCount atomic.Int32
	svc.SetRecoveryHandler(func(ctx context.Context) {
		wakeCount.Add(1)
	})

	// Configure prober for metadata SUCCESS
	prober.mu.Lock()
	prober.res = &mediasession.ProbeResult{
		Status:             mediasession.HealthHealthy,
		TestedAt:           now,
		MetadataOK:         true,
		UsableAudioFormats: true,
	}
	prober.err = nil
	prober.mu.Unlock()

	// Execute manual probe
	_, _, err = svc.ProbeSession(ctx, sess.ID)
	if err != nil {
		t.Fatalf("ProbeSession: %v", err)
	}

	// Assert recoveryHandler was NEVER called
	if count := wakeCount.Load(); count != 0 {
		t.Errorf("recoveryHandler was called %d times on metadata probe, want 0", count)
	}

	// Assert platform failure remains cooling
	if !pool.IsPlatformCooling() {
		t.Error("pool should still be cooling; metadata probe must NOT clear platform cooldown")
	}
	if _, ok := pool.LastPlatformFailure(); !ok {
		t.Error("LastPlatformFailure should still be present; metadata probe must NOT clear it")
	}
}

// TestPlatformRecovery_7_GenuineMediaSuccessResetsFailureCountAndCooldown verifies that
// genuine media success resets consecutive failures, cooldown, and failure reason.
func TestPlatformRecovery_7_GenuineMediaSuccessResetsFailureCountAndCooldown(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 9, 8, 14, 0, 0, 0, time.UTC)

	svc, repo, _, pool, _ := setupTestService(t)
	pool.SetNow(func() time.Time { return now })

	sess, err := svc.CreateSession(ctx, mediasession.CreateSessionRequest{Name: "Fail Sess"})
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	_, _, err = svc.UploadCookies(ctx, sess.ID, validNetscapeCookie("cookie-fail"))
	if err != nil {
		t.Fatalf("UploadCookies: %v", err)
	}

	// Put session into failed state with consecutive failures
	cd := now.Add(20 * time.Minute)
	_, _ = repo.UpdateHealth(ctx, sess.ID, mediasession.HealthUpdate{
		HealthStatus:        mediasession.HealthRateLimited,
		ConsecutiveFailures: 3,
		LastFailureReason:   "rate limit exceeded",
		CooldownUntil:       &cd,
	})
	dbSess, _ := repo.GetSession(ctx, sess.ID)
	pool.UpsertSession(dbSess)

	// Platform failure active
	pool.RecordPlatformFailure(apperr.New(apperr.CodeProviderRateLimited, "platform rate limit"), 15*time.Minute)

	// Real media success
	pool.RecordSuccess(ctx, sess.ID, now)

	updated, _ := repo.GetSession(ctx, sess.ID)
	if updated.HealthStatus != mediasession.HealthHealthy {
		t.Errorf("health_status = %v, want %v", updated.HealthStatus, mediasession.HealthHealthy)
	}
	if updated.ConsecutiveFailures != 0 {
		t.Errorf("consecutive_failures = %d, want 0", updated.ConsecutiveFailures)
	}
	if updated.CooldownUntil != nil {
		t.Errorf("cooldown_until = %v, want nil", updated.CooldownUntil)
	}
	if updated.LastFailureReason != "" {
		t.Errorf("last_failure_reason = %q, want empty", updated.LastFailureReason)
	}
	if updated.LastSuccessAt == nil || !updated.LastSuccessAt.Equal(now) {
		t.Errorf("last_success_at = %v, want %v", updated.LastSuccessAt, now)
	}
	if pool.IsPlatformCooling() {
		t.Error("pool should not be cooling after real media success")
	}
}
