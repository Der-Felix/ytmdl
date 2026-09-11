package orchestrator_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"ytdm/backend/internal/apperr"
	"ytdm/backend/internal/mediasession"
	"ytdm/backend/internal/music"
	"ytdm/backend/internal/provider"
	"ytdm/backend/internal/ytdlp"
)

// Age restrictions of single items, classified by the real yt-dlp taxonomy
// from the messages seen in production (ids replaced). Before the fix they
// were provider-systemic: every one paused the whole YouTube family, and the
// next attempt resolved the same candidate and paused it again.
func ageRestricted() error {
	return ytdlp.ClassifyError("ERROR: [youtube] vid-1: Sorry, this content is age-restricted", errors.New("exit status 1"))
}

func verifyYourAge() error {
	return ytdlp.ClassifyError("ERROR: [youtube] vid-2: Verify your age. Complete a brief check to show you're old enough to play this content. Learn more", errors.New("exit status 1"))
}

func classified(stderr string) error {
	return ytdlp.ClassifyError(stderr, errors.New("exit status 1"))
}

func assertNoFamilyPause(t *testing.T, pool *mediasession.SessionPool, cooldown *mockCooldown) {
	t.Helper()
	if _, cooling := cooldown.Remaining("youtube"); cooling {
		t.Fatal("an age-restricted candidate paused the YouTube family")
	}
	if pool.IsPlatformCooling() {
		t.Fatal("an age-restricted candidate recorded a platform-wide failure")
	}
	if pool.PlatformStrikes() != 0 {
		t.Fatalf("platform strikes = %d, want 0", pool.PlatformStrikes())
	}
}

func assertNoLeaseHeld(t *testing.T, pool *mediasession.SessionPool) {
	t.Helper()
	for _, rs := range pool.RuntimeSessions() {
		if n := rs.CurrentLeases(); n != 0 {
			t.Fatalf("session %s still holds %d lease(s)", rs.Session().ID, n)
		}
	}
}

func abbaCandidates(prov string) []provider.MediaCandidate {
	return []provider.MediaCandidate{
		{Provider: prov, ID: "vid-1", Title: "Dancing Queen", Artists: []string{"ABBA"}, DurationMS: 231000},
		{Provider: prov, ID: "vid-2", Title: "Dancing Queen (Official Audio)", Artists: []string{"ABBA"}, DurationMS: 231000},
		{Provider: prov, ID: "vid-3", Title: "Dancing Queen (Remastered)", Artists: []string{"ABBA"}, DurationMS: 231000},
	}
}

// An age-restricted candidate fails on its own; the next suitable candidate is
// resolved within the unchanged fallback bound, the family stays available and
// the session is neither charged nor healed.
func TestAgeRestrictedCandidateFallsBackWithoutFamilyPause(t *testing.T) {
	before := penalizedSession(time.Date(2026, 9, 1, 10, 0, 0, 0, time.UTC))
	orch, pool, ytm, _, cooldown := setupTestEnvironment(t, before)
	ytm.SetCandidates(abbaCandidates("ytmusic"))
	ytm.SetResolveErr("vid-1", ageRestricted())

	res, err := orch.ResolveMedia(context.Background(), "ytmusic", auditTrackABBA(), 5)
	if err != nil {
		t.Fatalf("ResolveMedia: %v", err)
	}
	if res.Candidate.ID != "vid-2" {
		t.Fatalf("resolved %s, want the next candidate vid-2", res.Candidate.ID)
	}
	if ytm.ResolveCalls() != 2 {
		t.Fatalf("resolve calls = %d, want 2", ytm.ResolveCalls())
	}
	assertNoFamilyPause(t, pool, cooldown)
	after := pool.Sessions()[0]
	assertSessionHealthUnchanged(t, after, before)
	if after.LastSuccessAt != nil {
		t.Fatal("a candidate failure and a resolve must not record a media success")
	}
}

// When every candidate is restricted the attempt ends with the existing
// "no candidate could be resolved" result: permanent, no family pause, every
// candidate resolved exactly once, the lease returned.
func TestAllCandidatesAgeRestrictedEndCleanly(t *testing.T) {
	before := penalizedSession(time.Date(2026, 9, 1, 10, 0, 0, 0, time.UTC))
	orch, pool, ytm, yt, cooldown := setupTestEnvironment(t, before)
	ytm.SetCandidates(abbaCandidates("ytmusic"))
	yt.SetCandidates(abbaCandidates("youtube")) // the YouTube search returns the same videos
	ytm.SetResolveErr("vid-1", ageRestricted())
	ytm.SetResolveErr("vid-2", verifyYourAge())
	ytm.SetResolveErr("vid-3", ageRestricted())

	_, err := orch.ResolveMedia(context.Background(), "ytmusic", auditTrackABBA(), 5)
	if apperr.CodeOf(err) != apperr.CodeTrackNotFound {
		t.Fatalf("code = %s, want %s (%v)", apperr.CodeOf(err), apperr.CodeTrackNotFound, err)
	}
	if apperr.Retryable(err) {
		t.Fatal("an attempt whose candidates are all restricted must not be scheduled for an immediate retry")
	}
	if !errors.Is(err, ytdlp.ErrAgeRestricted) {
		t.Fatalf("the cause is lost: %v", err)
	}
	if ytm.ResolveCalls() != 3 {
		t.Fatalf("ytmusic resolve calls = %d, want each candidate once (3)", ytm.ResolveCalls())
	}
	if yt.ResolveCalls() != 0 {
		t.Fatalf("youtube resolve calls = %d: a failed candidate was resolved again in the same attempt", yt.ResolveCalls())
	}
	assertNoFamilyPause(t, pool, cooldown)
	assertSessionHealthUnchanged(t, pool.Sessions()[0], before)
	if pool.Sessions()[0].LastSuccessAt != nil {
		t.Fatal("last_success_at changed")
	}
	assertNoLeaseHeld(t, pool)
}

