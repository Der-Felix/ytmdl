package mediasession_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"ytdm/backend/internal/apperr"
	"ytdm/backend/internal/mediasession"
	"ytdm/backend/internal/provider"
)

// mockRepo is an in-memory repository for unit testing the service.
type mockRepo struct {
	mu       sync.Mutex
	sessions map[string]*mediasession.Session
}

func newMockRepo() *mockRepo {
	return &mockRepo{sessions: make(map[string]*mediasession.Session)}
}

func (m *mockRepo) ListSessions(ctx context.Context, filter mediasession.Filter) ([]mediasession.Session, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var list []mediasession.Session
	for _, s := range m.sessions {
		if filter.Enabled != nil && s.Enabled != *filter.Enabled {
			continue
		}
		if filter.ProviderFamily != "" && string(s.ProviderFamily) != filter.ProviderFamily {
			continue
		}
		list = append(list, *s)
	}
	return list, nil
}

func (m *mockRepo) GetSession(ctx context.Context, id string) (*mediasession.Session, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	s, ok := m.sessions[id]
	if !ok {
		return nil, apperr.Newf(apperr.CodeSessionNotFound, "session %s not found", id)
	}
	cpy := *s
	return &cpy, nil
}

func (m *mockRepo) CreateSession(ctx context.Context, s *mediasession.Session) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if s.ID == "" {
		s.ID = fmt.Sprintf("sess_%d", len(m.sessions)+1)
	}
	now := time.Now().UTC()
	s.CreatedAt = now
	s.UpdatedAt = now
	cpy := *s
	m.sessions[s.ID] = &cpy
	return nil
}

func (m *mockRepo) UpdateSessionMetadata(ctx context.Context, id string, name string, enabled bool) (*mediasession.Session, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	s, ok := m.sessions[id]
	if !ok {
		return nil, apperr.Newf(apperr.CodeSessionNotFound, "session %s not found", id)
	}
	s.Name = name
	s.Enabled = enabled
	s.UpdatedAt = time.Now().UTC()
	cpy := *s
	return &cpy, nil
}

func (m *mockRepo) UpdateCookieRef(ctx context.Context, id string, cookieRef string) (*mediasession.Session, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	s, ok := m.sessions[id]
	if !ok {
		return nil, apperr.Newf(apperr.CodeSessionNotFound, "session %s not found", id)
	}
	s.CookieRef = cookieRef
	s.UpdatedAt = time.Now().UTC()
	cpy := *s
	return &cpy, nil
}

func (m *mockRepo) UpdateHealth(ctx context.Context, id string, params mediasession.HealthUpdate) (*mediasession.Session, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	s, ok := m.sessions[id]
	if !ok {
		return nil, apperr.Newf(apperr.CodeSessionNotFound, "session %s not found", id)
	}
	s.HealthStatus = params.HealthStatus
	s.ConsecutiveFailures = params.ConsecutiveFailures
	s.LastUsedAt = params.LastUsedAt
	s.LastSuccessAt = params.LastSuccessAt
	s.LastFailureAt = params.LastFailureAt
	s.LastFailureReason = params.LastFailureReason
	s.CooldownUntil = params.CooldownUntil
	s.UpdatedAt = time.Now().UTC()
	cpy := *s
	return &cpy, nil
}

func (m *mockRepo) DeleteSession(ctx context.Context, id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.sessions[id]; !ok {
		return apperr.Newf(apperr.CodeSessionNotFound, "session %s not found", id)
	}
	delete(m.sessions, id)
	return nil
}

// fakeProber allows injecting arbitrary probe results for testing.
type fakeProber struct {
	mu     sync.Mutex
	res    *mediasession.ProbeResult
	err    error
	probed []string
}

func (f *fakeProber) Probe(ctx context.Context, sessionID string, cookiePath string) (*mediasession.ProbeResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.probed = append(f.probed, sessionID)
	return f.res, f.err
}

