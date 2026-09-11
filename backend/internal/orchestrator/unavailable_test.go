package orchestrator_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"ytdm/backend/internal/apperr"
	"ytdm/backend/internal/mediasession"
	"ytdm/backend/internal/provider"
	"ytdm/backend/internal/ytdlp"
)

// "This video is unavailable", classified by the real taxonomy from the
// message in the 0.27.2-rc.1 log (id replaced). Before the fix it paused the
// whole YouTube family like a provider outage.
func thisVideoIsUnavailable(id string) error {
	return classified("ERROR: [youtube] " + id + ": This video is unavailable")
}

// The unavailable video fails on its own and the next suitable candidate is
// resolved; no pause, the session is neither charged nor healed.
func TestUnavailableCandidateFallsBackWithoutFamilyPause(t *testing.T) {
	before := penalizedSession(time.Date(2026, 9, 1, 10, 0, 0, 0, time.UTC))
	orch, pool, ytm, _, cooldown := setupTestEnvironment(t, before)
	ytm.SetCandidates(abbaCandidates("ytmusic"))
	ytm.SetResolveErr("vid-1", thisVideoIsUnavailable("vid-1"))

	res, err := orch.ResolveMedia(context.Background(), "ytmusic", auditTrackABBA(), 5)
	if err != nil {
		t.Fatalf("ResolveMedia: %v", err)
	}
	if res.Candidate.ID != "vid-2" || ytm.ResolveCalls() != 2 {
		t.Fatalf("resolved %s after %d resolves, want vid-2 after 2", res.Candidate.ID, ytm.ResolveCalls())
	}
	assertNoFamilyPause(t, pool, cooldown)
	after := pool.Sessions()[0]
	assertSessionHealthUnchanged(t, after, before)
	if after.LastSuccessAt != nil {
		t.Fatal("last_success_at changed")
	}
}

// All candidates unavailable (and restricted): the existing permanent result,
// no pause, each candidate once, the single lease free for the next item.
func TestAllCandidatesUnavailableEndCleanly(t *testing.T) {
	before := penalizedSession(time.Date(2026, 9, 1, 10, 0, 0, 0, time.UTC))
	orch, pool, ytm, yt, cooldown := setupTestEnvironment(t, before)
	ytm.SetCandidates(abbaCandidates("ytmusic"))
	yt.SetCandidates(abbaCandidates("youtube"))
	ytm.SetResolveErr("vid-1", thisVideoIsUnavailable("vid-1"))
	ytm.SetResolveErr("vid-2", ageRestricted())
	ytm.SetResolveErr("vid-3", thisVideoIsUnavailable("vid-3"))

	_, err := orch.ResolveMedia(context.Background(), "ytmusic", auditTrackABBA(), 5)
	if apperr.CodeOf(err) != apperr.CodeTrackNotFound || apperr.Retryable(err) {
		t.Fatalf("result = %v (retryable %v), want a permanent %s", err, apperr.Retryable(err), apperr.CodeTrackNotFound)
	}
	if !errors.Is(err, ytdlp.ErrItemUnavailable) {
		t.Fatalf("the cause is lost: %v", err)
	}
	if ytm.ResolveCalls() != 3 || yt.ResolveCalls() != 0 {
		t.Fatalf("resolve calls ytmusic=%d youtube=%d, want 3 and 0", ytm.ResolveCalls(), yt.ResolveCalls())
	}
	assertNoFamilyPause(t, pool, cooldown)
	assertSessionHealthUnchanged(t, pool.Sessions()[0], before)
	if pool.Sessions()[0].LastSuccessAt != nil {
		t.Fatal("last_success_at changed")
	}
	assertNoLeaseHeld(t, pool)

	ytm.SetCandidates([]provider.MediaCandidate{
		{Provider: "ytmusic", ID: "vid-9", Title: "Dancing Queen", Artists: []string{"ABBA"}, DurationMS: 231000},
	})
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if _, err := orch.ResolveMedia(ctx, "ytmusic", auditTrackABBA(), 5); err != nil {
		t.Fatalf("the next item did not run: %v", err)
	}
}

// Mixed messages keep the existing protection.
func TestUnavailableWithProtectionSignalKeepsProtection(t *testing.T) {
	for _, tc := range []struct {
		name       string
		stderr     string
		wantCode   apperr.Code
		wantHealth mediasession.HealthStatus
		wantPause  bool
	}{
		{"http 429", "ERROR: [youtube] vid-1: This video is unavailable: HTTP Error 429: Too Many Requests", apperr.CodeProviderRateLimited, mediasession.HealthHealthy, true},
		{"bot challenge", "ERROR: [youtube] vid-1: This video is unavailable. Sign in to confirm you're not a bot", apperr.CodeSessionBotChallenge, mediasession.HealthBotChallenge, false},
		{"auth", "ERROR: [youtube] vid-1: This video is unavailable. Your cookies are expired", apperr.CodeSessionAuthFailed, mediasession.HealthAuthFailed, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			orch, pool, ytm, _, cooldown := setupTestEnvironment(t, healthyAuditSession())
			ytm.SetCandidates(abbaCandidates("ytmusic"))
			ytm.SetResolveErr("vid-1", classified(tc.stderr))

			_, err := orch.ResolveMedia(context.Background(), "ytmusic", auditTrackABBA(), 5)
			if apperr.CodeOf(err) != tc.wantCode || ytm.ResolveCalls() != 1 {
				t.Fatalf("code = %s after %d resolves, want %s after 1", apperr.CodeOf(err), ytm.ResolveCalls(), tc.wantCode)
			}
			if got := pool.Sessions()[0].HealthStatus; got != tc.wantHealth {
				t.Fatalf("session health = %s, want %s", got, tc.wantHealth)
			}
			if _, cooling := cooldown.Remaining("youtube"); cooling != tc.wantPause {
				t.Fatalf("family pause = %v, want %v", cooling, tc.wantPause)
			}
		})
	}
}
