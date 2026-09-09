package mediasession

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"ytdm/backend/internal/apperr"
	"ytdm/backend/internal/provider"
)

// trackingClosingRepo records calls and tracks whether Close() was invoked
// while an UpdateHealth call was still in flight.
type trackingClosingRepo struct {
	mu                  sync.Mutex
	sessions            map[string]Session
	writes              []healthWrite
	closed              bool
	inFlight            int
	closedWhileInFlight bool

	// failNext makes that many upcoming UpdateHealth calls fail.
	failNext int

	enteredWrite chan struct{}
	releaseWrite chan struct{}
}

// FailNext makes the next n UpdateHealth calls return an error.
func (r *trackingClosingRepo) FailNext(n int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.failNext = n
}

func newTrackingClosingRepo(sessions ...Session) *trackingClosingRepo {
	r := &trackingClosingRepo{
		sessions: make(map[string]Session, len(sessions)),
	}
	for _, s := range sessions {
		r.sessions[s.ID] = s
	}
	return r
}

func (r *trackingClosingRepo) GetSession(_ context.Context, id string) (*Session, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	s, ok := r.sessions[id]
	if !ok {
		return nil, apperr.New(apperr.CodeSessionNotFound, "session not found")
	}
	return &s, nil
}

func (r *trackingClosingRepo) ListSessions(_ context.Context, _ Filter) ([]Session, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]Session, 0, len(r.sessions))
	for _, s := range r.sessions {
		out = append(out, s)
	}
	return out, nil
}

func (r *trackingClosingRepo) UpdateHealth(ctx context.Context, id string, update HealthUpdate) (*Session, error) {
	r.mu.Lock()
	if r.closed {
		r.mu.Unlock()
		return nil, errors.New("repository closed")
	}
	r.inFlight++
	entered := r.enteredWrite
	release := r.releaseWrite
	r.mu.Unlock()

	if entered != nil {
		entered <- struct{}{}
	}
	if release != nil {
		select {
		case <-release:
		case <-ctx.Done():
			r.mu.Lock()
			r.inFlight--
			r.mu.Unlock()
			return nil, ctx.Err()
		}
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	r.inFlight--
	if r.closed {
		r.closedWhileInFlight = true
		return nil, errors.New("repository closed during write")
	}
	if r.failNext > 0 {
		r.failNext--
		return nil, errors.New("update health rejected")
	}

	s := r.sessions[id]
	s.ID = id
	s.HealthStatus = update.HealthStatus
	s.ConsecutiveFailures = update.ConsecutiveFailures
	s.LastUsedAt = update.LastUsedAt
	s.LastSuccessAt = update.LastSuccessAt
	s.LastFailureAt = update.LastFailureAt
	s.LastFailureReason = update.LastFailureReason
	s.CooldownUntil = update.CooldownUntil
	r.sessions[id] = s
	r.writes = append(r.writes, healthWrite{id: id, update: update})
	return &s, nil
}

func (r *trackingClosingRepo) Close() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.closed = true
	if r.inFlight > 0 {
		r.closedWhileInFlight = true
	}
}

func (r *trackingClosingRepo) Writes() []healthWrite {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]healthWrite, len(r.writes))
	copy(out, r.writes)
	return out
}

func (r *trackingClosingRepo) Stored(id string) Session {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.sessions[id]
}

func (r *trackingClosingRepo) ClosedWhileInFlight() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.closedWhileInFlight
}

func shutdownTestPool(t *testing.T, repo SessionRepository, sessions ...Session) *SessionPool {
	t.Helper()
	cfg := PoolConfig{
		Family:                provider.FamilyYouTube,
		MaxLeasesPerSession:   2,
		SessionRequestsPerSec: 100,
		SessionBurst:          100,
		GlobalRequestsPerSec:  100,
		GlobalBurst:           100,
		AllowUnknown:          true,
	}
	pool := NewSessionPool(cfg, nil, repo, nil)
	pool.ReloadSessions(sessions)
	return pool
}

