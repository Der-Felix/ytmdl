package mediasession

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"ytdm/backend/internal/provider"
)

func TestLegacyCookieCompatibility_RuntimeSession(t *testing.T) {
	// Create synthetic legacy cookie file
	tempDir := t.TempDir()
	legacyFilePath := filepath.Join(tempDir, "synthetic.cookies.txt")
	syntheticData := []byte("# Synthetic Netscape Cookie File for Testing\n")
	if err := os.WriteFile(legacyFilePath, syntheticData, 0600); err != nil {
		t.Fatalf("failed to write synthetic legacy cookie: %v", err)
	}

	adapter := NewLegacyAdapter(legacyFilePath)
	if !adapter.IsConfigured() {
		t.Fatal("expected legacy adapter to report IsConfigured() = true")
	}

	// No managed sessions in database
	var emptySessions []Session
	repo := newMockRepo(emptySessions)
	storageDir := t.TempDir()
	storage, err := NewCookieStorage(storageDir, adapter)
	if err != nil {
		t.Fatalf("NewCookieStorage failed: %v", err)
	}

	cfg := DefaultPoolConfig(provider.FamilyYouTube)
	pool := NewSessionPool(cfg, storage, repo, adapter)
	pool.ReloadSessions(emptySessions)

	// 1. Verify exactly one synthetic session is available in pool
	sessions := pool.Sessions()
	if len(sessions) != 1 {
		t.Fatalf("expected 1 synthetic legacy session in pool, got %d", len(sessions))
	}

	legacySession := sessions[0]
	if legacySession.ID != LegacySessionID {
		t.Errorf("session ID = %q, want %q", legacySession.ID, LegacySessionID)
	}
	if legacySession.CookieRef != LegacyCookieRef {
		t.Errorf("cookieRef = %q, want %q", legacySession.CookieRef, LegacyCookieRef)
	}
	if legacySession.HealthStatus != HealthUnknown {
		t.Errorf("initial health = %q, want unknown", legacySession.HealthStatus)
	}

	// 2. Verify outward-facing representation does not leak host/container path
	if strings.Contains(legacySession.CookieRef, tempDir) || strings.Contains(legacySession.CookieRef, "synthetic.cookies.txt") {
		t.Fatalf("outward CookieRef leaks host filesystem path: %s", legacySession.CookieRef)
	}

	// 3. Verify acquiring lease resolves to the legacy cookie path internally
	lease, err := pool.Acquire(context.Background())
	if err != nil {
		t.Fatalf("Acquire on legacy session failed: %v", err)
	}
	defer lease.Release(nil)

	if lease.CookiePath() != legacyFilePath {
		t.Errorf("lease CookiePath = %q, want %q", lease.CookiePath(), legacyFilePath)
	}
	if lease.CookieRef() != LegacyCookieRef {
		t.Errorf("lease CookieRef = %q, want %q", lease.CookieRef(), LegacyCookieRef)
	}

	// 4. Verify no files were copied into managed storage directory
	storageEntries, err := os.ReadDir(storageDir)
	if err != nil {
		t.Fatalf("ReadDir failed: %v", err)
	}
	if len(storageEntries) != 0 {
		t.Errorf("storage directory should remain empty (no secret copied), got %d entries", len(storageEntries))
	}

	// 5. Verify no database rows created for synthetic session
	dbList, _ := repo.ListSessions(context.Background(), Filter{})
	if len(dbList) != 0 {
		t.Errorf("database should contain 0 sessions, got %d", len(dbList))
	}
}

func TestLegacyAndManagedCoexistence_ReloadSessions(t *testing.T) {
	// Create synthetic legacy cookie file
	tempDir := t.TempDir()
	legacyFilePath := filepath.Join(tempDir, "synthetic.cookies.txt")
	if err := os.WriteFile(legacyFilePath, []byte("# Legacy Cookie\n"), 0600); err != nil {
		t.Fatalf("write legacy cookie: %v", err)
	}

	adapter := NewLegacyAdapter(legacyFilePath)
	storageDir := t.TempDir()
	storage, err := NewCookieStorage(storageDir, adapter)
	if err != nil {
		t.Fatalf("NewCookieStorage failed: %v", err)
	}

	managedID := "managed-sess-1"
	managedCookieFile := filepath.Join(storageDir, managedID+".cookies.txt")
	if err := os.WriteFile(managedCookieFile, []byte("# Managed Cookie\n"), 0600); err != nil {
		t.Fatalf("write managed cookie: %v", err)
	}

	managedSession := Session{
		ID:             managedID,
		ProviderFamily: provider.FamilyYouTube,
		Name:           "Managed Session",
		CookieRef:      CookieRefPrefix + managedID,
		Enabled:        true,
		HealthStatus:   HealthHealthy,
	}

	repo := newMockRepo([]Session{managedSession})
	cfg := DefaultPoolConfig(provider.FamilyYouTube)
	pool := NewSessionPool(cfg, storage, repo, adapter)

	// Reload with managed session present
	pool.ReloadSessions([]Session{managedSession})

	sessions := pool.Sessions()
	if len(sessions) != 2 {
		t.Fatalf("expected 2 sessions in pool (legacy + managed), got %d: %+v", len(sessions), sessions)
	}

	hasLegacy := false
	hasManaged := false
	for _, s := range sessions {
		if s.ID == LegacySessionID {
			hasLegacy = true
		}
		if s.ID == managedID {
			hasManaged = true
		}
	}
	if !hasLegacy {
		t.Errorf("pool missing synthetic legacy session")
	}
	if !hasManaged {
		t.Errorf("pool missing managed session")
	}
}
