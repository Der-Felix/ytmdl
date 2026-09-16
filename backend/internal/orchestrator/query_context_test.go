package orchestrator_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"ytdm/backend/internal/apperr"
	"ytdm/backend/internal/mediasession"
	"ytdm/backend/internal/provider/youtube"
	"ytdm/backend/internal/ytdlp"
)

// stalledSearch builds the real YouTube Music media provider on an offline
// yt-dlp stand-in whose search never answers in time. No provider is
// contacted.
func stalledSearch(t *testing.T) *youtube.MediaProvider {
	t.Helper()
	stub := filepath.Join(t.TempDir(), "yt-dlp")
	if err := os.WriteFile(stub, []byte("#!/bin/sh\nsleep 30\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	p, err := youtube.New(youtube.Config{
		Name:         "ytmusic",
		Mode:         youtube.SearchMusic,
		MusicService: true,
		Client:       ytdlp.New(ytdlp.Options{Binary: stub, Timeout: time.Minute, DisableQueryCache: true}),
	})
	if err != nil {
		t.Fatal(err)
	}
	return p
}

// requireNothingPaused fails when the attempt left a trace on the provider
// family, the session pool or the session itself.
func requireNothingPaused(t *testing.T, pool *mediasession.SessionPool, cooldown *mockCooldown, before mediasession.Session) {
	t.Helper()
	if _, cooling := cooldown.Remaining("youtube"); cooling {
		t.Fatal("the YouTube family was paused")
	}
	lease, err := pool.Acquire(manualCtx())
	if err != nil {
		t.Fatalf("the pool records a platform failure: %v", err)
	}
	lease.ReleaseNeutral()
	after := pool.GetSession(before.ID).Session()
	if after.HealthStatus != before.HealthStatus || after.ConsecutiveFailures != before.ConsecutiveFailures || after.LastFailureAt != nil {
		t.Fatalf("session health changed: before %+v, after %+v", before, after)
	}
	if refs := pool.GetSession(before.ID).DataPlaneRefs(); refs != 0 {
		t.Fatalf("data-plane refs = %d, want 0", refs)
	}
}

// When the item's own time limit passes, or the user cancels, while a search
// is running, the attempt ends with that cause. It is not a provider outage:
// no family pause, no platform failure, no mark on the session.
func TestItemContextEndingDuringAQueryPausesNothing(t *testing.T) {
	for _, tc := range []struct {
		name string
		ctx  func() (context.Context, context.CancelFunc)
		want error
	}{
		{"track timeout", func() (context.Context, context.CancelFunc) {
			return context.WithTimeout(manualCtx(), 300*time.Millisecond)
		}, context.DeadlineExceeded},
		{"user cancel", func() (context.Context, context.CancelFunc) {
			ctx, cancel := context.WithCancel(manualCtx())
			time.AfterFunc(300*time.Millisecond, cancel)
			return ctx, cancel
		}, context.Canceled},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, pool, _, yt, sc, cooldown := setupPreRoutingEnv(t, healthyAuditSession())
			before := pool.GetSession(healthyAuditSession().ID).Session()
			orch := newOrchestrator(pool, cooldown, stalledSearch(t), yt, sc)
			ctx, cancel := tc.ctx()
			defer cancel()

			_, err := orch.ResolveMedia(ctx, "ytmusic", auditTrack(), 5)
			if apperr.CodeOf(err) != apperr.CodeJobCancelled || !errors.Is(err, tc.want) {
				t.Fatalf("err = %v, want JOB_CANCELLED carrying %v", err, tc.want)
			}
			requireNothingPaused(t, pool, cooldown, before)
		})
	}
}