func validNetscapeCookie(sentinel string) []byte {
	return []byte("# Netscape HTTP Cookie File\n" +
		"# https://curl.se/docs/http-cookies.html\n" +
		".youtube.com\tTRUE\t/\tTRUE\t2147483647\tSID\t" + sentinel + "\n" +
		".youtube.com\tTRUE\t/\tTRUE\t2147483647\tHSID\tABC123secret\n")
}

func setupTestService(t *testing.T) (*mediasession.Service, *mockRepo, *mediasession.CookieStorage, *mediasession.SessionPool, *fakeProber) {
	t.Helper()
	dir := t.TempDir()
	storage, err := mediasession.NewCookieStorage(dir, nil)
	if err != nil {
		t.Fatalf("NewCookieStorage: %v", err)
	}

	repo := newMockRepo()
	poolCfg := mediasession.PoolConfig{
		Family:                provider.FamilyYouTube,
		MaxLeasesPerSession:   2,
		SessionRequestsPerSec: 10,
		SessionBurst:          5,
		GlobalRequestsPerSec:  20,
		GlobalBurst:           10,
		AllowUnknown:          true,
	}
	pool := mediasession.NewSessionPool(poolCfg, storage, repo, nil)
	pool.SetSyncPersist(true)
	prober := &fakeProber{
		res: &mediasession.ProbeResult{
			Status:             mediasession.HealthHealthy,
			TestedAt:           time.Now().UTC(),
			MetadataOK:         true,
			UsableAudioFormats: true,
		},
	}

	svc := mediasession.NewService(mediasession.ServiceOptions{
		Repo:    repo,
		Storage: storage,
		Pool:    pool,
		Prober:  prober,
	})

	return svc, repo, storage, pool, prober
}

func TestService_CRUD(t *testing.T) {
	svc, _, storage, pool, _ := setupTestService(t)
	ctx := context.Background()

	// 1. Create Session
	created, err := svc.CreateSession(ctx, mediasession.CreateSessionRequest{
		Name: "Test Account 1",
	})
	if err != nil {
		t.Fatalf("CreateSession failed: %v", err)
	}

	if created.ID == "" {
		t.Fatalf("expected non-empty ID")
	}
	if created.HealthStatus != mediasession.HealthUnknown {
		t.Errorf("initial health = %s, want unknown", created.HealthStatus)
	}
	if created.HasCredentials {
		t.Errorf("has_credentials should be false initially")
	}
	if created.InUse {
		t.Errorf("in_use should be false")
	}

	// 2. List Sessions
	list, err := svc.ListSessions(ctx)
	if err != nil {
		t.Fatalf("ListSessions failed: %v", err)
	}
	if len(list) != 1 || list[0].ID != created.ID {
		t.Fatalf("ListSessions unexpected result: %+v", list)
	}

	// 3. Get Session
	got, err := svc.GetSession(ctx, created.ID)
	if err != nil {
		t.Fatalf("GetSession failed: %v", err)
	}
	if got.Name != "Test Account 1" {
		t.Errorf("got name = %s, want Test Account 1", got.Name)
	}

	// 4. Update Session (Rename & Disable)
	newName := "Renamed Account"
	disabled := false
	updated, err := svc.UpdateSession(ctx, created.ID, mediasession.UpdateSessionRequest{
		Name:    &newName,
		Enabled: &disabled,
	})
	if err != nil {
		t.Fatalf("UpdateSession failed: %v", err)
	}
	if updated.Name != newName || updated.Enabled != false {
		t.Errorf("UpdateSession unexpected result: %+v", updated)
	}
	if updated.HealthStatus != mediasession.HealthUnknown {
		t.Errorf("disabling session should not change health status, got %s", updated.HealthStatus)
	}

	// 5. Upload cookies
	_, _, err = svc.UploadCookies(ctx, created.ID, validNetscapeCookie("my-secret-token"))
	if err != nil {
		t.Fatalf("UploadCookies failed: %v", err)
	}

	// 6. Delete Session
	if err := svc.DeleteSession(ctx, created.ID); err != nil {
		t.Fatalf("DeleteSession failed: %v", err)
	}

	// Verify deleted from repo, pool, and storage
	_, err = svc.GetSession(ctx, created.ID)
	if err == nil || apperr.CodeOf(err) != apperr.CodeSessionNotFound {
		t.Errorf("GetSession after delete should return not found, got %v", err)
	}
	if pool.GetSession(created.ID) != nil {
		t.Errorf("session should be removed from pool")
	}
	if storage.HasCookie("managed://cookies/" + created.ID) {
		t.Errorf("cookie file should be removed from storage")
	}
}

