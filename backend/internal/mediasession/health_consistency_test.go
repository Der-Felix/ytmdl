package mediasession

import (
	"context"
	"errors"
	"testing"
	"time"

	"ytdm/backend/internal/apperr"
)

// assertHealthFieldsEqual compares the health-carrying fields of an in-memory
// session against the persisted row.
func assertHealthFieldsEqual(t *testing.T, what string, mem, db Session) {
	t.Helper()

	if mem.HealthStatus != db.HealthStatus {
		t.Errorf("%s: HealthStatus memory=%s db=%s", what, mem.HealthStatus, db.HealthStatus)
	}
	if mem.ConsecutiveFailures != db.ConsecutiveFailures {
		t.Errorf("%s: ConsecutiveFailures memory=%d db=%d", what, mem.ConsecutiveFailures, db.ConsecutiveFailures)
	}
	if mem.LastFailureReason != db.LastFailureReason {
		t.Errorf("%s: LastFailureReason memory=%q db=%q", what, mem.LastFailureReason, db.LastFailureReason)
	}
	assertTimePtrEqual(t, what+": LastFailureAt", mem.LastFailureAt, db.LastFailureAt)
	assertTimePtrEqual(t, what+": LastSuccessAt", mem.LastSuccessAt, db.LastSuccessAt)
	assertTimePtrEqual(t, what+": LastUsedAt", mem.LastUsedAt, db.LastUsedAt)
	assertTimePtrEqual(t, what+": CooldownUntil", mem.CooldownUntil, db.CooldownUntil)
}

func assertTimePtrEqual(t *testing.T, what string, mem, db *time.Time) {
	t.Helper()
	switch {
	case mem == nil && db == nil:
	case mem == nil:
		t.Errorf("%s: memory=nil db=%v", what, db)
	case db == nil:
		t.Errorf("%s: memory=%v db=nil", what, mem)
	case !mem.Equal(*db):
		t.Errorf("%s: memory=%v db=%v", what, mem, db)
	}
}

// FINDING A: a confirmed media success must leave the pool and the database in
// the same health state, including the cleared failure history.
func TestHealthConsistency_SuccessAfterFailure_MemoryMatchesDatabase(t *testing.T) {
	failedAt := time.Date(2026, 9, 1, 10, 0, 0, 0, time.UTC)
	succeededAt := time.Date(2026, 9, 2, 11, 0, 0, 0, time.UTC)
	cooldownUntil := failedAt.Add(24 * time.Hour)

	sess := healthTestSession("sess-consistency")
	sess.HealthStatus = HealthBotChallenge
	sess.ConsecutiveFailures = 2
	sess.LastFailureAt = &failedAt
	sess.LastFailureReason = "[SESSION_BOT_CHALLENGE] Sign in to confirm you're not a bot"
	sess.CooldownUntil = &cooldownUntil

	repo := newBlockingHealthRepo(0, sess)
	pool := healthPersistTestPool(t, repo, sess)
	pool.SetNow(func() time.Time { return succeededAt })

	// A genuine media success on the previously challenged session.
	pool.RecordSuccess(context.Background(), sess.ID, succeededAt)

	mem := pool.GetSession(sess.ID).Session()
	db := repo.Stored(sess.ID)

	if mem.HealthStatus != HealthHealthy {
		t.Fatalf("memory status = %s, want healthy", mem.HealthStatus)
	}
	if mem.LastFailureAt != nil {
		t.Errorf("memory LastFailureAt = %v, want nil (success clears failure history)", mem.LastFailureAt)
	}
	if mem.LastFailureReason != "" {
		t.Errorf("memory LastFailureReason = %q, want empty", mem.LastFailureReason)
	}
	if mem.ConsecutiveFailures != 0 {
		t.Errorf("memory ConsecutiveFailures = %d, want 0", mem.ConsecutiveFailures)
	}
	if mem.CooldownUntil != nil {
		t.Errorf("memory CooldownUntil = %v, want nil", mem.CooldownUntil)
	}

	assertHealthFieldsEqual(t, "after success", mem, db)
}

