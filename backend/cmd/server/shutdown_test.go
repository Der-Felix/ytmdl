package main

import (
	"bytes"
	"context"
	"io"
	"log/slog"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"ytdm/backend/internal/apperr"
	"ytdm/backend/internal/config"
	"ytdm/backend/internal/mediasession"
	"ytdm/backend/internal/provider"
)

// trackingHealthRepo implements mediasession.SessionRepository for testing shutdown.
type trackingHealthRepo struct {
	mu                  sync.Mutex
	sessions            map[string]mediasession.Session
	writes              []mediasession.HealthUpdate
	closed              bool
	inFlight            int
	closedWhileInFlight bool

	enteredWrite chan struct{}
	releaseWrite chan struct{}
}

func newTrackingHealthRepo(sessions ...mediasession.Session) *trackingHealthRepo {
	r := &trackingHealthRepo{
		sessions: make(map[string]mediasession.Session, len(sessions)),
	}
	for _, s := range sessions {
		r.sessions[s.ID] = s
	}
	return r
}

func (r *trackingHealthRepo) GetSession(_ context.Context, id string) (*mediasession.Session, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	s, ok := r.sessions[id]
	if !ok {
		return nil, apperr.New(apperr.CodeSessionNotFound, "not found")
	}
	return &s, nil
}

func (r *trackingHealthRepo) ListSessions(_ context.Context, _ mediasession.Filter) ([]mediasession.Session, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]mediasession.Session, 0, len(r.sessions))
	for _, s := range r.sessions {
		out = append(out, s)
	}
	return out, nil
}

func (r *trackingHealthRepo) UpdateHealth(ctx context.Context, id string, update mediasession.HealthUpdate) (*mediasession.Session, error) {
	r.mu.Lock()
	if r.closed {
		r.mu.Unlock()
		return nil, apperr.New(apperr.CodeInternal, "closed")
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
		return nil, apperr.New(apperr.CodeInternal, "closed while writing")
	}

	s := r.sessions[id]
	s.ID = id
	s.HealthStatus = update.HealthStatus
	s.CooldownUntil = update.CooldownUntil
	r.sessions[id] = s
	r.writes = append(r.writes, update)
	return &s, nil
}

func (r *trackingHealthRepo) Close() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.closed = true
	if r.inFlight > 0 {
		r.closedWhileInFlight = true
	}
}

// TestApplicationClose_OrderingAndFlush verifies that application.close():
// 1. flushes sessionPool health persistence
// 2. completes pending writes before database close
// 3. leaves no writes in flight when database closes
func TestApplicationClose_OrderingAndFlush(t *testing.T) {
	now := time.Date(2026, 9, 8, 23, 0, 0, 0, time.UTC)
	sess := mediasession.Session{
		ID:             "test-sess-1",
		ProviderFamily: provider.FamilyYouTube,
		HealthStatus:   mediasession.HealthHealthy,
	}
	repo := newTrackingHealthRepo(sess)

	poolCfg := mediasession.PoolConfig{
		Family:                provider.FamilyYouTube,
		MaxLeasesPerSession:   2,
		SessionRequestsPerSec: 100,
		SessionBurst:          100,
		GlobalRequestsPerSec:  100,
		GlobalBurst:           100,
		AllowUnknown:          true,
	}
	pool := mediasession.NewSessionPool(poolCfg, nil, repo, nil)
	pool.ReloadSessions([]mediasession.Session{sess})
	pool.SetSyncPersist(false) // async drainer
	pool.SetNow(func() time.Time { return now })

	// Enqueue a bot challenge failure asynchronously.
	pool.RecordOutcome(sess.ID, apperr.New(apperr.CodeSessionBotChallenge, "bot challenge"))

	app := &application{
		cfg: config.Config{
			Server: config.ServerConfig{
				ShutdownTimeout: 5 * time.Second,
			},
		},
		logger:      slog.New(slog.NewTextHandler(io.Discard, nil)),
		sessionPool: pool,
	}

	// Calling app.close() must flush the pending health write.
	app.close()
	repo.Close()

	if repo.closedWhileInFlight {
		t.Fatal("health write was still in flight when repository closed")
	}

	repo.mu.Lock()
	writesCount := len(repo.writes)
	stored := repo.sessions[sess.ID]
	repo.mu.Unlock()

	if writesCount != 1 {
		t.Fatalf("expected 1 write, got %d", writesCount)
	}
	if stored.HealthStatus != mediasession.HealthBotChallenge {
		t.Fatalf("stored status = %s, want %s", stored.HealthStatus, mediasession.HealthBotChallenge)
	}
}

