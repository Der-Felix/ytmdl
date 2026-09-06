package orchestrator_test

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"ytdm/backend/internal/matcher"
	"ytdm/backend/internal/mediasession"
	"ytdm/backend/internal/music"
	"ytdm/backend/internal/orchestrator"
	"ytdm/backend/internal/provider"
)

// TestConcurrency_FourWorkers_SessionIsolation verifies that 4+ concurrent workers resolving
// and downloading media across distinct sessions experience zero crossover and zero data races.
func TestConcurrency_FourWorkers_SessionIsolation(t *testing.T) {
	storageDir := t.TempDir()
	storage, err := mediasession.NewCookieStorage(storageDir, nil)
	if err != nil {
		t.Fatalf("NewCookieStorage: %v", err)
	}

	const numWorkers = 4
	sessions := make([]mediasession.Session, numWorkers)
	for i := 0; i < numWorkers; i++ {
		id := fmt.Sprintf("sess-%d", i+1)
		cookieRef, err := storage.Store(id, []byte(fmt.Sprintf("# Netscape cookie for %s\n", id)))
		if err != nil {
			t.Fatalf("Store %s: %v", id, err)
		}
		sessions[i] = mediasession.Session{
			ID:             id,
			ProviderFamily: provider.FamilyYouTube,
			Name:           fmt.Sprintf("Session %d", i+1),
			CookieRef:      cookieRef,
			Enabled:        true,
			HealthStatus:   mediasession.HealthHealthy,
		}
	}

	cfg := mediasession.PoolConfig{
		Family:                provider.FamilyYouTube,
		MaxLeasesPerSession:   1,
		SessionRequestsPerSec: 200.0,
		SessionBurst:          20,
		GlobalRequestsPerSec:  200.0,
		GlobalBurst:           20,
		AllowUnknown:          true,
	}

	pool := mediasession.NewSessionPool(cfg, storage, nil, nil)
	pool.ReloadSessions(sessions)

	ytm := newMockProvider("ytmusic")
	yt := newMockProvider("youtube")

	reg := provider.NewRegistry()
	reg.RegisterMedia(ytm)
	reg.RegisterMedia(yt)
	reg.SetDefaults("ytmusic", "ytmusic")

	engine := matcher.New(matcher.Options{
		MinScore:            70.0,
		DurationToleranceMS: 5000,
	})

	cooldown := newMockCooldown()

	orch := orchestrator.New(orchestrator.Options{
		Registry:    reg,
		SessionPool: pool,
		Matcher:     engine,
		Cooldown:    cooldown,
	})

	// Provide candidate pool for ytmusic
	cands := make([]provider.MediaCandidate, numWorkers)
	for i := 0; i < numWorkers; i++ {
		cands[i] = provider.MediaCandidate{
			Provider:   "ytmusic",
			ID:         fmt.Sprintf("vid-%d", i+1),
			Title:      fmt.Sprintf("Track %d", i+1),
			Artists:    []string{fmt.Sprintf("Artist %d", i+1)},
			DurationMS: 200000,
		}
	}
	ytm.SetCandidates(cands)

	var wg sync.WaitGroup
	var errorCount int32

	// Run 20 iterations across 4 concurrent workers
	for w := 0; w < numWorkers; w++ {
		wg.Add(1)
		go func(workerIdx int) {
			defer wg.Done()
			for iter := 0; iter < 10; iter++ {
				track := music.Track{
					Title:      fmt.Sprintf("Track %d", (workerIdx%numWorkers)+1),
					Artists:    []string{fmt.Sprintf("Artist %d", (workerIdx%numWorkers)+1)},
					DurationMS: 200000,
				}

				ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				res, err := orch.ResolveMedia(ctx, "ytmusic", track, 5)
				cancel()
				if err != nil {
					atomic.AddInt32(&errorCount, 1)
					t.Errorf("worker %d iter %d ResolveMedia failed: %v", workerIdx, iter, err)
					return
				}

				// Verify resolved session affinity
				if res.SessionID == "" {
					atomic.AddInt32(&errorCount, 1)
					t.Errorf("worker %d iter %d: expected non-empty SessionID", workerIdx, iter)
					return
				}
				if res.Source.SessionID != res.SessionID {
					atomic.AddInt32(&errorCount, 1)
					t.Errorf("worker %d: source session ID %s != res.SessionID %s", workerIdx, res.Source.SessionID, res.SessionID)
					return
				}

				// Data plane: download uses resolved session ID
				cookiePath := orch.ResolveCookiePath(res.SessionID)
				if cookiePath == "" {
					atomic.AddInt32(&errorCount, 1)
					t.Errorf("worker %d: failed to resolve cookie path for session %s", workerIdx, res.SessionID)
					return
				}
				if !strings.Contains(cookiePath, res.SessionID) {
					atomic.AddInt32(&errorCount, 1)
					t.Errorf("worker %d: cookie path %s does not match session %s", workerIdx, cookiePath, res.SessionID)
					return
				}

				// Simulate download outcome
				orch.RecordDownloadOutcome(context.Background(), res.SessionID, nil)
			}
		}(w)
	}

	wg.Wait()

	if errorCount > 0 {
		t.Fatalf("%d worker errors occurred during concurrent resolution", errorCount)
	}

	// Verify all leases are released at the end
	for _, rs := range pool.RuntimeSessions() {
		if rs.CurrentLeases() != 0 {
			t.Errorf("session %s leaked %d leases", rs.Session().ID, rs.CurrentLeases())
		}
	}
}

