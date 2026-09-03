package handlers_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
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

type realHTTPClient struct {
	client    *http.Client
	baseURL   string
	csrfToken string
}

func (c *realHTTPClient) do(method, path string, body any) (*http.Response, []byte, error) {
	var bodyReader io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return nil, nil, err
		}
		bodyReader = bytes.NewReader(data)
	}

	req, err := http.NewRequest(method, c.baseURL+path, bodyReader)
	if err != nil {
		return nil, nil, err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if c.csrfToken != "" {
		req.Header.Set("X-CSRF-Token", c.csrfToken)
	}

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, nil, err
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, nil, err
	}

	// Update CSRF token from header or cookie if present
	if token := resp.Header.Get("X-CSRF-Token"); token != "" {
		c.csrfToken = token
	}

	return resp, respBody, nil
}

func TestE2E_RealHTTP_AuditAndRepairWorkflow(t *testing.T) {
	db := dbtest.Open(t)
	t.Cleanup(func() { db.Close() })

	root := t.TempDir()
	lib, err := storage.NewLibrary(root)
	if err != nil {
		t.Fatalf("new library: %v", err)
	}

	catRepo := repository.NewCatalog(db)
	filesRepo := repository.NewFiles(db)
	auditRepo := repository.NewAudit(db)
	usersRepo := repository.NewUsers(db)
	sessionsRepo := repository.NewSessions(db)

	ctx := context.Background()
	limiter := auth.NewLimiter(100, time.Minute)
	t.Cleanup(limiter.Close)
	authSvc := auth.NewService(usersRepo, sessionsRepo, limiter, nil)

	_, err = authSvc.Setup(ctx, auth.SetupRequest{
		Username:    "admin",
		DisplayName: "Administrator",
		Password:    "password123!",
	}, "127.0.0.1", "test")
	if err != nil {
		t.Fatalf("setup admin: %v", err)
	}

	ffRunner := ffmpeg.New("ffmpeg", 30*time.Second)
	tagger := metadata.NewTagger(ffRunner)

	libSvc, err := library.NewService(library.ServiceOptions{
		Library: lib,
		Files:   filesRepo,
		Catalog: catRepo,
		Tagger:  tagger,
		Audit:   auditRepo,
	})
	if err != nil {
		t.Fatalf("new library service: %v", err)
	}

	deps := handlers.Deps{
		Catalog:        catRepo,
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

	ts := httptest.NewServer(router)
	t.Cleanup(ts.Close)

	jar, _ := cookiejar.New(nil)
	httpCli := &realHTTPClient{
		client:  &http.Client{Jar: jar, Timeout: 10 * time.Second},
		baseURL: ts.URL,
	}

	// 1. Seed CSRF Cookie via initial GET
	_, _, err = httpCli.do(http.MethodGet, "/api/v1/auth/status", nil)
	if err != nil {
		t.Fatalf("auth status failed: %v", err)
	}

	u, _ := url.Parse(ts.URL)
	for _, cookie := range jar.Cookies(u) {
		if cookie.Name == "ytmdl_csrf" {
			httpCli.csrfToken = cookie.Value
		}
	}

	// 2. Real HTTP Login as Admin
	resp, body, err := httpCli.do(http.MethodPost, "/api/v1/auth/login", auth.LoginRequest{
		Username: "admin",
		Password: "password123!",
	})
	if err != nil || resp.StatusCode != http.StatusOK {
		t.Fatalf("login failed: %v, body: %s", err, string(body))
	}

	for _, cookie := range jar.Cookies(u) {
		if cookie.Name == "ytmdl_csrf" {
			httpCli.csrfToken = cookie.Value
		}
	}

	// 2. Seed Test Fixtures on Disk and DB
	// Fixture A: Valid Track with PATH_MISMATCH
	artA, _ := catRepo.UpsertArtist(ctx, music.Artist{ID: music.NewID(), Name: "Beatles", Provider: "spotify", SourceID: "art_b"})
	relA, _ := catRepo.UpsertRelease(ctx, music.Release{ID: music.NewID(), Title: "Abbey Road", AlbumArtist: "Beatles", ReleaseType: music.ReleaseAlbum, Year: 1969, TrackCount: 1}, artA.ID)
	trkA, _ := catRepo.UpsertTrack(ctx, music.Track{ID: music.NewID(), ReleaseID: relA.ID, Title: "Come Together", Artists: []string{"Beatles"}, Album: relA.Title, AlbumArtist: relA.AlbumArtist, Year: 1969, TrackNumber: 1, DiscNumber: 1, DurationMS: 259000}, relA.ID, artA.ID, 0)
	oldPathA := "Beatles/Old Album/01 - Come Together.opus"
	absOldA := filepath.Join(root, oldPathA)
	_ = os.MkdirAll(filepath.Dir(absOldA), 0o755)
	_ = os.WriteFile(absOldA, []byte("beatles audio bytes payload"), 0o644)
	_, _ = filesRepo.Upsert(ctx, music.File{ID: music.NewID(), TrackID: trkA.ID, Path: oldPathA, SizeBytes: 28, DurationMS: 259000})

	// Fixture B: Legacy Duplicate Candidate for QUARANTINE
	legacyRel := "Beatles/Legacy/duplicate.opus"
	absLegacy := filepath.Join(root, legacyRel)
	_ = os.MkdirAll(filepath.Dir(absLegacy), 0o755)
	_ = os.WriteFile(absLegacy, []byte("legacy duplicate audio"), 0o644)

	// Fixture C: Valid Opus for RESTORE_TAGS
	artC, _ := catRepo.UpsertArtist(ctx, music.Artist{ID: music.NewID(), Name: "Nirvana", Provider: "spotify", SourceID: "art_n"})
	relC, _ := catRepo.UpsertRelease(ctx, music.Release{ID: music.NewID(), Title: "Nevermind", AlbumArtist: "Nirvana", ReleaseType: music.ReleaseAlbum, Year: 1991, TrackCount: 1}, artC.ID)
	trkC, _ := catRepo.UpsertTrack(ctx, music.Track{ID: music.NewID(), ReleaseID: relC.ID, Title: "Smells Like Teen Spirit", Artists: []string{"Nirvana"}, Album: relC.Title, AlbumArtist: relC.AlbumArtist, Year: 1991, TrackNumber: 1, DiscNumber: 1, DurationMS: 301000}, relC.ID, artC.ID, 0)
	nirvRel := "Nirvana/1991 - Nevermind/01 - Smells Like Teen Spirit.opus"
	absNirv := filepath.Join(root, nirvRel)
	_ = os.MkdirAll(filepath.Dir(absNirv), 0o755)
	cmd := exec.Command("ffmpeg", "-hide_banner", "-loglevel", "error", "-y",
		"-f", "lavfi", "-i", "sine=frequency=440:duration=1",
		"-c:a", "libopus", "-b:a", "64k", absNirv)
	_ = cmd.Run()
	infoNirv, _ := os.Stat(absNirv)
	var nirvSize int64
	if infoNirv != nil {
		nirvSize = infoNirv.Size()
	}
	_, _ = filesRepo.Upsert(ctx, music.File{ID: music.NewID(), TrackID: trkC.ID, Path: nirvRel, SizeBytes: nirvSize, DurationMS: 301000})

	// 3. Real HTTP Start Quick Audit
	resp, body, err = httpCli.do(http.MethodPost, "/api/v1/library/audits", handlers.StartAuditRequest{Mode: music.AuditModeQuick})
	if err != nil || resp.StatusCode != http.StatusAccepted {
		t.Fatalf("start audit via HTTP: %v, code: %d, body: %s", err, resp.StatusCode, string(body))
	}

	var startAuditResp struct {
		Data music.AuditRun `json:"data"`
	}
	_ = json.Unmarshal(body, &startAuditResp)
	runID := startAuditResp.Data.ID

	// Wait for audit to complete via polling
	var completedAudit music.AuditRun
	for i := 0; i < 40; i++ {
		time.Sleep(100 * time.Millisecond)
		resp, body, err = httpCli.do(http.MethodGet, "/api/v1/library/audits/"+runID, nil)
		if err == nil && resp.StatusCode == http.StatusOK {
			var getResp struct {
				Data music.AuditRun `json:"data"`
			}
			_ = json.Unmarshal(body, &getResp)
			if getResp.Data.Status == music.AuditRunCompleted {
				completedAudit = getResp.Data
				break
			}
		}
	}
	if completedAudit.Status != music.AuditRunCompleted {
		t.Fatalf("audit failed to complete: %+v", completedAudit)
	}

	// 4. Real HTTP Get Findings with Pagination
	resp, body, err = httpCli.do(http.MethodGet, fmt.Sprintf("/api/v1/library/audits/%s/findings?limit=10&offset=0", runID), nil)
	if err != nil || resp.StatusCode != http.StatusOK {
		t.Fatalf("get findings via HTTP: %v, code: %d", err, resp.StatusCode)
	}

	var findingsResp struct {
		Data []music.AuditFinding `json:"data"`
		Meta struct {
			Total int `json:"total"`
		} `json:"meta"`
	}
	_ = json.Unmarshal(body, &findingsResp)
	if len(findingsResp.Data) == 0 {
		t.Fatalf("expected findings, got 0")
	}

	// 5. Test Repair Preview via HTTP
	var moveFinding, quarantineFinding *music.AuditFinding
	for _, f := range findingsResp.Data {
		if f.FindingCode == music.FindingPathMismatch {
			moveFinding = &f
		} else if f.FindingCode == music.FindingLegacyDuplicate || f.FindingCode == music.FindingFileUntracked {
			quarantineFinding = &f
		}
	}

	if moveFinding != nil {
		prevReq := handlers.PreviewRepairsRequest{FindingIDs: []string{moveFinding.ID}}
		resp, body, err = httpCli.do(http.MethodPost, "/api/v1/library/repairs/preview", prevReq)
		if err != nil || resp.StatusCode != http.StatusOK {
			t.Fatalf("preview repair via HTTP: %v, body: %s", err, string(body))
		}
		var prevResp struct {
			Data []library.RepairPreview `json:"data"`
		}
		_ = json.Unmarshal(body, &prevResp)
		if len(prevResp.Data) != 1 || !prevResp.Data[0].Allowed {
			t.Fatalf("expected allowed move preview, got %+v", prevResp.Data)
		}

		// Apply MOVE_CANONICAL via HTTP
		applyReq := library.RepairApplyRequest{
			Confirm: true,
			Actions: []library.RepairItemAction{{FindingID: moveFinding.ID, Action: music.ActionMoveCanonical}},
		}
		resp, body, err = httpCli.do(http.MethodPost, "/api/v1/library/repairs/apply", applyReq)
		if err != nil || resp.StatusCode != http.StatusOK {
			t.Fatalf("apply move via HTTP: %v, body: %s", err, string(body))
		}

		// Verify physical file moved and old file gone
		canonicalRel := prevResp.Data[0].DestinationPath
		if !lib.Exists(filepath.Join(root, canonicalRel)) {
			t.Fatalf("canonical file missing after HTTP apply: %s", canonicalRel)
		}
		if lib.Exists(absOldA) {
			t.Fatalf("old file still exists after HTTP apply")
		}
	}

	if quarantineFinding != nil {
		// Preview Quarantine
		prevReq := handlers.PreviewRepairsRequest{FindingIDs: []string{quarantineFinding.ID}}
		resp, body, err = httpCli.do(http.MethodPost, "/api/v1/library/repairs/preview", prevReq)
		if err != nil || resp.StatusCode != http.StatusOK {
			t.Fatalf("preview quarantine via HTTP: %v", err)
		}

		// Apply Quarantine via HTTP
		applyReq := library.RepairApplyRequest{
			Confirm: true,
			Actions: []library.RepairItemAction{{FindingID: quarantineFinding.ID, Action: music.ActionQuarantineFile}},
		}
		resp, body, err = httpCli.do(http.MethodPost, "/api/v1/library/repairs/apply", applyReq)
		if err != nil || resp.StatusCode != http.StatusOK {
			t.Fatalf("apply quarantine via HTTP: %v", err)
		}

		// Verify quarantine destination exists in .ytmdl-trash
		trashFindDir := filepath.Join(root, ".ytmdl-trash", quarantineFinding.ID)
		if _, err := os.Stat(trashFindDir); os.IsNotExist(err) {
			t.Fatalf("quarantine directory missing: %s", trashFindDir)
		}
	}

	// 6. Test Deep Audit Start & Cancellation via HTTP
	resp, body, err = httpCli.do(http.MethodPost, "/api/v1/library/audits", handlers.StartAuditRequest{Mode: music.AuditModeDeep})
	if err == nil && resp.StatusCode == http.StatusAccepted {
		var deepStartResp struct {
			Data music.AuditRun `json:"data"`
		}
		_ = json.Unmarshal(body, &deepStartResp)
		deepID := deepStartResp.Data.ID

		// Cancel via HTTP
		resp, _, err = httpCli.do(http.MethodPost, fmt.Sprintf("/api/v1/library/audits/%s/cancel", deepID), nil)
		if err != nil || resp.StatusCode != http.StatusOK {
			t.Fatalf("cancel deep audit via HTTP: %v", err)
		}
	}

	// 7. HTTP STALE PREVIEW TEST (Same size, same mtime, different SHA)
	staleRel := "Beatles/1969 - Abbey Road/01 - StaleTest.opus"
	absStale := filepath.Join(root, staleRel)
	_ = os.MkdirAll(filepath.Dir(absStale), 0o755)
	payloadA := []byte("PAYLOAD_VERSION_A_32_BYTES_EXACT")
	_ = os.WriteFile(absStale, payloadA, 0o644)
	fixedTime := time.Date(2026, 2, 1, 10, 0, 0, 0, time.UTC)
	_ = os.Chtimes(absStale, fixedTime, fixedTime)

	staleFindID := music.NewID()
	actQ := music.ActionQuarantineFile
	_ = auditRepo.InsertFindings(ctx, []music.AuditFinding{{
		ID:              staleFindID,
		RunID:           runID,
		FindingCode:     music.FindingLegacyDuplicate,
		Severity:        music.SeverityWarning,
		RelativePath:    staleRel,
		SuggestedAction: &actQ,
		CreatedAt:       time.Now().UTC(),
	}})

	// Preview via HTTP -> SHA of payload A is persisted
	resp, _, err = httpCli.do(http.MethodPost, "/api/v1/library/repairs/preview", handlers.PreviewRepairsRequest{FindingIDs: []string{staleFindID}})
	if err != nil || resp.StatusCode != http.StatusOK {
		t.Fatalf("preview stale item: %v", err)
	}

	// Replace file with payload B (same size, same mtime, different SHA)
	payloadB := []byte("PAYLOAD_VERSION_B_32_BYTES_EXACT")
	_ = os.WriteFile(absStale, payloadB, 0o644)
	_ = os.Chtimes(absStale, fixedTime, fixedTime)

	// Apply via HTTP -> Must fail with STALE_REPAIR / warning
	resp, body, err = httpCli.do(http.MethodPost, "/api/v1/library/repairs/apply", library.RepairApplyRequest{
		Confirm: true,
		Actions: []library.RepairItemAction{{FindingID: staleFindID, Action: actQ}},
	})
	if err != nil || resp.StatusCode != http.StatusOK {
		t.Fatalf("apply HTTP: %v", err)
	}
	var staleApplyResp struct {
		Data library.RepairApplyResult `json:"data"`
	}
	_ = json.Unmarshal(body, &staleApplyResp)
	if staleApplyResp.Data.Failed != 1 {
		t.Fatalf("expected stale apply to fail, got %+v", staleApplyResp.Data)
	}
	// Verify payload B is still on disk intact (0 mutation)
	diskBytes, _ := os.ReadFile(absStale)
	if string(diskBytes) != string(payloadB) {
		t.Fatalf("disk file mutated during stale apply")
	}

	// 8. HTTP QUARANTINE CONFLICT TEST
	collRel := "Beatles/1969 - Abbey Road/01 - CollideTest.opus"
	absColl := filepath.Join(root, collRel)
	_ = os.WriteFile(absColl, []byte("collision test source"), 0o644)
	collFindID := music.NewID()
	_ = auditRepo.InsertFindings(ctx, []music.AuditFinding{{
		ID:              collFindID,
		RunID:           runID,
		FindingCode:     music.FindingLegacyDuplicate,
		Severity:        music.SeverityWarning,
		RelativePath:    collRel,
		SuggestedAction: &actQ,
		CreatedAt:       time.Now().UTC(),
	}})

	// Preview via HTTP
	_, _, _ = httpCli.do(http.MethodPost, "/api/v1/library/repairs/preview", handlers.PreviewRepairsRequest{FindingIDs: []string{collFindID}})

	// Create foreign colliding file in destination quarantine dir
	quarDestDir := filepath.Join(root, ".ytmdl-trash", collFindID)
	_ = os.MkdirAll(quarDestDir, 0o755)
	foreignDest := filepath.Join(quarDestDir, filepath.Base(collRel))
	foreignPayload := []byte("foreign pre-existing quarantine file")
	_ = os.WriteFile(foreignDest, foreignPayload, 0o644)

	// Apply via HTTP -> Must fail due to PATH_CONFLICT
	resp, body, err = httpCli.do(http.MethodPost, "/api/v1/library/repairs/apply", library.RepairApplyRequest{
		Confirm: true,
		Actions: []library.RepairItemAction{{FindingID: collFindID, Action: actQ}},
	})
	if err != nil || resp.StatusCode != http.StatusOK {
		t.Fatalf("apply collide HTTP: %v", err)
	}
	var collApplyResp struct {
		Data library.RepairApplyResult `json:"data"`
	}
	_ = json.Unmarshal(body, &collApplyResp)
	if collApplyResp.Data.Failed != 1 {
		t.Fatalf("expected collide apply to fail, got %+v", collApplyResp.Data)
	}
	// Verify foreign file was NOT overwritten
	foreignAfter, _ := os.ReadFile(foreignDest)
	if string(foreignAfter) != string(foreignPayload) {
		t.Fatalf("foreign file in quarantine was overwritten! got: %s", string(foreignAfter))
	}
}

func TestE2E_RealHTTP_StorageUnavailable(t *testing.T) {
	db := dbtest.Open(t)
	t.Cleanup(func() { db.Close() })

	missingRoot := filepath.Join(t.TempDir(), "nonexistent_storage_root")

	lib, err := storage.NewLibrary(missingRoot)
	if err != nil {
		t.Fatalf("new library: %v", err)
	}
	_ = os.RemoveAll(missingRoot)

	catRepo := repository.NewCatalog(db)
	filesRepo := repository.NewFiles(db)
	auditRepo := repository.NewAudit(db)
	usersRepo := repository.NewUsers(db)
	sessionsRepo := repository.NewSessions(db)

	ctx := context.Background()
	limiter := auth.NewLimiter(100, time.Minute)
	t.Cleanup(limiter.Close)
	authSvc := auth.NewService(usersRepo, sessionsRepo, limiter, nil)

	_, _ = authSvc.Setup(ctx, auth.SetupRequest{
		Username:    "admin",
		DisplayName: "Administrator",
		Password:    "password123!",
	}, "127.0.0.1", "test")

	libSvc, err := library.NewService(library.ServiceOptions{
		Library: lib,
		Files:   filesRepo,
		Catalog: catRepo,
		Audit:   auditRepo,
	})
	if err != nil {
		t.Fatalf("new service: %v", err)
	}

	deps := handlers.Deps{
		Catalog:        catRepo,
		Files:          filesRepo,
		Library:        lib,
		LibraryService: libSvc,
		Auth:           authSvc,
		StartedAt:      time.Now(),
		Version:        "0.13.6-test",
	}

	h := handlers.NewForTest(deps)
	router, _ := api.NewRouter(api.RouterOptions{Handlers: h, Auth: authSvc})
	ts := httptest.NewServer(router)
	t.Cleanup(ts.Close)

	jar, _ := cookiejar.New(nil)
	httpCli := &realHTTPClient{
		client:  &http.Client{Jar: jar, Timeout: 10 * time.Second},
		baseURL: ts.URL,
	}

	// 1. Establish CSRF & Login
	_, _, _ = httpCli.do(http.MethodGet, "/api/v1/auth/status", nil)
	u, _ := url.Parse(ts.URL)
	for _, cookie := range jar.Cookies(u) {
		if cookie.Name == "ytmdl_csrf" {
			httpCli.csrfToken = cookie.Value
		}
	}
	_, _, _ = httpCli.do(http.MethodPost, "/api/v1/auth/login", auth.LoginRequest{
		Username: "admin",
		Password: "password123!",
	})
	for _, cookie := range jar.Cookies(u) {
		if cookie.Name == "ytmdl_csrf" {
			httpCli.csrfToken = cookie.Value
		}
	}

	// 2. Start Audit with Unavailable Storage -> Must fail globally
	resp, body, err := httpCli.do(http.MethodPost, "/api/v1/library/audits", handlers.StartAuditRequest{Mode: music.AuditModeQuick})
	if err == nil && resp.StatusCode == http.StatusAccepted {
		var startResp struct {
			Data music.AuditRun `json:"data"`
		}
		_ = json.Unmarshal(body, &startResp)
		runID := startResp.Data.ID

		// Wait for run status
		var failedRun music.AuditRun
		for i := 0; i < 20; i++ {
			time.Sleep(100 * time.Millisecond)
			resp, body, err = httpCli.do(http.MethodGet, "/api/v1/library/audits/"+runID, nil)
			if err != nil {
				t.Logf("poll error: %v", err)
				continue
			}
			if resp.StatusCode != http.StatusOK {
				t.Logf("poll status: %d, body: %s", resp.StatusCode, string(body))
				continue
			}
			var getResp struct {
				Data music.AuditRun `json:"data"`
			}
			_ = json.Unmarshal(body, &getResp)
			t.Logf("poll data status: %s", getResp.Data.Status)
			if getResp.Data.Status == music.AuditRunFailed {
				failedRun = getResp.Data
				break
			}
		}
		if failedRun.Status != music.AuditRunFailed {
			t.Fatalf("expected audit to fail due to unavailable storage, got %q", failedRun.Status)
		}
		if failedRun.FindingsCount != 0 {
			t.Fatalf("expected 0 false findings on storage failure, got %d", failedRun.FindingsCount)
		}
	}

	// 3. Apply Repair with Unavailable Storage -> Must abort before mutation
	resp, body, err = httpCli.do(http.MethodPost, "/api/v1/library/repairs/apply", library.RepairApplyRequest{
		Confirm: true,
		Actions: []library.RepairItemAction{{FindingID: "dummy_id", Action: music.ActionQuarantineFile}},
	})
	if err != nil || (resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusConflict && resp.StatusCode != http.StatusBadRequest && resp.StatusCode != http.StatusBadGateway) {
		t.Fatalf("apply repair with unavailable storage returned unexpected status %d: %s", resp.StatusCode, string(body))
	}
	// Verify missing root was not created with wrong fallback paths
	if _, err := os.Stat(missingRoot); !os.IsNotExist(err) {
		t.Fatalf("fallback storage path should not be created")
	}
}
