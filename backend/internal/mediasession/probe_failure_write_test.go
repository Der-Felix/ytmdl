package mediasession_test

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"ytdm/backend/internal/apperr"
	"ytdm/backend/internal/mediasession"
)

// TestProbeFailure_1_BotChallenge_FirstOccurrence verifies that a first-time BOT_CHALLENGE
// probe failure results in exactly one UpdateHealth write, sets a finite 24h cooldown,
// increments failures from 0 to 1, and synchronizes in-memory pool and database.
func TestProbeFailure_1_BotChallenge_FirstOccurrence(t *testing.T) {
	svc, repo, _, pool, prober := setupTestService(t)
	ctx := context.Background()
	now := time.Date(2026, 9, 8, 12, 0, 0, 0, time.UTC)
	pool.SetNow(func() time.Time { return now })

	sess, err := svc.CreateSession(ctx, mediasession.CreateSessionRequest{Name: "Bot Session 1"})
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	_, _, err = svc.UploadCookies(ctx, sess.ID, validNetscapeCookie("bot-cookie-1"))
	if err != nil {
		t.Fatalf("UploadCookies: %v", err)
	}

	prober.mu.Lock()
	prober.res = &mediasession.ProbeResult{
		Status:          mediasession.HealthBotChallenge,
		TestedAt:        now,
		FailureCategory: "SESSION_BOT_CHALLENGE",
	}
	prober.err = apperr.New(apperr.CodeSessionBotChallenge, "Sign in to confirm you're not a bot")
	prober.mu.Unlock()

	repo.ResetHealthUpdates()

	res, view, err := svc.ProbeSession(ctx, sess.ID)
	if err != nil {
		t.Fatalf("ProbeSession unexpected error: %v", err)
	}
	if res == nil || res.Status != mediasession.HealthBotChallenge {
		t.Fatalf("expected probe result status bot_challenge, got %v", res)
	}

	// Requirement 6: exactly ONE health write (no second competing write)
	updates := repo.GetHealthUpdates()
	if len(updates) != 1 {
		t.Fatalf("expected exactly 1 UpdateHealth call, got %d", len(updates))
	}

	expected24h := now.Add(24 * time.Hour)

	// In-memory RuntimeSession assertions
	rs := pool.GetSession(sess.ID)
	if rs == nil {
		t.Fatalf("pool session nil")
	}
	poolSess := rs.Session()
	if poolSess.HealthStatus != mediasession.HealthBotChallenge {
		t.Errorf("pool HealthStatus = %s, want bot_challenge", poolSess.HealthStatus)
	}
	if poolSess.ConsecutiveFailures != 1 {
		t.Errorf("pool ConsecutiveFailures = %d, want 1", poolSess.ConsecutiveFailures)
	}
	if poolSess.CooldownUntil == nil || !poolSess.CooldownUntil.Equal(expected24h) {
		t.Errorf("pool CooldownUntil = %v, want 24h (%v)", poolSess.CooldownUntil, expected24h)
	}

	// Persisted DB assertions
	dbSess, _ := repo.GetSession(ctx, sess.ID)
	if dbSess.HealthStatus != mediasession.HealthBotChallenge {
		t.Errorf("db HealthStatus = %s, want bot_challenge", dbSess.HealthStatus)
	}
	if dbSess.ConsecutiveFailures != 1 {
		t.Errorf("db ConsecutiveFailures = %d, want 1", dbSess.ConsecutiveFailures)
	}
	if dbSess.CooldownUntil == nil || !dbSess.CooldownUntil.Equal(expected24h) {
		t.Errorf("db CooldownUntil = %v, want 24h (%v)", dbSess.CooldownUntil, expected24h)
	}

	// SessionView assertions
	if view.HealthStatus != mediasession.HealthBotChallenge {
		t.Errorf("view HealthStatus = %s, want bot_challenge", view.HealthStatus)
	}
	if view.CooldownUntil == nil || !view.CooldownUntil.Equal(expected24h) {
		t.Errorf("view CooldownUntil = %v, want 24h (%v)", view.CooldownUntil, expected24h)
	}
}