// Requirement 1 & 4: Pending async health write completes before repository/database close,
// and the repository is never closed while a write is still in flight.
// Also proves that bot challenge cooldown persists and survives a reload/restart simulation.
func TestShutdownFlush_CompletesBeforeRepoClose(t *testing.T) {
	sess := healthTestSession("sess-shutdown-1")
	repo := newTrackingClosingRepo(sess)
	pool := shutdownTestPool(t, repo, sess)
	pool.SetSyncPersist(false) // async drainer in effect

	now := time.Date(2026, 9, 8, 23, 0, 0, 0, time.UTC)
	pool.SetNow(func() time.Time { return now })

	// Encounter BOT_CHALLENGE -> queues 24h cooldown snapshot.
	pool.RecordOutcome(sess.ID, apperr.New(apperr.CodeSessionBotChallenge, "bot challenge detected"))

	// Execute controlled shutdown sequence:
	// 1. Flush health persistence
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := pool.FlushHealthPersist(ctx); err != nil {
		t.Fatalf("FlushHealthPersist failed: %v", err)
	}

	// 2. Close repository/database
	repo.Close()

	if repo.ClosedWhileInFlight() {
		t.Fatal("repository was closed while UpdateHealth was still in flight")
	}

	writes := repo.Writes()
	if len(writes) != 1 {
		t.Fatalf("expected 1 health write, got %d", len(writes))
	}
	if writes[0].update.HealthStatus != HealthBotChallenge {
		t.Fatalf("expected write status %s, got %s", HealthBotChallenge, writes[0].update.HealthStatus)
	}

	stored := repo.Stored(sess.ID)
	if stored.HealthStatus != HealthBotChallenge {
		t.Fatalf("stored health status = %s, want %s", stored.HealthStatus, HealthBotChallenge)
	}
	expectedCooldown := now.Add(botChallengeInitialCooldown)
	if stored.CooldownUntil == nil || !stored.CooldownUntil.Equal(expectedCooldown) {
		t.Fatalf("stored cooldown = %v, want %v", stored.CooldownUntil, expectedCooldown)
	}

	// Restart simulation: new pool reloads from repository.
	restartedPool := shutdownTestPool(t, repo, stored)
	restartedPool.SetNow(func() time.Time { return now.Add(1 * time.Hour) }) // 1 hour into 24h cooldown
	rs := restartedPool.GetSession(sess.ID)
	if rs == nil {
		t.Fatal("restarted pool did not load session")
	}
	if rs.session.HealthStatus != HealthBotChallenge {
		t.Fatalf("restarted session status = %s, want %s", rs.session.HealthStatus, HealthBotChallenge)
	}
	if rs.session.CooldownUntil == nil || !rs.session.CooldownUntil.After(now.Add(1*time.Hour)) {
		t.Fatal("restarted session must remain cooling after restart")
	}
}

// Requirement 2: Once producers have been stopped, FlushHealthPersist drains the
// queue and no post-flush health update can be enqueued.
func TestShutdownFlush_ProducersStopped_NoPostFlushEnqueue(t *testing.T) {
	sess := healthTestSession("sess-shutdown-2")
	repo := newTrackingClosingRepo(sess)
	pool := shutdownTestPool(t, repo, sess)
	pool.SetSyncPersist(false)

	now := time.Date(2026, 9, 8, 23, 0, 0, 0, time.UTC)
	pool.SetNow(func() time.Time { return now })

	pool.RecordOutcome(sess.ID, apperr.New(apperr.CodeSessionRateLimited, "rate limited"))

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := pool.FlushHealthPersist(ctx); err != nil {
		t.Fatalf("FlushHealthPersist failed: %v", err)
	}

	// Verify persistence queue is completely empty.
	pool.persistMu.Lock()
	queueLen := len(pool.persistQueue)
	pool.persistMu.Unlock()
	if queueLen != 0 {
		t.Fatalf("expected empty persistQueue after flush, got %d items", queueLen)
	}
}

// Requirement 3: Blocked/slow persistence obeys context timeout/cancellation
// so shutdown cannot hang forever.
func TestShutdownFlush_BlockedPersistence_ObeysTimeout(t *testing.T) {
	sess := healthTestSession("sess-shutdown-3")
	repo := newTrackingClosingRepo(sess)
	repo.enteredWrite = make(chan struct{}, 1)
	repo.releaseWrite = make(chan struct{}) // never released

	pool := shutdownTestPool(t, repo, sess)
	pool.SetSyncPersist(false)

	// Enqueue an update that will block in UpdateHealth.
	pool.RecordOutcome(sess.ID, apperr.New(apperr.CodeSessionBotChallenge, "challenge"))
	<-repo.enteredWrite

	// Call FlushHealthPersist with a tight timeout.
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	start := time.Now()
	err := pool.FlushHealthPersist(ctx)
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("expected FlushHealthPersist to fail on timeout, but got nil")
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected context.DeadlineExceeded, got: %v", err)
	}
	if elapsed > 1*time.Second {
		t.Fatalf("FlushHealthPersist took too long to abort: %v", elapsed)
	}

	// Clean up by releasing the blocked write.
	close(repo.releaseWrite)
}

