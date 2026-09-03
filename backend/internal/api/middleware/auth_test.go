package middleware

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"ytdm/backend/internal/auth"
	"ytdm/backend/internal/database/dbtest"
	"ytdm/backend/internal/database/repository"
)

func newTestAuthService(t *testing.T) *auth.Service {
	t.Helper()
	db := dbtest.Open(t)
	usersRepo := repository.NewUsers(db)
	sessionsRepo := repository.NewSessions(db)
	limiter := auth.NewLimiter(5, 5*time.Minute)
	t.Cleanup(limiter.Close)
	return auth.NewService(usersRepo, sessionsRepo, limiter, nil)
}

func TestMiddlewareAuthenticateAndRequireAuth(t *testing.T) {
	authService := newTestAuthService(t)

	// Setup initial user
	setupRes, err := authService.Setup(context.Background(), auth.SetupRequest{
		Username:    "auth_tester",
		DisplayName: "Auth Tester",
		Password:    "password_123",
	}, "127.0.0.1", "Test Agent")
	if err != nil {
		t.Fatalf("setup: %v", err)
	}

	handlerCalled := false
	testHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		handlerCalled = true
		u := UserFromContext(r.Context())
		if u == nil || u.Username != "auth_tester" {
			t.Errorf("unexpected user in context: %+v", u)
		}
		w.WriteHeader(http.StatusOK)
	})

	stack := Authenticate(authService)(RequireAuth(testHandler))

	// 1. Request with valid session cookie -> 200 OK
	rec1 := httptest.NewRecorder()
	req1 := httptest.NewRequest(http.MethodGet, "/protected", nil)
	req1.AddCookie(&http.Cookie{Name: SessionCookieName, Value: setupRes.SessionToken})
	stack.ServeHTTP(rec1, req1)

	if rec1.Code != http.StatusOK || !handlerCalled {
		t.Fatalf("code = %d, want 200 (handlerCalled=%v)", rec1.Code, handlerCalled)
	}

	// 2. Request without session cookie -> 401 Unauthorized
	handlerCalled = false
	rec2 := httptest.NewRecorder()
	req2 := httptest.NewRequest(http.MethodGet, "/protected", nil)
	stack.ServeHTTP(rec2, req2)

	if rec2.Code != http.StatusUnauthorized || handlerCalled {
		t.Fatalf("code = %d, want 401 (handlerCalled=%v)", rec2.Code, handlerCalled)
	}

	// 3. Request with invalid session cookie -> 401 Unauthorized
	handlerCalled = false
	rec3 := httptest.NewRecorder()
	req3 := httptest.NewRequest(http.MethodGet, "/protected", nil)
	req3.AddCookie(&http.Cookie{Name: SessionCookieName, Value: "invalid_hex_token_12345"})
	stack.ServeHTTP(rec3, req3)

	if rec3.Code != http.StatusUnauthorized || handlerCalled {
		t.Fatalf("code = %d, want 401 (handlerCalled=%v)", rec3.Code, handlerCalled)
	}
}

