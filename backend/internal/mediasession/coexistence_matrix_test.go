package mediasession

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"ytdm/backend/internal/apperr"
	"ytdm/backend/internal/provider"
)

func setupCoexistenceTest(t *testing.T, withLegacy bool, managed []Session) (*SessionPool, *CookieStorage, *LegacyAdapter, string) {
	t.Helper()
	tempDir := t.TempDir()
	storageDir := filepath.Join(tempDir, "storage")
	if err := os.MkdirAll(storageDir, 0700); err != nil {
		t.Fatalf("mkdir storage: %v", err)
	}

	var adapter *LegacyAdapter
	var legacyFilePath string
	if withLegacy {
		legacyFilePath = filepath.Join(tempDir, "legacy.cookies.txt")
		if err := os.WriteFile(legacyFilePath, []byte("# Netscape HTTP Cookie File\n.youtube.com TRUE / FALSE 0 SID test_legacy\n"), 0600); err != nil {
			t.Fatalf("write legacy cookie: %v", err)
		}
		adapter = NewLegacyAdapter(legacyFilePath)
	}

	storage, err := NewCookieStorage(storageDir, adapter)
	if err != nil {
		t.Fatalf("NewCookieStorage: %v", err)
	}

	for _, m := range managed {
		if m.CookieRef != "" && m.ID != LegacySessionID {
			cookieFile := filepath.Join(storageDir, m.ID+".cookies.txt")
			if err := os.WriteFile(cookieFile, []byte("# Netscape HTTP Cookie File\n.youtube.com TRUE / FALSE 0 SID test_managed\n"), 0600); err != nil {
				t.Fatalf("write managed cookie: %v", err)
			}
		}
	}

	repo := newMockRepo(managed)
	cfg := DefaultPoolConfig(provider.FamilyYouTube)
	pool := NewSessionPool(cfg, storage, repo, adapter)
	pool.ReloadSessions(managed)

	return pool, storage, adapter, tempDir
}

// CASE 1: legacy only -> legacy eligible
func TestCoexistenceMatrix_Case1_LegacyOnly(t *testing.T) {
	pool, _, _, _ := setupCoexistenceTest(t, true, nil)

	sessions := pool.Sessions()
	if len(sessions) != 1 || sessions[0].ID != LegacySessionID {
		t.Fatalf("expected exactly 1 legacy session, got %+v", sessions)
	}

	lease, err := pool.Acquire(context.Background())
	if err != nil {
		t.Fatalf("Acquire: %v", err)
	}
	defer lease.Release(nil)

	if lease.SessionID() != LegacySessionID {
		t.Fatalf("expected legacy session leased, got %s", lease.SessionID())
	}
}

// CASE 2: managed only -> managed eligible
func TestCoexistenceMatrix_Case2_ManagedOnly(t *testing.T) {
	m1 := Session{
		ID:             "m1",
		ProviderFamily: provider.FamilyYouTube,
		Name:           "Managed 1",
		CookieRef:      CookieRefPrefix + "m1",
		Enabled:        true,
		HealthStatus:   HealthHealthy,
	}
	pool, _, _, _ := setupCoexistenceTest(t, false, []Session{m1})

	sessions := pool.Sessions()
	if len(sessions) != 1 || sessions[0].ID != "m1" {
		t.Fatalf("expected exactly 1 managed session, got %+v", sessions)
	}

	lease, err := pool.Acquire(context.Background())
	if err != nil {
		t.Fatalf("Acquire: %v", err)
	}
	defer lease.Release(nil)

	if lease.SessionID() != "m1" {
		t.Fatalf("expected managed session leased, got %s", lease.SessionID())
	}
}

// CASE 3: legacy HEALTHY + managed HEALTHY -> both present and eligible
func TestCoexistenceMatrix_Case3_LegacyHealthy_ManagedHealthy(t *testing.T) {
	m1 := Session{
		ID:             "m1",
		ProviderFamily: provider.FamilyYouTube,
		Name:           "Managed 1",
		CookieRef:      CookieRefPrefix + "m1",
		Enabled:        true,
		HealthStatus:   HealthHealthy,
	}
	pool, _, _, _ := setupCoexistenceTest(t, true, []Session{m1})

	sessions := pool.Sessions()
	if len(sessions) != 2 {
		t.Fatalf("expected 2 sessions in pool, got %d", len(sessions))
	}

	// Mark legacy healthy by acquiring and releasing with success
	lease1, err := pool.Acquire(context.Background())
	if err != nil {
		t.Fatalf("Acquire 1: %v", err)
	}
	lease1.Release(nil)

	lease2, err := pool.Acquire(context.Background())
	if err != nil {
		t.Fatalf("Acquire 2: %v", err)
	}
	lease2.Release(nil)
}