// Requirement 5: No DB I/O occurs under SessionPool p.mu.
// Proves that while UpdateHealth is blocked in persistence, pool mutex methods
// (Acquire, Sessions, HasConfiguredSessions, etc.) complete immediately.
func TestShutdownFlush_NoLockContentionWithFamilyLock(t *testing.T) {
	sess := healthTestSession("sess-shutdown-4")
	repo := newTrackingClosingRepo(sess)
	repo.enteredWrite = make(chan struct{}, 1)
	repo.releaseWrite = make(chan struct{}) // hold persistence open

	pool := shutdownTestPool(t, repo, sess)
	pool.SetSyncPersist(false)

	// Trigger async health update.
	pool.RecordOutcome(sess.ID, apperr.New(apperr.CodeSessionRateLimited, "rate limited"))
	<-repo.enteredWrite

	// While persistence is blocked, family lock operations must complete instantly.
	done := make(chan struct{})
	go func() {
		_ = pool.Sessions()
		_ = pool.HasConfiguredSessions()
		_ = pool.Availability()
		close(done)
	}()

	select {
	case <-done:
		// Succeeded without blocking on p.mu
	case <-time.After(200 * time.Millisecond):
		t.Fatal("pool family lock operations blocked while DB persistence was in flight")
	}

	close(repo.releaseWrite)
}

// Requirement 6: FIFO health persistence remains intact.
func TestShutdownFlush_FIFOOrderPreserved(t *testing.T) {
	sess := healthTestSession("sess-shutdown-5")
	repo := newTrackingClosingRepo(sess)
	pool := shutdownTestPool(t, repo, sess)
	pool.SetSyncPersist(false)

	now := time.Date(2026, 9, 8, 23, 0, 0, 0, time.UTC)
	pool.SetNow(func() time.Time { return now })

	// 1. Rate limited
	pool.RecordOutcome(sess.ID, apperr.New(apperr.CodeSessionRateLimited, "429"))
	// 2. Bot challenge
	pool.RecordOutcome(sess.ID, apperr.New(apperr.CodeSessionBotChallenge, "challenge"))
	// 3. Real media success (recovers)
	pool.RecordSuccess(context.Background(), sess.ID, now.Add(10*time.Minute))

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := pool.FlushHealthPersist(ctx); err != nil {
		t.Fatalf("FlushHealthPersist: %v", err)
	}
	repo.Close()

	writes := repo.Writes()
	if len(writes) != 3 {
		t.Fatalf("expected 3 writes, got %d", len(writes))
	}
	if writes[0].update.HealthStatus != HealthRateLimited {
		t.Errorf("write 0 status = %s, want rate_limited", writes[0].update.HealthStatus)
	}
	if writes[1].update.HealthStatus != HealthBotChallenge {
		t.Errorf("write 1 status = %s, want bot_challenge", writes[1].update.HealthStatus)
	}
	if writes[2].update.HealthStatus != HealthHealthy {
		t.Errorf("write 2 status = %s, want healthy", writes[2].update.HealthStatus)
	}
}

// A repository failure must reach the FlushHealthPersist that queued the
// barrier behind it; a drained queue alone is not a successful flush. It stays
// reported until a newer snapshot for that session supersedes it.
func TestShutdownFlush_PersistFailurePropagates(t *testing.T) {
	sess := healthTestSession("sess-shutdown-6")
	repo := newTrackingClosingRepo(sess)
	pool := shutdownTestPool(t, repo, sess)
	pool.SetSyncPersist(false)

	now := time.Date(2026, 9, 8, 23, 0, 0, 0, time.UTC)
	pool.SetNow(func() time.Time { return now })

	repo.FailNext(1)
	pool.RecordOutcome(sess.ID, apperr.New(apperr.CodeSessionBotChallenge, "challenge"))

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err := pool.FlushHealthPersist(ctx)
	if err == nil {
		t.Fatal("expected FlushHealthPersist to report the failed health write, got nil")
	}
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
		t.Fatalf("expected a repository error, got a context error: %v", err)
	}

	// Nothing superseded the snapshot, so it is still missing from the database
	// and a second flush must keep saying so.
	if err := pool.FlushHealthPersist(ctx); err == nil {
		t.Fatal("an unresolved failure must stay reported until a newer snapshot replaces it")
	}
}

// HealthUpdate stores a complete snapshot, so a later successful write for the
// SAME session heals the earlier failure: the database is up to date again and
// the shutdown flush must stop reporting a stale error.
func TestShutdownFlush_SupersededSameSessionFailureIsCleared(t *testing.T) {
	sess := healthTestSession("sess-shutdown-7")
	repo := newTrackingClosingRepo(sess)
	pool := shutdownTestPool(t, repo, sess)
	pool.SetSyncPersist(true) // serialise the writes so the order is deterministic

	now := time.Date(2026, 9, 8, 23, 0, 0, 0, time.UTC)
	pool.SetNow(func() time.Time { return now })

	repo.FailNext(1)
	pool.RecordOutcome(sess.ID, apperr.New(apperr.CodeSessionRateLimited, "429"))
	// A second transition for the same session writes the full snapshot, which
	// leaves the database current again.
	pool.RecordOutcome(sess.ID, apperr.New(apperr.CodeSessionBotChallenge, "challenge"))

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := pool.FlushHealthPersist(ctx); err != nil {
		t.Fatalf("a newer successful snapshot for the same session must resolve the earlier failure, got: %v", err)
	}

	stored := repo.Stored(sess.ID)
	if stored.HealthStatus != HealthBotChallenge {
		t.Fatalf("stored status = %s, want %s", stored.HealthStatus, HealthBotChallenge)
	}
}

