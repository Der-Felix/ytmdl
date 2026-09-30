package orchestrator_test

import (
	"testing"

	"ytdm/backend/internal/apperr"
	"ytdm/backend/internal/mediasession"
	"ytdm/backend/internal/provider"
)

// resolvedCombinedSession resolves a combined source through the real
// orchestrator, so the session holds the data-plane reference a download
// outcome has to give back.
func resolvedCombinedSession(t *testing.T) (*mediasession.SessionPool, *mockCooldown, string, func(error)) {
	t.Helper()
	_, pool, ytm, yt, sc, cooldown := setupPreRoutingEnv(t, healthyAuditSession())
	ytm.SetCandidates([]provider.MediaCandidate{auditCandidate("ytmusic", "a")})
	ytm.SetSource("a", &provider.MediaSource{
		Provider: "ytmusic", ID: "a", URL: "https://ytmusic.example/a", DurationMS: 200000,
		Formats: []provider.AudioFormat{{
			ID: "18", Codec: "mp4a.40.2", Container: "m4a", Combined: true,
			VideoCodec: "avc1.42001E", BitrateKbps: 96, TransferBitrateKbps: 500,
		}},
	})
	orch := newOrchestrator(pool, cooldown, ytm, yt, sc)
	res, err := orch.ResolveMedia(manualCtx(), "ytmusic", auditTrack(), 5)
	if err != nil {
		t.Fatalf("ResolveMedia: %v", err)
	}
	if res.SessionID == "" {
		t.Fatal("the resolved source lost its session affinity")
	}
	if refs := pool.GetSession(res.SessionID).DataPlaneRefs(); refs != 1 {
		t.Fatalf("data-plane refs after resolve = %d, want 1", refs)
	}
	record := func(err error) { orch.RecordDownloadOutcome(manualCtx(), res.SessionID, err) }
	return pool, cooldown, res.SessionID, record
}

// A combined transfer this backend stopped because of its own byte or time
// budget says nothing about YouTube: no family pause, no platform failure on
// the session pool, no mark on the session - and the session's data-plane
// reference is given back.
func TestTransferBudgetStopPausesNothing(t *testing.T) {
	for _, tc := range []struct {
		name string
		err  error
	}{
		{"byte budget", apperr.New(apperr.CodeTransferBudgetExceeded,
			"The combined stream exceeded the transfer budget of 1024 bytes and was stopped.")},
		{"time budget", apperr.New(apperr.CodeTransferBudgetExceeded,
			"The combined stream did not finish within the transfer time budget and was stopped.")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			pool, cooldown, sessionID, record := resolvedCombinedSession(t)
			before := pool.GetSession(sessionID).Session()

			record(tc.err)

			if _, cooling := cooldown.Remaining("youtube"); cooling {
				t.Fatal("a local budget stop paused the YouTube family")
			}
			lease, err := pool.Acquire(manualCtx())
			if err != nil {
				t.Fatalf("a local budget stop left a platform failure on the pool: %v", err)
			}
			lease.ReleaseNeutral()

			after := pool.GetSession(sessionID).Session()
			if after.HealthStatus != before.HealthStatus || after.ConsecutiveFailures != before.ConsecutiveFailures ||
				after.LastFailureAt != nil || after.LastSuccessAt != nil {
				t.Fatalf("session health changed: before %+v, after %+v", before, after)
			}
			if refs := pool.GetSession(sessionID).DataPlaneRefs(); refs != 0 {
				t.Fatalf("data-plane refs after the outcome = %d, want 0", refs)
			}
		})
	}
}

// The guard above is only meaningful if a real provider condition still has
// its effect: the same outcome path pauses the family and the pool.
func TestProviderFailureStillPausesTheFamily(t *testing.T) {
	pool, cooldown, sessionID, record := resolvedCombinedSession(t)

	record(apperr.New(apperr.CodeProviderUnavailable, "network error contacting media provider"))

	if _, cooling := cooldown.Remaining("youtube"); !cooling {
		t.Fatal("a provider failure no longer pauses the YouTube family")
	}
	if _, err := pool.Acquire(manualCtx()); apperr.CodeOf(err) != apperr.CodeSessionUnavailable {
		t.Fatalf("Acquire err = %v, want the platform pause", err)
	}
	if refs := pool.GetSession(sessionID).DataPlaneRefs(); refs != 0 {
		t.Fatalf("data-plane refs after the outcome = %d, want 0", refs)
	}
}
