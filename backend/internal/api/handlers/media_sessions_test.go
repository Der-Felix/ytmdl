package handlers_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"ytdm/backend/internal/api"
	"ytdm/backend/internal/api/handlers"
	"ytdm/backend/internal/api/middleware"
	"ytdm/backend/internal/apperr"
	"ytdm/backend/internal/auth"
	"ytdm/backend/internal/database/dbtest"
	"ytdm/backend/internal/database/repository"
	"ytdm/backend/internal/mediasession"
	"ytdm/backend/internal/provider"
)

type mediaSessionTestEnv struct {
	router     http.Handler
	adminToken string
	userToken  string
	csrfToken  string
	svc        *mediasession.Service
	storage    *mediasession.CookieStorage
	pool       *mediasession.SessionPool
	fakeProber *fakeHTTPProber
	legacyDir  string
	legacyFile string
	cookieDir  string
}

type fakeHTTPProber struct {
	status HealthStatusWrapper
	err    error
}

type HealthStatusWrapper struct {
	status mediasession.HealthStatus
}

func (f *fakeHTTPProber) Probe(ctx context.Context, sessionID string, cookiePath string) (*mediasession.ProbeResult, error) {
	st := mediasession.HealthHealthy
	if f.status.status != "" {
		st = f.status.status
	}
	return &mediasession.ProbeResult{
		Status:             st,
		TestedAt:           time.Now().UTC(),
		MetadataOK:         st == mediasession.HealthHealthy,
		UsableAudioFormats: st == mediasession.HealthHealthy,
	}, f.err
}

func setupMediaSessionTestEnv(t *testing.T) *mediaSessionTestEnv {
	t.Helper()
	db := dbtest.Open(t)
	usersRepo := repository.NewUsers(db)
	sessionsRepo := repository.NewSessions(db)
	limiter := auth.NewLimiter(20, 5*time.Minute)
	t.Cleanup(limiter.Close)
	authService := auth.NewService(usersRepo, sessionsRepo, limiter, nil)

	cookieDir := t.TempDir()
	legacyDir := t.TempDir()
	legacyFile := filepath.Join(legacyDir, "legacy.cookies.txt")
	if err := os.WriteFile(legacyFile, []byte("# Netscape HTTP Cookie File\n.youtube.com\tTRUE\t/\tTRUE\t2147483647\tSID\tLEGACY_SENTINEL_TOKEN_ABC999\n"), 0600); err != nil {
		t.Fatalf("write legacy file: %v", err)
	}

	legacyAdapter := mediasession.NewLegacyAdapter(legacyFile)
	storage, err := mediasession.NewCookieStorage(cookieDir, legacyAdapter)
	if err != nil {
		t.Fatalf("NewCookieStorage: %v", err)
	}

	mediaSessionsRepo := repository.NewMediaSessions(db)
	poolCfg := mediasession.PoolConfig{
		Family:                provider.FamilyYouTube,
		MaxLeasesPerSession:   2,
		SessionRequestsPerSec: 10,
		SessionBurst:          5,
		GlobalRequestsPerSec:  20,
		GlobalBurst:           10,
		AllowUnknown:          true,
	}
	pool := mediasession.NewSessionPool(poolCfg, storage, mediaSessionsRepo, legacyAdapter)
	pool.SetSyncPersist(true)

	prober := &fakeHTTPProber{}
	svc := mediasession.NewService(mediasession.ServiceOptions{
		Repo:          mediaSessionsRepo,
		Storage:       storage,
		Pool:          pool,
		LegacyAdapter: legacyAdapter,
		Prober:        prober,
	})

	h := handlers.NewForTest(handlers.Deps{
		Auth:          authService,
		MediaSessions: svc,
	})

	router, err := api.NewRouter(api.RouterOptions{
		Handlers: h,
		Auth:     authService,
	})
	if err != nil {
		t.Fatalf("NewRouter: %v", err)
	}

	ctx := context.Background()

	// 1. Create Admin
	setupRes, err := authService.Setup(ctx, auth.SetupRequest{
		Username:    "admin_ms",
		DisplayName: "Admin MS",
		Password:    "password123!",
	}, "127.0.0.1", "test")
	if err != nil {
		t.Fatalf("setup admin: %v", err)
	}

	// 2. Create Standard User
	_, err = authService.CreateUser(ctx, auth.CreateUserRequest{
		Username:    "standard_ms",
		DisplayName: "Standard User",
		Password:    "password123!",
		Role:        auth.RoleUser,
	})
	if err != nil {
		t.Fatalf("create user: %v", err)
	}

	loginRes, err := authService.Login(ctx, auth.LoginRequest{
		Username: "standard_ms",
		Password: "password123!",
	}, "127.0.0.1", "test")
	if err != nil {
		t.Fatalf("login standard user: %v", err)
	}

	csrfToken, err := auth.GenerateCSRFToken()
	if err != nil {
		t.Fatalf("generate CSRF: %v", err)
	}

	return &mediaSessionTestEnv{
		router:     router,
		adminToken: setupRes.SessionToken,
		userToken:  loginRes.SessionToken,
		csrfToken:  csrfToken,
		svc:        svc,
		storage:    storage,
		pool:       pool,
		fakeProber: prober,
		legacyDir:  legacyDir,
		legacyFile: legacyFile,
		cookieDir:  cookieDir,
	}
}