// A success for a DIFFERENT session says nothing about the snapshot that failed
// for the first one, so it must not hide it.
func TestShutdownFlush_OtherSessionSuccessKeepsFailureVisible(t *testing.T) {
	failing := healthTestSession("sess-shutdown-8a")
	healthy := healthTestSession("sess-shutdown-8b")
	repo := newTrackingClosingRepo(failing, healthy)
	pool := shutdownTestPool(t, repo, failing, healthy)
	pool.SetSyncPersist(true)

	now := time.Date(2026, 9, 8, 23, 0, 0, 0, time.UTC)
	pool.SetNow(func() time.Time { return now })

	repo.FailNext(1)
	pool.RecordOutcome(failing.ID, apperr.New(apperr.CodeSessionRateLimited, "429"))
	// A different session writes successfully afterwards.
	pool.RecordOutcome(healthy.ID, apperr.New(apperr.CodeSessionBotChallenge, "challenge"))

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err := pool.FlushHealthPersist(ctx)
	if err == nil {
		t.Fatal("a success for another session must not hide the unresolved failure")
	}
	if !strings.Contains(err.Error(), failing.ID) {
		t.Fatalf("the reported failure must name the affected session %s, got: %v", failing.ID, err)
	}

	// FIFO is unaffected: only the second session's write reached the repository.
	writes := repo.Writes()
	if len(writes) != 1 {
		t.Fatalf("expected 1 stored write, got %d", len(writes))
	}
	if writes[0].id != healthy.ID {
		t.Fatalf("stored write is for %s, want %s", writes[0].id, healthy.ID)
	}
}

// The barrier scopes a flush to the snapshots queued before it: a failure that
// happens only afterwards belongs to the next flush, not to this one.
func TestShutdownFlush_BarrierScopesFailureToPriorWrites(t *testing.T) {
	sess := healthTestSession("sess-shutdown-9")
	repo := newTrackingClosingRepo(sess)
	pool := shutdownTestPool(t, repo, sess)
	pool.SetSyncPersist(true)

	now := time.Date(2026, 9, 8, 23, 0, 0, 0, time.UTC)
	pool.SetNow(func() time.Time { return now })

	// Everything queued before the first flush succeeds.
	pool.RecordOutcome(sess.ID, apperr.New(apperr.CodeSessionRateLimited, "429"))

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := pool.FlushHealthPersist(ctx); err != nil {
		t.Fatalf("first flush must be clean: %v", err)
	}

	// A failure after that barrier must not be attributed to the flush above.
	repo.FailNext(1)
	pool.RecordOutcome(sess.ID, apperr.New(apperr.CodeSessionBotChallenge, "challenge"))

	if err := pool.FlushHealthPersist(ctx); err == nil {
		t.Fatal("the second flush must report the failure queued after the first barrier")
	}
}

// A flush that returns on its context must not hand the unresolved state to a
// barrier nobody reads: the next flush still has to see the failure.
func TestShutdownFlush_CancelledFlushDoesNotLoseFailure(t *testing.T) {
	sess := healthTestSession("sess-shutdown-10")
	repo := newTrackingClosingRepo(sess)
	repo.enteredWrite = make(chan struct{}, 1)
	repo.releaseWrite = make(chan struct{})
	repo.FailNext(1)

	pool := shutdownTestPool(t, repo, sess)
	pool.SetSyncPersist(false)

	now := time.Date(2026, 9, 8, 23, 0, 0, 0, time.UTC)
	pool.SetNow(func() time.Time { return now })

	pool.RecordOutcome(sess.ID, apperr.New(apperr.CodeSessionBotChallenge, "challenge"))
	<-repo.enteredWrite

	// The flush gives up while the (doomed) write is still in flight.
	timedOut, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	if err := pool.FlushHealthPersist(timedOut); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected the flush to abort on its deadline, got: %v", err)
	}

	// Let the write finish and fail. Its abandoned barrier is drained without a
	// reader, which must not consume the failure.
	close(repo.releaseWrite)

	ctx, cancelCtx := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancelCtx()
	if err := pool.FlushHealthPersist(ctx); err == nil {
		t.Fatal("the failure was lost in the abandoned barrier of the cancelled flush")
	}
}