// TestConcurrency_DownloadAffinity_NoCrossover explicitly tests Worker A (Session A)
// and Worker B (Session B) concurrently resolving and downloading without crossover.
func TestConcurrency_DownloadAffinity_NoCrossover(t *testing.T) {
	storageDir := t.TempDir()
	storage, err := mediasession.NewCookieStorage(storageDir, nil)
	if err != nil {
		t.Fatalf("NewCookieStorage: %v", err)
	}

	cookieRefA, _ := storage.Store("sess-a", []byte("# cookie A\n"))
	cookieRefB, _ := storage.Store("sess-b", []byte("# cookie B\n"))

	sessA := mediasession.Session{
		ID:             "sess-a",
		ProviderFamily: provider.FamilyYouTube,
		Name:           "Session A",
		CookieRef:      cookieRefA,
		Enabled:        true,
		HealthStatus:   mediasession.HealthHealthy,
	}
	sessB := mediasession.Session{
		ID:             "sess-b",
		ProviderFamily: provider.FamilyYouTube,
		Name:           "Session B",
		CookieRef:      cookieRefB,
		Enabled:        true,
		HealthStatus:   mediasession.HealthHealthy,
	}

	pool := mediasession.NewSessionPool(mediasession.PoolConfig{
		Family:                provider.FamilyYouTube,
		MaxLeasesPerSession:   1,
		SessionRequestsPerSec: 100.0,
		SessionBurst:          10,
		GlobalRequestsPerSec:  100.0,
		GlobalBurst:           10,
		AllowUnknown:          true,
	}, storage, nil, nil)
	pool.ReloadSessions([]mediasession.Session{sessA, sessB})

	ytm := newMockProvider("ytmusic")
	reg := provider.NewRegistry()
	reg.RegisterMedia(ytm)
	reg.SetDefaults("ytmusic", "ytmusic")

	engine := matcher.New(matcher.Options{
		MinScore:            70.0,
		DurationToleranceMS: 5000,
	})

	orch := orchestrator.New(orchestrator.Options{
		Registry:    reg,
		SessionPool: pool,
		Matcher:     engine,
	})

	ytm.SetCandidates([]provider.MediaCandidate{{
		Provider:   "ytmusic",
		ID:         "vid-1",
		Title:      "Test Track",
		Artists:    []string{"Test Artist"},
		DurationMS: 200000,
	}})

	track := music.Track{Title: "Test Track", Artists: []string{"Test Artist"}, DurationMS: 200000}

	var (
		wg      sync.WaitGroup
		startCh = make(chan struct{})
		resA    *orchestrator.ResolvedMedia
		resB    *orchestrator.ResolvedMedia
		errA    error
		errB    error
	)

	wg.Add(2)
	go func() {
		defer wg.Done()
		<-startCh
		resA, errA = orch.ResolveMedia(context.Background(), "ytmusic", track, 5)
	}()
	go func() {
		defer wg.Done()
		<-startCh
		resB, errB = orch.ResolveMedia(context.Background(), "ytmusic", track, 5)
	}()

	close(startCh)
	wg.Wait()

	if errA != nil || errB != nil {
		t.Fatalf("resolution failed: errA=%v, errB=%v", errA, errB)
	}

	// Each worker resolved under a distinct session because MaxLeasesPerSession is 1
	if resA.SessionID == resB.SessionID {
		t.Fatalf("expected distinct sessions for concurrent workers, both got %s", resA.SessionID)
	}

	// Resolve cookie paths for both workers
	cookiePathA := orch.ResolveCookiePath(resA.SessionID)
	cookiePathB := orch.ResolveCookiePath(resB.SessionID)

	if cookiePathA == "" || cookiePathB == "" {
		t.Fatalf("empty cookie paths: A=%s, B=%s", cookiePathA, cookiePathB)
	}
	if cookiePathA == cookiePathB {
		t.Fatalf("cookie paths crossed over: A=%s, B=%s", cookiePathA, cookiePathB)
	}
	if !strings.Contains(cookiePathA, resA.SessionID) {
		t.Fatalf("cookie path A %s does not contain session A ID %s", cookiePathA, resA.SessionID)
	}
	if !strings.Contains(cookiePathB, resB.SessionID) {
		t.Fatalf("cookie path B %s does not contain session B ID %s", cookiePathB, resB.SessionID)
	}
}