// TestApplicationClose_SlowDBBounded verifies that a blocked repository does not
// cause app.close() to hang indefinitely.
func TestApplicationClose_SlowDBBounded(t *testing.T) {
	sess := mediasession.Session{
		ID:             "test-sess-2",
		ProviderFamily: provider.FamilyYouTube,
		HealthStatus:   mediasession.HealthHealthy,
	}
	repo := newTrackingHealthRepo(sess)
	repo.enteredWrite = make(chan struct{}, 1)
	repo.releaseWrite = make(chan struct{}) // blocked

	poolCfg := mediasession.PoolConfig{
		Family:                provider.FamilyYouTube,
		MaxLeasesPerSession:   2,
		SessionRequestsPerSec: 100,
		SessionBurst:          100,
		GlobalRequestsPerSec:  100,
		GlobalBurst:           100,
		AllowUnknown:          true,
	}
	pool := mediasession.NewSessionPool(poolCfg, nil, repo, nil)
	pool.ReloadSessions([]mediasession.Session{sess})
	pool.SetSyncPersist(false)

	pool.RecordOutcome(sess.ID, apperr.New(apperr.CodeSessionBotChallenge, "challenge"))
	<-repo.enteredWrite

	app := &application{
		cfg: config.Config{
			Server: config.ServerConfig{
				ShutdownTimeout: 50 * time.Millisecond,
			},
		},
		logger:      slog.New(slog.NewTextHandler(io.Discard, nil)),
		sessionPool: pool,
	}

	done := make(chan struct{})
	go func() {
		app.close()
		close(done)
	}()

	select {
	case <-done:
		// Succeeded: close completed boundedly
	case <-time.After(1 * time.Second):
		t.Fatal("app.close() hung indefinitely waiting for blocked health persist")
	}

	close(repo.releaseWrite)
}

// TestApplicationClose_ProducersStoppedBeforeFlush verifies that workers and producers
// are stopped before the final health flush runs, ensuring no post-flush health update
// can be enqueued.
func TestApplicationClose_ProducersStoppedBeforeFlush(t *testing.T) {
	now := time.Date(2026, 9, 8, 23, 0, 0, 0, time.UTC)
	sess := mediasession.Session{
		ID:             "test-sess-3",
		ProviderFamily: provider.FamilyYouTube,
		HealthStatus:   mediasession.HealthHealthy,
	}
	repo := newTrackingHealthRepo(sess)

	poolCfg := mediasession.PoolConfig{
		Family:                provider.FamilyYouTube,
		MaxLeasesPerSession:   2,
		SessionRequestsPerSec: 100,
		SessionBurst:          100,
		GlobalRequestsPerSec:  100,
		GlobalBurst:           100,
		AllowUnknown:          true,
	}
	pool := mediasession.NewSessionPool(poolCfg, nil, repo, nil)
	pool.ReloadSessions([]mediasession.Session{sess})
	pool.SetSyncPersist(false)
	pool.SetNow(func() time.Time { return now })

	app := &application{
		cfg: config.Config{
			Server: config.ServerConfig{
				ShutdownTimeout: 5 * time.Second,
			},
		},
		logger:      slog.New(slog.NewTextHandler(io.Discard, nil)),
		sessionPool: pool,
	}

	// Producer queues an update before shutdown
	pool.RecordOutcome(sess.ID, apperr.New(apperr.CodeSessionRateLimited, "rate limited"))

	// Close application
	app.close()
	repo.Close()

	// Persistence queue must be fully drained
	if err := pool.FlushHealthPersist(context.Background()); err != nil {
		t.Fatalf("FlushHealthPersist after close: %v", err)
	}

	repo.mu.Lock()
	count := len(repo.writes)
	repo.mu.Unlock()

	if count != 1 {
		t.Fatalf("expected 1 write completed, got %d", count)
	}
}