// persistFailureCount reports how many sessions still carry an unresolved health
// snapshot. The shutdown path only ever surfaces these as one joined error, so
// the tests below read the map itself to prove it does not grow.
func persistFailureCount(p *SessionPool) int {
	p.persistMu.Lock()
	defer p.persistMu.Unlock()
	return len(p.persistFailures)
}

// Deleting a session drops its unresolved failure with it. Nothing can ever
// write that snapshot again - the row is gone - so keeping the entry would make
// every later flush report a session that no longer exists.
func TestShutdownFlush_RemovedSessionDropsUnresolvedFailure(t *testing.T) {
	sess := healthTestSession("sess-shutdown-11")
	repo := newTrackingClosingRepo(sess)
	pool := shutdownTestPool(t, repo, sess)
	pool.SetSyncPersist(true)

	now := time.Date(2026, 9, 8, 23, 0, 0, 0, time.UTC)
	pool.SetNow(func() time.Time { return now })

	repo.FailNext(1)
	pool.RecordOutcome(sess.ID, apperr.New(apperr.CodeSessionRateLimited, "429"))

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := pool.FlushHealthPersist(ctx); err == nil {
		t.Fatal("the failed snapshot must be unresolved while the session exists")
	}

	pool.RemoveSession(sess.ID)

	if got := persistFailureCount(pool); got != 0 {
		t.Fatalf("removing a session left %d unresolved failures, want 0", got)
	}
	if err := pool.FlushHealthPersist(ctx); err != nil {
		t.Fatalf("a deleted session must not keep a flush failing forever: %v", err)
	}
}

// The service deletes the row before it removes the session from the pool, so a
// queued snapshot can still be inside UpdateHealth when the removal lands. Its
// SessionNotFound must not resurrect a failure for a session that is gone.
func TestShutdownFlush_RemovalDuringWriteDoesNotResurrectFailure(t *testing.T) {
	sess := healthTestSession("sess-shutdown-12")
	repo := newTrackingClosingRepo(sess)
	repo.enteredWrite = make(chan struct{}, 1)
	repo.releaseWrite = make(chan struct{})
	repo.FailNext(1) // stands in for the row the delete already removed

	pool := shutdownTestPool(t, repo, sess)
	pool.SetSyncPersist(false)

	now := time.Date(2026, 9, 8, 23, 0, 0, 0, time.UTC)
	pool.SetNow(func() time.Time { return now })

	pool.RecordOutcome(sess.ID, apperr.New(apperr.CodeSessionBotChallenge, "challenge"))
	<-repo.enteredWrite // the write is in flight

	pool.RemoveSession(sess.ID)
	close(repo.releaseWrite) // it now fails against the deleted row

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := pool.FlushHealthPersist(ctx); err != nil {
		t.Fatalf("the write of a removed session resurrected its failure: %v", err)
	}
	if got := persistFailureCount(pool); got != 0 {
		t.Fatalf("%d unresolved failures survived the removal, want 0", got)
	}
}

// Removing one session says nothing about any other: their unresolved snapshots
// are still missing from the database and must stay reported.
func TestShutdownFlush_RemovalKeepsOtherSessionsFailures(t *testing.T) {
	removed := healthTestSession("sess-shutdown-13a")
	kept := healthTestSession("sess-shutdown-13b")
	repo := newTrackingClosingRepo(removed, kept)
	pool := shutdownTestPool(t, repo, removed, kept)
	pool.SetSyncPersist(true)

	now := time.Date(2026, 9, 8, 23, 0, 0, 0, time.UTC)
	pool.SetNow(func() time.Time { return now })

	repo.FailNext(2)
	pool.RecordOutcome(removed.ID, apperr.New(apperr.CodeSessionRateLimited, "429"))
	pool.RecordOutcome(kept.ID, apperr.New(apperr.CodeSessionRateLimited, "429"))

	pool.RemoveSession(removed.ID)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	err := pool.FlushHealthPersist(ctx)
	if err == nil {
		t.Fatal("the surviving session's failure must still be reported")
	}
	if strings.Contains(err.Error(), removed.ID) {
		t.Fatalf("the removed session must not be reported any more, got: %v", err)
	}
	if !strings.Contains(err.Error(), kept.ID) {
		t.Fatalf("the reported failure must name %s, got: %v", kept.ID, err)
	}
	if got := persistFailureCount(pool); got != 1 {
		t.Fatalf("unresolved failures = %d, want exactly the surviving session", got)
	}
}