// TestProbeFailure_2_BotChallenge_Repeated verifies that repeat BOT_CHALLENGE probe
// transitions to 72h finite cooldown, increments failures to 2, does NOT reset to 1,
// does NOT produce a nil cooldown dead-end, and performs exactly one authoritative write.
func TestProbeFailure_2_BotChallenge_Repeated(t *testing.T) {
	svc, repo, _, pool, prober := setupTestService(t)
	ctx := context.Background()
	now := time.Date(2026, 9, 8, 12, 0, 0, 0, time.UTC)
	pool.SetNow(func() time.Time { return now })

	sess, err := svc.CreateSession(ctx, mediasession.CreateSessionRequest{Name: "Repeat Bot Session"})
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	_, _, err = svc.UploadCookies(ctx, sess.ID, validNetscapeCookie("bot-cookie-2"))
	if err != nil {
		t.Fatalf("UploadCookies: %v", err)
	}

	// Session already has 1 failure
	firstCd := now.Add(24 * time.Hour)
	_, _ = repo.UpdateHealth(ctx, sess.ID, mediasession.HealthUpdate{
		HealthStatus:        mediasession.HealthBotChallenge,
		ConsecutiveFailures: 1,
		LastFailureReason:   "Sign in to confirm you're not a bot",
		CooldownUntil:       &firstCd,
	})
	firstDb, _ := repo.GetSession(ctx, sess.ID)
	pool.UpsertSession(firstDb)

	// Repeat probe failure
	probeTime := now.Add(24*time.Hour + time.Minute)
	pool.SetNow(func() time.Time { return probeTime })

	prober.mu.Lock()
	prober.res = &mediasession.ProbeResult{
		Status:          mediasession.HealthBotChallenge,
		TestedAt:        probeTime,
		FailureCategory: "SESSION_BOT_CHALLENGE",
		CooldownUntil:   &firstCd, // prober.go hardcoded 24h must NOT overwrite pool's 72h
	}
	prober.err = apperr.New(apperr.CodeSessionBotChallenge, "Sign in to confirm you're not a bot")
	prober.mu.Unlock()

	repo.ResetHealthUpdates()

	res, view, err := svc.ProbeSession(ctx, sess.ID)
	if err != nil {
		t.Fatalf("ProbeSession: %v", err)
	}
	if res.Status != mediasession.HealthBotChallenge {
		t.Fatalf("expected probe result status bot_challenge")
	}

	// Single authoritative write
	updates := repo.GetHealthUpdates()
	if len(updates) != 1 {
		t.Fatalf("expected exactly 1 UpdateHealth call, got %d", len(updates))
	}

	expected72h := probeTime.Add(72 * time.Hour)

	// View assertions
	if view.HealthStatus != mediasession.HealthBotChallenge {
		t.Errorf("view HealthStatus = %s, want bot_challenge", view.HealthStatus)
	}
	if view.CooldownUntil == nil || !view.CooldownUntil.Equal(expected72h) {
		t.Errorf("view CooldownUntil = %v, want 72h (%v)", view.CooldownUntil, expected72h)
	}

	// Persisted DB assertions
	dbSess, _ := repo.GetSession(ctx, sess.ID)
	if dbSess.ConsecutiveFailures != 2 {
		t.Errorf("db ConsecutiveFailures = %d, want 2 (must not be reset to 1)", dbSess.ConsecutiveFailures)
	}
	if dbSess.CooldownUntil == nil {
		t.Fatalf("db CooldownUntil is nil (must NOT be nil dead-end)")
	}
	if !dbSess.CooldownUntil.Equal(expected72h) {
		t.Errorf("db CooldownUntil = %v, want 72h (%v)", dbSess.CooldownUntil, expected72h)
	}

	// DB == pool check
	rs := pool.GetSession(sess.ID)
	if rs == nil {
		t.Fatalf("pool session nil")
	}
	poolSess := rs.Session()
	if poolSess.ConsecutiveFailures != dbSess.ConsecutiveFailures {
		t.Errorf("pool failures (%d) != db failures (%d)", poolSess.ConsecutiveFailures, dbSess.ConsecutiveFailures)
	}
	if poolSess.CooldownUntil == nil || !poolSess.CooldownUntil.Equal(*dbSess.CooldownUntil) {
		t.Errorf("pool cooldown (%v) != db cooldown (%v)", poolSess.CooldownUntil, dbSess.CooldownUntil)
	}
}