func TestMiddlewareRequireAdmin(t *testing.T) {
	authService := newTestAuthService(t)

	// Setup initial admin
	_, err := authService.Setup(context.Background(), auth.SetupRequest{
		Username:    "admin_boss",
		DisplayName: "Admin Boss",
		Password:    "password_123",
	}, "127.0.0.1", "Admin Agent")
	if err != nil {
		t.Fatalf("setup admin: %v", err)
	}

	// Create normal user
	normalUserSummary, err := authService.CreateUser(context.Background(), auth.CreateUserRequest{
		Username:    "normal_joe",
		DisplayName: "Normal Joe",
		Password:    "password_123",
		Role:        auth.RoleUser,
	})
	if err != nil {
		t.Fatalf("create normal user: %v", err)
	}

	adminLogin, err := authService.Login(context.Background(), auth.LoginRequest{Username: "admin_boss", Password: "password_123"}, "127.0.0.1", "Agent")
	if err != nil {
		t.Fatalf("admin login: %v", err)
	}
	joeLogin, err := authService.Login(context.Background(), auth.LoginRequest{Username: "normal_joe", Password: "password_123"}, "127.0.0.1", "Agent")
	if err != nil {
		t.Fatalf("joe login: %v", err)
	}

	adminHandlerCalled := false
	adminHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		adminHandlerCalled = true
		w.WriteHeader(http.StatusOK)
	})

	stack := Authenticate(authService)(RequireAdmin(adminHandler))

	// 1. Normal user attempts admin endpoint -> 403 Forbidden
	recJoe := httptest.NewRecorder()
	reqJoe := httptest.NewRequest(http.MethodGet, "/admin/dashboard", nil)
	reqJoe.AddCookie(&http.Cookie{Name: SessionCookieName, Value: joeLogin.SessionToken})
	stack.ServeHTTP(recJoe, reqJoe)

	if recJoe.Code != http.StatusForbidden || adminHandlerCalled {
		t.Fatalf("joe code = %d, want 403 Forbidden (called=%v)", recJoe.Code, adminHandlerCalled)
	}

	// 2. Admin user attempts admin endpoint -> 200 OK
	adminHandlerCalled = false
	recAdmin := httptest.NewRecorder()
	reqAdmin := httptest.NewRequest(http.MethodGet, "/admin/dashboard", nil)
	reqAdmin.AddCookie(&http.Cookie{Name: SessionCookieName, Value: adminLogin.SessionToken})
	stack.ServeHTTP(recAdmin, reqAdmin)

	if recAdmin.Code != http.StatusOK || !adminHandlerCalled {
		t.Fatalf("admin code = %d, want 200 OK (called=%v)", recAdmin.Code, adminHandlerCalled)
	}

	_ = normalUserSummary
}

func TestMiddlewareCSRFValidation(t *testing.T) {
	handlerCalled := false
	dummyHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		handlerCalled = true
		w.WriteHeader(http.StatusOK)
	})

	stack := CSRF(dummyHandler)
	csrfToken := "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"

	// 1. GET requests bypass CSRF check
	recGET := httptest.NewRecorder()
	reqGET := httptest.NewRequest(http.MethodGet, "/items", nil)
	stack.ServeHTTP(recGET, reqGET)
	if recGET.Code != http.StatusOK || !handlerCalled {
		t.Fatalf("GET code = %d, want 200", recGET.Code)
	}

	// 2. POST without CSRF cookie -> 403 Forbidden
	handlerCalled = false
	recPostNoCookie := httptest.NewRecorder()
	reqPostNoCookie := httptest.NewRequest(http.MethodPost, "/items", nil)
	stack.ServeHTTP(recPostNoCookie, reqPostNoCookie)
	if recPostNoCookie.Code != http.StatusForbidden || handlerCalled {
		t.Fatalf("POST no cookie code = %d, want 403", recPostNoCookie.Code)
	}

	// 3. POST with cookie but without header -> 403 Forbidden
	handlerCalled = false
	recPostNoHeader := httptest.NewRecorder()
	reqPostNoHeader := httptest.NewRequest(http.MethodPost, "/items", nil)
	reqPostNoHeader.AddCookie(&http.Cookie{Name: CSRFCookieName, Value: csrfToken})
	stack.ServeHTTP(recPostNoHeader, reqPostNoHeader)
	if recPostNoHeader.Code != http.StatusForbidden || handlerCalled {
		t.Fatalf("POST no header code = %d, want 403", recPostNoHeader.Code)
	}

	// 4. POST with mismatched header -> 403 Forbidden
	handlerCalled = false
	recPostWrong := httptest.NewRecorder()
	reqPostWrong := httptest.NewRequest(http.MethodPost, "/items", nil)
	reqPostWrong.AddCookie(&http.Cookie{Name: CSRFCookieName, Value: csrfToken})
	reqPostWrong.Header.Set(CSRFHeaderName, "different_token_12345")
	stack.ServeHTTP(recPostWrong, reqPostWrong)
	if recPostWrong.Code != http.StatusForbidden || handlerCalled {
		t.Fatalf("POST mismatched token code = %d, want 403", recPostWrong.Code)
	}

	// 5. POST with matching token in cookie and header -> 200 OK
	handlerCalled = false
	recPostOK := httptest.NewRecorder()
	reqPostOK := httptest.NewRequest(http.MethodPost, "/items", nil)
	reqPostOK.AddCookie(&http.Cookie{Name: CSRFCookieName, Value: csrfToken})
	reqPostOK.Header.Set(CSRFHeaderName, csrfToken)
	stack.ServeHTTP(recPostOK, reqPostOK)
	if recPostOK.Code != http.StatusOK || !handlerCalled {
		t.Fatalf("POST valid CSRF code = %d, want 200", recPostOK.Code)
	}
}