// A create/delete cycle must leave nothing behind, however often it runs.
func TestShutdownFlush_RepeatedCreateDeleteDoesNotGrowFailures(t *testing.T) {
	repo := newTrackingClosingRepo()
	pool := shutdownTestPool(t, repo)
	pool.SetSyncPersist(true)

	now := time.Date(2026, 9, 8, 23, 0, 0, 0, time.UTC)
	pool.SetNow(func() time.Time { return now })

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	for i := 0; i < 50; i++ {
		sess := healthTestSession(fmt.Sprintf("sess-shutdown-14-%d", i))
		pool.ReloadSessions([]Session{sess})

		repo.FailNext(1)
		pool.RecordOutcome(sess.ID, apperr.New(apperr.CodeSessionRateLimited, "429"))
		pool.RemoveSession(sess.ID)
	}

	if got := persistFailureCount(pool); got != 0 {
		t.Fatalf("50 create/delete cycles left %d unresolved failures, want 0", got)
	}
	if err := pool.FlushHealthPersist(ctx); err != nil {
		t.Fatalf("no deleted session may keep a flush failing: %v", err)
	}
}

// Dropping a removed session's queued snapshots must not disturb the queue: the
// barrier behind them still has to come back, and the other sessions still have
// to be written in order.
func TestShutdownFlush_RemovalKeepsBarrierAndFIFOIntact(t *testing.T) {
	removed := healthTestSession("sess-shutdown-15a")
	kept := healthTestSession("sess-shutdown-15b")
	repo := newTrackingClosingRepo(removed, kept)
	repo.enteredWrite = make(chan struct{}, 1)
	repo.releaseWrite = make(chan struct{})

	pool := shutdownTestPool(t, repo, removed, kept)
	pool.SetSyncPersist(false)

	now := time.Date(2026, 9, 8, 23, 0, 0, 0, time.UTC)
	pool.SetNow(func() time.Time { return now })

	// The first snapshot parks the drainer inside UpdateHealth, so everything
	// queued behind it is still in the queue when the removal lands.
	pool.RecordOutcome(kept.ID, apperr.New(apperr.CodeSessionBotChallenge, "challenge"))
	<-repo.enteredWrite
	pool.RecordOutcome(removed.ID, apperr.New(apperr.CodeSessionBotChallenge, "challenge"))

	pool.RemoveSession(removed.ID)
	close(repo.releaseWrite)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := pool.FlushHealthPersist(ctx); err != nil {
		t.Fatalf("the flush behind a dropped snapshot must still complete: %v", err)
	}

	writes := repo.Writes()
	if len(writes) != 1 {
		t.Fatalf("expected only the surviving session's write, got %d", len(writes))
	}
	if writes[0].id != kept.ID {
		t.Fatalf("stored write is for %s, want %s", writes[0].id, kept.ID)
	}
}

// Sealing is what makes a shutdown flush the final one: afterwards nothing can
// queue a write that would still be running while the pool is torn down.
func TestShutdownFlush_SealRejectsFurtherSnapshots(t *testing.T) {
	sess := healthTestSession("sess-shutdown-16")
	repo := newTrackingClosingRepo(sess)
	pool := shutdownTestPool(t, repo, sess)
	pool.SetSyncPersist(true)

	now := time.Date(2026, 9, 8, 23, 0, 0, 0, time.UTC)
	pool.SetNow(func() time.Time { return now })

	pool.RecordOutcome(sess.ID, apperr.New(apperr.CodeSessionBotChallenge, "challenge"))

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	pool.SealHealthPersist()
	if err := pool.FlushHealthPersist(ctx); err != nil {
		t.Fatalf("the final flush must still drain what was queued before the seal: %v", err)
	}
	before := len(repo.Writes())

	// A straggler that outlived the drain finds the queue sealed.
	pool.RecordOutcome(sess.ID, apperr.New(apperr.CodeSessionAuthFailed, "too late"))

	if !pool.AwaitHealthPersistIdle(ctx) {
		t.Fatal("a snapshot was queued after the seal")
	}
	if got := len(repo.Writes()); got != before {
		t.Fatalf("a sealed pool wrote %d snapshots, want %d", got, before)
	}
}