// TestProbeFailure_3_RateLimited verifies progressive backoff on repeated RATE_LIMITED probes
// and ensures each transition performs exactly one write with matching DB and pool states.
func TestProbeFailure_3_RateLimited(t *testing.T) {
	svc, repo, _, pool, prober := setupTestService(t)
	ctx := context.Background()
	now := time.Date(2026, 9, 8, 12, 0, 0, 0, time.UTC)
	pool.SetNow(func() time.Time { return now })

	sess, _ := svc.CreateSession(ctx, mediasession.CreateSessionRequest{Name: "RL Session"})
	_, _, _ = svc.UploadCookies(ctx, sess.ID, validNetscapeCookie("rl-cookie"))

	expectedCooldowns := []time.Duration{
		1 * time.Minute,
		2 * time.Minute,
	}

	currentTime := now
	for i, expectedCd := range expectedCooldowns {
		if i > 0 {
			// Bypass the 2s debounce between admin probe requests
			time.Sleep(2100 * time.Millisecond)
		}
		currentTime = currentTime.Add(expectedCd + 5*time.Second)
		pool.SetNow(func() time.Time { return currentTime })

		prober.mu.Lock()
		prober.res = &mediasession.ProbeResult{
			Status:          mediasession.HealthRateLimited,
			TestedAt:        currentTime,
			FailureCategory: "SESSION_RATE_LIMITED",
		}
		prober.err = apperr.New(apperr.CodeSessionRateLimited, "The media session was rate limited: 429")
		prober.mu.Unlock()

		repo.ResetHealthUpdates()

		_, view, err := svc.ProbeSession(ctx, sess.ID)
		if err != nil {
			t.Fatalf("step %d probe failed: %v", i, err)
		}

		updates := repo.GetHealthUpdates()
		if len(updates) != 1 {
			t.Fatalf("step %d: expected 1 UpdateHealth call, got %d", i, len(updates))
		}

		wantFailures := i + 1
		wantCooldown := currentTime.Add(expectedCd)

		dbSess, _ := repo.GetSession(ctx, sess.ID)
		if dbSess.ConsecutiveFailures != wantFailures {
			t.Errorf("step %d: db failures = %d, want %d", i, dbSess.ConsecutiveFailures, wantFailures)
		}
		if dbSess.CooldownUntil == nil || !dbSess.CooldownUntil.Equal(wantCooldown) {
			t.Errorf("step %d: db cooldown = %v, want %v", i, dbSess.CooldownUntil, wantCooldown)
		}

		rs := pool.GetSession(sess.ID)
		poolSess := rs.Session()
		if poolSess.ConsecutiveFailures != dbSess.ConsecutiveFailures {
			t.Errorf("step %d: pool failures (%d) != db failures (%d)", i, poolSess.ConsecutiveFailures, dbSess.ConsecutiveFailures)
		}
		if poolSess.CooldownUntil == nil || !poolSess.CooldownUntil.Equal(*dbSess.CooldownUntil) {
			t.Errorf("step %d: pool cooldown (%v) != db cooldown (%v)", i, poolSess.CooldownUntil, dbSess.CooldownUntil)
		}
		if view.HealthStatus != mediasession.HealthRateLimited {
			t.Errorf("step %d: view status = %s, want rate_limited", i, view.HealthStatus)
		}
	}
}