func TestService_UploadCookies_NetscapeValidation(t *testing.T) {
	svc, _, _, _, _ := setupTestService(t)
	ctx := context.Background()

	sess, err := svc.CreateSession(ctx, mediasession.CreateSessionRequest{Name: "Validation Test"})
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	secretSentinel := "ULTRA_CONFIDENTIAL_SENTINEL_TOKEN_12345"

	tests := []struct {
		name        string
		data        []byte
		wantErrCode apperr.Code
	}{
		{
			name:        "empty file",
			data:        []byte{},
			wantErrCode: apperr.CodeInvalidRequest,
		},
		{
			name:        "oversized file",
			data:        make([]byte, 1024*1024+10),
			wantErrCode: apperr.CodeInvalidRequest,
		},
		{
			name:        "binary NUL byte",
			data:        []byte("# Netscape HTTP Cookie File\n.youtube.com\tTRUE\t/\tTRUE\t2147483647\tSID\tsecret\x00corrupt\n"),
			wantErrCode: apperr.CodeInvalidRequest,
		},
		{
			name:        "malformed record (missing fields)",
			data:        []byte("# Netscape HTTP Cookie File\n.youtube.com\tTRUE\t/\tnot_enough_fields\n"),
			wantErrCode: apperr.CodeInvalidRequest,
		},
		{
			name:        "extremely long line",
			data:        []byte("# Netscape HTTP Cookie File\n" + strings.Repeat("A", 4097) + "\n"),
			wantErrCode: apperr.CodeInvalidRequest,
		},
		{
			name:        "unrecognized non-cookie text",
			data:        []byte("just some random prose that is not cookies\nanother line\n"),
			wantErrCode: apperr.CodeInvalidRequest,
		},
		{
			name:        "valid Netscape file",
			data:        validNetscapeCookie(secretSentinel),
			wantErrCode: "",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			view, _, err := svc.UploadCookies(ctx, sess.ID, tc.data)
			if tc.wantErrCode != "" {
				if err == nil {
					t.Fatalf("expected error code %s, got nil", tc.wantErrCode)
				}
				if apperr.CodeOf(err) != tc.wantErrCode {
					t.Errorf("error code = %s, want %s", apperr.CodeOf(err), tc.wantErrCode)
				}
				// CRITICAL SECURITY ASSERTION: Sentinel must NEVER be echoed in error
				if strings.Contains(err.Error(), secretSentinel) {
					t.Fatalf("SECURITY VIOLATION: secret sentinel was leaked in error message: %s", err.Error())
				}
			} else {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				if !view.HasCredentials {
					t.Errorf("has_credentials should be true after upload")
				}
				// Initial upload leaves health UNKNOWN
				if view.HealthStatus != mediasession.HealthUnknown {
					t.Errorf("initial upload health = %s, want unknown", view.HealthStatus)
				}
			}
		})
	}
}