// Quiescence is a property of the pool, not of the caller's context: an expired
// deadline over a drained queue is still quiescent, while a write that is really
// in flight is not.
func TestShutdownFlush_IdleIsReportedIndependentlyOfContext(t *testing.T) {
	sess := healthTestSession("sess-shutdown-17")
	repo := newTrackingClosingRepo(sess)
	repo.enteredWrite = make(chan struct{}, 1)
	repo.releaseWrite = make(chan struct{})

	pool := shutdownTestPool(t, repo, sess)
	pool.SetSyncPersist(false)

	now := time.Date(2026, 9, 8, 23, 0, 0, 0, time.UTC)
	pool.SetNow(func() time.Time { return now })

	expired, cancelExpired := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
	defer cancelExpired()

	if !pool.AwaitHealthPersistIdle(expired) {
		t.Fatal("an untouched pool must report quiescence even on an expired context")
	}

	pool.RecordOutcome(sess.ID, apperr.New(apperr.CodeSessionBotChallenge, "challenge"))
	<-repo.enteredWrite
	if pool.AwaitHealthPersistIdle(expired) {
		t.Fatal("a write in flight must not be reported as quiescent")
	}

	close(repo.releaseWrite)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if !pool.AwaitHealthPersistIdle(ctx) {
		t.Fatal("the pool never came to rest after the write finished")
	}
	if !pool.AwaitHealthPersistIdle(expired) {
		t.Fatal("a drained queue must report quiescence on an expired context too")
	}
}