// TestProbeFailure_4_AuthFailed verifies that AUTH_FAILED probe sets HealthAuthFailed,
// nil CooldownUntil (indefinite exclusion), exactly one write, and matching DB/pool.
func TestProbeFailure_4_AuthFailed(t *testing.T) {
	svc, repo, _, pool, prober := setupTestService(t)
	ctx := context.Background()
	now := time.Date(2026, 9, 8, 12, 0, 0, 0, time.UTC)
	pool.SetNow(func() time.Time { return now })

	sess, _ := svc.CreateSession(ctx, mediasession.CreateSessionRequest{Name: "Auth Session"})
	_, _, _ = svc.UploadCookies(ctx, sess.ID, validNetscapeCookie("auth-cookie"))

	prober.mu.Lock()
	prober.res = &mediasession.ProbeResult{
		Status:          mediasession.HealthAuthFailed,
		TestedAt:        now,
		FailureCategory: "SESSION_AUTH_FAILED",
	}
	prober.err = apperr.New(apperr.CodeSessionAuthFailed, "login required")
	prober.mu.Unlock()

	repo.ResetHealthUpdates()

	_, view, err := svc.ProbeSession(ctx, sess.ID)
	if err != nil {
		t.Fatalf("ProbeSession: %v", err)
	}

	updates := repo.GetHealthUpdates()
	if len(updates) != 1 {
		t.Fatalf("expected exactly 1 UpdateHealth call, got %d", len(updates))
	}

	if view.HealthStatus != mediasession.HealthAuthFailed {
		t.Errorf("view status = %s, want auth_failed", view.HealthStatus)
	}
	if view.CooldownUntil != nil {
		t.Errorf("view cooldown = %v, want nil", view.CooldownUntil)
	}

	dbSess, _ := repo.GetSession(ctx, sess.ID)
	if dbSess.HealthStatus != mediasession.HealthAuthFailed {
		t.Errorf("db status = %s, want auth_failed", dbSess.HealthStatus)
	}
	if dbSess.CooldownUntil != nil {
		t.Errorf("db cooldown = %v, want nil", dbSess.CooldownUntil)
	}

	rs := pool.GetSession(sess.ID)
	poolSess := rs.Session()
	if poolSess.HealthStatus != dbSess.HealthStatus {
		t.Errorf("pool status (%s) != db status (%s)", poolSess.HealthStatus, dbSess.HealthStatus)
	}
	if poolSess.CooldownUntil != dbSess.CooldownUntil {
		t.Errorf("pool cooldown (%v) != db cooldown (%v)", poolSess.CooldownUntil, dbSess.CooldownUntil)
	}
}

// TestProbeFailure_5_FailedProbeCannotOverwritePoolCooldown verifies that a failed probe
// cannot overwrite a pool-calculated cooldown with independently constructed values.
func TestProbeFailure_5_FailedProbeCannotOverwritePoolCooldown(t *testing.T) {
	svc, repo, _, pool, prober := setupTestService(t)
	ctx := context.Background()
	now := time.Date(2026, 9, 8, 12, 0, 0, 0, time.UTC)
	pool.SetNow(func() time.Time { return now })

	sess, _ := svc.CreateSession(ctx, mediasession.CreateSessionRequest{Name: "Protection Session"})
	_, _, _ = svc.UploadCookies(ctx, sess.ID, validNetscapeCookie("prot-cookie"))

	// Session already has 1 failure and is cooling for 24h
	cd := now.Add(24 * time.Hour)
	_, _ = repo.UpdateHealth(ctx, sess.ID, mediasession.HealthUpdate{
		HealthStatus:        mediasession.HealthBotChallenge,
		ConsecutiveFailures: 1,
		CooldownUntil:       &cd,
	})
	dbS, _ := repo.GetSession(ctx, sess.ID)
	pool.UpsertSession(dbS)

	// Prober returns an arbitrary different cooldown (e.g. 5m)
	staleCd := now.Add(5 * time.Minute)
	prober.mu.Lock()
	prober.res = &mediasession.ProbeResult{
		Status:          mediasession.HealthBotChallenge,
		TestedAt:        now,
		FailureCategory: "SESSION_BOT_CHALLENGE",
		CooldownUntil:   &staleCd,
	}
	prober.err = apperr.New(apperr.CodeSessionBotChallenge, "bot challenge")
	prober.mu.Unlock()

	_, _, err := svc.ProbeSession(ctx, sess.ID)
	if err != nil {
		t.Fatalf("ProbeSession: %v", err)
	}

	// Repeat failure must result in 72h cooldown, NOT the prober's 5m or 24h!
	expected72h := now.Add(72 * time.Hour)
	dbAfter, _ := repo.GetSession(ctx, sess.ID)
	if dbAfter.CooldownUntil == nil || !dbAfter.CooldownUntil.Equal(expected72h) {
		t.Errorf("db cooldown = %v, want pool-calculated 72h (%v)", dbAfter.CooldownUntil, expected72h)
	}
}