func (env *mediaSessionTestEnv) doRequest(method, path string, body []byte, token, csrf string, contentType string) *httptest.ResponseRecorder {
	var bodyReader io.Reader
	if body != nil {
		bodyReader = bytes.NewReader(body)
	}
	req := httptest.NewRequest(method, path, bodyReader)
	if token != "" {
		req.AddCookie(&http.Cookie{Name: "ytmdl_session", Value: token})
	}
	if csrf != "" {
		req.AddCookie(&http.Cookie{Name: middleware.CSRFCookieName, Value: csrf})
		req.Header.Set(middleware.CSRFHeaderName, csrf)
	}
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	} else if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	rec := httptest.NewRecorder()
	env.router.ServeHTTP(rec, req)
	return rec
}

func TestMediaSessionsAPI_AuthorizationMatrix(t *testing.T) {
	env := setupMediaSessionTestEnv(t)

	// Step 1: Create a test session via Service directly to have an ID for URL-param endpoints
	ctx := context.Background()
	sess, err := env.svc.CreateSession(ctx, mediasession.CreateSessionRequest{Name: "Auth Matrix Target"})
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	endpoints := []struct {
		name       string
		method     string
		path       string
		body       []byte
		isMutating bool
	}{
		{
			name:       "List sessions",
			method:     http.MethodGet,
			path:       "/api/v1/admin/media-sessions",
			body:       nil,
			isMutating: false,
		},
		{
			name:       "Get session",
			method:     http.MethodGet,
			path:       "/api/v1/admin/media-sessions/" + sess.ID,
			body:       nil,
			isMutating: false,
		},
		{
			name:       "Create session",
			method:     http.MethodPost,
			path:       "/api/v1/admin/media-sessions",
			body:       []byte(`{"name":"New Test Session"}`),
			isMutating: true,
		},
		{
			name:       "Upload cookies",
			method:     http.MethodPost,
			path:       "/api/v1/admin/media-sessions/" + sess.ID + "/cookies",
			body:       []byte("# Netscape HTTP Cookie File\n.youtube.com\tTRUE\t/\tTRUE\t2147483647\tSID\tsecret123\n"),
			isMutating: true,
		},
		{
			name:       "Probe session",
			method:     http.MethodPost,
			path:       "/api/v1/admin/media-sessions/" + sess.ID + "/probe",
			body:       nil,
			isMutating: true,
		},
		{
			name:       "Update session",
			method:     http.MethodPatch,
			path:       "/api/v1/admin/media-sessions/" + sess.ID,
			body:       []byte(`{"name":"Renamed Session"}`),
			isMutating: true,
		},
		{
			name:       "Delete session",
			method:     http.MethodDelete,
			path:       "/api/v1/admin/media-sessions/" + sess.ID,
			body:       nil,
			isMutating: true,
		},
	}

	for _, ep := range endpoints {
		t.Run(ep.name+"_Unauthenticated", func(t *testing.T) {
			rec := env.doRequest(ep.method, ep.path, ep.body, "", "", "")
			if rec.Code != http.StatusUnauthorized {
				t.Fatalf("%s unauthenticated code = %d, want 401; body = %s", ep.name, rec.Code, rec.Body.String())
			}
		})

		t.Run(ep.name+"_StandardUser", func(t *testing.T) {
			rec := env.doRequest(ep.method, ep.path, ep.body, env.userToken, "", "")
			if rec.Code != http.StatusForbidden {
				t.Fatalf("%s standard user code = %d, want 403; body = %s", ep.name, rec.Code, rec.Body.String())
			}
		})

		if ep.isMutating {
			t.Run(ep.name+"_AdminMissingCSRF", func(t *testing.T) {
				rec := env.doRequest(ep.method, ep.path, ep.body, env.adminToken, "", "")
				if rec.Code != http.StatusForbidden {
					t.Fatalf("%s admin without CSRF code = %d, want 403; body = %s", ep.name, rec.Code, rec.Body.String())
				}
			})
		}
	}
}