// TestConcurrency_Cancellation_ReleasesTemporaryResources verifies that cancelling
// a context during resolve immediately cleans up the lease without leaking.
func TestConcurrency_Cancellation_ReleasesTemporaryResources(t *testing.T) {
	storageDir := t.TempDir()
	storage, err := mediasession.NewCookieStorage(storageDir, nil)
	if err != nil {
		t.Fatalf("NewCookieStorage: %v", err)
	}

	cookieRef, _ := storage.Store("sess-1", []byte("# cookie\n"))
	sess := mediasession.Session{
		ID:             "sess-1",
		ProviderFamily: provider.FamilyYouTube,
		Name:           "Session 1",
		CookieRef:      cookieRef,
		Enabled:        true,
		HealthStatus:   mediasession.HealthHealthy,
	}

	pool := mediasession.NewSessionPool(mediasession.PoolConfig{
		Family:                provider.FamilyYouTube,
		MaxLeasesPerSession:   1,
		SessionRequestsPerSec: 100.0,
		SessionBurst:          10,
		GlobalRequestsPerSec:  100.0,
		GlobalBurst:           10,
		AllowUnknown:          true,
	}, storage, nil, nil)
	pool.ReloadSessions([]mediasession.Session{sess})

	ytm := newMockProvider("ytmusic")
	reg := provider.NewRegistry()
	reg.RegisterMedia(ytm)
	reg.SetDefaults("ytmusic", "ytmusic")

	orch := orchestrator.New(orchestrator.Options{
		Registry:    reg,
		SessionPool: pool,
		Matcher:     matcher.New(matcher.Options{MinScore: 70}),
	})

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel upfront

	track := music.Track{Title: "Test Track", Artists: []string{"Test Artist"}, DurationMS: 200000}
	_, err = orch.ResolveMedia(ctx, "ytmusic", track, 5)
	if err == nil {
		t.Fatal("expected error on cancelled context, got nil")
	}

	// Verify lease count is 0
	rs := pool.RuntimeSessions()[0]
	if rs.CurrentLeases() != 0 {
		t.Fatalf("expected 0 leases after cancellation, got %d", rs.CurrentLeases())
	}
}

