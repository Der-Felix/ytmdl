package mediasession

import (
	"context"
	"testing"
	"time"

	"ytdm/backend/internal/apperr"
	"ytdm/backend/internal/provider"
	"ytdm/backend/internal/throughput"
)

// escalationPool is a single healthy session on a controllable clock with a
// neutral jitter, so pause lengths are exact.
func escalationPool(t *testing.T) (*SessionPool, *mockRepo, *time.Time) {
	t.Helper()
	now := time.Date(2026, 9, 11, 12, 0, 0, 0, time.UTC)
	s := Session{
		ID:             "session-escalation",
		ProviderFamily: provider.FamilyYouTube,
		Name:           "Escalation",
		CookieRef:      CookieRefPrefix + "session-escalation",
		Enabled:        true,
		HealthStatus:   HealthHealthy,
	}
	repo := newMockRepo([]Session{s})
	pool := NewSessionPool(DefaultPoolConfig(provider.FamilyYouTube), createTestStorage(t, s.ID), repo, nil)
	pool.SetNow(func() time.Time { return now })
	pool.SetSyncPersist(true)
	pool.setJitter(func() float64 { return 0.5 })
	pool.ReloadSessions([]Session{s})
	return pool, repo, &now
}

func rateLimited() error {
	return apperr.New(apperr.CodeProviderRateLimited, "account rate limited")
}

func pauseLength(t *testing.T, pool *SessionPool, now time.Time) time.Duration {
	t.Helper()
	fail, ok := pool.LastPlatformFailure()
	if !ok {
		t.Fatal("no platform failure recorded")
	}
	return fail.CooldownUntil.Sub(now)
}

func TestPlatformRateLimitPauseEscalatesAcrossExpiredPauses(t *testing.T) {
	pool, _, now := escalationPool(t)

	want := []time.Duration{2 * time.Minute, 4 * time.Minute, 6 * time.Minute, 6 * time.Minute}
	for i, step := range want {
		pool.RecordOutcome("session-escalation", rateLimited())
		if got := pauseLength(t, pool, *now); got != step {
			t.Fatalf("rate limit %d: pause %v, want %v", i+1, got, step)
		}
		// The next rate limit arrives right after this pause ended.
		*now = now.Add(step + time.Second)
	}
	if pool.PlatformStrikes() != 4 {
		t.Fatalf("strikes = %d", pool.PlatformStrikes())
	}
}

func TestInFlightRateLimitDuringPauseDoesNotEscalate(t *testing.T) {
	pool, _, now := escalationPool(t)

	pool.RecordOutcome("session-escalation", rateLimited())
	firstUntil, _ := pool.LastPlatformFailure()

	// A second worker's request was already running and reports the same
	// block a few seconds later.
	*now = now.Add(5 * time.Second)
	pool.RecordOutcome("session-escalation", rateLimited())
	if pool.PlatformStrikes() != 1 {
		t.Fatalf("an in-flight rate limit escalated the pause: strikes=%d", pool.PlatformStrikes())
	}
	second, _ := pool.LastPlatformFailure()
	if second.CooldownUntil.Sub(firstUntil.CooldownUntil) > 5*time.Second {
		t.Fatalf("pause grew by %v", second.CooldownUntil.Sub(firstUntil.CooldownUntil))
	}
}

func TestMilderFailureNeverShortensPause(t *testing.T) {
	pool, _, now := escalationPool(t)
	pool.RecordOutcome("session-escalation", rateLimited())
	until, _ := pool.LastPlatformFailure()

	*now = now.Add(10 * time.Second)
	pool.RecordOutcome("session-escalation", apperr.New(apperr.CodeProviderUnavailable, "network"))
	after, _ := pool.LastPlatformFailure()
	if after.CooldownUntil.Before(until.CooldownUntil) {
		t.Fatalf("pause shortened from %v to %v", until.CooldownUntil, after.CooldownUntil)
	}
}