func TestTrustedProxyAndIPExtraction(t *testing.T) {
	// Reset to default (only loopback trusted)
	SetTrustedProxies(nil)
	t.Cleanup(func() {
		SetTrustedProxies(nil)
	})

	// 1. Untrusted public peer: X-Forwarded-For and X-Forwarded-Proto must be IGNORED
	reqUntrusted := httptest.NewRequest(http.MethodGet, "/", nil)
	reqUntrusted.RemoteAddr = "203.0.113.195:12345"
	reqUntrusted.Header.Set("X-Forwarded-For", "198.51.100.1")
	reqUntrusted.Header.Set("X-Forwarded-Proto", "https")

	ip := ClientIP(reqUntrusted)
	if ip != "203.0.113.195" {
		t.Fatalf("untrusted client IP = %q, want %q", ip, "203.0.113.195")
	}
	if IsSecure(reqUntrusted, false) {
		t.Fatal("untrusted request must not be treated as secure from spoofed X-Forwarded-Proto")
	}

	// 2. Untrusted private subnet (when not configured): 172.31.250.2 must NOT be trusted by default
	reqUnconfiguredSubnet := httptest.NewRequest(http.MethodGet, "/", nil)
	reqUnconfiguredSubnet.RemoteAddr = "172.31.250.2:45678"
	reqUnconfiguredSubnet.Header.Set("X-Forwarded-For", "198.51.100.1")
	reqUnconfiguredSubnet.Header.Set("X-Forwarded-Proto", "https")

	if ClientIP(reqUnconfiguredSubnet) != "172.31.250.2" {
		t.Fatalf("unconfigured subnet client IP = %q, want direct IP %q", ClientIP(reqUnconfiguredSubnet), "172.31.250.2")
	}
	if IsSecure(reqUnconfiguredSubnet, false) {
		t.Fatal("unconfigured subnet must not spoof HTTPS")
	}

	// 3. Trusted loopback proxy (127.0.0.1): headers ARE accepted
	reqLoopback := httptest.NewRequest(http.MethodGet, "/", nil)
	reqLoopback.RemoteAddr = "127.0.0.1:54321"
	reqLoopback.Header.Set("X-Forwarded-For", "198.51.100.1, 10.0.0.1")
	reqLoopback.Header.Set("X-Forwarded-Proto", "https")

	trustedIP := ClientIP(reqLoopback)
	if trustedIP != "198.51.100.1" {
		t.Fatalf("loopback trusted client IP = %q, want %q", trustedIP, "198.51.100.1")
	}
	if !IsSecure(reqLoopback, false) {
		t.Fatal("loopback trusted request with X-Forwarded-Proto=https should be secure")
	}

	// 4. Configured custom subnet (e.g. 172.31.250.0/28 from compose): headers ARE accepted
	SetTrustedProxies([]string{"172.31.250.0/28"})

	reqConfigured := httptest.NewRequest(http.MethodGet, "/", nil)
	reqConfigured.RemoteAddr = "172.31.250.2:45678"
	reqConfigured.Header.Set("X-Forwarded-For", "198.51.100.99")
	reqConfigured.Header.Set("X-Forwarded-Proto", "https")

	if ClientIP(reqConfigured) != "198.51.100.99" {
		t.Fatalf("configured proxy client IP = %q, want %q", ClientIP(reqConfigured), "198.51.100.99")
	}
	if !IsSecure(reqConfigured, false) {
		t.Fatal("configured proxy with X-Forwarded-Proto=https should be secure")
	}
}