// The whole controlled shutdown must run on one absolute budget: the deadline is
// stamped once and the two tail reserves - worker quiescence, then the final
// health flush - are carved out of it rather than added to it.
func TestApplicationShutdownBudget_SingleDeadline(t *testing.T) {
	app := &application{
		cfg: config.Config{
			Server: config.ServerConfig{ShutdownTimeout: 30 * time.Second},
		},
		logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
	}

	deadline := app.beginShutdownBudget()
	if deadline.IsZero() {
		t.Fatal("beginShutdownBudget must stamp a deadline")
	}

	// A second caller (close after serve) must reuse the very same budget
	// instead of granting a fresh ShutdownTimeout.
	if again := app.beginShutdownBudget(); !again.Equal(deadline) {
		t.Fatalf("second beginShutdownBudget returned a new deadline %v, want %v", again, deadline)
	}

	reserve := app.phaseReserve()
	if reserve <= 0 {
		t.Fatalf("phase reserve must be positive, got %v", reserve)
	}
	if reserve > maxShutdownPhaseReserve {
		t.Fatalf("phase reserve %v exceeds cap %v", reserve, maxShutdownPhaseReserve)
	}
	if 2*reserve >= app.shutdownTimeout() {
		t.Fatalf("the two reserves (%v) must leave time for the drain phase (budget %v)", 2*reserve, app.shutdownTimeout())
	}

	// drain -> worker quiescence -> final flush, each strictly inside the one
	// deadline. The database teardown needs no reserve of its own: it only runs
	// once the workers and the health drainer have come to rest, and closing an
	// idle pool returns at once.
	drain, workers := app.drainDeadline(), app.workersDeadline()
	if !drain.Before(workers) || !workers.Before(deadline) {
		t.Fatalf("phases must be ordered drain(%v) < workers(%v) < deadline(%v)", drain, workers, deadline)
	}
	if got := deadline.Sub(workers); got != reserve {
		t.Fatalf("final health flush reserve = %v, want %v", got, reserve)
	}
	if got := workers.Sub(drain); got != reserve {
		t.Fatalf("worker quiescence reserve = %v, want %v", got, reserve)
	}
}

// A shutdown budget stamped by serve must also bound close: every phase runs
// inside the remaining budget, never on a second full ShutdownTimeout.
func TestApplicationClose_UsesRemainingBudgetOnly(t *testing.T) {
	sess := mediasession.Session{
		ID:             "test-sess-4",
		ProviderFamily: provider.FamilyYouTube,
		HealthStatus:   mediasession.HealthHealthy,
	}
	repo := newTrackingHealthRepo(sess)
	repo.enteredWrite = make(chan struct{}, 1)
	repo.releaseWrite = make(chan struct{}) // never released: the flush can only time out

	pool := shutdownTestPool(repo, sess)
	pool.SetSyncPersist(false)

	pool.RecordOutcome(sess.ID, apperr.New(apperr.CodeSessionBotChallenge, "challenge"))
	<-repo.enteredWrite

	// A generous per-shutdown budget that serve has almost entirely spent
	// already. The old behaviour restarted the full timeout here.
	app := &application{
		cfg: config.Config{
			Server: config.ServerConfig{ShutdownTimeout: 10 * time.Second},
		},
		logger:      slog.New(slog.NewTextHandler(io.Discard, nil)),
		sessionPool: pool,
	}
	app.shutdownDeadline = time.Now().Add(300 * time.Millisecond)

	done := make(chan struct{})
	go func() {
		app.close()
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(app.shutdownTimeout()):
		t.Fatal("app.close() ignored the shared shutdown budget and started a fresh one")
	}

	// The real assertion: close finished by the stamped deadline, not merely
	// somewhere below the full ShutdownTimeout.
	if over := time.Since(app.shutdownDeadline); over > closeSlack {
		t.Fatalf("app.close() overran the shared shutdown deadline by %v (tolerance %v)", over, closeSlack)
	}

	close(repo.releaseWrite)
}