// CASE 4: legacy HEALTHY + managed BOT_CHALLENGE -> legacy remains eligible
func TestCoexistenceMatrix_Case4_LegacyHealthy_ManagedBotChallenge(t *testing.T) {
	future := time.Now().Add(24 * time.Hour)
	m1 := Session{
		ID:             "m1",
		ProviderFamily: provider.FamilyYouTube,
		Name:           "Managed 1",
		CookieRef:      CookieRefPrefix + "m1",
		Enabled:        true,
		HealthStatus:   HealthBotChallenge,
		CooldownUntil:  &future,
	}
	pool, _, _, _ := setupCoexistenceTest(t, true, []Session{m1})

	lease, err := pool.Acquire(context.Background())
	if err != nil {
		t.Fatalf("Acquire: %v", err)
	}
	defer lease.Release(nil)

	if lease.SessionID() != LegacySessionID {
		t.Fatalf("expected legacy session to be eligible when managed in bot_challenge, got %s", lease.SessionID())
	}
}

// CASE 5: legacy HEALTHY + managed RATE_LIMITED -> legacy remains eligible
func TestCoexistenceMatrix_Case5_LegacyHealthy_ManagedRateLimited(t *testing.T) {
	future := time.Now().Add(10 * time.Minute)
	m1 := Session{
		ID:             "m1",
		ProviderFamily: provider.FamilyYouTube,
		Name:           "Managed 1",
		CookieRef:      CookieRefPrefix + "m1",
		Enabled:        true,
		HealthStatus:   HealthRateLimited,
		CooldownUntil:  &future,
	}
	pool, _, _, _ := setupCoexistenceTest(t, true, []Session{m1})

	lease, err := pool.Acquire(context.Background())
	if err != nil {
		t.Fatalf("Acquire: %v", err)
	}
	defer lease.Release(nil)

	if lease.SessionID() != LegacySessionID {
		t.Fatalf("expected legacy session to be eligible when managed in rate_limited, got %s", lease.SessionID())
	}
}

// CASE 6: legacy HEALTHY + managed AUTH_FAILED -> legacy remains eligible
func TestCoexistenceMatrix_Case6_LegacyHealthy_ManagedAuthFailed(t *testing.T) {
	m1 := Session{
		ID:             "m1",
		ProviderFamily: provider.FamilyYouTube,
		Name:           "Managed 1",
		CookieRef:      CookieRefPrefix + "m1",
		Enabled:        true,
		HealthStatus:   HealthAuthFailed,
	}
	pool, _, _, _ := setupCoexistenceTest(t, true, []Session{m1})

	lease, err := pool.Acquire(context.Background())
	if err != nil {
		t.Fatalf("Acquire: %v", err)
	}
	defer lease.Release(nil)

	if lease.SessionID() != LegacySessionID {
		t.Fatalf("expected legacy session to be eligible when managed in auth_failed, got %s", lease.SessionID())
	}
}

// CASE 7: legacy unavailable + managed HEALTHY -> managed eligible
func TestCoexistenceMatrix_Case7_LegacyUnavailable_ManagedHealthy(t *testing.T) {
	m1 := Session{
		ID:             "m1",
		ProviderFamily: provider.FamilyYouTube,
		Name:           "Managed 1",
		CookieRef:      CookieRefPrefix + "m1",
		Enabled:        true,
		HealthStatus:   HealthHealthy,
	}
	pool, _, _, _ := setupCoexistenceTest(t, true, []Session{m1})

	// Put legacy session into cooldown
	future := time.Now().Add(1 * time.Hour)
	pool.mu.Lock()
	if rs := pool.sessions[LegacySessionID]; rs != nil {
		s := rs.Session()
		s.HealthStatus = HealthBotChallenge
		s.CooldownUntil = &future
		rs.UpdateSession(s)
	}
	pool.mu.Unlock()

	lease, err := pool.Acquire(context.Background())
	if err != nil {
		t.Fatalf("Acquire: %v", err)
	}
	defer lease.Release(nil)

	if lease.SessionID() != "m1" {
		t.Fatalf("expected managed session to be eligible when legacy in cooldown, got %s", lease.SessionID())
	}
}