// A direct-id candidate that is restricted falls back to the generic search
// without a pause, and the search does not resolve it a second time.
func TestAgeRestrictedDirectIDFallsBackToSearch(t *testing.T) {
	orch, pool, ytm, _, cooldown := setupTestEnvironment(t, healthyAuditSession())
	ytm.SetCandidates(abbaCandidates("ytmusic"))
	ytm.SetResolveErr("vid-1", ageRestricted())
	track := auditTrackABBA()
	track.SourceID = "vid-1"

	res, err := orch.ResolveMedia(context.Background(), "ytmusic", track, 5)
	if err != nil {
		t.Fatalf("ResolveMedia: %v", err)
	}
	if res.Candidate.ID != "vid-2" {
		t.Fatalf("resolved %s, want vid-2", res.Candidate.ID)
	}
	if ytm.ResolveCalls() != 2 {
		t.Fatalf("resolve calls = %d, want 2 (the restricted direct id only once)", ytm.ResolveCalls())
	}
	assertNoFamilyPause(t, pool, cooldown)
}

// The session's single lease is back after a restricted attempt, so the next
// eligible item runs at once instead of waiting for a pause.
func TestAgeRestrictedAttemptKeepsOtherWorkRunnable(t *testing.T) {
	orch, pool, ytm, _, cooldown := setupTestEnvironment(t, healthyAuditSession())
	ytm.SetCandidates(abbaCandidates("ytmusic")[:1])
	ytm.SetResolveErr("vid-1", ageRestricted())

	if _, err := orch.ResolveMedia(context.Background(), "ytmusic", auditTrackABBA(), 5); apperr.CodeOf(err) != apperr.CodeTrackNotFound {
		t.Fatalf("first attempt: %v", err)
	}
	assertNoLeaseHeld(t, pool)
	assertNoFamilyPause(t, pool, cooldown)

	// Another item, another candidate, same (single-lease) session.
	ytm.SetCandidates([]provider.MediaCandidate{
		{Provider: "ytmusic", ID: "vid-9", Title: "Dancing Queen", Artists: []string{"ABBA"}, DurationMS: 231000},
	})
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	res, err := orch.ResolveMedia(ctx, "ytmusic", auditTrackABBA(), 5)
	if err != nil {
		t.Fatalf("the next item did not run: %v", err)
	}
	if res.Candidate.ID != "vid-9" || res.SessionID != healthyAuditSession().ID {
		t.Fatalf("unexpected result: candidate %s, session %s", res.Candidate.ID, res.SessionID)
	}
}