// FINDING A: a session failure must not silently null the persisted success
// history; the persisted snapshot mirrors the in-memory session.
func TestHealthConsistency_FailureTransition_PreservesSuccessHistory(t *testing.T) {
	succeededAt := time.Date(2026, 9, 1, 9, 0, 0, 0, time.UTC)
	failedAt := time.Date(2026, 9, 2, 12, 0, 0, 0, time.UTC)

	sess := healthTestSession("sess-failure-history")
	sess.LastSuccessAt = &succeededAt
	sess.LastUsedAt = &succeededAt

	repo := newBlockingHealthRepo(0, sess)
	pool := healthPersistTestPool(t, repo, sess)
	pool.SetNow(func() time.Time { return failedAt })

	pool.RecordFailure(context.Background(), sess.ID,
		apperr.New(apperr.CodeSessionRateLimited, "HTTP 429"), failedAt)

	mem := pool.GetSession(sess.ID).Session()
	db := repo.Stored(sess.ID)

	if mem.LastSuccessAt == nil || !mem.LastSuccessAt.Equal(succeededAt) {
		t.Fatalf("memory LastSuccessAt = %v, want %v", mem.LastSuccessAt, succeededAt)
	}
	if db.LastSuccessAt == nil || !db.LastSuccessAt.Equal(succeededAt) {
		t.Errorf("db LastSuccessAt = %v, want %v (a failure must not erase success history)", db.LastSuccessAt, succeededAt)
	}

	assertHealthFieldsEqual(t, "after failure", mem, db)
}

// FINDING A: reloading the pool from the persisted rows reproduces exactly the
// state the pool held before the restart.
func TestHealthConsistency_ReloadAfterRestart_MatchesPreRestartMemory(t *testing.T) {
	failedAt := time.Date(2026, 9, 1, 10, 0, 0, 0, time.UTC)
	succeededAt := time.Date(2026, 9, 2, 11, 0, 0, 0, time.UTC)

	sess := healthTestSession("sess-restart")
	sess.HealthStatus = HealthRateLimited
	sess.ConsecutiveFailures = 3
	sess.LastFailureAt = &failedAt
	sess.LastFailureReason = "[SESSION_RATE_LIMITED] HTTP 429"
	cooldown := failedAt.Add(5 * time.Minute)
	sess.CooldownUntil = &cooldown

	repo := newBlockingHealthRepo(0, sess)
	pool := healthPersistTestPool(t, repo, sess)
	pool.SetNow(func() time.Time { return succeededAt })

	pool.RecordSuccess(context.Background(), sess.ID, succeededAt)
	before := pool.GetSession(sess.ID).Session()

	// Restart: a fresh pool loads the persisted rows.
	stored, err := repo.ListSessions(context.Background(), Filter{})
	if err != nil {
		t.Fatalf("ListSessions: %v", err)
	}
	restarted := healthPersistTestPool(t, repo, stored...)
	after := restarted.GetSession(sess.ID).Session()

	assertHealthFieldsEqual(t, "after restart", before, after)
}

// FINDING A: the persisted snapshot for every transition carries the complete
// health state, so no column is nulled by omission.
func TestHealthConsistency_EveryPersistedUpdateIsCompleteSnapshot(t *testing.T) {
	base := time.Date(2026, 9, 1, 8, 0, 0, 0, time.UTC)

	sess := healthTestSession("sess-snapshot")
	repo := newBlockingHealthRepo(0, sess)
	pool := healthPersistTestPool(t, repo, sess)

	steps := []struct {
		name string
		at   time.Time
		err  error
	}{
		{"rate limited", base.Add(1 * time.Hour), apperr.New(apperr.CodeSessionRateLimited, "HTTP 429")},
		{"recovered", base.Add(2 * time.Hour), nil},
		{"bot challenge", base.Add(3 * time.Hour), apperr.New(apperr.CodeSessionBotChallenge, "challenge")},
		{"recovered again", base.Add(4 * time.Hour), nil},
	}

	for _, step := range steps {
		at := step.at
		pool.SetNow(func() time.Time { return at })
		if step.err == nil {
			pool.RecordSuccess(context.Background(), sess.ID, at)
		} else {
			pool.RecordFailure(context.Background(), sess.ID, step.err, at)
		}

		mem := pool.GetSession(sess.ID).Session()
		db := repo.Stored(sess.ID)
		assertHealthFieldsEqual(t, step.name, mem, db)
	}
}

