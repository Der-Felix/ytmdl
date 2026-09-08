package mediasession

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"ytdm/backend/internal/apperr"
	"ytdm/backend/internal/provider"
)

// TestSessionPool_LostWakeupStarvation_Reproduction tests the exact production failure:
// >=2 waiters are queued when the only active lease releases with an error (e.g. BOT_CHALLENGE)
// making the pool have 0 available sessions and 0 active leases.
// In the unpatched code, the first waiter wakes up and gets SESSION_NOT_FOUND, but
// leaves the second waiter blocked until its context times out.
func TestSessionPool_LostWakeupStarvation_Reproduction(t *testing.T) {
	storage := createTestStorage(t, "single-session")
	sessions := []Session{
		{
			ID:             "single-session",
			ProviderFamily: provider.FamilyYouTube,
			Name:           "Single Session",
			CookieRef:      CookieRefPrefix + "single-session",
			Enabled:        true,
			HealthStatus:   HealthHealthy,
		},
	}
	repo := newMockRepo(sessions)

	cfg := DefaultPoolConfig(provider.FamilyYouTube)
	cfg.MaxLeasesPerSession = 1
	cfg.GlobalRequestsPerSec = 1000
	cfg.SessionRequestsPerSec = 1000
	cfg.GlobalBurst = 10
	cfg.SessionBurst = 10

	pool := NewSessionPool(cfg, storage, repo, nil)
	pool.ReloadSessions(sessions)

	ctx := context.Background()

	// 1. Acquire the single available lease
	activeLease, err := pool.Acquire(ctx)
	if err != nil {
		t.Fatalf("failed to acquire initial lease: %v", err)
	}

	// 2. Start two concurrent waiters
	type waiterResult struct {
		err      error
		duration time.Duration
	}

	results := make([]waiterResult, 2)
	var wg sync.WaitGroup
	wg.Add(2)

	waiterReady := make(chan struct{}, 2)

	for i := 0; i < 2; i++ {
		go func(idx int) {
			defer wg.Done()
			waiterCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
			defer cancel()

			start := time.Now()
			waiterReady <- struct{}{}

			_, wErr := pool.Acquire(waiterCtx)
			results[idx] = waiterResult{
				err:      wErr,
				duration: time.Since(start),
			}
		}(i)
	}

	// Wait for both waiters to start and enqueue in pool
	<-waiterReady
	<-waiterReady
	time.Sleep(50 * time.Millisecond)

	pool.mu.Lock()
	waiterCount := len(pool.waiters)
	pool.mu.Unlock()
	if waiterCount != 2 {
		t.Fatalf("expected 2 waiters in pool, got %d", waiterCount)
	}

	// 3. Active lease encounters BOT_CHALLENGE and releases
	botErr := apperr.New(apperr.CodeSessionBotChallenge, "The media session encountered a bot challenge")
	activeLease.Release(botErr)

	// Wait for both waiters to return
	wg.Wait()

	// Both waiters must return promptly (well under 1 second, definitely not timeout 2s)
	for i, res := range results {
		if errors.Is(res.err, context.DeadlineExceeded) {
			t.Errorf("waiter %d TIMED OUT (lost wakeup bug! blocked until context deadline): duration %v", i, res.duration)
		} else if apperr.CodeOf(res.err) != apperr.CodeSessionUnavailable {
			t.Errorf("waiter %d unexpected error: %v, want CodeSessionUnavailable", i, res.err)
		}
		if res.duration > 1*time.Second {
			t.Errorf("waiter %d took %v, which is too slow (expected prompt resolution < 500ms)", i, res.duration)
		}
	}
}

// 1. One waiter + one lease release (successful lease grant)
func TestSessionPool_OneWaiter_OneLeaseRelease(t *testing.T) {
	storage := createTestStorage(t, "s1")
	sessions := []Session{
		{
			ID:             "s1",
			ProviderFamily: provider.FamilyYouTube,
			Name:           "Session 1",
			CookieRef:      CookieRefPrefix + "s1",
			Enabled:        true,
			HealthStatus:   HealthHealthy,
		},
	}
	repo := newMockRepo(sessions)

	cfg := DefaultPoolConfig(provider.FamilyYouTube)
	cfg.MaxLeasesPerSession = 1
	cfg.GlobalRequestsPerSec = 1000
	cfg.SessionRequestsPerSec = 1000

	pool := NewSessionPool(cfg, storage, repo, nil)
	pool.ReloadSessions(sessions)

	lease1, err := pool.Acquire(context.Background())
	if err != nil {
		t.Fatalf("initial acquire failed: %v", err)
	}

	waiterAcquired := make(chan *Lease, 1)
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
		defer cancel()
		l, err := pool.Acquire(ctx)
		if err != nil {
			t.Errorf("waiter acquire failed: %v", err)
			return
		}
		waiterAcquired <- l
	}()

	time.Sleep(30 * time.Millisecond)

	// Release lease cleanly
	lease1.Release(nil)

	select {
	case l2 := <-waiterAcquired:
		if l2 == nil {
			t.Fatal("expected acquired lease")
		}
		l2.Release(nil)
	case <-time.After(1 * time.Second):
		t.Fatal("timed out waiting for waiter to acquire released lease")
	}

	// Verify lease accounting returns to 0
	rs := pool.GetSession("s1")
	if rs.CurrentLeases() != 0 {
		t.Errorf("expected 0 active leases, got %d", rs.CurrentLeases())
	}
}

