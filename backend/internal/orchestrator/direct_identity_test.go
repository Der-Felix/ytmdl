package orchestrator_test

import (
	"context"
	"errors"
	"testing"
	"ytdm/backend/internal/apperr"
	"ytdm/backend/internal/provider"
	"ytdm/backend/internal/ytdlp"
)

func TestDirectSearchFallbackCannotBypassMatching(t *testing.T) {
	for _, tc := range []struct {
		name, id string
		duration int
	}{
		{"another video with misleading title", "other", 231000},
		{"complete movement at the same source", "direct", 863001},
	} {
		t.Run(tc.name, func(t *testing.T) {
			orch, pool, ytm, _, cooldown := setupTestEnvironment(t, healthyAuditSession())
			track := auditTrackABBA()
			track.SourceProvider = "ytmusic"
			track.SourceID = "direct"
			ytm.SetCandidates([]provider.MediaCandidate{{Provider: "ytmusic", ID: tc.id, Title: "Different Title", Artists: []string{"Someone Else"}, DurationMS: tc.duration}})
			_, err := orch.ResolveMedia(context.Background(), "ytmusic", track, 5)
			if apperr.CodeOf(err) != apperr.CodeMatchFailed {
				t.Fatalf("untrusted hit bypassed matching: %v", err)
			}
			if ytm.ResolveCalls() != 0 {
				t.Fatal("wrong recording was resolved")
			}
			assertNoFamilyPause(t, pool, cooldown)
			assertNoLeaseHeld(t, pool)
		})
	}
}

func TestKnownSourceFailureIsNotHiddenByWeakSearchHits(t *testing.T) {
	for _, stderr := range []string{
		"ERROR: [youtube] direct: This video is unavailable",
		"ERROR: [youtube] direct: Sign in to confirm your age",
	} {
		_, pool, ytm, yt, sc, cooldown := setupPreRoutingEnv(t, healthyAuditSession())
		cause := classified(stderr)
		ytm.SetSearchErr(cause)
		yt.SetCandidates([]provider.MediaCandidate{{Provider: "youtube", ID: "unrelated", Title: "Another Recording"}})
		track := auditTrackABBA()
		track.SourceID = "direct"
		track.SourceProvider = "ytmusic"
		orch := newOrchestrator(pool, cooldown, ytm, yt, sc)
		_, err := orch.ResolveMedia(context.Background(), "ytmusic", track, 5)
		if apperr.CodeOf(err) != apperr.CodeOf(cause) || apperr.MessageOf(err) != apperr.MessageOf(cause) {
			t.Fatalf("source cause hidden: %v", err)
		}
		if !errors.Is(err, ytdlp.ErrItemUnavailable) && !errors.Is(err, ytdlp.ErrAgeRestricted) {
			t.Fatal("typed cause lost")
		}
		assertNoFamilyPause(t, pool, cooldown)
		assertNoLeaseHeld(t, pool)
	}
}

func TestResolvedCandidateFailuresOutrankUnrelatedWeakHit(t *testing.T) {
	_, pool, ytm, yt, sc, cooldown := setupPreRoutingEnv(t, healthyAuditSession())
	ytm.SetCandidates([]provider.MediaCandidate{auditCandidate("ytmusic", "restricted")})
	ytm.SetResolveErr("restricted", ageRestricted())
	yt.SetCandidates([]provider.MediaCandidate{{Provider: "youtube", ID: "unrelated", Title: "Wrong Recording"}})
	orch := newOrchestrator(pool, cooldown, ytm, yt, sc)
	_, err := orch.ResolveMedia(manualCtx(), "ytmusic", auditTrack(), 5)
	if apperr.CodeOf(err) != apperr.CodeMediaAgeRestricted || !errors.Is(err, ytdlp.ErrAgeRestricted) {
		t.Fatalf("restricted source hidden by weak hit: %v", err)
	}
	assertNoFamilyPause(t, pool, cooldown)
	assertNoLeaseHeld(t, pool)
}