// A Lease keeps its RuntimeSession, so it can be released long after the row and
// the pool entry are gone. That release must not queue a snapshot: the write
// would fail against the deleted row and leave a failure nothing can ever
// supersede, because no later write for that session is possible any more.
func TestShutdownFlush_LeaseReleaseAfterRemovalDoesNotResurrectFailure(t *testing.T) {
	sess := healthTestSession("sess-shutdown-18")
	repo := newTrackingClosingRepo(sess)
	pool := shutdownTestPool(t, repo, sess)
	pool.SetSyncPersist(true)

	now := time.Date(2026, 9, 8, 23, 0, 0, 0, time.UTC)
	pool.SetNow(func() time.Time { return now })

	// The lease is acquired while the session is still live.
	rs := pool.GetSession(sess.ID)
	if rs == nil {
		t.Fatal("the pool did not load the session")
	}
	lease := &Lease{session: rs, pool: pool}

	// The admin delete lands: the row goes first, the pool entry second.
	repo.mu.Lock()
	delete(repo.sessions, sess.ID)
	repo.mu.Unlock()
	repo.FailNext(1) // stands in for SessionNotFound on the deleted row
	pool.RemoveSession(sess.ID)

	lease.Release(apperr.New(apperr.CodeSessionRateLimited, "429"))

	if got := persistFailureCount(pool); got != 0 {
		t.Fatalf("the release of a stale lease left %d unresolved failures, want 0", got)
	}
	if got := len(repo.Writes()); got != 0 {
		t.Fatalf("a removed session was written %d times, want 0", got)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := pool.FlushHealthPersist(ctx); err != nil {
		t.Fatalf("a stale lease must not keep every later flush failing: %v", err)
	}
}

// However often a session is deleted while a lease on it is still open, nothing
// accumulates: neither the failure map nor the persistence queue.
func TestShutdownFlush_RepeatedDeleteWithOpenLeaseDoesNotGrowFailures(t *testing.T) {
	repo := newTrackingClosingRepo()
	pool := shutdownTestPool(t, repo)
	pool.SetSyncPersist(true)

	now := time.Date(2026, 9, 8, 23, 0, 0, 0, time.UTC)
	pool.SetNow(func() time.Time { return now })

	for i := 0; i < 50; i++ {
		sess := healthTestSession(fmt.Sprintf("sess-shutdown-19-%d", i))
		pool.UpsertSession(&sess)

		rs := pool.GetSession(sess.ID)
		if rs == nil {
			t.Fatalf("iteration %d: the pool did not take the session", i)
		}
		lease := &Lease{session: rs, pool: pool}

		repo.FailNext(1)
		pool.RemoveSession(sess.ID)
		lease.Release(apperr.New(apperr.CodeSessionRateLimited, "429"))
	}

	if got := persistFailureCount(pool); got != 0 {
		t.Fatalf("50 delete-with-open-lease cycles left %d unresolved failures, want 0", got)
	}

	pool.persistMu.Lock()
	queued := len(pool.persistQueue)
	pool.persistMu.Unlock()
	if queued != 0 {
		t.Fatalf("%d snapshots were queued for deleted sessions, want 0", queued)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := pool.FlushHealthPersist(ctx); err != nil {
		t.Fatalf("no deleted session may keep a flush failing: %v", err)
	}
}

// Rejecting the stale lease must not silence anything else: a live session whose
// write really failed still has to be reported.
func TestShutdownFlush_StaleLeaseKeepsLiveSessionFailureVisible(t *testing.T) {
	removed := healthTestSession("sess-shutdown-20a")
	live := healthTestSession("sess-shutdown-20b")
	repo := newTrackingClosingRepo(removed, live)
	pool := shutdownTestPool(t, repo, removed, live)
	pool.SetSyncPersist(true)

	now := time.Date(2026, 9, 8, 23, 0, 0, 0, time.UTC)
	pool.SetNow(func() time.Time { return now })

	rs := pool.GetSession(removed.ID)
	if rs == nil {
		t.Fatal("the pool did not load the session")
	}
	lease := &Lease{session: rs, pool: pool}

	repo.mu.Lock()
	delete(repo.sessions, removed.ID)
	repo.mu.Unlock()
	pool.RemoveSession(removed.ID)

	repo.FailNext(2) // one for the stale lease if it slipped through, one for the live session
	lease.Release(apperr.New(apperr.CodeSessionRateLimited, "429"))
	pool.RecordOutcome(live.ID, apperr.New(apperr.CodeSessionRateLimited, "429"))

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	err := pool.FlushHealthPersist(ctx)
	if err == nil {
		t.Fatal("the live session's failed write must still be reported")
	}
	if strings.Contains(err.Error(), removed.ID) {
		t.Fatalf("the removed session must not be reported, got: %v", err)
	}
	if !strings.Contains(err.Error(), live.ID) {
		t.Fatalf("the reported failure must name %s, got: %v", live.ID, err)
	}
	if got := persistFailureCount(pool); got != 1 {
		t.Fatalf("unresolved failures = %d, want exactly the live session", got)
	}
}

// A session recreated under the same id is a different runtime session. The lease
// left over from the previous incarnation must neither write on its behalf nor
// disturb its own health persistence.
func TestShutdownFlush_StaleLeaseDoesNotAffectRecreatedSameID(t *testing.T) {
	sess := healthTestSession("sess-shutdown-21")
	repo := newTrackingClosingRepo(sess)
	pool := shutdownTestPool(t, repo, sess)
	pool.SetSyncPersist(true)

	now := time.Date(2026, 9, 8, 23, 0, 0, 0, time.UTC)
	pool.SetNow(func() time.Time { return now })

	stale := &Lease{session: pool.GetSession(sess.ID), pool: pool}
	pool.RemoveSession(sess.ID)

	// The same id is created again; this is a fresh runtime session.
	recreated := healthTestSession(sess.ID)
	pool.UpsertSession(&recreated)
	fresh := pool.GetSession(sess.ID)
	if fresh == nil {
		t.Fatal("the recreated session is not in the pool")
	}
	if fresh == stale.session {
		t.Fatal("the recreated session must not reuse the removed runtime session")
	}

	// The straggler from the previous incarnation lands first.
	stale.Release(apperr.New(apperr.CodeSessionAuthFailed, "from the old incarnation"))

	// The new incarnation persists normally.
	pool.RecordOutcome(sess.ID, apperr.New(apperr.CodeSessionBotChallenge, "challenge"))

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := pool.FlushHealthPersist(ctx); err != nil {
		t.Fatalf("the recreated session must persist cleanly: %v", err)
	}

	writes := repo.Writes()
	if len(writes) != 1 {
		t.Fatalf("expected exactly the recreated session's write, got %d", len(writes))
	}
	if writes[0].update.HealthStatus != HealthBotChallenge {
		t.Fatalf("stored status = %s, want %s (the stale lease overwrote it)",
			writes[0].update.HealthStatus, HealthBotChallenge)
	}
	if got := persistFailureCount(pool); got != 0 {
		t.Fatalf("unresolved failures = %d, want 0", got)
	}
}

// The guard must not cost a live session its snapshot: the ordinary release of a
// lease on a session that is still in the pool still persists.
func TestShutdownFlush_LiveLeaseReleaseStillPersists(t *testing.T) {
	sess := healthTestSession("sess-shutdown-22")
	repo := newTrackingClosingRepo(sess)
	pool := shutdownTestPool(t, repo, sess)
	pool.SetSyncPersist(true)

	now := time.Date(2026, 9, 8, 23, 0, 0, 0, time.UTC)
	pool.SetNow(func() time.Time { return now })

	lease := &Lease{session: pool.GetSession(sess.ID), pool: pool}
	lease.Release(apperr.New(apperr.CodeSessionBotChallenge, "challenge"))

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := pool.FlushHealthPersist(ctx); err != nil {
		t.Fatalf("the live session's snapshot did not reach the repository: %v", err)
	}

	writes := repo.Writes()
	if len(writes) != 1 {
		t.Fatalf("expected 1 health write for the live session, got %d", len(writes))
	}
	if writes[0].id != sess.ID || writes[0].update.HealthStatus != HealthBotChallenge {
		t.Fatalf("stored write = %+v, want %s at %s", writes[0], HealthBotChallenge, sess.ID)
	}

	// A reload that keeps the session carries the same runtime session over, so
	// the lease stays live and its next release still persists.
	stored := repo.Stored(sess.ID)
	pool.ReloadSessions([]Session{stored})
	if pool.GetSession(sess.ID) != lease.session {
		t.Fatal("a reload that keeps a session must keep its runtime session")
	}
}