func TestService_SafeReplace(t *testing.T) {
	svc, _, storage, _, prober := setupTestService(t)
	ctx := context.Background()

	sess, err := svc.CreateSession(ctx, mediasession.CreateSessionRequest{Name: "Replace Test"})
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	initialSecret := "INITIAL_GOOD_COOKIE_VAL_111"
	_, _, err = svc.UploadCookies(ctx, sess.ID, validNetscapeCookie(initialSecret))
	if err != nil {
		t.Fatalf("initial upload: %v", err)
	}

	initialCookieRef := "managed://cookies/" + sess.ID
	readInitial, err := storage.Read(initialCookieRef)
	if err != nil {
		t.Fatalf("read initial cookie: %v", err)
	}
	if !strings.Contains(string(readInitial), initialSecret) {
		t.Fatalf("initial secret not found in storage")
	}

	// 1. Replacement upload is malformed -> old cookie remains intact
	_, _, err = svc.UploadCookies(ctx, sess.ID, []byte("MALFORMED GARBAGE"))
	if err == nil {
		t.Fatalf("expected error on malformed replacement")
	}
	readAfterMalformed, _ := storage.Read(initialCookieRef)
	if string(readAfterMalformed) != string(readInitial) {
		t.Fatalf("old cookie was corrupted by malformed replacement!")
	}

	// 2. Replacement upload is valid format, but probe fails (e.g. AUTH_FAILED)
	prober.mu.Lock()
	prober.res = &mediasession.ProbeResult{
		Status:          mediasession.HealthAuthFailed,
		FailureCategory: "SESSION_AUTH_FAILED",
	}
	prober.err = apperr.New(apperr.CodeSessionAuthFailed, "cookies are expired")
	prober.mu.Unlock()

	badReplacementSecret := "BAD_REPLACEMENT_COOKIE_VAL_222"
	_, _, err = svc.UploadCookies(ctx, sess.ID, validNetscapeCookie(badReplacementSecret))
	if err == nil {
		t.Fatalf("expected error on failed candidate probe")
	}
	readAfterFailedProbe, _ := storage.Read(initialCookieRef)
	if string(readAfterFailedProbe) != string(readInitial) {
		t.Fatalf("old cookie was destroyed despite failed candidate probe!")
	}
	if strings.Contains(string(readAfterFailedProbe), badReplacementSecret) {
		t.Fatalf("failed candidate secret was promoted to official storage!")
	}

	// 3. Replacement upload is valid and probe succeeds -> atomically promoted!
	goodReplacementSecret := "GOOD_REPLACEMENT_COOKIE_VAL_333"
	prober.mu.Lock()
	prober.res = &mediasession.ProbeResult{
		Status:             mediasession.HealthHealthy,
		TestedAt:           time.Now().UTC(),
		MetadataOK:         true,
		UsableAudioFormats: true,
	}
	prober.err = nil
	prober.mu.Unlock()

	view, probeRes, err := svc.UploadCookies(ctx, sess.ID, validNetscapeCookie(goodReplacementSecret))
	if err != nil {
		t.Fatalf("replacement failed: %v", err)
	}
	if probeRes == nil || probeRes.Status != mediasession.HealthHealthy {
		t.Fatalf("expected healthy probe result on successful replacement")
	}
	if view.HealthStatus != mediasession.HealthHealthy {
		t.Errorf("health status after successful replacement = %s, want healthy", view.HealthStatus)
	}

	readAfterSuccess, _ := storage.Read(initialCookieRef)
	if !strings.Contains(string(readAfterSuccess), goodReplacementSecret) {
		t.Fatalf("good replacement secret was not promoted!")
	}
}