// CASE 8: all configured sessions unavailable -> only then no eligible session
func TestCoexistenceMatrix_Case8_AllUnavailable(t *testing.T) {
	future := time.Now().Add(1 * time.Hour)
	m1 := Session{
		ID:             "m1",
		ProviderFamily: provider.FamilyYouTube,
		Name:           "Managed 1",
		CookieRef:      CookieRefPrefix + "m1",
		Enabled:        true,
		HealthStatus:   HealthBotChallenge,
		CooldownUntil:  &future,
	}
	pool, _, _, _ := setupCoexistenceTest(t, true, []Session{m1})

	// Put legacy into cooldown too
	pool.mu.Lock()
	if rs := pool.sessions[LegacySessionID]; rs != nil {
		s := rs.Session()
		s.HealthStatus = HealthBotChallenge
		s.CooldownUntil = &future
		rs.UpdateSession(s)
	}
	pool.mu.Unlock()

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	_, err := pool.Acquire(ctx)
	if err == nil {
		t.Fatal("expected Acquire to fail when all sessions are unavailable, got success")
	}
	if apperr.CodeOf(err) != apperr.CodeSessionUnavailable && err != context.DeadlineExceeded {
		t.Fatalf("expected CodeSessionUnavailable or DeadlineExceeded, got: %v", err)
	}
}

// CASE 9: repeated ReloadSessions -> no duplicate legacy session
func TestCoexistenceMatrix_Case9_RepeatedReloadSessions_NoDuplicates(t *testing.T) {
	m1 := Session{
		ID:             "m1",
		ProviderFamily: provider.FamilyYouTube,
		Name:           "Managed 1",
		CookieRef:      CookieRefPrefix + "m1",
		Enabled:        true,
		HealthStatus:   HealthHealthy,
	}
	pool, _, _, _ := setupCoexistenceTest(t, true, []Session{m1})

	for i := 0; i < 5; i++ {
		pool.ReloadSessions([]Session{m1})
	}

	sessions := pool.Sessions()
	if len(sessions) != 2 {
		t.Fatalf("expected exactly 2 sessions after repeated ReloadSessions, got %d: %+v", len(sessions), sessions)
	}

	legacyCount := 0
	managedCount := 0
	for _, s := range sessions {
		if s.ID == LegacySessionID {
			legacyCount++
		}
		if s.ID == "m1" {
			managedCount++
		}
	}

	if legacyCount != 1 {
		t.Errorf("legacy session count = %d, want 1", legacyCount)
	}
	if managedCount != 1 {
		t.Errorf("managed session count = %d, want 1", managedCount)
	}
}

// CASE 10: restart/reconstruction -> legacy + managed coexist exactly once
func TestCoexistenceMatrix_Case10_RestartReconstruction(t *testing.T) {
	tempDir := t.TempDir()
	storageDir := filepath.Join(tempDir, "storage")
	_ = os.MkdirAll(storageDir, 0700)
	legacyPath := filepath.Join(tempDir, "legacy.cookies.txt")
	_ = os.WriteFile(legacyPath, []byte("# Legacy Cookie\n"), 0600)
	adapter := NewLegacyAdapter(legacyPath)
	storage, _ := NewCookieStorage(storageDir, adapter)

	m1 := Session{
		ID:             "m1",
		ProviderFamily: provider.FamilyYouTube,
		Name:           "Managed 1",
		CookieRef:      CookieRefPrefix + "m1",
		Enabled:        true,
		HealthStatus:   HealthHealthy,
	}
	_ = os.WriteFile(filepath.Join(storageDir, "m1.cookies.txt"), []byte("# Managed Cookie\n"), 0600)
	repo := newMockRepo([]Session{m1})

	cfg := DefaultPoolConfig(provider.FamilyYouTube)

	// Startup 1
	pool1 := NewSessionPool(cfg, storage, repo, adapter)
	pool1.ReloadSessions([]Session{m1})
	if len(pool1.Sessions()) != 2 {
		t.Fatalf("Startup 1: expected 2 sessions, got %d", len(pool1.Sessions()))
	}

	// Simulated restart: Startup 2
	pool2 := NewSessionPool(cfg, storage, repo, adapter)
	pool2.ReloadSessions([]Session{m1})
	if len(pool2.Sessions()) != 2 {
		t.Fatalf("Startup 2: expected 2 sessions, got %d", len(pool2.Sessions()))
	}

	hasLegacy := false
	hasManaged := false
	for _, s := range pool2.Sessions() {
		if s.ID == LegacySessionID {
			hasLegacy = true
		}
		if s.ID == "m1" {
			hasManaged = true
		}
	}
	if !hasLegacy || !hasManaged {
		t.Fatalf("Startup 2 missing expected sessions: legacy=%v, managed=%v", hasLegacy, hasManaged)
	}
}