// ---------------------------------------------------------------------------
// FINDING B: recovery notifications must run on a bounded, cancellable context.
// ---------------------------------------------------------------------------

// recoveryProbe captures what a recovery handler observed. Calls are serialized
// by the pool's notification path, so plain fields are safe here.
type recoveryProbe struct {
	entered  chan struct{}
	released chan struct{}
	block    bool

	calls    int
	hadCtx   bool
	deadline time.Time
	ctxErr   error
}

func newRecoveryProbe(block bool) *recoveryProbe {
	return &recoveryProbe{
		entered:  make(chan struct{}, 8),
		released: make(chan struct{}),
		block:    block,
	}
}

func (r *recoveryProbe) handler(ctx context.Context) {
	r.calls++
	r.hadCtx = ctx != nil && ctx.Done() != nil
	if dl, ok := ctx.Deadline(); ok {
		r.deadline = dl
	}
	if !r.block {
		return
	}
	r.entered <- struct{}{}
	// Stand in for a database call that only returns when the context ends.
	select {
	case <-ctx.Done():
		r.ctxErr = ctx.Err()
	case <-r.released:
	}
}

func recoveryTestPool(t *testing.T, sess Session) (*SessionPool, *blockingHealthRepo) {
	t.Helper()
	repo := newBlockingHealthRepo(0, sess)
	pool := healthPersistTestPool(t, repo, sess)
	return pool, repo
}

// FINDING B: the handler receives a cancellable context carrying a deadline.
func TestRecoveryContext_IsBoundedAndCancellable(t *testing.T) {
	sess := healthTestSession("sess-recovery-ctx")
	sess.HealthStatus = HealthUnknown
	pool, _ := recoveryTestPool(t, sess)

	now := time.Date(2026, 9, 8, 12, 0, 0, 0, time.UTC)
	pool.SetNow(func() time.Time { return now })

	lifecycle, cancel := context.WithCancel(context.Background())
	defer cancel()
	pool.SetLifecycleContext(lifecycle)

	probe := newRecoveryProbe(false)
	pool.SetRecoveryHandler(probe.handler)

	start := time.Now()
	pool.RecordSuccess(context.Background(), sess.ID, now)

	if probe.calls != 1 {
		t.Fatalf("recovery handler calls = %d, want 1", probe.calls)
	}
	if !probe.hadCtx {
		t.Fatal("recovery handler received a non-cancellable context")
	}
	if probe.deadline.IsZero() {
		t.Fatal("recovery handler received a context without a deadline")
	}
	if budget := probe.deadline.Sub(start); budget <= 0 || budget > recoveryNotifyTimeout+time.Second {
		t.Fatalf("recovery deadline budget = %v, want (0, %v]", budget, recoveryNotifyTimeout)
	}
}

// FINDING B: without an installed lifecycle context the fallback is still bounded.
func TestRecoveryContext_FallbackIsStillBounded(t *testing.T) {
	sess := healthTestSession("sess-recovery-fallback")
	sess.HealthStatus = HealthUnknown
	pool, _ := recoveryTestPool(t, sess)

	now := time.Date(2026, 9, 8, 12, 0, 0, 0, time.UTC)
	pool.SetNow(func() time.Time { return now })

	probe := newRecoveryProbe(false)
	pool.SetRecoveryHandler(probe.handler)

	pool.RecordSuccess(context.Background(), sess.ID, now)

	if probe.calls != 1 {
		t.Fatalf("recovery handler calls = %d, want 1", probe.calls)
	}
	if probe.deadline.IsZero() {
		t.Fatal("fallback recovery context has no deadline")
	}
}