func TestService_InUseMutationProtection(t *testing.T) {
	svc, _, _, pool, _ := setupTestService(t)
	ctx := context.Background()

	sess, err := svc.CreateSession(ctx, mediasession.CreateSessionRequest{Name: "In Use Test"})
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	_, _, err = svc.UploadCookies(ctx, sess.ID, validNetscapeCookie("in-use-cookie"))
	if err != nil {
		t.Fatalf("UploadCookies: %v", err)
	}

	// Simulate active download retaining data-plane reference
	pool.RetainDataPlane(sess.ID)
	if !pool.IsInUse(sess.ID) {
		t.Fatalf("expected IsInUse = true")
	}

	// 1. DELETE while in use -> 409 Conflict
	err = svc.DeleteSession(ctx, sess.ID)
	if err == nil {
		t.Fatalf("expected error deleting in-use session")
	}
	if apperr.CodeOf(err) != apperr.CodeSessionInUse {
		t.Errorf("delete in-use error code = %s, want %s", apperr.CodeOf(err), apperr.CodeSessionInUse)
	}

	// 2. REPLACE while in use -> 409 Conflict
	_, _, err = svc.UploadCookies(ctx, sess.ID, validNetscapeCookie("new-cookie"))
	if err == nil {
		t.Fatalf("expected error replacing in-use session cookies")
	}
	if apperr.CodeOf(err) != apperr.CodeSessionInUse {
		t.Errorf("replace in-use error code = %s, want %s", apperr.CodeOf(err), apperr.CodeSessionInUse)
	}

	// 3. DISABLE while in use -> allowed, excludes new leases without corrupting current operation
	disabled := false
	updated, err := svc.UpdateSession(ctx, sess.ID, mediasession.UpdateSessionRequest{Enabled: &disabled})
	if err != nil {
		t.Fatalf("UpdateSession (disable) failed: %v", err)
	}
	if updated.Enabled != false {
		t.Errorf("expected enabled = false")
	}

	// 4. Release in-use reference -> DELETE now succeeds
	pool.ReleaseDataPlane(sess.ID)
	if pool.IsInUse(sess.ID) {
		t.Fatalf("expected IsInUse = false after release")
	}

	if err := svc.DeleteSession(ctx, sess.ID); err != nil {
		t.Fatalf("DeleteSession failed after release: %v", err)
	}
}

func TestService_ProbeOutcomes(t *testing.T) {
	svc, repo, _, _, prober := setupTestService(t)
	ctx := context.Background()

	sess, err := svc.CreateSession(ctx, mediasession.CreateSessionRequest{Name: "Probe Outcomes"})
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	_, _, err = svc.UploadCookies(ctx, sess.ID, validNetscapeCookie("probe-creds"))
	if err != nil {
		t.Fatalf("UploadCookies: %v", err)
	}

	// 1. Probe Success -> HealthHealthy
	prober.mu.Lock()
	prober.res = &mediasession.ProbeResult{
		Status:             mediasession.HealthHealthy,
		TestedAt:           time.Now().UTC(),
		MetadataOK:         true,
		UsableAudioFormats: true,
	}
	prober.err = nil
	prober.mu.Unlock()

	probeRes, view, err := svc.ProbeSession(ctx, sess.ID)
	if err != nil {
		t.Fatalf("ProbeSession: %v", err)
	}
	if probeRes.Status != mediasession.HealthHealthy || view.HealthStatus != mediasession.HealthHealthy {
		t.Errorf("expected healthy status, got probe=%s, view=%s", probeRes.Status, view.HealthStatus)
	}

	// Check DB persistence
	dbSess, _ := repo.GetSession(ctx, sess.ID)
	if dbSess.HealthStatus != mediasession.HealthHealthy {
		t.Errorf("db health status = %s, want healthy", dbSess.HealthStatus)
	}

	// Advance time to bypass debounce
	time.Sleep(2100 * time.Millisecond)

	// 2. Probe Auth Failed -> HealthAuthFailed
	prober.mu.Lock()
	prober.res = &mediasession.ProbeResult{
		Status:          mediasession.HealthAuthFailed,
		FailureCategory: "SESSION_AUTH_FAILED",
	}
	prober.err = apperr.New(apperr.CodeSessionAuthFailed, "login required")
	prober.mu.Unlock()

	probeRes, view, err = svc.ProbeSession(ctx, sess.ID)
	if err != nil {
		t.Fatalf("ProbeSession unexpected error: %v", err)
	}
	if probeRes.Status != mediasession.HealthAuthFailed {
		t.Errorf("probe status = %s, want auth_failed", probeRes.Status)
	}
	if view != nil && view.HealthStatus != mediasession.HealthAuthFailed {
		t.Errorf("view health = %s, want auth_failed", view.HealthStatus)
	}
}