// TestProbeFailure_6_GenericFailure_NoStaleOverwrite verifies that generic/candidate
// probe failures do NOT overwrite existing protection or increment failure counters.
func TestProbeFailure_6_GenericFailure_NoStaleOverwrite(t *testing.T) {
	svc, repo, _, pool, prober := setupTestService(t)
	ctx := context.Background()
	now := time.Date(2026, 9, 8, 12, 0, 0, 0, time.UTC)
	pool.SetNow(func() time.Time { return now })

	sess, _ := svc.CreateSession(ctx, mediasession.CreateSessionRequest{Name: "Generic Probe Test"})
	_, _, _ = svc.UploadCookies(ctx, sess.ID, validNetscapeCookie("generic-cookie"))

	// Session is currently cooling under 72h BOT_CHALLENGE with 2 failures
	cd72h := now.Add(72 * time.Hour)
	_, _ = repo.UpdateHealth(ctx, sess.ID, mediasession.HealthUpdate{
		HealthStatus:        mediasession.HealthBotChallenge,
		ConsecutiveFailures: 2,
		CooldownUntil:       &cd72h,
		LastFailureReason:   "Sign in to confirm you're not a bot",
	})
	dbS, _ := repo.GetSession(ctx, sess.ID)
	pool.UpsertSession(dbS)

	// Probe encounters a candidate/format failure (e.g. no usable audio formats)
	prober.mu.Lock()
	prober.res = &mediasession.ProbeResult{
		Status:             mediasession.HealthUnknown,
		TestedAt:           now,
		MetadataOK:         true,
		UsableAudioFormats: false,
		FailureCategory:    "NO_USABLE_AUDIO_FORMATS",
	}
	prober.err = apperr.New(apperr.CodeMediaVerifyFailed, "no usable audio formats found")
	prober.mu.Unlock()

	repo.ResetHealthUpdates()

	_, _, err := svc.ProbeSession(ctx, sess.ID)
	if err != nil {
		t.Fatalf("ProbeSession: %v", err)
	}

	// CRITICAL: Must NOT overwrite the 72h BOT_CHALLENGE protection!
	dbAfter, _ := repo.GetSession(ctx, sess.ID)
	if dbAfter.HealthStatus != mediasession.HealthBotChallenge {
		t.Errorf("db health = %s, want bot_challenge (protection destroyed!)", dbAfter.HealthStatus)
	}
	if dbAfter.ConsecutiveFailures != 2 {
		t.Errorf("db failures = %d, want 2 (corrupted to %d!)", dbAfter.ConsecutiveFailures, dbAfter.ConsecutiveFailures)
	}
	if dbAfter.CooldownUntil == nil || !dbAfter.CooldownUntil.Equal(cd72h) {
		t.Errorf("db cooldown = %v, want %v (cooldown destroyed!)", dbAfter.CooldownUntil, cd72h)
	}
}