// FINDING B: cancelling the application lifecycle unblocks a recovery callback
// that is stuck on a slow or closing database.
func TestRecoveryContext_ShutdownCancelsBlockedHandler(t *testing.T) {
	sess := healthTestSession("sess-recovery-shutdown")
	sess.HealthStatus = HealthUnknown
	pool, _ := recoveryTestPool(t, sess)

	now := time.Date(2026, 9, 8, 12, 0, 0, 0, time.UTC)
	pool.SetNow(func() time.Time { return now })

	lifecycle, cancel := context.WithCancel(context.Background())
	pool.SetLifecycleContext(lifecycle)

	probe := newRecoveryProbe(true)
	pool.SetRecoveryHandler(probe.handler)

	done := make(chan struct{})
	go func() {
		defer close(done)
		pool.RecordSuccess(context.Background(), sess.ID, now)
	}()

	// Deterministic: the recovery callback is now inside its "database" call.
	<-probe.entered

	// The pool must stay usable while the callback runs: it is invoked outside p.mu.
	mustNotBlock(t, "Availability during recovery callback", func() { _ = pool.Availability() })
	mustNotBlock(t, "Sessions during recovery callback", func() { _ = pool.Sessions() })
	mustNotBlock(t, "Acquire during recovery callback", func() {
		lease, err := pool.Acquire(context.Background())
		if err == nil && lease != nil {
			lease.ReleaseNeutral()
		}
	})

	// Application shutdown must be able to abort the in-flight recovery work.
	cancel()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("recovery callback did not return after the lifecycle context was cancelled")
	}

	if !errors.Is(probe.ctxErr, context.Canceled) {
		t.Fatalf("recovery context error = %v, want context.Canceled", probe.ctxErr)
	}
}

// FINDING B: the callback runs synchronously on the notifying goroutine, so it
// cannot outlive the caller as a detached goroutine.
func TestRecoveryContext_HandlerRunsSynchronouslyWithoutLeak(t *testing.T) {
	sess := healthTestSession("sess-recovery-sync")
	sess.HealthStatus = HealthUnknown
	pool, _ := recoveryTestPool(t, sess)

	now := time.Date(2026, 9, 8, 12, 0, 0, 0, time.UTC)
	pool.SetNow(func() time.Time { return now })
	pool.SetLifecycleContext(context.Background())

	probe := newRecoveryProbe(true)
	pool.SetRecoveryHandler(probe.handler)

	done := make(chan struct{})
	go func() {
		defer close(done)
		pool.RecordSuccess(context.Background(), sess.ID, now)
	}()

	<-probe.entered
	select {
	case <-done:
		t.Fatal("RecordSuccess returned while the recovery callback was still running")
	default:
	}

	close(probe.released)
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("RecordSuccess did not return after the recovery callback finished")
	}
	if probe.calls != 1 {
		t.Fatalf("recovery handler calls = %d, want 1", probe.calls)
	}
}

// FINDING B: normal recovery still fires, exactly once per genuine recovery
// transition - no wake storm on repeated successes.
func TestRecoveryContext_NormalRecoveryWakesOnceNoStorm(t *testing.T) {
	sess := healthTestSession("sess-recovery-once")
	sess.HealthStatus = HealthBotChallenge
	failedAt := time.Date(2026, 9, 1, 10, 0, 0, 0, time.UTC)
	cooldown := failedAt.Add(24 * time.Hour)
	sess.LastFailureAt = &failedAt
	sess.CooldownUntil = &cooldown

	pool, _ := recoveryTestPool(t, sess)
	now := time.Date(2026, 9, 8, 12, 0, 0, 0, time.UTC)
	pool.SetNow(func() time.Time { return now })
	pool.SetLifecycleContext(context.Background())

	probe := newRecoveryProbe(false)
	pool.SetRecoveryHandler(probe.handler)

	// First real media success: recovery fires and waiters are woken.
	pool.RecordSuccess(context.Background(), sess.ID, now)
	if probe.calls != 1 {
		t.Fatalf("recovery calls after first success = %d, want 1", probe.calls)
	}

	// Further successes on an already healthy session with no platform failure
	// must not re-trigger the wake.
	pool.RecordSuccess(context.Background(), sess.ID, now.Add(time.Minute))
	pool.RecordSuccess(context.Background(), sess.ID, now.Add(2*time.Minute))
	if probe.calls != 1 {
		t.Fatalf("recovery calls after repeated successes = %d, want 1 (no wake storm)", probe.calls)
	}

	// A platform failure followed by a real success recovers again (F5).
	pool.RecordPlatformFailure(apperr.New(apperr.CodeProviderRateLimited, "429"), time.Minute)
	pool.RecordSuccess(context.Background(), sess.ID, now.Add(3*time.Minute))
	if probe.calls != 2 {
		t.Fatalf("recovery calls after platform failure recovery = %d, want 2", probe.calls)
	}
	if pool.IsPlatformCooling() {
		t.Error("platform failure was not cleared by a real media success")
	}
}

