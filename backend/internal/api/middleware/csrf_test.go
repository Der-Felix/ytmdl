package middleware

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"ytdm/backend/internal/auth"
)

// The raw session token the fake authenticator accepts.
const testSessionToken = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"

// fakeAuthenticate stands in for Authenticate: a request carrying
// testSessionToken is signed in, any other session cookie is not.
func fakeAuthenticate(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if c, err := r.Cookie(SessionCookieName); err == nil && c.Value == testSessionToken {
			ctx := ContextWithUser(r.Context(), &auth.User{ID: "u1", Enabled: true})
			ctx = ContextWithSession(ctx, &auth.Session{ID: "s1"})
			r = r.WithContext(ctx)
		}
		next.ServeHTTP(w, r)
	})
}

// csrfTestRouter mirrors the order in api.NewRouter: Authenticate, then
// EnsureCSRF on everything, IssueCSRF on the status endpoint only, CSRF on
// mutating routes.
func csrfTestRouter() http.Handler {
	ok := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) })
	mux := http.NewServeMux()
	mux.Handle("GET /auth/status", IssueCSRF(false)(ok))
	mux.Handle("GET /health", ok)
	mux.Handle("GET /jobs", ok)
	mux.Handle("POST /auth/login", CSRF(ok))
	mux.Handle("POST /items", CSRF(ok))
	return fakeAuthenticate(EnsureCSRF(false)(mux))
}

type csrfCase struct {
	method, path   string
	session        string
	cookie, header string
}