// TestConcurrency_FamilyCooldown_VisibleToAllWorkers verifies that a family cooldown
// triggered by one worker is immediately visible to other concurrent workers.
func TestConcurrency_FamilyCooldown_VisibleToAllWorkers(t *testing.T) {
	storageDir := t.TempDir()
	storage, err := mediasession.NewCookieStorage(storageDir, nil)
	if err != nil {
		t.Fatalf("NewCookieStorage: %v", err)
	}

	cookieRef, _ := storage.Store("sess-1", []byte("# cookie\n"))
	sess := mediasession.Session{
		ID:             "sess-1",
		ProviderFamily: provider.FamilyYouTube,
		Name:           "Session 1",
		CookieRef:      cookieRef,
		Enabled:        true,
		HealthStatus:   mediasession.HealthHealthy,
	}

	pool := mediasession.NewSessionPool(mediasession.PoolConfig{
		Family:                provider.FamilyYouTube,
		MaxLeasesPerSession:   2,
		SessionRequestsPerSec: 100.0,
		SessionBurst:          10,
		GlobalRequestsPerSec:  100.0,
		GlobalBurst:           10,
		AllowUnknown:          true,
	}, storage, nil, nil)
	pool.ReloadSessions([]mediasession.Session{sess})

	ytm := newMockProvider("ytmusic")
	reg := provider.NewRegistry()
	reg.RegisterMedia(ytm)
	reg.SetDefaults("ytmusic", "ytmusic")

	cooldown := newMockCooldown()

	orch := orchestrator.New(orchestrator.Options{
		Registry:    reg,
		SessionPool: pool,
		Matcher:     matcher.New(matcher.Options{MinScore: 70}),
		Cooldown:    cooldown,
	})

	// Trigger family cooldown
	cooldown.Trigger("youtube", 5*time.Second)

	// Attempt resolution with a short context timeout
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	track := music.Track{Title: "Test", Artists: []string{"Test"}, DurationMS: 200000}
	_, err = orch.ResolveMedia(ctx, "ytmusic", track, 5)
	if err == nil {
		t.Fatal("expected error waiting on active cooldown, got nil")
	}

	// Should not have called search on provider while cooling down
	if ytm.SearchCalls() != 0 {
		t.Fatalf("expected 0 search calls during active cooldown, got %d", ytm.SearchCalls())
	}
}

// TestConcurrency_DataPlaneLock_PreventsConcurrentCookieMutation verifies that two
// concurrent downloads using the same session never access the writable cookie file simultaneously.
func TestConcurrency_DataPlaneLock_PreventsConcurrentCookieMutation(t *testing.T) {
	storageDir := t.TempDir()
	storage, err := mediasession.NewCookieStorage(storageDir, nil)
	if err != nil {
		t.Fatalf("NewCookieStorage: %v", err)
	}

	cookieRef, _ := storage.Store("sess-shared", []byte("# shared cookie\n"))
	sess := mediasession.Session{
		ID:             "sess-shared",
		ProviderFamily: provider.FamilyYouTube,
		Name:           "Shared Session",
		CookieRef:      cookieRef,
		Enabled:        true,
		HealthStatus:   mediasession.HealthHealthy,
	}

	pool := mediasession.NewSessionPool(mediasession.PoolConfig{
		Family:                provider.FamilyYouTube,
		MaxLeasesPerSession:   2,
		SessionRequestsPerSec: 100.0,
		SessionBurst:          10,
		GlobalRequestsPerSec:  100.0,
		GlobalBurst:           10,
		AllowUnknown:          true,
	}, storage, nil, nil)
	pool.ReloadSessions([]mediasession.Session{sess})

	orch := orchestrator.New(orchestrator.Options{
		SessionPool: pool,
	})

	const concurrentWorkers = 4
	var activeInCriticalSection int32
	var maxConcurrencyObserved int32
	var wg sync.WaitGroup

	for w := 0; w < concurrentWorkers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()

			release, err := orch.AcquireDataPlaneLock(ctx, "sess-shared")
			if err != nil {
				t.Errorf("AcquireDataPlaneLock failed: %v", err)
				return
			}
			defer release()

			curr := atomic.AddInt32(&activeInCriticalSection, 1)
			for {
				oldMax := atomic.LoadInt32(&maxConcurrencyObserved)
				if curr <= oldMax || atomic.CompareAndSwapInt32(&maxConcurrencyObserved, oldMax, curr) {
					break
				}
			}

			// Simulate yt-dlp write access
			time.Sleep(10 * time.Millisecond)
			atomic.AddInt32(&activeInCriticalSection, -1)
		}()
	}

	wg.Wait()

	if maxConcurrencyObserved > 1 {
		t.Fatalf("expected at most 1 concurrent worker accessing cookie file, observed %d", maxConcurrencyObserved)
	}
}

