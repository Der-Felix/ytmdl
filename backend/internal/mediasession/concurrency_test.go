package mediasession

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"ytdm/backend/internal/provider"
)

func TestSessionPool_ConcurrencyAndRace(t *testing.T) {
	storage := createTestStorage(t, "worker-session-1", "worker-session-2")
	sessions := []Session{
		{
			ID:             "worker-session-1",
			ProviderFamily: provider.FamilyYouTube,
			Name:           "Worker Session 1",
			CookieRef:      CookieRefPrefix + "worker-session-1",
			Enabled:        true,
			HealthStatus:   HealthHealthy,
		},
		{
			ID:             "worker-session-2",
			ProviderFamily: provider.FamilyYouTube,
			Name:           "Worker Session 2",
			CookieRef:      CookieRefPrefix + "worker-session-2",
			Enabled:        true,
			HealthStatus:   HealthHealthy,
		},
	}
	repo := newMockRepo(sessions)

	cfg := DefaultPoolConfig(provider.FamilyYouTube)
	cfg.MaxLeasesPerSession = 2
	cfg.GlobalRequestsPerSec = 1000 // speed up concurrency test
	cfg.SessionRequestsPerSec = 1000
	cfg.GlobalBurst = 50
	cfg.SessionBurst = 50

	pool := NewSessionPool(cfg, storage, repo, nil)
	pool.ReloadSessions(sessions)

	ctx := context.Background()
	var wg sync.WaitGroup
	var completedCount int64

	numGoroutines := 8
	iterations := 25

	for g := 0; g < numGoroutines; g++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for i := 0; i < iterations; i++ {
				lease, err := pool.Acquire(ctx)
				if err != nil {
					t.Errorf("worker %d iteration %d acquire failed: %v", id, i, err)
					return
				}

				// Simulate brief operation
				time.Sleep(time.Duration(id%3) * time.Millisecond)

				// Release lease
				lease.Release(nil)
				atomic.AddInt64(&completedCount, 1)
			}
		}(g)
	}

	wg.Wait()

	expectedTotal := int64(numGoroutines * iterations)
	if completedCount != expectedTotal {
		t.Errorf("completed %d acquisitions, want %d", completedCount, expectedTotal)
	}

	// Verify all runtime sessions have lease count exactly 0
	for _, rs := range pool.RuntimeSessions() {
		if cur := rs.CurrentLeases(); cur != 0 {
			t.Errorf("session %s leaked leases: currentLeases = %d, want 0", rs.Session().ID, cur)
		}
	}
}

func TestLease_DoubleReleaseSafety(t *testing.T) {
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
	pool := NewSessionPool(cfg, storage, repo, nil)
	pool.ReloadSessions(sessions)

	lease, err := pool.Acquire(context.Background())
	if err != nil {
		t.Fatalf("Acquire failed: %v", err)
	}

	// 1st release
	lease.Release(nil)

	// 2nd release (must be safe no-op via sync.Once)
	lease.Release(nil)

	// 3rd release
	lease.Release(nil)

	for _, rs := range pool.RuntimeSessions() {
		if cur := rs.CurrentLeases(); cur != 0 {
			t.Errorf("currentLeases = %d, want 0 (double release went negative or leaked)", cur)
		}
	}
}

func TestSessionPool_CancellationWhileWaiting(t *testing.T) {
	storage := createTestStorage(t, "cap-session")
	sessions := []Session{
		{
			ID:             "cap-session",
			ProviderFamily: provider.FamilyYouTube,
			Name:           "Capacity Session",
			CookieRef:      CookieRefPrefix + "cap-session",
			Enabled:        true,
			HealthStatus:   HealthHealthy,
		},
	}
	repo := newMockRepo(sessions)

	cfg := DefaultPoolConfig(provider.FamilyYouTube)
	cfg.MaxLeasesPerSession = 1
	pool := NewSessionPool(cfg, storage, repo, nil)
	pool.ReloadSessions(sessions)

	// Acquire sole lease
	lease, err := pool.Acquire(context.Background())
	if err != nil {
		t.Fatalf("Acquire 1 failed: %v", err)
	}
	defer lease.Release(nil)

	// Attempt second acquire with timeout
	ctxTimeout, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()

	_, err = pool.Acquire(ctxTimeout)
	if err != context.DeadlineExceeded {
		t.Errorf("expected context.DeadlineExceeded, got %v", err)
	}

	// Verify no orphan waiters remain
	pool.mu.Lock()
	waiterCount := len(pool.waiters)
	pool.mu.Unlock()

	if waiterCount != 0 {
		t.Errorf("waiter list length = %d, want 0 after cancellation", waiterCount)
	}
}

func TestSessionPool_SessionDisabledWhileLeased(t *testing.T) {
	storage := createTestStorage(t, "disabling-session")
	sessions := []Session{
		{
			ID:             "disabling-session",
			ProviderFamily: provider.FamilyYouTube,
			Name:           "Disabling Session",
			CookieRef:      CookieRefPrefix + "disabling-session",
			Enabled:        true,
			HealthStatus:   HealthHealthy,
		},
	}
	repo := newMockRepo(sessions)

	cfg := DefaultPoolConfig(provider.FamilyYouTube)
	pool := NewSessionPool(cfg, storage, repo, nil)
	pool.ReloadSessions(sessions)

	lease, err := pool.Acquire(context.Background())
	if err != nil {
		t.Fatalf("Acquire failed: %v", err)
	}

	// Disable session while lease is active
	s := lease.session.Session()
	s.Enabled = false
	lease.session.UpdateSession(s)

	// Release lease
	lease.Release(nil)

	// Subsequent acquire must fail because session is now disabled
	_, err = pool.Acquire(context.Background())
	if err == nil {
		t.Fatal("expected Acquire to fail on disabled session, got nil")
	}

	// Health status must remain healthy (not overwritten by disabled)
	if s.HealthStatus != HealthHealthy {
		t.Errorf("health_status = %q, want healthy", s.HealthStatus)
	}
}