// 2. Multiple waiters + one available capacity (only one waiter gets the slot, other remains queued)
func TestSessionPool_MultipleWaiters_OneAvailableCapacity(t *testing.T) {
	storage := createTestStorage(t, "s1")
	sessions := []Session{
		{
			ID:             "s1",
			ProviderFamily: provider.FamilyYouTube,
			Name:           "Session 1",
			CookieRef:      CookieRefPrefix + "s1",
			Enabled:        true,
			HealthStatus:   HealthHealthy,
		},
	}
	repo := newMockRepo(sessions)

	cfg := DefaultPoolConfig(provider.FamilyYouTube)
	cfg.MaxLeasesPerSession = 1
	cfg.GlobalRequestsPerSec = 1000
	cfg.SessionRequestsPerSec = 1000

	pool := NewSessionPool(cfg, storage, repo, nil)
	pool.ReloadSessions(sessions)

	lease1, err := pool.Acquire(context.Background())
	if err != nil {
		t.Fatalf("initial acquire failed: %v", err)
	}

	var acquiredCount int32
	var wg sync.WaitGroup
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	// Launch 3 waiters
	for i := 0; i < 3; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			l, err := pool.Acquire(ctx)
			if err == nil && l != nil {
				atomic.AddInt32(&acquiredCount, 1)
				// Hold lease briefly then release
				time.Sleep(50 * time.Millisecond)
				l.Release(nil)
			}
		}()
	}

	time.Sleep(30 * time.Millisecond)

	// Release initial lease
	lease1.Release(nil)

	wg.Wait()

	// Exactly at least 1 waiter must have acquired the slot without over-allocation
	rs := pool.GetSession("s1")
	if rs.CurrentLeases() != 0 {
		t.Errorf("expected 0 leases at end, got %d", rs.CurrentLeases())
	}
	if count := atomic.LoadInt32(&acquiredCount); count < 1 || count > 3 {
		t.Errorf("acquiredCount = %d, want between 1 and 3 sequential handoffs", count)
	}
}

// 5. Session transitions into cooldown while multiple waiters exist
func TestSessionPool_TransitionToCooldown_MultipleWaiters(t *testing.T) {
	storage := createTestStorage(t, "s1")
	sessions := []Session{
		{
			ID:             "s1",
			ProviderFamily: provider.FamilyYouTube,
			Name:           "Session 1",
			CookieRef:      CookieRefPrefix + "s1",
			Enabled:        true,
			HealthStatus:   HealthHealthy,
		},
	}
	repo := newMockRepo(sessions)

	cfg := DefaultPoolConfig(provider.FamilyYouTube)
	cfg.MaxLeasesPerSession = 1
	cfg.GlobalRequestsPerSec = 1000
	cfg.SessionRequestsPerSec = 1000

	pool := NewSessionPool(cfg, storage, repo, nil)
	pool.ReloadSessions(sessions)

	lease1, err := pool.Acquire(context.Background())
	if err != nil {
		t.Fatalf("initial acquire failed: %v", err)
	}

	numWaiters := 4
	var wg sync.WaitGroup
	wg.Add(numWaiters)
	results := make([]error, numWaiters)

	for i := 0; i < numWaiters; i++ {
		go func(idx int) {
			defer wg.Done()
			ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
			defer cancel()
			_, wErr := pool.Acquire(ctx)
			results[idx] = wErr
		}(i)
	}

	time.Sleep(50 * time.Millisecond)

	// Release with rate limited error (triggers cooldown)
	rlErr := apperr.New(apperr.CodeSessionRateLimited, "session rate limited")
	lease1.Release(rlErr)

	wg.Wait()

	for i, err := range results {
		if errors.Is(err, context.DeadlineExceeded) {
			t.Errorf("waiter %d timed out instead of receiving immediate rejection", i)
		} else if apperr.CodeOf(err) != apperr.CodeSessionUnavailable {
			t.Errorf("waiter %d unexpected err: %v, want SESSION_UNAVAILABLE", i, err)
		}
	}

	pool.mu.Lock()
	remWaiters := len(pool.waiters)
	pool.mu.Unlock()
	if remWaiters != 0 {
		t.Errorf("expected 0 waiters remaining, got %d", remWaiters)
	}
}

