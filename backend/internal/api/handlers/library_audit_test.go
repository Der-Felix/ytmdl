package handlers_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"ytdm/backend/internal/api"
	"ytdm/backend/internal/api/handlers"
	"ytdm/backend/internal/auth"
	"ytdm/backend/internal/database/dbtest"
	"ytdm/backend/internal/database/repository"
	"ytdm/backend/internal/ffmpeg"
	"ytdm/backend/internal/library"
	"ytdm/backend/internal/metadata"
	"ytdm/backend/internal/music"
	"ytdm/backend/internal/storage"
)

type auditTestEnv struct {
	server     http.Handler
	adminToken string
	userToken  string
	svc        *library.Service
	audit      *repository.Audit
	catalog    *repository.Catalog
	files      *repository.Files
	lib        *storage.Library
	root       string
}

func setupAuditTestEnv(t *testing.T) *auditTestEnv {
	t.Helper()
	db := dbtest.Open(t)
	t.Cleanup(func() { db.Close() })

	root := t.TempDir()
	lib, err := storage.NewLibrary(root)
	if err != nil {
		t.Fatalf("new library: %v", err)
	}

	catalogRepo := repository.NewCatalog(db)
	filesRepo := repository.NewFiles(db)
	auditRepo := repository.NewAudit(db)
	usersRepo := repository.NewUsers(db)
	sessionsRepo := repository.NewSessions(db)

	ctx := context.Background()
	limiter := auth.NewLimiter(100, time.Minute)
	t.Cleanup(limiter.Close)
	authSvc := auth.NewService(usersRepo, sessionsRepo, limiter, nil)

	adminSetup, err := authSvc.Setup(ctx, auth.SetupRequest{
		Username:    "admin",
		DisplayName: "Administrator",
		Password:    "password123!",
	}, "127.0.0.1", "test")
	if err != nil {
		t.Fatalf("setup admin: %v", err)
	}

	_, err = authSvc.CreateUser(ctx, auth.CreateUserRequest{
		Username:    "regular",
		DisplayName: "Regular User",
		Password:    "password123!",
		Role:        auth.RoleUser,
	})
	if err != nil {
		t.Fatalf("create user: %v", err)
	}

	userLogin, err := authSvc.Login(ctx, auth.LoginRequest{
		Username: "regular",
		Password: "password123!",
	}, "127.0.0.1", "test")
	if err != nil {
		t.Fatalf("login user: %v", err)
	}

	ffRunner := ffmpeg.New("ffmpeg", 30*time.Second)
	tagger := metadata.NewTagger(ffRunner)

	libSvc, err := library.NewService(library.ServiceOptions{
		Library: lib,
		Files:   filesRepo,
		Catalog: catalogRepo,
		Tagger:  tagger,
		Audit:   auditRepo,
	})
	if err != nil {
		t.Fatalf("new library service: %v", err)
	}

	// Minimal deps for router
	deps := handlers.Deps{
		Catalog:        catalogRepo,
		Files:          filesRepo,
		Library:        lib,
		LibraryService: libSvc,
		Auth:           authSvc,
		StartedAt:      time.Now(),
		Version:        "0.13.6-test",
	}

	h := handlers.NewForTest(deps)

	router, err := api.NewRouter(api.RouterOptions{
		Handlers: h,
		Auth:     authSvc,
	})
	if err != nil {
		t.Fatalf("NewRouter: %v", err)
	}

	return &auditTestEnv{
		server:     router,
		adminToken: adminSetup.SessionToken,
		userToken:  userLogin.SessionToken,
		svc:        libSvc,
		audit:      auditRepo,
		catalog:    catalogRepo,
		files:      filesRepo,
		lib:        lib,
		root:       root,
	}
}

func doRequest(t *testing.T, handler http.Handler, method, target string, body any, sessionToken string) *httptest.ResponseRecorder {
	t.Helper()
	var bodyReader *bytes.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("marshal body: %v", err)
		}
		bodyReader = bytes.NewReader(data)
	} else {
		bodyReader = bytes.NewReader(nil)
	}

	req := httptest.NewRequest(method, target, bodyReader)
	req.Header.Set("Content-Type", "application/json")

	// Set CSRF header & cookie for mutating requests
	req.Header.Set("X-CSRF-Token", "test-csrf-token")
	req.AddCookie(&http.Cookie{Name: "ytmdl_csrf", Value: "test-csrf-token"})

	if sessionToken != "" {
		req.AddCookie(&http.Cookie{Name: "ytmdl_session", Value: sessionToken})
	}

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	return rec
}