// Bot challenge, sign-in/auth failure and HTTP 429 keep their protection even
// when the same output also states an age restriction.
func TestProtectionSignalsWithAgeStatementKeepProtection(t *testing.T) {
	for _, tc := range []struct {
		name       string
		stderr     string
		wantCode   apperr.Code
		wantHealth mediasession.HealthStatus
		wantPause  bool
	}{
		{"bot challenge", "ERROR: [youtube] vid-1: Sorry, this content is age-restricted. Sign in to confirm you're not a bot", apperr.CodeSessionBotChallenge, mediasession.HealthBotChallenge, false},
		{"sign in to confirm your age", "ERROR: [youtube] vid-1: Sign in to confirm your age. This video may be inappropriate for some users.", apperr.CodeSessionAuthFailed, mediasession.HealthAuthFailed, false},
		{"http 429", "ERROR: [youtube] vid-1: Sorry, this content is age-restricted: HTTP Error 429: Too Many Requests", apperr.CodeProviderRateLimited, mediasession.HealthHealthy, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			orch, pool, ytm, _, cooldown := setupTestEnvironment(t, healthyAuditSession())
			ytm.SetCandidates(abbaCandidates("ytmusic"))
			ytm.SetResolveErr("vid-1", classified(tc.stderr))

			_, err := orch.ResolveMedia(context.Background(), "ytmusic", auditTrackABBA(), 5)
			if apperr.CodeOf(err) != tc.wantCode {
				t.Fatalf("code = %s, want %s", apperr.CodeOf(err), tc.wantCode)
			}
			if ytm.ResolveCalls() != 1 {
				t.Fatalf("resolve calls = %d: candidate fan-out did not stop", ytm.ResolveCalls())
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

// An age statement that also asks for credentials is ambiguous: it keeps the
// previous, systemic handling instead of being weakened to a candidate failure.
func TestAmbiguousAgeStatementKeepsSystemicHandling(t *testing.T) {
	orch, _, ytm, _, cooldown := setupTestEnvironment(t, healthyAuditSession())
	ytm.SetCandidates(abbaCandidates("ytmusic"))
	ytm.SetResolveErr("vid-1", classified("ERROR: [youtube] vid-1: Sorry, this content is age-restricted. Use --cookies-from-browser or --cookies for the authentication"))

	_, err := orch.ResolveMedia(context.Background(), "ytmusic", auditTrackABBA(), 5)
	if apperr.CodeOf(err) != apperr.CodeProviderUnavailable {
		t.Fatalf("code = %s, want %s", apperr.CodeOf(err), apperr.CodeProviderUnavailable)
	}
	if ytm.ResolveCalls() != 1 {
		t.Fatalf("resolve calls = %d, want the fan-out stopped (1)", ytm.ResolveCalls())
	}
	if _, cooling := cooldown.Remaining("youtube"); !cooling {
		t.Fatal("the ambiguous message no longer pauses the family")
	}
}

// Locks that are already active stay active: a restricted candidate neither
// lifts a platform pause nor a penalised session's cooldown, and a restriction
// reported by the download changes nothing either.
func TestAgeRestrictionDoesNotLiftActiveLocks(t *testing.T) {
	now := time.Now()
	blockedUntil := now.Add(30 * time.Minute)
	blocked := mediasession.Session{
		ID:                  "sess-blocked",
		ProviderFamily:      provider.FamilyYouTube,
		Name:                "Blocked",
		CookieRef:           "managed://cookies/sess-blocked",
		Enabled:             true,
		HealthStatus:        mediasession.HealthBotChallenge,
		ConsecutiveFailures: 1,
		LastFailureAt:       &now,
		LastFailureReason:   "[SESSION_BOT_CHALLENGE] earlier",
		CooldownUntil:       &blockedUntil,
	}
	usable := penalizedSession(time.Date(2026, 9, 1, 10, 0, 0, 0, time.UTC))
	orch, pool, ytm, _, cooldown := setupTestEnvironment(t, usable, blocked)
	cooldown.Trigger("soundcloud", 10*time.Minute)
	ytm.SetCandidates(abbaCandidates("ytmusic")[:1])
	ytm.SetResolveErr("vid-1", ageRestricted())

	if _, err := orch.ResolveMedia(context.Background(), "ytmusic", auditTrackABBA(), 5); apperr.CodeOf(err) != apperr.CodeTrackNotFound {
		t.Fatalf("attempt: %v", err)
	}
	for _, s := range pool.Sessions() {
		switch s.ID {
		case blocked.ID:
			assertSessionHealthUnchanged(t, s, blocked)
		case usable.ID:
			assertSessionHealthUnchanged(t, s, usable)
		}
	}
	if _, cooling := cooldown.Remaining("soundcloud"); !cooling {
		t.Fatal("an unrelated active pause was lifted")
	}

	// A platform pause that is running survives a restriction reported later
	// by the download, and the restriction adds nothing to it.
	pool.RecordPlatformFailure(apperr.New(apperr.CodeProviderRateLimited, "HTTP Error 429"), 10*time.Minute)
	pause, _ := pool.LastPlatformFailure()
	strikes := pool.PlatformStrikes()
	orch.RecordDownloadOutcome(context.Background(), usable.ID, ageRestricted())
	if !pool.IsPlatformCooling() {
		t.Fatal("the running platform pause was lifted")
	}
	if got, _ := pool.LastPlatformFailure(); !got.CooldownUntil.Equal(pause.CooldownUntil) || apperr.CodeOf(got.Err) != apperr.CodeProviderRateLimited {
		t.Fatalf("platform pause changed: %+v", got)
	}
	if pool.PlatformStrikes() != strikes {
		t.Fatalf("platform strikes = %d, want %d", pool.PlatformStrikes(), strikes)
	}
	if _, cooling := cooldown.Remaining("youtube"); cooling {
		t.Fatal("a restriction reported by the download paused the family")
	}
	for _, s := range pool.Sessions() {
		if s.ID == usable.ID {
			assertSessionHealthUnchanged(t, s, usable)
			if s.LastSuccessAt != nil {
				t.Fatal("last_success_at changed")
			}
		}
	}
}

func auditTrackABBA() music.Track {
	return music.Track{Title: "Dancing Queen", Artists: []string{"ABBA"}, DurationMS: 231000}
}