func serveCSRF(t *testing.T, h http.Handler, c csrfCase) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(c.method, c.path, nil)
	if c.session != "" {
		req.AddCookie(&http.Cookie{Name: SessionCookieName, Value: c.session})
	}
	if c.cookie != "" {
		req.AddCookie(&http.Cookie{Name: CSRFCookieName, Value: c.cookie})
	}
	if c.header != "" {
		req.Header.Set(CSRFHeaderName, c.header)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func issuedCSRF(rec *httptest.ResponseRecorder) string {
	for _, c := range rec.Result().Cookies() {
		if c.Name == CSRFCookieName {
			return c.Value
		}
	}
	return ""
}

func TestCSRFSignedOutOnlyStatusIssuesToken(t *testing.T) {
	h := csrfTestRouter()
	for _, path := range []string{"/health", "/jobs"} {
		if got := issuedCSRF(serveCSRF(t, h, csrfCase{method: http.MethodGet, path: path})); got != "" {
			t.Fatalf("GET %s issued a CSRF token to a signed-out client: %q", path, got)
		}
	}
	if got := issuedCSRF(serveCSRF(t, h, csrfCase{method: http.MethodGet, path: "/auth/status"})); len(got) != 64 {
		t.Fatalf("GET /auth/status issued %q, want a 64-character token", got)
	}
	// A client that already holds a token keeps it.
	if got := issuedCSRF(serveCSRF(t, h, csrfCase{method: http.MethodGet, path: "/auth/status", cookie: "held"})); got != "" {
		t.Fatalf("GET /auth/status replaced an existing token with %q", got)
	}
}

// A fresh client opens with several parallel requests and no cookie. Whatever
// token it read, from whichever response, its sign-in must pass.
func TestCSRFParallelFirstRequestsThenLogin(t *testing.T) {
	h := csrfTestRouter()
	paths := []string{"/auth/status", "/health", "/jobs", "/health", "/jobs", "/jobs"}
	issued := make([]string, len(paths))
	var wg sync.WaitGroup
	for i, path := range paths {
		wg.Add(1)
		go func() {
			defer wg.Done()
			issued[i] = issuedCSRF(serveCSRF(t, h, csrfCase{method: http.MethodGet, path: path}))
		}()
	}
	wg.Wait()

	var tokens []string
	for _, tok := range issued {
		if tok != "" {
			tokens = append(tokens, tok)
		}
	}
	if len(tokens) != 1 {
		t.Fatalf("parallel first requests set %d CSRF cookies, want exactly 1: %v", len(tokens), issued)
	}
	// The browser's jar holds the only token issued; the client read the same.
	rec := serveCSRF(t, h, csrfCase{method: http.MethodPost, path: "/auth/login", cookie: tokens[0], header: tokens[0]})
	if rec.Code != http.StatusOK {
		t.Fatalf("login after parallel first requests: status %d, want 200", rec.Code)
	}
}

// After sign-in every response that sets the cookie sets the same value, so a
// client firing parallel requests without a CSRF cookie cannot end up with a
// header that differs from its cookie.
func TestCSRFParallelSignedInRequestsAgree(t *testing.T) {
	h := csrfTestRouter()
	want := auth.SessionCSRFToken(testSessionToken)
	issued := make([]string, 8)
	var wg sync.WaitGroup
	for i := range issued {
		wg.Add(1)
		go func() {
			defer wg.Done()
			path := []string{"/auth/status", "/jobs", "/health"}[i%3]
			issued[i] = issuedCSRF(serveCSRF(t, h, csrfCase{method: http.MethodGet, path: path, session: testSessionToken}))
		}()
	}
	wg.Wait()
	for i, got := range issued {
		if got != want {
			t.Fatalf("response %d set CSRF %q, want the session-bound token %q", i, got, want)
		}
	}
	for _, read := range issued {
		for _, jar := range issued {
			rec := serveCSRF(t, h, csrfCase{method: http.MethodPost, path: "/items", session: testSessionToken, cookie: jar, header: read})
			if rec.Code != http.StatusOK {
				t.Fatalf("mutation with header %q and cookie %q: status %d, want 200", read, jar, rec.Code)
			}
		}
	}
}

// A late pre-login response may replace the session-bound cookie after the
// client read it. The session-bound header still proves the request.
func TestCSRFSessionTokenSurvivesReplacedCookie(t *testing.T) {
	h := csrfTestRouter()
	rec := serveCSRF(t, h, csrfCase{method: http.MethodPost, path: "/items", session: testSessionToken,
		cookie: strings.Repeat("a", 64), header: auth.SessionCSRFToken(testSessionToken)})
	if rec.Code != http.StatusOK {
		t.Fatalf("session-bound header with a replaced cookie: status %d, want 200", rec.Code)
	}
	// An existing cookie is never overwritten outside sign-in.
	rec = serveCSRF(t, h, csrfCase{method: http.MethodGet, path: "/jobs", session: testSessionToken, cookie: "legacy"})
	if got := issuedCSRF(rec); got != "" {
		t.Fatalf("signed-in GET replaced an existing CSRF cookie with %q", got)
	}
	// A legacy random cookie from before the upgrade keeps working as a double submit.
	rec = serveCSRF(t, h, csrfCase{method: http.MethodPost, path: "/items", session: testSessionToken, cookie: "legacy", header: "legacy"})
	if rec.Code != http.StatusOK {
		t.Fatalf("legacy double-submit token after upgrade: status %d, want 200", rec.Code)
	}
}

func TestCSRFRefusesMissingWrongAndForgedTokens(t *testing.T) {
	h := csrfTestRouter()
	sessionToken := auth.SessionCSRFToken(testSessionToken)
	otherSessionToken := auth.SessionCSRFToken(strings.Repeat("f", 64))
	cases := []struct {
		name string
		c    csrfCase
	}{
		{"signed out, no cookie, no header", csrfCase{path: "/auth/login"}},
		{"signed out, cookie, no header", csrfCase{path: "/auth/login", cookie: "tok"}},
		{"signed out, header, no cookie", csrfCase{path: "/auth/login", header: "tok"}},
		{"signed out, wrong header", csrfCase{path: "/auth/login", cookie: "tok", header: "other"}},
		// Without a valid session a session-bound token is just another string.
		{"signed out, session-derived header without session", csrfCase{path: "/auth/login", header: sessionToken}},
		{"invalid session, its derived header", csrfCase{path: "/items", session: strings.Repeat("e", 64),
			header: auth.SessionCSRFToken(strings.Repeat("e", 64))}},
		{"signed in, no header", csrfCase{path: "/items", session: testSessionToken, cookie: sessionToken}},
		{"signed in, wrong header", csrfCase{path: "/items", session: testSessionToken, cookie: sessionToken, header: "forged"}},
		{"signed in, another session's token", csrfCase{path: "/items", session: testSessionToken, header: otherSessionToken}},
		{"signed in, another session's token in cookie and header", csrfCase{path: "/items", session: testSessionToken,
			cookie: otherSessionToken + "x", header: otherSessionToken}},
		{"signed in, raw session token as header", csrfCase{path: "/items", session: testSessionToken, header: testSessionToken}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tc.c.method = http.MethodPost
			rec := serveCSRF(t, h, tc.c)
			if rec.Code != http.StatusForbidden || !strings.Contains(rec.Body.String(), "CSRF_INVALID") {
				t.Fatalf("status %d body %s, want 403 CSRF_INVALID", rec.Code, rec.Body.String())
			}
		})
	}
}

func TestSessionCSRFToken(t *testing.T) {
	a := auth.SessionCSRFToken(testSessionToken)
	if a != auth.SessionCSRFToken(testSessionToken) {
		t.Fatal("session CSRF token is not deterministic")
	}
	if len(a) != 64 || a == testSessionToken || a == auth.HashToken(testSessionToken) {
		t.Fatalf("session CSRF token %q must be a distinct 64-character value", a)
	}
	if a == auth.SessionCSRFToken(strings.Repeat("f", 64)) {
		t.Fatal("different sessions share a CSRF token")
	}
	if auth.SessionCSRFToken("") != "" {
		t.Fatal("an empty session token must not yield a CSRF token")
	}
}