// FINDING B: no recovery handler and no lifecycle context must stay safe.
func TestRecoveryContext_NoHandlerInstalled_IsSafe(t *testing.T) {
	sess := healthTestSession("sess-recovery-none")
	sess.HealthStatus = HealthUnknown
	pool, _ := recoveryTestPool(t, sess)

	now := time.Date(2026, 9, 8, 12, 0, 0, 0, time.UTC)
	pool.SetNow(func() time.Time { return now })

	pool.RecordSuccess(context.Background(), sess.ID, now)

	if s := pool.GetSession(sess.ID).Session(); s.HealthStatus != HealthHealthy {
		t.Fatalf("status = %s, want healthy", s.HealthStatus)
	}
}

// ---------------------------------------------------------------------------
// v0.26.0 FINAL CODE CORRECTNESS FIX: success health persistence regressions
// ---------------------------------------------------------------------------

// REGRESSION 1 & 2: HealthHealthy + stale failure metadata -> genuine success
// clears failure fields in memory and DB, and reload reproduces exact same state.
func TestHealthConsistency_HealthyWithStaleFailureMetadata_SuccessClearsMemoryAndDB(t *testing.T) {
	failedAt := time.Date(2026, 9, 8, 10, 0, 0, 0, time.UTC)
	succeededAt := time.Date(2026, 9, 8, 12, 0, 0, 0, time.UTC)

	sess := healthTestSession("sess-stale-failure")
	sess.HealthStatus = HealthHealthy
	sess.ConsecutiveFailures = 2
	sess.LastFailureAt = &failedAt
	sess.LastFailureReason = "[SESSION_UNAVAILABLE] backend worker unavailable"
	sess.CooldownUntil = nil

	repo := newBlockingHealthRepo(0, sess)
	pool := healthPersistTestPool(t, repo, sess)
	pool.SetNow(func() time.Time { return succeededAt })

	pool.RecordSuccess(context.Background(), sess.ID, succeededAt)

	mem := pool.GetSession(sess.ID).Session()
	db := repo.Stored(sess.ID)

	if mem.HealthStatus != HealthHealthy {
		t.Fatalf("memory status = %s, want healthy", mem.HealthStatus)
	}
	if mem.ConsecutiveFailures != 0 {
		t.Errorf("memory ConsecutiveFailures = %d, want 0", mem.ConsecutiveFailures)
	}
	if mem.LastFailureAt != nil {
		t.Errorf("memory LastFailureAt = %v, want nil", mem.LastFailureAt)
	}
	if mem.LastFailureReason != "" {
		t.Errorf("memory LastFailureReason = %q, want empty", mem.LastFailureReason)
	}
	if mem.CooldownUntil != nil {
		t.Errorf("memory CooldownUntil = %v, want nil", mem.CooldownUntil)
	}

	if db.HealthStatus != HealthHealthy {
		t.Fatalf("db status = %s, want healthy", db.HealthStatus)
	}
	if db.ConsecutiveFailures != 0 {
		t.Errorf("db ConsecutiveFailures = %d, want 0", db.ConsecutiveFailures)
	}
	if db.LastFailureAt != nil {
		t.Errorf("db LastFailureAt = %v, want nil", db.LastFailureAt)
	}
	if db.LastFailureReason != "" {
		t.Errorf("db LastFailureReason = %q, want empty", db.LastFailureReason)
	}
	if db.CooldownUntil != nil {
		t.Errorf("db CooldownUntil = %v, want nil", db.CooldownUntil)
	}

	assertHealthFieldsEqual(t, "after healthy success with cleared failure", mem, db)

	// REGRESSION 2: restart/reload after that success reproduces exact same health state
	stored, err := repo.ListSessions(context.Background(), Filter{})
	if err != nil {
		t.Fatalf("ListSessions: %v", err)
	}
	restarted := healthPersistTestPool(t, repo, stored...)
	after := restarted.GetSession(sess.ID).Session()
	assertHealthFieldsEqual(t, "after restart/reload", mem, after)
}