// close must not start the worker stop a second time, and must not wait on it
// without a bound.
func TestApplicationClose_WorkerStopIsBoundedAndRunsOnce(t *testing.T) {
	app := &application{
		cfg: config.Config{
			Server: config.ServerConfig{ShutdownTimeout: 200 * time.Millisecond},
		},
		logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
	}

	first := app.stopWorkers()
	second := app.stopWorkers()
	if first != second {
		t.Fatal("stopWorkers must run the stop sequence once and hand out the same channel")
	}

	select {
	case <-first:
	case <-time.After(2 * time.Second):
		t.Fatal("the worker stop sequence never finished")
	}

	// close over an already finished sequence stays well inside the budget.
	start := time.Now()
	app.close()
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Fatalf("app.close() took %v over an already stopped worker set", elapsed)
	}
}

// jobs.Manager.Stop ends with the interrupted-job requeue and the workers write
// their final item status on uncancellable contexts. close must let those finish
// before it tears the database down.
func TestApplicationClose_WaitsForFinalWorkerWritesBeforeDatabaseClose(t *testing.T) {
	app := &application{
		cfg: config.Config{
			Server: config.ServerConfig{ShutdownTimeout: 2 * time.Second},
		},
		logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
	}
	// Stand in for the stop sequence so the test controls when the workers'
	// final writes are done.
	workersStopped := installFakeWorkerStop(app)
	deadline := app.beginShutdownBudget()

	// Deliberately later than the drain deadline and earlier than the overall
	// one: only a wait in the teardown phase can observe this write.
	finish := time.Until(app.drainDeadline()) + 300*time.Millisecond
	var finalWriteDone atomic.Bool
	go func() {
		time.Sleep(finish)
		finalWriteDone.Store(true)
		close(workersStopped)
	}()

	app.close()

	if !finalWriteDone.Load() {
		t.Fatal("close() reached the database teardown while a final worker write was still running")
	}
	if time.Now().Before(app.drainDeadline()) {
		t.Fatal("close() returned before the drain deadline; the test did not exercise the worker reserve")
	}
	if over := time.Since(deadline); over > closeSlack {
		t.Fatalf("app.close() overran the shared shutdown deadline by %v (tolerance %v)", over, closeSlack)
	}
}

// When the budget is gone with writes still in flight, close must skip the pool
// close instead of blocking in pgxpool.Close: the process is exiting anyway, and
// a blocking close is exactly the unbounded tail the shared budget prevents.
func TestApplicationClose_ExhaustedBudgetSkipsDatabaseClose(t *testing.T) {
	var logs safeBuffer
	app := &application{
		cfg: config.Config{
			Server: config.ServerConfig{ShutdownTimeout: 400 * time.Millisecond},
		},
		logger: slog.New(slog.NewTextHandler(&logs, nil)),
	}
	// Workers that never come down.
	installFakeWorkerStop(app)

	app.close()

	if over := time.Since(app.shutdownDeadline); over > closeSlack {
		t.Fatalf("app.close() overran the shared shutdown deadline by %v (tolerance %v)", over, closeSlack)
	}
	if !strings.Contains(logs.String(), "leaving the pool to process exit") {
		t.Fatalf("close() must report that it skipped the database close; logs:\n%s", logs.String())
	}
}