// TestProbeFailure_7_MetadataProbeSuccessCannotClearProtection verifies that metadata-only
// probe success does NOT clear protection, erase failure count, or mark the session healthy (Step 2).
func TestProbeFailure_7_MetadataProbeSuccessCannotClearProtection(t *testing.T) {
	svc, repo, _, pool, prober := setupTestService(t)
	ctx := context.Background()
	now := time.Date(2026, 9, 8, 12, 0, 0, 0, time.UTC)
	pool.SetNow(func() time.Time { return now })

	sess, _ := svc.CreateSession(ctx, mediasession.CreateSessionRequest{Name: "Step2 Check"})
	_, _, _ = svc.UploadCookies(ctx, sess.ID, validNetscapeCookie("step2-cookie"))

	cd := now.Add(24 * time.Hour)
	_, _ = repo.UpdateHealth(ctx, sess.ID, mediasession.HealthUpdate{
		HealthStatus:        mediasession.HealthBotChallenge,
		ConsecutiveFailures: 1,
		CooldownUntil:       &cd,
	})
	dbS, _ := repo.GetSession(ctx, sess.ID)
	pool.UpsertSession(dbS)

	prober.mu.Lock()
	prober.res = &mediasession.ProbeResult{
		Status:             mediasession.HealthHealthy,
		TestedAt:           now,
		MetadataOK:         true,
		UsableAudioFormats: true,
	}
	prober.err = nil
	prober.mu.Unlock()

	repo.ResetHealthUpdates()

	res, view, err := svc.ProbeSession(ctx, sess.ID)
	if err != nil {
		t.Fatalf("ProbeSession: %v", err)
	}
	if !res.MetadataOK {
		t.Fatalf("expected MetadataOK = true")
	}

	// No health updates must be written on metadata probe success
	updates := repo.GetHealthUpdates()
	if len(updates) != 0 {
		t.Errorf("expected 0 UpdateHealth calls on metadata probe success, got %d", len(updates))
	}

	// View must preserve protection
	if view.HealthStatus != mediasession.HealthBotChallenge {
		t.Errorf("view HealthStatus = %s, want bot_challenge", view.HealthStatus)
	}
	if view.CooldownUntil == nil || !view.CooldownUntil.Equal(cd) {
		t.Errorf("view CooldownUntil = %v, want %v", view.CooldownUntil, cd)
	}

	// DB must preserve protection
	dbAfter, _ := repo.GetSession(ctx, sess.ID)
	if dbAfter.HealthStatus != mediasession.HealthBotChallenge {
		t.Errorf("db HealthStatus = %s, want bot_challenge", dbAfter.HealthStatus)
	}
	if dbAfter.ConsecutiveFailures != 1 {
		t.Errorf("db ConsecutiveFailures = %d, want 1", dbAfter.ConsecutiveFailures)
	}
	if dbAfter.CooldownUntil == nil || !dbAfter.CooldownUntil.Equal(cd) {
		t.Errorf("db CooldownUntil = %v, want %v", dbAfter.CooldownUntil, cd)
	}
}

// TestProbeFailure_8_MetadataProbeSuccessCannotWakeWaiters verifies that metadata probe
// success does NOT call recovery handler (Step 2).
func TestProbeFailure_8_MetadataProbeSuccessCannotWakeWaiters(t *testing.T) {
	svc, repo, _, pool, prober := setupTestService(t)
	ctx := context.Background()
	now := time.Date(2026, 9, 8, 12, 0, 0, 0, time.UTC)
	pool.SetNow(func() time.Time { return now })

	var wakeCount atomic.Int32
	svc.SetRecoveryHandler(func(ctx context.Context) {
		wakeCount.Add(1)
	})

	sess, _ := svc.CreateSession(ctx, mediasession.CreateSessionRequest{Name: "Wake Check"})
	_, _, _ = svc.UploadCookies(ctx, sess.ID, validNetscapeCookie("wake-cookie"))

	cd := now.Add(24 * time.Hour)
	_, _ = repo.UpdateHealth(ctx, sess.ID, mediasession.HealthUpdate{
		HealthStatus:        mediasession.HealthBotChallenge,
		ConsecutiveFailures: 1,
		CooldownUntil:       &cd,
	})
	dbS, _ := repo.GetSession(ctx, sess.ID)
	pool.UpsertSession(dbS)

	prober.mu.Lock()
	prober.res = &mediasession.ProbeResult{
		Status:             mediasession.HealthHealthy,
		TestedAt:           now,
		MetadataOK:         true,
		UsableAudioFormats: true,
	}
	prober.err = nil
	prober.mu.Unlock()

	_, _, err := svc.ProbeSession(ctx, sess.ID)
	if err != nil {
		t.Fatalf("ProbeSession: %v", err)
	}

	if count := wakeCount.Load(); count != 0 {
		t.Errorf("wakeCount = %d, want 0", count)
	}
}