// TestConcurrency_InFlightTracking_ProtectsSessionLifetime verifies that resolved media
// holds an in-flight data-plane reference protecting the session from deletion or replacement.
func TestConcurrency_InFlightTracking_ProtectsSessionLifetime(t *testing.T) {
	storageDir := t.TempDir()
	storage, err := mediasession.NewCookieStorage(storageDir, nil)
	if err != nil {
		t.Fatalf("NewCookieStorage: %v", err)
	}

	cookieRef, _ := storage.Store("sess-inflight", []byte("# cookie\n"))
	sess := mediasession.Session{
		ID:             "sess-inflight",
		ProviderFamily: provider.FamilyYouTube,
		Name:           "Inflight Session",
		CookieRef:      cookieRef,
		Enabled:        true,
		HealthStatus:   mediasession.HealthHealthy,
	}

	pool := mediasession.NewSessionPool(mediasession.PoolConfig{
		Family:                provider.FamilyYouTube,
		MaxLeasesPerSession:   1,
		SessionRequestsPerSec: 100.0,
		SessionBurst:          10,
		GlobalRequestsPerSec:  100.0,
		GlobalBurst:           10,
		AllowUnknown:          true,
	}, storage, nil, nil)
	pool.ReloadSessions([]mediasession.Session{sess})

	ytm := newMockProvider("ytmusic")
	reg := provider.NewRegistry()
	reg.RegisterMedia(ytm)
	reg.SetDefaults("ytmusic", "ytmusic")

	orch := orchestrator.New(orchestrator.Options{
		Registry:    reg,
		SessionPool: pool,
		Matcher:     matcher.New(matcher.Options{MinScore: 70}),
	})

	ytm.SetCandidates([]provider.MediaCandidate{{
		Provider:   "ytmusic",
		ID:         "vid-1",
		Title:      "Track 1",
		Artists:    []string{"Artist 1"},
		DurationMS: 200000,
	}})

	// Initially not in use
	if pool.IsInUse("sess-inflight") {
		t.Fatal("expected session not in use initially")
	}

	track := music.Track{Title: "Track 1", Artists: []string{"Artist 1"}, DurationMS: 200000}
	res, err := orch.ResolveMedia(context.Background(), "ytmusic", track, 5)
	if err != nil {
		t.Fatalf("ResolveMedia failed: %v", err)
	}

	// Control lease is released, but data-plane reference is retained!
	rs := pool.RuntimeSessions()[0]
	if rs.CurrentLeases() != 0 {
		t.Fatalf("expected 0 control-plane leases, got %d", rs.CurrentLeases())
	}
	if !pool.IsInUse("sess-inflight") {
		t.Fatal("expected session in use while data-plane reference is retained")
	}

	// Simulate download completion
	orch.RecordDownloadOutcome(context.Background(), res.SessionID, nil)

	// In-flight reference is now released!
	if pool.IsInUse("sess-inflight") {
		t.Fatal("expected session no longer in use after download outcome recorded")
	}
}