// A flush that ends on its context leaves the drainer possibly holding a
// connection, so the database must not be closed underneath it either.
func TestApplicationClose_TimedOutFlushKeepsDatabaseOpen(t *testing.T) {
	sess := mediasession.Session{
		ID:             "test-sess-5",
		ProviderFamily: provider.FamilyYouTube,
		HealthStatus:   mediasession.HealthHealthy,
	}
	repo := newTrackingHealthRepo(sess)
	repo.enteredWrite = make(chan struct{}, 1)
	repo.releaseWrite = make(chan struct{}) // the drainer stays inside UpdateHealth

	pool := shutdownTestPool(repo, sess)
	pool.SetSyncPersist(false)
	pool.RecordOutcome(sess.ID, apperr.New(apperr.CodeSessionBotChallenge, "challenge"))
	<-repo.enteredWrite

	var logs safeBuffer
	app := &application{
		cfg: config.Config{
			Server: config.ServerConfig{ShutdownTimeout: 400 * time.Millisecond},
		},
		logger:      slog.New(slog.NewTextHandler(&logs, nil)),
		sessionPool: pool,
	}

	app.close()

	if over := time.Since(app.shutdownDeadline); over > closeSlack {
		t.Fatalf("app.close() overran the shared shutdown deadline by %v (tolerance %v)", over, closeSlack)
	}
	if !strings.Contains(logs.String(), "leaving the pool to process exit") {
		t.Fatalf("a timed-out flush must keep the pool open; logs:\n%s", logs.String())
	}

	close(repo.releaseWrite)
}

// The final flush must sit behind worker quiescence, not in front of it. A
// worker that records a session outcome on its way down queues a health snapshot
// after the drain deadline; only a flush that runs after the stop sequence has
// finished can still persist it.
func TestApplicationClose_FinalFlushRunsAfterWorkerQuiescence(t *testing.T) {
	sess := mediasession.Session{
		ID:             "test-sess-6",
		ProviderFamily: provider.FamilyYouTube,
		HealthStatus:   mediasession.HealthHealthy,
	}
	repo := newTrackingHealthRepo(sess)
	pool := shutdownTestPool(repo, sess)
	pool.SetSyncPersist(false)

	app := &application{
		cfg: config.Config{
			Server: config.ServerConfig{ShutdownTimeout: 2 * time.Second},
		},
		logger:      slog.New(slog.NewTextHandler(io.Discard, nil)),
		sessionPool: pool,
	}
	workersStopped := installFakeWorkerStop(app)
	app.beginShutdownBudget()

	// Past the drain deadline and inside the worker reserve: the old ordering
	// flushed before this point and would have left the snapshot behind.
	enqueueAt := time.Until(app.drainDeadline()) + 300*time.Millisecond
	go func() {
		time.Sleep(enqueueAt)
		pool.RecordOutcome(sess.ID, apperr.New(apperr.CodeSessionBotChallenge, "challenge"))
		close(workersStopped)
	}()

	app.close()
	repo.Close()

	if repo.closedWhileInFlight {
		t.Fatal("the repository was closed while a health write was still in flight")
	}

	repo.mu.Lock()
	writes := len(repo.writes)
	stored := repo.sessions[sess.ID]
	repo.mu.Unlock()

	if writes != 1 {
		t.Fatalf("the snapshot queued during the worker stop was not flushed: %d writes, want 1", writes)
	}
	if stored.HealthStatus != mediasession.HealthBotChallenge {
		t.Fatalf("stored status = %s, want %s", stored.HealthStatus, mediasession.HealthBotChallenge)
	}
}