func TestService_ProbeDebounce(t *testing.T) {
	svc, _, _, _, _ := setupTestService(t)
	ctx := context.Background()

	sess, err := svc.CreateSession(ctx, mediasession.CreateSessionRequest{Name: "Debounce Test"})
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	_, _, _ = svc.UploadCookies(ctx, sess.ID, validNetscapeCookie("creds"))

	// First probe
	_, _, err = svc.ProbeSession(ctx, sess.ID)
	if err != nil {
		t.Fatalf("first probe failed: %v", err)
	}

	// Immediate second probe should be debounced with RateLimited
	_, _, err = svc.ProbeSession(ctx, sess.ID)
	if err == nil {
		t.Fatalf("expected second probe to be debounced")
	}
	if apperr.CodeOf(err) != apperr.CodeRateLimited {
		t.Errorf("debounced probe error code = %s, want %s", apperr.CodeOf(err), apperr.CodeRateLimited)
	}
}

func TestService_LegacySession(t *testing.T) {
	dir := t.TempDir()
	legacyFile := filepath.Join(dir, "legacy.cookies.txt")
	sentinel := "LEGACY_SECRET_SENTINEL_999"
	if err := os.WriteFile(legacyFile, validNetscapeCookie(sentinel), 0600); err != nil {
		t.Fatalf("write legacy file: %v", err)
	}

	legacyAdapter := mediasession.NewLegacyAdapter(legacyFile)
	storage, _ := mediasession.NewCookieStorage(dir, legacyAdapter)
	repo := newMockRepo()
	poolCfg := mediasession.PoolConfig{
		Family:       provider.FamilyYouTube,
		AllowUnknown: true,
	}
	pool := mediasession.NewSessionPool(poolCfg, storage, nil, legacyAdapter)
	prober := &fakeProber{
		res: &mediasession.ProbeResult{
			Status:             mediasession.HealthHealthy,
			MetadataOK:         true,
			UsableAudioFormats: true,
		},
	}

	svc := mediasession.NewService(mediasession.ServiceOptions{
		Repo:          repo,
		Storage:       storage,
		Pool:          pool,
		LegacyAdapter: legacyAdapter,
		Prober:        prober,
	})
	ctx := context.Background()

	// 1. List includes legacy session
	list, err := svc.ListSessions(ctx)
	if err != nil {
		t.Fatalf("ListSessions: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("expected 1 session (legacy), got %d", len(list))
	}
	leg := list[0]
	if leg.ID != mediasession.LegacySessionID {
		t.Errorf("legacy ID = %s, want %s", leg.ID, mediasession.LegacySessionID)
	}
	if !leg.HasCredentials {
		t.Errorf("legacy has_credentials should be true")
	}

	// CRITICAL SECURITY ASSERTION: raw filesystem path must NEVER appear in DTO
	if strings.Contains(leg.Name, dir) || strings.Contains(leg.Name, "legacy.cookies.txt") {
		t.Fatalf("raw filesystem path leaked in session name: %s", leg.Name)
	}

	// 2. Probe legacy session succeeds
	probeRes, _, err := svc.ProbeSession(ctx, mediasession.LegacySessionID)
	if err != nil {
		t.Fatalf("ProbeSession legacy failed: %v", err)
	}
	if probeRes.Status != mediasession.HealthHealthy {
		t.Errorf("legacy probe status = %s, want healthy", probeRes.Status)
	}

	// 3. Mutating operations rejected
	if err := svc.DeleteSession(ctx, mediasession.LegacySessionID); err == nil {
		t.Errorf("expected delete legacy to be rejected")
	}
	if _, _, err := svc.UploadCookies(ctx, mediasession.LegacySessionID, validNetscapeCookie("x")); err == nil {
		t.Errorf("expected upload cookies to legacy to be rejected")
	}
	newName := "Hack"
	if _, err := svc.UpdateSession(ctx, mediasession.LegacySessionID, mediasession.UpdateSessionRequest{Name: &newName}); err == nil {
		t.Errorf("expected update legacy to be rejected")
	}

	// File on disk must remain untouched
	content, _ := os.ReadFile(legacyFile)
	if !strings.Contains(string(content), sentinel) {
		t.Fatalf("legacy file on disk was corrupted or modified!")
	}
}