// Data-Plane Safety: Same jar serialized, independent jars concurrent, no crossover
func TestCoexistence_DataPlaneSafetyAndNoCrossover(t *testing.T) {
	m1 := Session{
		ID:             "m1",
		ProviderFamily: provider.FamilyYouTube,
		Name:           "Managed 1",
		CookieRef:      CookieRefPrefix + "m1",
		Enabled:        true,
		HealthStatus:   HealthHealthy,
	}
	pool, _, _, tempDir := setupCoexistenceTest(t, true, []Session{m1})

	legacyPath := pool.ResolveCookiePath(LegacySessionID)
	managedPath := pool.ResolveCookiePath("m1")

	if legacyPath == "" || managedPath == "" {
		t.Fatalf("expected non-empty cookie paths: legacy=%q, managed=%q", legacyPath, managedPath)
	}
	if legacyPath == managedPath {
		t.Fatalf("credential crossover: legacy and managed share same cookie path %q", legacyPath)
	}
	if !filepath.IsAbs(legacyPath) || !filepath.IsAbs(managedPath) {
		t.Fatalf("paths must be absolute: legacy=%s, managed=%s", legacyPath, managedPath)
	}
	if filepath.Dir(legacyPath) == filepath.Dir(managedPath) {
		t.Fatalf("legacy and managed should reside in separate directory scopes")
	}

	ctx := context.Background()

	// 1. In-flight data plane tracking on Legacy
	pool.RetainDataPlane(LegacySessionID)
	if !pool.IsInUse(LegacySessionID) {
		t.Errorf("expected legacy IsInUse = true while data-plane retained")
	}
	pool.ReleaseDataPlane(LegacySessionID)
	if pool.IsInUse(LegacySessionID) {
		t.Errorf("expected legacy IsInUse = false after data-plane released")
	}

	// 2. Acquire Data-Plane mutual exclusion on Legacy
	unlockLeg1, err := pool.AcquireDataPlane(ctx, LegacySessionID)
	if err != nil {
		t.Fatalf("AcquireDataPlane legacy: %v", err)
	}

	// 3. Concurrent Acquire on same Legacy jar must block/timeout
	timeoutCtx, cancelTimeout := context.WithTimeout(ctx, 30*time.Millisecond)
	defer cancelTimeout()
	_, errBlocked := pool.AcquireDataPlane(timeoutCtx, LegacySessionID)
	if errBlocked != context.DeadlineExceeded {
		t.Errorf("expected DeadlineExceeded for concurrent legacy data-plane, got: %v", errBlocked)
	}

	// 4. Managed jar can proceed independently while legacy jar is locked
	unlockM1, errM1 := pool.AcquireDataPlane(ctx, "m1")
	if errM1 != nil {
		t.Fatalf("AcquireDataPlane managed failed while legacy held: %v", errM1)
	}

	// 5. Concurrent Acquire on same Managed jar must block/timeout
	timeoutCtx2, cancelTimeout2 := context.WithTimeout(ctx, 30*time.Millisecond)
	defer cancelTimeout2()
	_, errBlockedM1 := pool.AcquireDataPlane(timeoutCtx2, "m1")
	if errBlockedM1 != context.DeadlineExceeded {
		t.Errorf("expected DeadlineExceeded for concurrent managed data-plane, got: %v", errBlockedM1)
	}

	// Release locks
	unlockLeg1()
	unlockM1()
	_ = tempDir
}

// Limiter: Verify per-session limiters and shared family global limiter
func TestCoexistence_LimitersParticipation(t *testing.T) {
	m1 := Session{
		ID:             "m1",
		ProviderFamily: provider.FamilyYouTube,
		Name:           "Managed 1",
		CookieRef:      CookieRefPrefix + "m1",
		Enabled:        true,
		HealthStatus:   HealthHealthy,
	}
	pool, _, _, _ := setupCoexistenceTest(t, true, []Session{m1})

	globalLimiter := pool.GlobalLimiter()
	if globalLimiter == nil {
		t.Fatal("expected non-nil pool.GlobalLimiter()")
	}

	rsLegacy := pool.GetSession(LegacySessionID)
	if rsLegacy == nil {
		t.Fatal("expected legacy RuntimeSession in pool")
	}
	if rsLegacy.Limiter() == nil {
		t.Fatal("expected legacy session to have its own per-session limiter")
	}

	rsManaged := pool.GetSession("m1")
	if rsManaged == nil {
		t.Fatal("expected managed RuntimeSession in pool")
	}
	if rsManaged.Limiter() == nil {
		t.Fatal("expected managed session to have its own per-session limiter")
	}

	// Distinct per-session limiters
	if rsLegacy.Limiter() == rsManaged.Limiter() {
		t.Fatal("legacy and managed must have distinct per-session limiters")
	}
}