// REGRESSION 3: ConsecutiveFailures reset persists even when status was already HEALTHY.
func TestHealthConsistency_ConsecutiveFailuresResetPersists_WhenAlreadyHealthy(t *testing.T) {
	succeededAt := time.Date(2026, 9, 8, 12, 0, 0, 0, time.UTC)

	sess := healthTestSession("sess-cf-reset")
	sess.HealthStatus = HealthHealthy
	sess.ConsecutiveFailures = 3

	repo := newBlockingHealthRepo(0, sess)
	pool := healthPersistTestPool(t, repo, sess)
	pool.SetNow(func() time.Time { return succeededAt })

	pool.RecordSuccess(context.Background(), sess.ID, succeededAt)

	mem := pool.GetSession(sess.ID).Session()
	db := repo.Stored(sess.ID)

	if mem.ConsecutiveFailures != 0 {
		t.Fatalf("memory ConsecutiveFailures = %d, want 0", mem.ConsecutiveFailures)
	}
	if db.ConsecutiveFailures != 0 {
		t.Fatalf("db ConsecutiveFailures = %d, want 0", db.ConsecutiveFailures)
	}
	if len(repo.Writes()) != 1 {
		t.Fatalf("expected 1 write, got %d", len(repo.Writes()))
	}
	if repo.Writes()[0].update.ConsecutiveFailures != 0 {
		t.Errorf("persisted ConsecutiveFailures = %d, want 0", repo.Writes()[0].update.ConsecutiveFailures)
	}
}

// REGRESSION 4: LastFailureAt reset persists even when status was already HEALTHY.
func TestHealthConsistency_LastFailureAtResetPersists(t *testing.T) {
	failedAt := time.Date(2026, 9, 8, 10, 0, 0, 0, time.UTC)
	succeededAt := time.Date(2026, 9, 8, 12, 0, 0, 0, time.UTC)

	sess := healthTestSession("sess-lfa-reset")
	sess.HealthStatus = HealthHealthy
	sess.LastFailureAt = &failedAt

	repo := newBlockingHealthRepo(0, sess)
	pool := healthPersistTestPool(t, repo, sess)
	pool.SetNow(func() time.Time { return succeededAt })

	pool.RecordSuccess(context.Background(), sess.ID, succeededAt)

	mem := pool.GetSession(sess.ID).Session()
	db := repo.Stored(sess.ID)

	if mem.LastFailureAt != nil {
		t.Fatalf("memory LastFailureAt = %v, want nil", mem.LastFailureAt)
	}
	if db.LastFailureAt != nil {
		t.Fatalf("db LastFailureAt = %v, want nil", db.LastFailureAt)
	}
	if len(repo.Writes()) != 1 {
		t.Fatalf("expected 1 write, got %d", len(repo.Writes()))
	}
	if repo.Writes()[0].update.LastFailureAt != nil {
		t.Errorf("persisted LastFailureAt = %v, want nil", repo.Writes()[0].update.LastFailureAt)
	}
}

// REGRESSION 5: LastFailureReason reset persists even when status was already HEALTHY.
func TestHealthConsistency_LastFailureReasonResetPersists(t *testing.T) {
	succeededAt := time.Date(2026, 9, 8, 12, 0, 0, 0, time.UTC)

	sess := healthTestSession("sess-lfr-reset")
	sess.HealthStatus = HealthHealthy
	sess.LastFailureReason = "[SESSION_UNAVAILABLE] transient error"

	repo := newBlockingHealthRepo(0, sess)
	pool := healthPersistTestPool(t, repo, sess)
	pool.SetNow(func() time.Time { return succeededAt })

	pool.RecordSuccess(context.Background(), sess.ID, succeededAt)

	mem := pool.GetSession(sess.ID).Session()
	db := repo.Stored(sess.ID)

	if mem.LastFailureReason != "" {
		t.Fatalf("memory LastFailureReason = %q, want empty", mem.LastFailureReason)
	}
	if db.LastFailureReason != "" {
		t.Fatalf("db LastFailureReason = %q, want empty", db.LastFailureReason)
	}
	if len(repo.Writes()) != 1 {
		t.Fatalf("expected 1 write, got %d", len(repo.Writes()))
	}
	if repo.Writes()[0].update.LastFailureReason != "" {
		t.Errorf("persisted LastFailureReason = %q, want empty", repo.Writes()[0].update.LastFailureReason)
	}
}

