package mediasession

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"ytdm/backend/internal/provider"
	"ytdm/backend/internal/ytdlp"
)

func executionTestPool() *SessionPool {
	cfg := DefaultPoolConfig(provider.FamilyYouTube)
	cfg.SessionRequestsPerSec = 10_000
	cfg.GlobalRequestsPerSec = 10_000
	cfg.SessionBurst = 1
	cfg.GlobalBurst = 10
	pool := NewSessionPool(cfg, nil, nil, nil)
	pool.ReloadSessions([]Session{
		{ID: "one", ProviderFamily: provider.FamilyYouTube, CookieRef: CookieRefPrefix + "one", Enabled: true, HealthStatus: HealthHealthy},
		{ID: "two", ProviderFamily: provider.FamilyYouTube, CookieRef: CookieRefPrefix + "two", Enabled: true, HealthStatus: HealthHealthy},
	})
	return pool
}

func TestExecutionGateSameSessionSerialDifferentSessionsConcurrent(t *testing.T) {
	pool := executionTestPool()
	one := pool.ExecutionGate("one")
	two := pool.ExecutionGate("two")

	releaseOne, err := one.Acquire(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	blockedCtx, cancel := context.WithTimeout(context.Background(), 40*time.Millisecond)
	defer cancel()
	if _, err := one.Acquire(blockedCtx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("same-session execution overlapped: %v", err)
	}

	releaseTwo, err := two.Acquire(context.Background())
	if err != nil {
		t.Fatalf("different session was unnecessarily serialized: %v", err)
	}
	releaseTwo()
	releaseOne()
	releaseAgain, err := one.Acquire(context.Background())
	if err != nil {
		t.Fatalf("cancelled waiter leaked the session gate: %v", err)
	}
	releaseAgain()
}

func TestResolutionAndDownloadShareSessionExecutionGate(t *testing.T) {
	gate := executionTestPool().ExecutionGate("one")
	releaseResolution, err := gate.Acquire(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	downloadCtx, cancel := context.WithTimeout(context.Background(), 40*time.Millisecond)
	defer cancel()
	if _, err := gate.Acquire(downloadCtx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("download overlapped resolution on the same session: %v", err)
	}
	releaseResolution()
}

func TestExecutionGatePacesEveryProcessStart(t *testing.T) {
	cfg := DefaultPoolConfig(provider.FamilyYouTube)
	cfg.SessionRequestsPerSec = 20
	cfg.SessionBurst = 1
	cfg.GlobalRequestsPerSec = 10_000
	cfg.GlobalBurst = 10
	pool := NewSessionPool(cfg, nil, nil, nil)
	pool.ReloadSessions([]Session{{
		ID: "paced", ProviderFamily: provider.FamilyYouTube,
		CookieRef: CookieRefPrefix + "paced", Enabled: true, HealthStatus: HealthHealthy,
	}})
	gate := pool.ExecutionGate("paced")
	first, err := gate.Acquire(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	first()
	started := time.Now()
	second, err := gate.Acquire(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer second()
	if elapsed := time.Since(started); elapsed < 35*time.Millisecond {
		t.Fatalf("second process start escaped pacing: %v", elapsed)
	}
}

type countingExecutionGate struct{ calls atomic.Int32 }

func (g *countingExecutionGate) Acquire(context.Context) (func(), error) {
	g.calls.Add(1)
	return func() {}, nil
}

func TestManualProbeUsesSessionExecutionGate(t *testing.T) {
	dir := t.TempDir()
	binary := filepath.Join(dir, "fake-ytdlp")
	if err := os.WriteFile(binary, []byte("#!/bin/sh\necho '{\"id\":\"probe\",\"formats\":[{\"format_id\":\"1\",\"acodec\":\"opus\"}]}'\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	gate := &countingExecutionGate{}
	prober := NewYTDLPProber(ytdlp.New(ytdlp.Options{Binary: binary}), "ytsearch1:test")
	prober.SetExecutionGateResolver(func(string) ytdlp.ExecutionGate { return gate })
	if _, err := prober.Probe(context.Background(), "one", "/tmp/test-cookies"); err != nil {
		t.Fatalf("probe failed: %v", err)
	}
	if got := gate.calls.Load(); got != 1 {
		t.Fatalf("manual probe gate acquisitions = %d, want 1", got)
	}
}