// Once close has flushed behind quiescent workers, that flush is final: nothing
// may queue another health write that could still be running while the pool is
// torn down.
func TestApplicationClose_NothingEnqueuesAfterTheFinalFlush(t *testing.T) {
	sess := mediasession.Session{
		ID:             "test-sess-7",
		ProviderFamily: provider.FamilyYouTube,
		HealthStatus:   mediasession.HealthHealthy,
	}
	repo := newTrackingHealthRepo(sess)
	pool := shutdownTestPool(repo, sess)
	pool.SetSyncPersist(false)

	app := &application{
		cfg: config.Config{
			Server: config.ServerConfig{ShutdownTimeout: 2 * time.Second},
		},
		logger:      slog.New(slog.NewTextHandler(io.Discard, nil)),
		sessionPool: pool,
	}
	close(installFakeWorkerStop(app)) // workers are already down

	pool.RecordOutcome(sess.ID, apperr.New(apperr.CodeSessionBotChallenge, "challenge"))
	app.close()

	repo.mu.Lock()
	afterClose := len(repo.writes)
	repo.mu.Unlock()
	if afterClose != 1 {
		t.Fatalf("the final flush persisted %d writes, want 1", afterClose)
	}

	// A straggler that outlived the drain must find the queue sealed.
	pool.RecordOutcome(sess.ID, apperr.New(apperr.CodeSessionAuthFailed, "too late"))
	if !pool.AwaitHealthPersistIdle(context.Background()) {
		t.Fatal("a health snapshot was queued after the final flush")
	}

	repo.mu.Lock()
	total := len(repo.writes)
	repo.mu.Unlock()
	if total != afterClose {
		t.Fatalf("a post-flush outcome reached the repository: %d writes, want %d", total, afterClose)
	}
}

// An exhausted budget over drained persistence is still quiescence: the flush
// returns at once without touching its context, so the expired context alone
// must not be read as "a write is in flight" and keep the pool open.
func TestApplicationClose_ExpiredBudgetOverDrainedQueueStillClosesDatabase(t *testing.T) {
	sess := mediasession.Session{
		ID:             "test-sess-8",
		ProviderFamily: provider.FamilyYouTube,
		HealthStatus:   mediasession.HealthHealthy,
	}
	repo := newTrackingHealthRepo(sess)
	pool := shutdownTestPool(repo, sess)
	pool.SetSyncPersist(true) // the snapshot is written before close even starts

	pool.RecordOutcome(sess.ID, apperr.New(apperr.CodeSessionBotChallenge, "challenge"))
	if err := pool.FlushHealthPersist(context.Background()); err != nil {
		t.Fatalf("preparatory flush: %v", err)
	}

	var logs safeBuffer
	app := &application{
		cfg: config.Config{
			Server: config.ServerConfig{ShutdownTimeout: time.Second},
		},
		logger:      slog.New(slog.NewTextHandler(&logs, nil)),
		sessionPool: pool,
	}
	close(installFakeWorkerStop(app))
	// Every phase deadline is already in the past.
	app.shutdownDeadline = time.Now().Add(-time.Second)

	if quiescent := app.flushSessionHealth(app.shutdownDeadline); !quiescent {
		t.Fatal("a drained queue must report quiescence even on an expired deadline")
	}

	app.close()

	if strings.Contains(logs.String(), "leaving the pool to process exit") {
		t.Fatalf("close() kept the pool open although nothing was in flight; logs:\n%s", logs.String())
	}
}

/* ------------------------------------------------------------- test helpers */

// closeSlack is the scheduling tolerance allowed on top of the stamped shutdown
// deadline. It is small on purpose: the point of these tests is that close()
// finishes on the shared deadline, not merely somewhere below ShutdownTimeout.
const closeSlack = 750 * time.Millisecond

// installFakeWorkerStop trips stopWorkersOnce with a channel the test owns, so
// stopWorkers hands that channel out instead of starting the real sequence.
func installFakeWorkerStop(app *application) chan struct{} {
	stopped := make(chan struct{})
	app.stopWorkersOnce.Do(func() { app.workersStopped = stopped })
	return stopped
}

func shutdownTestPool(repo mediasession.SessionRepository, sessions ...mediasession.Session) *mediasession.SessionPool {
	pool := mediasession.NewSessionPool(mediasession.PoolConfig{
		Family:                provider.FamilyYouTube,
		MaxLeasesPerSession:   2,
		SessionRequestsPerSec: 100,
		SessionBurst:          100,
		GlobalRequestsPerSec:  100,
		GlobalBurst:           100,
		AllowUnknown:          true,
	}, nil, repo, nil)
	pool.ReloadSessions(sessions)
	return pool
}

// safeBuffer collects log output from the shutdown goroutines.
type safeBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *safeBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *safeBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}