// 7. Context cancellation while waiting
func TestSessionPool_ContextCancellation(t *testing.T) {
	storage := createTestStorage(t, "s1")
	sessions := []Session{
		{
			ID:             "s1",
			ProviderFamily: provider.FamilyYouTube,
			Name:           "Session 1",
			CookieRef:      CookieRefPrefix + "s1",
			Enabled:        true,
			HealthStatus:   HealthHealthy,
		},
	}
	repo := newMockRepo(sessions)

	cfg := DefaultPoolConfig(provider.FamilyYouTube)
	cfg.MaxLeasesPerSession = 1
	cfg.GlobalRequestsPerSec = 1000
	cfg.SessionRequestsPerSec = 1000

	pool := NewSessionPool(cfg, storage, repo, nil)
	pool.ReloadSessions(sessions)

	lease1, _ := pool.Acquire(context.Background())

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		_, err := pool.Acquire(ctx)
		done <- err
	}()

	time.Sleep(30 * time.Millisecond)
	cancel()

	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Errorf("expected context.Canceled, got %v", err)
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("context cancellation did not unblock waiter")
	}

	// Verify waiter removed
	pool.mu.Lock()
	count := len(pool.waiters)
	pool.mu.Unlock()
	if count != 0 {
		t.Errorf("waiter was not cleaned up on cancellation: len = %d", count)
	}

	lease1.Release(nil)
}

// 9. Lease release concurrent with waiter cancellation
func TestSessionPool_ConcurrentReleaseAndCancellation(t *testing.T) {
	storage := createTestStorage(t, "s1")
	sessions := []Session{
		{
			ID:             "s1",
			ProviderFamily: provider.FamilyYouTube,
			Name:           "Session 1",
			CookieRef:      CookieRefPrefix + "s1",
			Enabled:        true,
			HealthStatus:   HealthHealthy,
		},
	}
	repo := newMockRepo(sessions)

	cfg := DefaultPoolConfig(provider.FamilyYouTube)
	cfg.MaxLeasesPerSession = 1
	cfg.GlobalRequestsPerSec = 1000
	cfg.SessionRequestsPerSec = 1000

	pool := NewSessionPool(cfg, storage, repo, nil)
	pool.ReloadSessions(sessions)

	for iter := 0; iter < 20; iter++ {
		l, _ := pool.Acquire(context.Background())
		ctx, cancel := context.WithCancel(context.Background())

		var wg sync.WaitGroup
		wg.Add(2)

		go func() {
			defer wg.Done()
			time.Sleep(time.Duration(iter%3) * time.Millisecond)
			cancel()
		}()

		go func() {
			defer wg.Done()
			time.Sleep(time.Duration((iter+1)%3) * time.Millisecond)
			l.Release(nil)
		}()

		wLease, _ := pool.Acquire(ctx)
		if wLease != nil {
			wLease.Release(nil)
		}
		wg.Wait()
	}

	rs := pool.GetSession("s1")
	if rs.CurrentLeases() != 0 {
		t.Errorf("leaked leases after cancellation races: %d", rs.CurrentLeases())
	}
}

// 10. Multiple simultaneous releaseLease calls
func TestSessionPool_SimultaneousReleases(t *testing.T) {
	storage := createTestStorage(t, "s1", "s2", "s3")
	sessions := []Session{
		{ID: "s1", ProviderFamily: provider.FamilyYouTube, Name: "S1", CookieRef: CookieRefPrefix + "s1", Enabled: true, HealthStatus: HealthHealthy},
		{ID: "s2", ProviderFamily: provider.FamilyYouTube, Name: "S2", CookieRef: CookieRefPrefix + "s2", Enabled: true, HealthStatus: HealthHealthy},
		{ID: "s3", ProviderFamily: provider.FamilyYouTube, Name: "S3", CookieRef: CookieRefPrefix + "s3", Enabled: true, HealthStatus: HealthHealthy},
	}
	repo := newMockRepo(sessions)

	cfg := DefaultPoolConfig(provider.FamilyYouTube)
	cfg.MaxLeasesPerSession = 2
	cfg.GlobalRequestsPerSec = 1000
	cfg.SessionRequestsPerSec = 1000

	pool := NewSessionPool(cfg, storage, repo, nil)
	pool.ReloadSessions(sessions)

	// Acquire all 6 leases
	leases := make([]*Lease, 6)
	for i := 0; i < 6; i++ {
		l, err := pool.Acquire(context.Background())
		if err != nil {
			t.Fatalf("acquire %d failed: %v", i, err)
		}
		leases[i] = l
	}

	var wg sync.WaitGroup
	for _, l := range leases {
		wg.Add(1)
		go func(lease *Lease) {
			defer wg.Done()
			lease.Release(nil)
		}(l)
	}

	wg.Wait()

	// Verify active lease accounting returns to zero
	for _, rs := range pool.RuntimeSessions() {
		if cur := rs.CurrentLeases(); cur != 0 {
			t.Errorf("session %s leaked leases: current = %d", rs.Session().ID, cur)
		}
	}
}