func TestLibraryAuditAPI_FullLifecycle(t *testing.T) {
	env := setupAuditTestEnv(t)
	ctx := context.Background()

	// 1. Setup DB state
	artist := music.Artist{ID: music.NewID(), Name: "Massive Attack", Provider: "spotify", SourceID: "art_ma"}
	release := music.Release{ID: music.NewID(), Title: "Mezzanine", AlbumArtist: "Massive Attack", ReleaseType: music.ReleaseAlbum, Year: 1998, TrackCount: 11}
	track := music.Track{ID: music.NewID(), ReleaseID: release.ID, Title: "Angel", Artists: []string{"Massive Attack"}, TrackNumber: 1, DiscNumber: 1, DurationMS: 379000}

	_, _ = env.catalog.UpsertArtist(ctx, artist)
	_, _ = env.catalog.UpsertRelease(ctx, release, artist.ID)
	_, _ = env.catalog.UpsertTrack(ctx, track, release.ID, artist.ID, 0)

	// File missing on disk, but registered in DB
	missingRel := "Massive Attack/1998 - Mezzanine/01 - Angel.opus"
	_, _ = env.files.Upsert(ctx, music.File{ID: music.NewID(), TrackID: track.ID, Path: missingRel, SizeBytes: 30, DurationMS: 379000})

	// Untracked file on disk
	untrackedRel := "Massive Attack/2026 - Bootlegs/01 - Dissolved Girl (Live).opus"
	untrackedAbs := filepath.Join(env.root, untrackedRel)
	_ = os.MkdirAll(filepath.Dir(untrackedAbs), 0o755)
	_ = os.WriteFile(untrackedAbs, []byte("live audio data"), 0o644)

	// 2. Start Audit (Quick mode) as Admin -> 202 Accepted
	startReq := handlers.StartAuditRequest{Mode: music.AuditModeQuick}
	rec := doRequest(t, env.server, http.MethodPost, "/api/v1/library/audits", startReq, env.adminToken)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("expected 202 Accepted, got %d: %s", rec.Code, rec.Body.String())
	}

	var startResp struct {
		Data music.AuditRun `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &startResp); err != nil {
		t.Fatalf("unmarshal start resp: %v", err)
	}
	runID := startResp.Data.ID

	// Wait for audit to complete
	_, err := env.svc.WaitForAudit(ctx, runID, 5*time.Second)
	if err != nil {
		t.Fatalf("wait for audit: %v", err)
	}

	// 3. GET /api/v1/library/audits/current -> 200 OK
	recCurr := doRequest(t, env.server, http.MethodGet, "/api/v1/library/audits/current", nil, env.adminToken)
	if recCurr.Code != http.StatusOK {
		t.Fatalf("expected 200 for current audit, got %d: %s", recCurr.Code, recCurr.Body.String())
	}

	// 4. GET /api/v1/library/audits -> 200 OK
	recList := doRequest(t, env.server, http.MethodGet, "/api/v1/library/audits?limit=10&offset=0", nil, env.adminToken)
	if recList.Code != http.StatusOK {
		t.Fatalf("expected 200 for list audits, got %d", recList.Code)
	}

	// 5. GET /api/v1/library/audits/{id} -> 200 OK
	recGet := doRequest(t, env.server, http.MethodGet, "/api/v1/library/audits/"+runID, nil, env.adminToken)
	if recGet.Code != http.StatusOK {
		t.Fatalf("expected 200 for get audit, got %d", recGet.Code)
	}

	// 6. GET /api/v1/library/audits/{id}/findings -> 200 OK
	recFindings := doRequest(t, env.server, http.MethodGet, "/api/v1/library/audits/"+runID+"/findings?limit=50", nil, env.adminToken)
	if recFindings.Code != http.StatusOK {
		t.Fatalf("expected 200 for get findings, got %d: %s", recFindings.Code, recFindings.Body.String())
	}

	var findingsResp struct {
		Data []music.AuditFinding `json:"data"`
		Meta struct {
			Count int `json:"count"`
			Total int `json:"total"`
		} `json:"meta"`
	}
	if err := json.Unmarshal(recFindings.Body.Bytes(), &findingsResp); err != nil {
		t.Fatalf("unmarshal findings: %v", err)
	}
	if len(findingsResp.Data) == 0 {
		t.Fatalf("expected findings, got 0")
	}

	// 7. Preview Repair for finding
	var untrackedFinding *music.AuditFinding
	for _, f := range findingsResp.Data {
		if f.FindingCode == music.FindingFileUntracked {
			untrackedFinding = &f
			break
		}
	}
	if untrackedFinding == nil {
		t.Fatalf("expected FILE_UNTRACKED finding")
	}

	prevReq := handlers.PreviewRepairsRequest{
		FindingIDs: []string{untrackedFinding.ID},
	}
	recPrev := doRequest(t, env.server, http.MethodPost, "/api/v1/library/repairs/preview", prevReq, env.adminToken)
	if recPrev.Code != http.StatusOK {
		t.Fatalf("expected 200 for preview repairs, got %d: %s", recPrev.Code, recPrev.Body.String())
	}

	// 8. Apply Quarantine Repair
	applyReq := library.RepairApplyRequest{
		Confirm: true,
		Actions: []library.RepairItemAction{
			{FindingID: untrackedFinding.ID, Action: music.ActionQuarantineFile},
		},
	}
	recApply := doRequest(t, env.server, http.MethodPost, "/api/v1/library/repairs/apply", applyReq, env.adminToken)
	if recApply.Code != http.StatusOK {
		t.Fatalf("expected 200 for apply repairs, got %d: %s", recApply.Code, recApply.Body.String())
	}

	var applyResp struct {
		Data library.RepairApplyResult `json:"data"`
	}
	if err := json.Unmarshal(recApply.Body.Bytes(), &applyResp); err != nil {
		t.Fatalf("unmarshal apply resp: %v", err)
	}
	if applyResp.Data.Quarantined != 1 {
		t.Fatalf("expected quarantined=1, got %+v", applyResp.Data)
	}
}

func TestLibraryAuditAPI_AuthRestrictions(t *testing.T) {
	env := setupAuditTestEnv(t)
	ctx := context.Background()

	runID := music.NewID()
	now := time.Now().UTC()
	_ = env.audit.CreateRun(ctx, music.AuditRun{
		ID:        runID,
		Mode:      music.AuditModeQuick,
		Status:    music.AuditRunRunning,
		StartedAt: now,
		CreatedAt: now,
	})

	// 1. GET /api/v1/library/audits
	// unauth -> 401
	rec := doRequest(t, env.server, http.MethodGet, "/api/v1/library/audits", nil, "")
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("GET /audits unauth: expected 401, got %d", rec.Code)
	}
	// regular user -> 200
	rec = doRequest(t, env.server, http.MethodGet, "/api/v1/library/audits", nil, env.userToken)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /audits user: expected 200, got %d", rec.Code)
	}
	// admin -> 200
	rec = doRequest(t, env.server, http.MethodGet, "/api/v1/library/audits", nil, env.adminToken)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /audits admin: expected 200, got %d", rec.Code)
	}

	// 2. GET /api/v1/library/audits/{id}
	rec = doRequest(t, env.server, http.MethodGet, "/api/v1/library/audits/"+runID, nil, "")
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("GET /audits/{id} unauth: expected 401, got %d", rec.Code)
	}
	rec = doRequest(t, env.server, http.MethodGet, "/api/v1/library/audits/"+runID, nil, env.userToken)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /audits/{id} user: expected 200, got %d", rec.Code)
	}
	rec = doRequest(t, env.server, http.MethodGet, "/api/v1/library/audits/"+runID, nil, env.adminToken)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /audits/{id} admin: expected 200, got %d", rec.Code)
	}

	// 3. GET /api/v1/library/audits/{id}/findings
	rec = doRequest(t, env.server, http.MethodGet, "/api/v1/library/audits/"+runID+"/findings", nil, "")
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("GET /audits/{id}/findings unauth: expected 401, got %d", rec.Code)
	}
	rec = doRequest(t, env.server, http.MethodGet, "/api/v1/library/audits/"+runID+"/findings", nil, env.userToken)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /audits/{id}/findings user: expected 200, got %d", rec.Code)
	}
	rec = doRequest(t, env.server, http.MethodGet, "/api/v1/library/audits/"+runID+"/findings", nil, env.adminToken)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /audits/{id}/findings admin: expected 200, got %d", rec.Code)
	}

	// 4. POST /api/v1/library/audits (start audit)
	rec = doRequest(t, env.server, http.MethodPost, "/api/v1/library/audits", handlers.StartAuditRequest{Mode: music.AuditModeQuick}, "")
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("POST /audits unauth: expected 401, got %d", rec.Code)
	}
	rec = doRequest(t, env.server, http.MethodPost, "/api/v1/library/audits", handlers.StartAuditRequest{Mode: music.AuditModeQuick}, env.userToken)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("POST /audits user: expected 403, got %d", rec.Code)
	}

	// 5. POST /api/v1/library/audits/{id}/cancel
	rec = doRequest(t, env.server, http.MethodPost, "/api/v1/library/audits/"+runID+"/cancel", nil, "")
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("POST /cancel unauth: expected 401, got %d", rec.Code)
	}
	rec = doRequest(t, env.server, http.MethodPost, "/api/v1/library/audits/"+runID+"/cancel", nil, env.userToken)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("POST /cancel user: expected 403, got %d", rec.Code)
	}
	rec = doRequest(t, env.server, http.MethodPost, "/api/v1/library/audits/"+runID+"/cancel", nil, env.adminToken)
	if rec.Code != http.StatusOK {
		t.Fatalf("POST /cancel admin valid CSRF: expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	// 6. POST /api/v1/library/repairs/preview
	prevReq := handlers.PreviewRepairsRequest{FindingIDs: []string{"some_id"}}
	rec = doRequest(t, env.server, http.MethodPost, "/api/v1/library/repairs/preview", prevReq, "")
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("POST /preview unauth: expected 401, got %d", rec.Code)
	}
	rec = doRequest(t, env.server, http.MethodPost, "/api/v1/library/repairs/preview", prevReq, env.userToken)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("POST /preview user: expected 403, got %d", rec.Code)
	}
	rec = doRequest(t, env.server, http.MethodPost, "/api/v1/library/repairs/preview", prevReq, env.adminToken)
	if rec.Code != http.StatusOK {
		t.Fatalf("POST /preview admin valid CSRF: expected 200, got %d", rec.Code)
	}

	// 7. POST /api/v1/library/repairs/apply
	applyReq := library.RepairApplyRequest{Confirm: true}
	rec = doRequest(t, env.server, http.MethodPost, "/api/v1/library/repairs/apply", applyReq, "")
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("POST /apply unauth: expected 401, got %d", rec.Code)
	}
	rec = doRequest(t, env.server, http.MethodPost, "/api/v1/library/repairs/apply", applyReq, env.userToken)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("POST /apply user: expected 403, got %d", rec.Code)
	}

	// 8. CSRF Protection for all mutating POST endpoints
	mutatingEndpoints := []struct {
		url  string
		body any
	}{
		{"/api/v1/library/audits", handlers.StartAuditRequest{Mode: music.AuditModeQuick}},
		{"/api/v1/library/audits/" + runID + "/cancel", nil},
		{"/api/v1/library/repairs/preview", prevReq},
		{"/api/v1/library/repairs/apply", applyReq},
	}
	for _, ep := range mutatingEndpoints {
		var reqBody []byte
		if ep.body != nil {
			reqBody, _ = json.Marshal(ep.body)
		}
		reqNoCSRF := httptest.NewRequest(http.MethodPost, ep.url, bytes.NewReader(reqBody))
		reqNoCSRF.Header.Set("Content-Type", "application/json")
		reqNoCSRF.AddCookie(&http.Cookie{Name: "ytmdl_session", Value: env.adminToken})
		// Omit X-CSRF-Token
		recNoCSRF := httptest.NewRecorder()
		env.server.ServeHTTP(recNoCSRF, reqNoCSRF)
		if recNoCSRF.Code != http.StatusForbidden {
			t.Fatalf("expected 403 Forbidden for missing CSRF on %s, got %d", ep.url, recNoCSRF.Code)
		}
	}
}
