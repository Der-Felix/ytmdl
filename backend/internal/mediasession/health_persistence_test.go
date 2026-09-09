package mediasession

import (
	"context"
	"sync"
	"testing"
	"time"

	"ytdm/backend/internal/apperr"
	"ytdm/backend/internal/provider"
)

// healthWrite records one repository health persistence call in completion order.
type healthWrite struct {
	id     string
	update HealthUpdate
}

// blockingHealthRepo is a SessionRepository whose UpdateHealth can be held open
// deterministically, so tests can observe pool behaviour while a database write
// is in flight and can prove the persistence ordering.
type blockingHealthRepo struct {
	mu       sync.Mutex
	sessions map[string]Session
	writes   []healthWrite
	calls    int
	blockN   int

	entered chan struct{}
	release chan struct{}
}

func newBlockingHealthRepo(blockN int, sessions ...Session) *blockingHealthRepo {
	r := &blockingHealthRepo{
		sessions: make(map[string]Session, len(sessions)),
		blockN:   blockN,
		entered:  make(chan struct{}, 8),
		release:  make(chan struct{}),
	}
	for _, s := range sessions {
		r.sessions[s.ID] = s
	}
	return r
}

func (r *blockingHealthRepo) GetSession(_ context.Context, id string) (*Session, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	s, ok := r.sessions[id]
	if !ok {
		return nil, apperr.New(apperr.CodeSessionNotFound, "session not found")
	}
	return &s, nil
}

func (r *blockingHealthRepo) ListSessions(_ context.Context, _ Filter) ([]Session, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]Session, 0, len(r.sessions))
	for _, s := range r.sessions {
		out = append(out, s)
	}
	return out, nil
}