func TestMediaSessionsAPI_SecretLeakageProtection(t *testing.T) {
	env := setupMediaSessionTestEnv(t)
	sentinelSecret := "TOP_SECRET_COOKIE_SENTINEL_STRING_XYZ_777"
	cookiePayload := fmt.Sprintf("# Netscape HTTP Cookie File\n.youtube.com\tTRUE\t/\tTRUE\t2147483647\tSID\t%s\n", sentinelSecret)

	// 1. Create session
	recCreate := env.doRequest(http.MethodPost, "/api/v1/admin/media-sessions", []byte(`{"name":"Leakage Check"}`), env.adminToken, env.csrfToken, "")
	if recCreate.Code != http.StatusCreated {
		t.Fatalf("create session code = %d, want 201; body = %s", recCreate.Code, recCreate.Body.String())
	}

	var createResp struct {
		Data mediasession.SessionView `json:"data"`
	}
	if err := json.Unmarshal(recCreate.Body.Bytes(), &createResp); err != nil {
		t.Fatalf("unmarshal create: %v", err)
	}
	sessID := createResp.Data.ID

	// 2. Upload cookies containing sentinel secret
	recUpload := env.doRequest(http.MethodPost, fmt.Sprintf("/api/v1/admin/media-sessions/%s/cookies", sessID), []byte(cookiePayload), env.adminToken, env.csrfToken, "text/plain")
	if recUpload.Code != http.StatusOK {
		t.Fatalf("upload cookies code = %d, want 200; body = %s", recUpload.Code, recUpload.Body.String())
	}

	// CRITICAL ASSERTION: Sentinel must NEVER appear in JSON response
	if strings.Contains(recUpload.Body.String(), sentinelSecret) {
		t.Fatalf("SECURITY LEAK: sentinel secret found in upload response: %s", recUpload.Body.String())
	}
	// CRITICAL ASSERTION: Absolute filesystem path must NEVER appear in JSON response
	if strings.Contains(recUpload.Body.String(), env.cookieDir) {
		t.Fatalf("SECURITY LEAK: absolute cookieDir found in upload response: %s", recUpload.Body.String())
	}

	// 3. List sessions: check sentinel and path leakage
	recList := env.doRequest(http.MethodGet, "/api/v1/admin/media-sessions", nil, env.adminToken, "", "")
	if recList.Code != http.StatusOK {
		t.Fatalf("list sessions code = %d, want 200", recList.Code)
	}
	if strings.Contains(recList.Body.String(), sentinelSecret) {
		t.Fatalf("SECURITY LEAK: sentinel secret found in list response: %s", recList.Body.String())
	}
	if strings.Contains(recList.Body.String(), env.cookieDir) || strings.Contains(recList.Body.String(), env.legacyDir) {
		t.Fatalf("SECURITY LEAK: filesystem path found in list response: %s", recList.Body.String())
	}

	// 4. Get session: check sentinel and path leakage
	recGet := env.doRequest(http.MethodGet, fmt.Sprintf("/api/v1/admin/media-sessions/%s", sessID), nil, env.adminToken, "", "")
	if recGet.Code != http.StatusOK {
		t.Fatalf("get session code = %d, want 200", recGet.Code)
	}
	if strings.Contains(recGet.Body.String(), sentinelSecret) {
		t.Fatalf("SECURITY LEAK: sentinel secret found in get response: %s", recGet.Body.String())
	}
	if strings.Contains(recGet.Body.String(), env.cookieDir) {
		t.Fatalf("SECURITY LEAK: filesystem path found in get response: %s", recGet.Body.String())
	}

	// 5. Probe session: check sentinel and path leakage
	recProbe := env.doRequest(http.MethodPost, fmt.Sprintf("/api/v1/admin/media-sessions/%s/probe", sessID), nil, env.adminToken, env.csrfToken, "")
	if recProbe.Code != http.StatusOK {
		t.Fatalf("probe session code = %d, want 200; body = %s", recProbe.Code, recProbe.Body.String())
	}
	if strings.Contains(recProbe.Body.String(), sentinelSecret) {
		t.Fatalf("SECURITY LEAK: sentinel secret found in probe response: %s", recProbe.Body.String())
	}
	if strings.Contains(recProbe.Body.String(), env.cookieDir) {
		t.Fatalf("SECURITY LEAK: filesystem path found in probe response: %s", recProbe.Body.String())
	}

	// 6. Upload invalid cookies containing sentinel: verify sentinel NEVER in error message
	badCookie := fmt.Sprintf("# Netscape HTTP Cookie File\n.youtube.com\tTRUE\t/\tINVALID_NUMBER_OF_FIELDS\t%s\n", sentinelSecret)
	recBadUpload := env.doRequest(http.MethodPost, fmt.Sprintf("/api/v1/admin/media-sessions/%s/cookies", sessID), []byte(badCookie), env.adminToken, env.csrfToken, "text/plain")
	if recBadUpload.Code == http.StatusOK {
		t.Fatalf("expected error on invalid cookie upload")
	}
	if strings.Contains(recBadUpload.Body.String(), sentinelSecret) {
		t.Fatalf("SECURITY LEAK: sentinel secret found in error response: %s", recBadUpload.Body.String())
	}
}