func TestOnlyVerifiedAcquisitionResetsEscalation(t *testing.T) {
	pool, _, now := escalationPool(t)
	for i := 0; i < 3; i++ {
		pool.RecordOutcome("session-escalation", rateLimited())
		*now = now.Add(7 * time.Minute)
	}
	if pool.PlatformStrikes() != 3 {
		t.Fatalf("strikes = %d, want 3", pool.PlatformStrikes())
	}

	// Neutral lease releases, candidate failures and wait states prove nothing.
	lease, err := pool.Acquire(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	lease.ReleaseNeutral()
	pool.RecordOutcome("session-escalation", apperr.New(apperr.CodeTrackNotFound, "unavailable"))
	pool.RecordOutcome("session-escalation", apperr.NewRetryAfter(apperr.CodeSessionUnavailable, "paused", time.Minute))
	if pool.PlatformStrikes() != 3 {
		t.Fatalf("a non-acquisition reset the escalation: strikes=%d", pool.PlatformStrikes())
	}

	// A verified media acquisition does.
	pool.RecordOutcome("session-escalation", nil)
	if pool.PlatformStrikes() != 0 {
		t.Fatalf("strikes = %d after verified acquisition", pool.PlatformStrikes())
	}
	pool.RecordOutcome("session-escalation", rateLimited())
	if got := pauseLength(t, pool, *now); got != 2*time.Minute {
		t.Fatalf("pause after reset %v, want 2m", got)
	}
}

func TestEscalationForgottenAfterQuietPeriod(t *testing.T) {
	pool, _, now := escalationPool(t)
	pool.RecordOutcome("session-escalation", rateLimited())
	*now = now.Add(3 * time.Minute)
	pool.RecordOutcome("session-escalation", rateLimited())
	if pool.PlatformStrikes() != 2 {
		t.Fatalf("strikes = %d", pool.PlatformStrikes())
	}
	*now = now.Add(platformStrikeReset + time.Minute)
	pool.RecordOutcome("session-escalation", rateLimited())
	if got := pauseLength(t, pool, *now); got != 2*time.Minute {
		t.Fatalf("pause after quiet period %v, want 2m", got)
	}
}

func TestPauseJitterStaysBounded(t *testing.T) {
	for _, j := range []float64{0, 0.999999} {
		pool, _, now := escalationPool(t)
		pool.setJitter(func() float64 { return j })
		pool.RecordOutcome("session-escalation", rateLimited())
		got := pauseLength(t, pool, *now)
		if got < 108*time.Second || got > 132*time.Second {
			t.Fatalf("jitter %v produced pause %v outside 2m±10%%", j, got)
		}
	}
}

func TestLeaseGateRefusesRequestsDuringPlatformPause(t *testing.T) {
	pool, repo, now := escalationPool(t)
	rec := throughput.New()
	pool.SetRecorder(rec)

	lease, err := pool.Acquire(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	// Another worker's request meets a rate limit while this lease is held.
	pool.RecordOutcome("session-escalation", rateLimited())

	release, err := lease.Acquire(context.Background())
	if err == nil {
		release()
		t.Fatal("gate started a request during the platform pause")
	}
	if !apperr.IsSessionWait(err) {
		t.Fatalf("refusal is not a wait state: %v", err)
	}
	if wait, ok := apperr.RetryAfter(err); !ok || wait < 100*time.Second {
		t.Fatalf("refusal lacks the pause as retry hint: %v %v", wait, ok)
	}

	// The refusal is attributed to nobody.
	lease.Release(err)
	stored, _ := repo.GetSession(context.Background(), "session-escalation")
	if stored.HealthStatus != HealthHealthy || stored.ConsecutiveFailures != 0 {
		t.Fatalf("wait state charged the session: %+v", stored)
	}

	// After the pause the gate opens again.
	*now = now.Add(3 * time.Minute)
	lease2, err := pool.Acquire(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	release, err = lease2.Acquire(context.Background())
	if err != nil {
		t.Fatalf("gate still closed after the pause: %v", err)
	}
	release()
	lease2.ReleaseNeutral()

	counts := rec.Drain().Counts
	if counts["platform.youtube.PROVIDER_RATE_LIMITED"] != 1 || counts["platform.youtube.cooldown_ms"] != 120000 {
		t.Fatalf("counters: %v", counts)
	}
}