func (r *blockingHealthRepo) UpdateHealth(ctx context.Context, id string, update HealthUpdate) (*Session, error) {
	r.mu.Lock()
	r.calls++
	shouldBlock := r.calls <= r.blockN
	r.mu.Unlock()

	if shouldBlock {
		r.entered <- struct{}{}
		select {
		case <-r.release:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	s := r.sessions[id]
	s.ID = id
	// Mirrors repository.MediaSessions.UpdateHealth: every health column is
	// rewritten, so a field the update omits is nulled, not preserved.
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

func (r *blockingHealthRepo) Writes() []healthWrite {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]healthWrite, len(r.writes))
	copy(out, r.writes)
	return out
}

func (r *blockingHealthRepo) Stored(id string) Session {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.sessions[id]
}

func healthPersistTestPool(t *testing.T, repo SessionRepository, sessions ...Session) *SessionPool {
	t.Helper()
	ids := make([]string, 0, len(sessions))
	for _, s := range sessions {
		ids = append(ids, s.ID)
	}
	storage := createTestStorage(t, ids...)
	cfg := PoolConfig{
		Family:                provider.FamilyYouTube,
		MaxLeasesPerSession:   1,
		SessionRequestsPerSec: 100,
		SessionBurst:          10,
		GlobalRequestsPerSec:  100,
		GlobalBurst:           10,
		AllowUnknown:          true,
	}
	pool := NewSessionPool(cfg, storage, repo, nil)
	pool.ReloadSessions(sessions)
	return pool
}

func healthTestSession(id string) Session {
	return Session{
		ID:             id,
		ProviderFamily: provider.FamilyYouTube,
		Name:           id,
		CookieRef:      CookieRefPrefix + id,
		Enabled:        true,
		HealthStatus:   HealthHealthy,
	}
}

// mustNotBlock runs fn and fails the test if it does not return promptly. The
// passing path returns immediately; only a lock held across database I/O hangs.
func mustNotBlock(t *testing.T, what string, fn func()) {
	t.Helper()
	done := make(chan struct{})
	go func() {
		defer close(done)
		fn()
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatalf("%s did not return while a repository health write was in flight (pool lock held across DB I/O)", what)
	}
}

// FINDING 3 / CROSS-FINDING F: a blocking repository health write must not hold
// the family-wide pool mutex, so Availability and Acquire stay responsive.
func TestHealthPersist_BlockedRepositoryWrite_DoesNotBlockPool(t *testing.T) {
	sess := healthTestSession("sess-block")
	repo := newBlockingHealthRepo(1, sess)
	pool := healthPersistTestPool(t, repo, sess)

	now := time.Date(2026, 9, 8, 12, 0, 0, 0, time.UTC)
	pool.SetNow(func() time.Time { return now })

	recordDone := make(chan struct{})
	go func() {
		defer close(recordDone)
		// RecordFailure persists synchronously for its caller (F1), so this
		// goroutine stays blocked until the repository write is released.
		pool.RecordFailure(context.Background(), sess.ID, apperr.New(apperr.CodeSessionBotChallenge, "challenge"), now)
	}()

	// Deterministic: the repository write is now in flight.
	<-repo.entered

	mustNotBlock(t, "Availability", func() { _ = pool.Availability() })
	mustNotBlock(t, "Sessions", func() { _ = pool.Sessions() })
	mustNotBlock(t, "HasConfiguredSessions", func() { _ = pool.HasConfiguredSessions() })
	mustNotBlock(t, "Acquire", func() {
		lease, err := pool.Acquire(context.Background())
		if err == nil && lease != nil {
			lease.ReleaseNeutral()
		}
	})

	close(repo.release)
	<-recordDone

	if got := len(repo.Writes()); got != 1 {
		t.Fatalf("expected 1 health write, got %d", got)
	}
	if stored := repo.Stored(sess.ID); stored.HealthStatus != HealthBotChallenge {
		t.Fatalf("stored status = %s, want bot_challenge", stored.HealthStatus)
	}
}

// FINDING 3 / CROSS-FINDING G: consecutive health transitions persist strictly in
// transition order, so an older snapshot can never overwrite a newer one.
func TestHealthPersist_ConsecutiveTransitions_PersistInOrder(t *testing.T) {
	sess := healthTestSession("sess-order")
	repo := newBlockingHealthRepo(1, sess)
	pool := healthPersistTestPool(t, repo, sess)
	// Production configuration: callers do not wait for persistence.
	pool.SetSyncPersist(false)

	now := time.Date(2026, 9, 8, 12, 0, 0, 0, time.UTC)
	pool.SetNow(func() time.Time { return now })

	// Transition 1: rate limited (short cooldown).
	pool.RecordOutcome(sess.ID, apperr.New(apperr.CodeSessionRateLimited, "429"))
	// Deterministic: write 1 is in flight and cannot complete yet.
	<-repo.entered

	// Transition 2: bot challenge (24h cooldown) queues behind the in-flight write.
	pool.RecordOutcome(sess.ID, apperr.New(apperr.CodeSessionBotChallenge, "challenge"))

	close(repo.release)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := pool.FlushHealthPersist(ctx); err != nil {
		t.Fatalf("FlushHealthPersist: %v", err)
	}

	writes := repo.Writes()
	if len(writes) != 2 {
		t.Fatalf("expected 2 health writes, got %d", len(writes))
	}
	if writes[0].update.HealthStatus != HealthRateLimited {
		t.Fatalf("write[0] status = %s, want rate_limited", writes[0].update.HealthStatus)
	}
	if writes[1].update.HealthStatus != HealthBotChallenge {
		t.Fatalf("write[1] status = %s, want bot_challenge (newer snapshot must persist last)", writes[1].update.HealthStatus)
	}

	// The stale snapshot must not have overwritten the newer protection state.
	stored := repo.Stored(sess.ID)
	if stored.HealthStatus != HealthBotChallenge {
		t.Fatalf("stored status = %s, want bot_challenge", stored.HealthStatus)
	}
	if stored.CooldownUntil == nil || !stored.CooldownUntil.Equal(now.Add(botChallengeInitialCooldown)) {
		t.Fatalf("stored cooldown = %v, want %v (newer cooldown regressed)", stored.CooldownUntil, now.Add(botChallengeInitialCooldown))
	}

	// DB and in-memory runtime session must agree.
	inMemory := pool.GetSession(sess.ID).Session()
	if inMemory.HealthStatus != stored.HealthStatus {
		t.Fatalf("pool status (%s) != db status (%s)", inMemory.HealthStatus, stored.HealthStatus)
	}
	if inMemory.ConsecutiveFailures != stored.ConsecutiveFailures {
		t.Fatalf("pool failures (%d) != db failures (%d)", inMemory.ConsecutiveFailures, stored.ConsecutiveFailures)
	}
}

// FINDING 3: RecordFailure/RecordSuccess keep their synchronous contract for the
// caller (F1: the database and the pool agree when the call returns).
func TestHealthPersist_RecordFailureAndSuccess_RemainSynchronousForCaller(t *testing.T) {
	sess := healthTestSession("sess-sync")
	repo := newBlockingHealthRepo(0, sess)
	pool := healthPersistTestPool(t, repo, sess)
	pool.SetSyncPersist(false)

	now := time.Date(2026, 9, 8, 12, 0, 0, 0, time.UTC)
	pool.SetNow(func() time.Time { return now })

	pool.RecordFailure(context.Background(), sess.ID, apperr.New(apperr.CodeSessionBotChallenge, "challenge"), now)

	stored := repo.Stored(sess.ID)
	if stored.HealthStatus != HealthBotChallenge {
		t.Fatalf("db status after RecordFailure = %s, want bot_challenge", stored.HealthStatus)
	}
	if stored.ConsecutiveFailures != 1 {
		t.Fatalf("db failures after RecordFailure = %d, want 1", stored.ConsecutiveFailures)
	}
	if inMemory := pool.GetSession(sess.ID).Session(); inMemory.ConsecutiveFailures != stored.ConsecutiveFailures {
		t.Fatalf("pool failures (%d) != db failures (%d)", inMemory.ConsecutiveFailures, stored.ConsecutiveFailures)
	}

	pool.RecordSuccess(context.Background(), sess.ID, now.Add(time.Minute))

	stored = repo.Stored(sess.ID)
	if stored.HealthStatus != HealthHealthy {
		t.Fatalf("db status after RecordSuccess = %s, want healthy", stored.HealthStatus)
	}
	if stored.ConsecutiveFailures != 0 {
		t.Fatalf("db failures after RecordSuccess = %d, want 0", stored.ConsecutiveFailures)
	}
	if stored.CooldownUntil != nil {
		t.Fatalf("db cooldown after RecordSuccess = %v, want nil", stored.CooldownUntil)
	}
}

// FINDING 3: under concurrent health transitions the database converges on the
// pool's authoritative final state (single owner, ordered persistence).
func TestHealthPersist_ConcurrentTransitions_ConvergeWithPool(t *testing.T) {
	sess := healthTestSession("sess-concurrent")
	repo := newBlockingHealthRepo(0, sess)
	pool := healthPersistTestPool(t, repo, sess)
	pool.SetSyncPersist(false)

	now := time.Date(2026, 9, 8, 12, 0, 0, 0, time.UTC)
	pool.SetNow(func() time.Time { return now })

	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			if i%2 == 0 {
				pool.RecordOutcome(sess.ID, apperr.New(apperr.CodeSessionRateLimited, "429"))
			} else {
				_ = pool.Availability()
			}
		}(i)
	}
	wg.Wait()

	// A final authoritative success transition.
	pool.RecordSuccess(context.Background(), sess.ID, now.Add(time.Hour))

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := pool.FlushHealthPersist(ctx); err != nil {
		t.Fatalf("FlushHealthPersist: %v", err)
	}

	inMemory := pool.GetSession(sess.ID).Session()
	stored := repo.Stored(sess.ID)
	if stored.HealthStatus != inMemory.HealthStatus {
		t.Fatalf("db status (%s) != pool status (%s)", stored.HealthStatus, inMemory.HealthStatus)
	}
	if stored.ConsecutiveFailures != inMemory.ConsecutiveFailures {
		t.Fatalf("db failures (%d) != pool failures (%d)", stored.ConsecutiveFailures, inMemory.ConsecutiveFailures)
	}
	if stored.HealthStatus != HealthHealthy {
		t.Fatalf("final status = %s, want healthy", stored.HealthStatus)
	}
}