// REGRESSION 6: CooldownUntil reset persists where applicable even when status was HEALTHY.
func TestHealthConsistency_CooldownUntilResetPersistsWhereApplicable(t *testing.T) {
	succeededAt := time.Date(2026, 9, 8, 12, 0, 0, 0, time.UTC)
	cooldown := succeededAt.Add(5 * time.Minute)

	sess := healthTestSession("sess-cdu-reset")
	sess.HealthStatus = HealthHealthy
	sess.CooldownUntil = &cooldown

	repo := newBlockingHealthRepo(0, sess)
	pool := healthPersistTestPool(t, repo, sess)
	pool.SetNow(func() time.Time { return succeededAt })

	pool.RecordSuccess(context.Background(), sess.ID, succeededAt)

	mem := pool.GetSession(sess.ID).Session()
	db := repo.Stored(sess.ID)

	if mem.CooldownUntil != nil {
		t.Fatalf("memory CooldownUntil = %v, want nil", mem.CooldownUntil)
	}
	if db.CooldownUntil != nil {
		t.Fatalf("db CooldownUntil = %v, want nil", db.CooldownUntil)
	}
	if len(repo.Writes()) != 1 {
		t.Fatalf("expected 1 write, got %d", len(repo.Writes()))
	}
	if repo.Writes()[0].update.CooldownUntil != nil {
		t.Errorf("persisted CooldownUntil = %v, want nil", repo.Writes()[0].update.CooldownUntil)
	}
}

// REGRESSION 7: normal healthy success does not create pathological persistence or wake behavior.
func TestHealthConsistency_NormalHealthySuccess_NoPathologicalPersistenceOrWake(t *testing.T) {
	now := time.Date(2026, 9, 8, 12, 0, 0, 0, time.UTC)
	current := now

	sess := healthTestSession("sess-healthy-ordinary")
	sess.HealthStatus = HealthHealthy
	sess.ConsecutiveFailures = 0
	sess.LastFailureAt = nil
	sess.LastFailureReason = ""
	sess.CooldownUntil = nil

	repo := newBlockingHealthRepo(0, sess)
	pool := healthPersistTestPool(t, repo, sess)
	pool.SetNow(func() time.Time { return current })
	pool.SetLifecycleContext(context.Background())

	probe := newRecoveryProbe(false)
	pool.SetRecoveryHandler(probe.handler)

	// Repeated ordinary successes on an already healthy session with no failure metadata
	pool.RecordSuccess(context.Background(), sess.ID, current)
	current = current.Add(time.Minute)
	pool.RecordSuccess(context.Background(), sess.ID, current)
	current = current.Add(time.Minute)
	pool.RecordOutcome(sess.ID, nil)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := pool.FlushHealthPersist(ctx); err != nil {
		t.Fatalf("FlushHealthPersist: %v", err)
	}

	// Must NOT create write storm
	if got := len(repo.Writes()); got != 0 {
		t.Fatalf("expected 0 health writes for ordinary healthy successes, got %d", got)
	}

	// Must NOT create wake storm
	if probe.calls != 0 {
		t.Fatalf("expected 0 recovery handler calls for ordinary healthy successes, got %d", probe.calls)
	}

	// Memory was updated with the latest success timestamp
	mem := pool.GetSession(sess.ID).Session()
	if mem.LastSuccessAt == nil || !mem.LastSuccessAt.Equal(current) {
		t.Errorf("memory LastSuccessAt = %v, want %v", mem.LastSuccessAt, current)
	}
}