func TestMediaSessionsAPI_MultipartUpload(t *testing.T) {
	env := setupMediaSessionTestEnv(t)

	// Create session
	recCreate := env.doRequest(http.MethodPost, "/api/v1/admin/media-sessions", []byte(`{"name":"Multipart Upload Target"}`), env.adminToken, env.csrfToken, "")
	if recCreate.Code != http.StatusCreated {
		t.Fatalf("create session code = %d, want 201; body = %s", recCreate.Code, recCreate.Body.String())
	}

	var createResp struct {
		Data mediasession.SessionView `json:"data"`
	}
	_ = json.Unmarshal(recCreate.Body.Bytes(), &createResp)
	sessID := createResp.Data.ID

	// Create multipart form body
	bodyBuf := &bytes.Buffer{}
	mpWriter := multipart.NewWriter(bodyBuf)
	part, err := mpWriter.CreateFormFile("cookie_file", "my_exported_cookies.txt")
	if err != nil {
		t.Fatalf("CreateFormFile: %v", err)
	}
	cookieContent := "# Netscape HTTP Cookie File\n.youtube.com\tTRUE\t/\tTRUE\t2147483647\tSID\tMULTIPART_TOKEN_12345\n"
	_, _ = part.Write([]byte(cookieContent))
	_ = mpWriter.Close()

	recUpload := env.doRequest(http.MethodPost, fmt.Sprintf("/api/v1/admin/media-sessions/%s/cookies", sessID), bodyBuf.Bytes(), env.adminToken, env.csrfToken, mpWriter.FormDataContentType())
	if recUpload.Code != http.StatusOK {
		t.Fatalf("multipart upload code = %d, want 200; body = %s", recUpload.Code, recUpload.Body.String())
	}

	// Verify session now has credentials
	recGet := env.doRequest(http.MethodGet, fmt.Sprintf("/api/v1/admin/media-sessions/%s", sessID), nil, env.adminToken, "", "")
	if recGet.Code != http.StatusOK {
		t.Fatalf("get session code = %d", recGet.Code)
	}
	var getResp struct {
		Data mediasession.SessionView `json:"data"`
	}
	_ = json.Unmarshal(recGet.Body.Bytes(), &getResp)
	if !getResp.Data.HasCredentials {
		t.Errorf("has_credentials should be true after multipart upload")
	}
}

