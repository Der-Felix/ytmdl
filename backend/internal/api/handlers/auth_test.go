package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"ytdm/backend/internal/api/middleware"
	"ytdm/backend/internal/apperr"
	"ytdm/backend/internal/auth"
	"ytdm/backend/internal/database/dbtest"
	"ytdm/backend/internal/database/repository"
)

func newTestAuthHandlers(t *testing.T) (*Handlers, *auth.Service) {
	t.Helper()
	db := dbtest.Open(t)
	usersRepo := repository.NewUsers(db)
	sessionsRepo := repository.NewSessions(db)
	limiter := auth.NewLimiter(5, 5*time.Minute)
	t.Cleanup(limiter.Close)
	authService := auth.NewService(usersRepo, sessionsRepo, limiter, nil)

	h := &Handlers{
		deps: Deps{
			Auth:         authService,
			CookieSecure: false,
			StartedAt:    time.Now(),
		},
		healthCache: make(map[string]checkResult),
	}
	return h, authService
}

func TestAuthStatusEndpoint(t *testing.T) {
	h, authService := newTestAuthHandlers(t)

	// Status on empty DB
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/auth/status", nil)
	h.Status(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status code = %d, want 200", rec.Code)
	}

	var res struct {
		Data auth.AuthStatus `json:"data"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&res); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !res.Data.SetupRequired || res.Data.Authenticated {
		t.Fatalf("expected setup_required=true, authenticated=false, got %+v", res.Data)
	}

	// Create an admin
	setupRes, err := authService.Setup(context.Background(), auth.SetupRequest{
		Username:    "admin",
		DisplayName: "Admin",
		Password:    "password_123",
	}, "127.0.0.1", "Test")
	if err != nil {
		t.Fatalf("setup: %v", err)
	}

	// Status with session cookie
	rec2 := httptest.NewRecorder()
	req2 := httptest.NewRequest(http.MethodGet, "/api/v1/auth/status", nil)
	req2.AddCookie(&http.Cookie{Name: middleware.SessionCookieName, Value: setupRes.SessionToken})
	h.Status(rec2, req2)

	var res2 struct {
		Data auth.AuthStatus `json:"data"`
	}
	if err := json.NewDecoder(rec2.Body).Decode(&res2); err != nil {
		t.Fatalf("decode 2: %v", err)
	}
	if res2.Data.SetupRequired || !res2.Data.Authenticated || res2.Data.User == nil {
		t.Fatalf("expected setup_required=false, authenticated=true, got %+v", res2.Data)
	}
	if res2.Data.User.Username != "admin" {
		t.Fatalf("expected username admin, got %s", res2.Data.User.Username)
	}
}

func TestAuthSetupAndLoginHandlers(t *testing.T) {
	h, _ := newTestAuthHandlers(t)

	// Setup First Admin
	setupBody := `{"username":"Admin_Master","display_name":"Master Admin","password":"strong_password_123"}`
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/setup", bytes.NewBufferString(setupBody))
	req.Header.Set("Content-Type", "application/json")
	h.Setup(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("setup code = %d, want 201; body = %s", rec.Code, rec.Body.String())
	}

	// Verify session and csrf cookies were set
	cookies := rec.Result().Cookies()
	var hasSessionCookie, hasCSRFCookie bool
	for _, c := range cookies {
		if c.Name == middleware.SessionCookieName && c.Value != "" && c.HttpOnly {
			hasSessionCookie = true
		}
		if c.Name == middleware.CSRFCookieName && c.Value != "" && !c.HttpOnly {
			hasCSRFCookie = true
		}
	}
	if !hasSessionCookie {
		t.Fatal("expected HttpOnly session cookie from setup")
	}
	if !hasCSRFCookie {
		t.Fatal("expected readable CSRF cookie from setup")
	}

	// Duplicate setup must fail with 409 Conflict
	recDup := httptest.NewRecorder()
	reqDup := httptest.NewRequest(http.MethodPost, "/api/v1/auth/setup", bytes.NewBufferString(setupBody))
	reqDup.Header.Set("Content-Type", "application/json")
	h.Setup(recDup, reqDup)

	if recDup.Code != http.StatusConflict {
		t.Fatalf("duplicate setup code = %d, want 409 Conflict", recDup.Code)
	}

	// Login with correct credentials
	loginBody := `{"username":"ADMIN_MASTER","password":"strong_password_123"}`
	recLogin := httptest.NewRecorder()
	reqLogin := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", bytes.NewBufferString(loginBody))
	reqLogin.Header.Set("Content-Type", "application/json")
	h.Login(recLogin, reqLogin)

	if recLogin.Code != http.StatusOK {
		t.Fatalf("login code = %d, want 200; body = %s", recLogin.Code, recLogin.Body.String())
	}

	// Login with wrong password
	wrongBody := `{"username":"admin_master","password":"wrong_password"}`
	recWrong := httptest.NewRecorder()
	reqWrong := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", bytes.NewBufferString(wrongBody))
	reqWrong.Header.Set("Content-Type", "application/json")
	h.Login(recWrong, reqWrong)

	if recWrong.Code != http.StatusUnauthorized {
		t.Fatalf("wrong password code = %d, want 401 Unauthorized", recWrong.Code)
	}
}

func TestAuthLogoutAndMeHandlers(t *testing.T) {
	h, authService := newTestAuthHandlers(t)

	setupRes, err := authService.Setup(context.Background(), auth.SetupRequest{
		Username:    "user_one",
		DisplayName: "User One",
		Password:    "password_123",
	}, "127.0.0.1", "Test")
	if err != nil {
		t.Fatalf("setup: %v", err)
	}

	// Me endpoint with user in context
	user, _, err := authService.VerifySession(context.Background(), setupRes.SessionToken)
	if err != nil {
		t.Fatalf("verify: %v", err)
	}

	recMe := httptest.NewRecorder()
	reqMe := httptest.NewRequest(http.MethodGet, "/api/v1/auth/me", nil)
	ctx := middleware.ContextWithUser(reqMe.Context(), user)
	h.Me(recMe, reqMe.WithContext(ctx))

	if recMe.Code != http.StatusOK {
		t.Fatalf("me code = %d, want 200", recMe.Code)
	}

	// Logout
	recLogout := httptest.NewRecorder()
	reqLogout := httptest.NewRequest(http.MethodPost, "/api/v1/auth/logout", nil)
	reqLogout.AddCookie(&http.Cookie{Name: middleware.SessionCookieName, Value: setupRes.SessionToken})
	h.Logout(recLogout, reqLogout)

	if recLogout.Code != http.StatusNoContent {
		t.Fatalf("logout code = %d, want 204 NoContent", recLogout.Code)
	}

	// Verify cookie cleared
	clearedCookie := recLogout.Result().Cookies()
	var foundCleared bool
	for _, c := range clearedCookie {
		if c.Name == middleware.SessionCookieName && (c.MaxAge <= 0 || c.Value == "") {
			foundCleared = true
		}
	}
	if !foundCleared {
		t.Fatal("expected cleared session cookie on logout")
	}

	// Session is now invalidated
	_, _, err = authService.VerifySession(context.Background(), setupRes.SessionToken)
	if err == nil || apperr.CodeOf(err) != apperr.CodeUnauthenticated {
		t.Fatalf("expected session to be invalid after logout, got %v", err)
	}
}