// TestProbeFailure_9_RealMediaSuccessRecoveryUnchanged verifies that real media operation
// success clears protection and triggers recovery handler (Step 2).
func TestProbeFailure_9_RealMediaSuccessRecoveryUnchanged(t *testing.T) {
	svc, repo, _, pool, _ := setupTestService(t)
	ctx := context.Background()
	now := time.Date(2026, 9, 8, 12, 0, 0, 0, time.UTC)
	pool.SetNow(func() time.Time { return now })

	var wakeCount atomic.Int32
	svc.SetRecoveryHandler(func(ctx context.Context) {
		wakeCount.Add(1)
	})

	sess, _ := svc.CreateSession(ctx, mediasession.CreateSessionRequest{Name: "Media Success"})
	_, _, _ = svc.UploadCookies(ctx, sess.ID, validNetscapeCookie("success-cookie"))

	cd := now.Add(24 * time.Hour)
	_, _ = repo.UpdateHealth(ctx, sess.ID, mediasession.HealthUpdate{
		HealthStatus:        mediasession.HealthBotChallenge,
		ConsecutiveFailures: 1,
		CooldownUntil:       &cd,
	})
	dbS, _ := repo.GetSession(ctx, sess.ID)
	pool.UpsertSession(dbS)

	// Real media operation succeeds via pool.RecordOutcome
	pool.RecordOutcome(sess.ID, nil)

	if count := wakeCount.Load(); count != 1 {
		t.Errorf("wakeCount = %d, want 1", count)
	}

	dbAfter, _ := repo.GetSession(ctx, sess.ID)
	if dbAfter.HealthStatus != mediasession.HealthHealthy {
		t.Errorf("db status = %s, want healthy", dbAfter.HealthStatus)
	}
	if dbAfter.ConsecutiveFailures != 0 {
		t.Errorf("db failures = %d, want 0", dbAfter.ConsecutiveFailures)
	}
	if dbAfter.CooldownUntil != nil {
		t.Errorf("db cooldown = %v, want nil", dbAfter.CooldownUntil)
	}
}

// TestProbeFailure_10_SyncPersistenceEvenWithDefaultPoolConfig verifies that in production
// environments (where pool.syncPersist is false by default), ProbeSession failure persists
// synchronously so that the DB and pool immediately agree when ProbeSession returns.
func TestProbeFailure_10_SyncPersistenceEvenWithDefaultPoolConfig(t *testing.T) {
	svc, repo, _, pool, prober := setupTestService(t)
	// Explicitly turn off syncPersist to mimic production main.go!
	pool.SetSyncPersist(false)

	ctx := context.Background()
	now := time.Date(2026, 9, 8, 12, 0, 0, 0, time.UTC)
	pool.SetNow(func() time.Time { return now })

	sess, _ := svc.CreateSession(ctx, mediasession.CreateSessionRequest{Name: "Prod Mode Session"})
	_, _, _ = svc.UploadCookies(ctx, sess.ID, validNetscapeCookie("prod-cookie"))

	prober.mu.Lock()
	prober.res = &mediasession.ProbeResult{
		Status:          mediasession.HealthBotChallenge,
		TestedAt:        now,
		FailureCategory: "SESSION_BOT_CHALLENGE",
	}
	prober.err = apperr.New(apperr.CodeSessionBotChallenge, "Sign in to confirm you're not a bot")
	prober.mu.Unlock()

	repo.ResetHealthUpdates()

	_, view, err := svc.ProbeSession(ctx, sess.ID)
	if err != nil {
		t.Fatalf("ProbeSession: %v", err)
	}

	// Even with pool.SetSyncPersist(false), ProbeSession MUST persist synchronously
	updates := repo.GetHealthUpdates()
	if len(updates) != 1 {
		t.Fatalf("expected exactly 1 UpdateHealth call, got %d", len(updates))
	}

	dbSess, _ := repo.GetSession(ctx, sess.ID)
	if dbSess.HealthStatus != mediasession.HealthBotChallenge {
		t.Errorf("db status = %s, want bot_challenge", dbSess.HealthStatus)
	}
	if dbSess.ConsecutiveFailures != 1 {
		t.Errorf("db failures = %d, want 1", dbSess.ConsecutiveFailures)
	}

	rs := pool.GetSession(sess.ID)
	poolSess := rs.Session()
	if poolSess.ConsecutiveFailures != dbSess.ConsecutiveFailures {
		t.Errorf("pool failures (%d) != db failures (%d)", poolSess.ConsecutiveFailures, dbSess.ConsecutiveFailures)
	}
	if view.HealthStatus != dbSess.HealthStatus {
		t.Errorf("view status (%s) != db status (%s)", view.HealthStatus, dbSess.HealthStatus)
	}
}