func TestMediaSessionsAPI_InUseConflict(t *testing.T) {
	env := setupMediaSessionTestEnv(t)

	// Create session and upload initial cookie
	recCreate := env.doRequest(http.MethodPost, "/api/v1/admin/media-sessions", []byte(`{"name":"In-Use API Target"}`), env.adminToken, env.csrfToken, "")
	var createResp struct {
		Data mediasession.SessionView `json:"data"`
	}
	_ = json.Unmarshal(recCreate.Body.Bytes(), &createResp)
	sessID := createResp.Data.ID

	cookieContent := "# Netscape HTTP Cookie File\n.youtube.com\tTRUE\t/\tTRUE\t2147483647\tSID\tTOKEN_111\n"
	recUpload := env.doRequest(http.MethodPost, fmt.Sprintf("/api/v1/admin/media-sessions/%s/cookies", sessID), []byte(cookieContent), env.adminToken, env.csrfToken, "text/plain")
	if recUpload.Code != http.StatusOK {
		t.Fatalf("initial upload failed: %d", recUpload.Code)
	}

	// Mark session in use in pool
	env.pool.RetainDataPlane(sessID)

	// DELETE while in use -> 409 Conflict (SESSION_IN_USE)
	recDel := env.doRequest(http.MethodDelete, fmt.Sprintf("/api/v1/admin/media-sessions/%s", sessID), nil, env.adminToken, env.csrfToken, "")
	if recDel.Code != http.StatusConflict {
		t.Fatalf("delete in-use code = %d, want 409; body = %s", recDel.Code, recDel.Body.String())
	}
	if !strings.Contains(recDel.Body.String(), string(apperr.CodeSessionInUse)) {
		t.Errorf("expected error code %s in body: %s", apperr.CodeSessionInUse, recDel.Body.String())
	}

	// REPLACE while in use -> 409 Conflict (SESSION_IN_USE)
	newCookie := "# Netscape HTTP Cookie File\n.youtube.com\tTRUE\t/\tTRUE\t2147483647\tSID\tTOKEN_222\n"
	recReplace := env.doRequest(http.MethodPost, fmt.Sprintf("/api/v1/admin/media-sessions/%s/cookies", sessID), []byte(newCookie), env.adminToken, env.csrfToken, "text/plain")
	if recReplace.Code != http.StatusConflict {
		t.Fatalf("replace in-use code = %d, want 409; body = %s", recReplace.Code, recReplace.Body.String())
	}

	// Release in use
	env.pool.ReleaseDataPlane(sessID)

	// DELETE after release -> 204 No Content
	recDelSuccess := env.doRequest(http.MethodDelete, fmt.Sprintf("/api/v1/admin/media-sessions/%s", sessID), nil, env.adminToken, env.csrfToken, "")
	if recDelSuccess.Code != http.StatusNoContent {
		t.Fatalf("delete after release code = %d, want 204; body = %s", recDelSuccess.Code, recDelSuccess.Body.String())
	}
}

func TestMediaSessionsAPI_LegacySessionHandling(t *testing.T) {
	env := setupMediaSessionTestEnv(t)

	// List sessions includes legacy session
	recList := env.doRequest(http.MethodGet, "/api/v1/admin/media-sessions", nil, env.adminToken, "", "")
	if recList.Code != http.StatusOK {
		t.Fatalf("list sessions code = %d", recList.Code)
	}

	var listResp struct {
		Data []mediasession.SessionView `json:"data"`
	}
	if err := json.Unmarshal(recList.Body.Bytes(), &listResp); err != nil {
		t.Fatalf("unmarshal list: %v", err)
	}

	var foundLegacy bool
	for _, s := range listResp.Data {
		if s.ID == mediasession.LegacySessionID {
			foundLegacy = true
			if !s.HasCredentials {
				t.Errorf("legacy session should have credentials")
			}
			break
		}
	}
	if !foundLegacy {
		t.Fatalf("legacy session was not included in session list")
	}

	// Probe legacy session -> 200 OK
	recProbe := env.doRequest(http.MethodPost, fmt.Sprintf("/api/v1/admin/media-sessions/%s/probe", mediasession.LegacySessionID), nil, env.adminToken, env.csrfToken, "")
	if recProbe.Code != http.StatusOK {
		t.Fatalf("probe legacy code = %d, want 200; body = %s", recProbe.Code, recProbe.Body.String())
	}

	// DELETE legacy session -> 400 Bad Request
	recDel := env.doRequest(http.MethodDelete, fmt.Sprintf("/api/v1/admin/media-sessions/%s", mediasession.LegacySessionID), nil, env.adminToken, env.csrfToken, "")
	if recDel.Code != http.StatusBadRequest {
		t.Fatalf("delete legacy code = %d, want 400; body = %s", recDel.Code, recDel.Body.String())
	}

	// Upload to legacy session -> 400 Bad Request
	recUpload := env.doRequest(http.MethodPost, fmt.Sprintf("/api/v1/admin/media-sessions/%s/cookies", mediasession.LegacySessionID), []byte("foo"), env.adminToken, env.csrfToken, "text/plain")
	if recUpload.Code != http.StatusBadRequest {
		t.Fatalf("upload to legacy code = %d, want 400; body = %s", recUpload.Code, recUpload.Body.String())
	}
}