// FINDING 3: a neutral lease release frees the concurrency slot and wakes
// waiters without touching session health or writing to the repository.
func TestHealthPersist_NeutralRelease_NoHealthWriteNoPenalty(t *testing.T) {
	sess := healthTestSession("sess-neutral")
	sess.HealthStatus = HealthUnknown
	repo := newBlockingHealthRepo(0, sess)
	pool := healthPersistTestPool(t, repo, sess)

	now := time.Date(2026, 9, 8, 12, 0, 0, 0, time.UTC)
	pool.SetNow(func() time.Time { return now })

	lease, err := pool.Acquire(context.Background())
	if err != nil {
		t.Fatalf("Acquire: %v", err)
	}
	lease.ReleaseNeutral()

	if got := len(repo.Writes()); got != 0 {
		t.Fatalf("expected 0 health writes for a neutral release, got %d", got)
	}
	s := pool.GetSession(sess.ID).Session()
	if s.HealthStatus != HealthUnknown {
		t.Fatalf("status = %s, want unknown (neutral release must not certify health)", s.HealthStatus)
	}
	if s.ConsecutiveFailures != 0 {
		t.Fatalf("failures = %d, want 0", s.ConsecutiveFailures)
	}

	// The slot must be free again.
	lease2, err := pool.Acquire(context.Background())
	if err != nil {
		t.Fatalf("second Acquire after neutral release: %v", err)
	}
	lease2.ReleaseNeutral()
}
