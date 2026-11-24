package middleware

import (
	"context"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"ytdm/backend/internal/api/response"
	"ytdm/backend/internal/apperr"
	"ytdm/backend/internal/auth"
)

const (
	// SessionCookieName is the name of the HTTP-only session cookie.
	SessionCookieName = "ytmdl_session"
	// CSRFCookieName is the name of the readable double-submit CSRF cookie.
	CSRFCookieName = "ytmdl_csrf"
	// CSRFHeaderName is the HTTP header the client must send for mutating requests.
	CSRFHeaderName = "X-CSRF-Token"
)

type contextKey string

const (
	userKey    contextKey = "auth_user"
	sessionKey contextKey = "auth_session"
)

// UserFromContext retrieves the authenticated user from the context, or nil.
func UserFromContext(ctx context.Context) *auth.User {
	if u, ok := ctx.Value(userKey).(*auth.User); ok {
		return u
	}
	return nil
}

// SessionFromContext retrieves the current session from the context, or nil.
func SessionFromContext(ctx context.Context) *auth.Session {
	if s, ok := ctx.Value(sessionKey).(*auth.Session); ok {
		return s
	}
	return nil
}

// ContextWithUser returns a new context with the given user attached.
func ContextWithUser(ctx context.Context, u *auth.User) context.Context {
	return context.WithValue(ctx, userKey, u)
}

// ContextWithSession returns a new context with the given session attached.
func ContextWithSession(ctx context.Context, s *auth.Session) context.Context {
	return context.WithValue(ctx, sessionKey, s)
}

// ProxyChecker determines if a remote address is a trusted proxy.
type ProxyChecker struct {
	nets []*net.IPNet
	ips  []net.IP
}

// NewProxyChecker creates a new ProxyChecker from CIDRs and IP strings.
// Loopback (127.0.0.1, ::1) is always trusted.
func NewProxyChecker(trusted []string) *ProxyChecker {
	pc := &ProxyChecker{}
	for _, entry := range trusted {
		entry = strings.TrimSpace(entry)
		if entry == "" {
			continue
		}
		if _, ipnet, err := net.ParseCIDR(entry); err == nil {
			pc.nets = append(pc.nets, ipnet)
		} else if ip := net.ParseIP(entry); ip != nil {
			pc.ips = append(pc.ips, ip)
		}
	}
	return pc
}

// IsTrusted reports whether the direct remote address is a trusted peer.
func (pc *ProxyChecker) IsTrusted(remoteAddr string) bool {
	host, _, err := net.SplitHostPort(remoteAddr)
	if err != nil {
		host = remoteAddr
	}
	return pc.isTrustedIP(net.ParseIP(host))
}

// isTrustedIP reports whether ip is loopback or one of the configured proxies.
func (pc *ProxyChecker) isTrustedIP(ip net.IP) bool {
	if ip == nil {
		return false
	}
	if ip.IsLoopback() {
		return true
	}
	for _, ipnet := range pc.nets {
		if ipnet.Contains(ip) {
			return true
		}
	}
	for _, tip := range pc.ips {
		if tip.Equal(ip) {
			return true
		}
	}
	return false
}

var (
	trustedProxyMu     sync.RWMutex
	globalProxyChecker = NewProxyChecker(nil)
)

// SetTrustedProxies configures the global trusted proxy list for the middleware.
func SetTrustedProxies(trusted []string) {
	trustedProxyMu.Lock()
	defer trustedProxyMu.Unlock()
	globalProxyChecker = NewProxyChecker(trusted)
}

// currentProxyChecker returns the configured checker.
func currentProxyChecker() *ProxyChecker {
	trustedProxyMu.RLock()
	defer trustedProxyMu.RUnlock()
	return globalProxyChecker
}

// isTrustedPeer checks if the direct connection originates from a trusted peer.
func isTrustedPeer(remoteAddr string) bool {
	return currentProxyChecker().IsTrusted(remoteAddr)
}

// ClientIP resolves the client IP. X-Forwarded-For is only consulted when the
// direct peer is a trusted proxy (loopback, the ytmdl-frontend container of
// the two-image deployment, or a reverse proxy named in
// MUSICDL_TRUSTED_PROXIES).
//
// The header is read from the right: every proxy appends the address it
// received the request from, so only the entries added by trusted proxies are
// trustworthy, and the first untrusted entry from the right is the client.
// Everything to its left was supplied by the client and may be forged. The
// bundled nginx overwrites the header with a single entry, for which this is
// the same as taking the first one; a reverse proxy that appends instead
// (nginx's $proxy_add_x_forwarded_for, Traefik, Caddy) no longer lets a
// client choose the address its rate limits are counted against.
//
// An entry that is not an IP address ends the walk at the last address that
// was established, so a malformed header can only make the client look like
// its proxy, never like an address of its choosing.
func ClientIP(r *http.Request) string {
	peer := clientIP(r)
	checker := currentProxyChecker()
	if !checker.IsTrusted(r.RemoteAddr) {
		return peer
	}
	values := r.Header.Values("X-Forwarded-For")
	var entries []string
	for _, value := range values {
		entries = append(entries, strings.Split(value, ",")...)
	}
	client := peer
	for i := len(entries) - 1; i >= 0; i-- {
		entry := strings.TrimSpace(entries[i])
		if entry == "" {
			continue
		}
		ip := net.ParseIP(entry)
		if ip == nil {
			return client
		}
		client = entry
		if !checker.isTrustedIP(ip) {
			return client
		}
	}
	return client
}

// IsSecure reports whether the connection is secure (TLS/HTTPS), evaluating
// X-Forwarded-Proto only when received from a trusted proxy.
func IsSecure(r *http.Request, forceSecure bool) bool {
	if forceSecure {
		return true
	}
	if r.TLS != nil {
		return true
	}
	if isTrustedPeer(r.RemoteAddr) {
		if proto := r.Header.Get("X-Forwarded-Proto"); strings.EqualFold(proto, "https") {
			return true
		}
	}
	return false
}

// Authenticate extracts the session cookie and attaches the user and session to context if valid.
func Authenticate(authService *auth.Service) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			cookie, err := r.Cookie(SessionCookieName)
			if err == nil && cookie.Value != "" {
				user, sess, err := authService.VerifySession(r.Context(), cookie.Value)
				if err == nil && user != nil && user.Enabled {
					ctx := context.WithValue(r.Context(), userKey, user)
					ctx = context.WithValue(ctx, sessionKey, sess)
					r = r.WithContext(ctx)
				}
			}
			next.ServeHTTP(w, r)
		})
	}
}

// RequireAuth blocks requests from unauthenticated or disabled users with HTTP 401.
func RequireAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user := UserFromContext(r.Context())
		if user == nil || !user.Enabled {
			response.Fail(w, r, apperr.CodeUnauthenticated, "Authentifizierung erforderlich.")
			return
		}
		next.ServeHTTP(w, r)
	})
}

// RequireAdmin blocks non-admin requests with HTTP 403.
func RequireAdmin(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user := UserFromContext(r.Context())
		if user == nil || !user.Enabled {
			response.Fail(w, r, apperr.CodeUnauthenticated, "Authentifizierung erforderlich.")
			return
		}
		if user.Role != auth.RoleAdmin {
			response.Fail(w, r, apperr.CodeForbidden, "Administratorrechte erforderlich.")
			return
		}
		next.ServeHTTP(w, r)
	})
}

// How the CSRF cookie is issued.
//
// A token is never minted per request: a client that opens with several
// parallel requests would otherwise receive a different token in each
// response, the last Set-Cookie would win, and a mutating request sent in that
// window would carry a header token that no longer matches its cookie.
// Instead:
//
//   - Signed in: the token is SessionCSRFToken(session), the same value for
//     every response of that session. EnsureCSRF sets it on any request that
//     arrives without a CSRF cookie; parallel responses agree.
//   - Signed out: only GET /auth/status issues a random token (IssueCSRF), and
//     only when the client has none. Clients fetch the status before signing
//     in, so no other pre-login response can replace the token a client read.
//   - Sign-in (login, setup, device poll) rotates the cookie to the new
//     session's token, so a token known before sign-in is not the one used
//     after it.
//
// An existing CSRF cookie is never replaced outside sign-in.

// sessionCSRFToken returns the session-bound CSRF token for a request that
// carries a valid session, or "" when the request is signed out.
func sessionCSRFToken(r *http.Request) string {
	if SessionFromContext(r.Context()) == nil {
		return ""
	}
	cookie, err := r.Cookie(SessionCookieName)
	if err != nil {
		return ""
	}
	return auth.SessionCSRFToken(cookie.Value)
}

func hasCSRFCookie(r *http.Request) bool {
	cookie, err := r.Cookie(CSRFCookieName)
	return err == nil && cookie.Value != ""
}

// EnsureCSRF gives a signed-in client without a CSRF cookie its session-bound
// token. It must run after Authenticate. It never issues a token to a
// signed-out client; see IssueCSRF.
func EnsureCSRF(forceSecure bool) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !hasCSRFCookie(r) {
				if token := sessionCSRFToken(r); token != "" {
					SetCSRFCookie(w, token, IsSecure(r, forceSecure))
				}
			}
			next.ServeHTTP(w, r)
		})
	}
}

// IssueCSRF gives a signed-out client without a CSRF cookie a random token. It
// belongs on the one endpoint clients call before signing in (GET
// /auth/status), so that a client's first parallel requests cannot hand it
// competing tokens. It must run after EnsureCSRF, which covers signed-in
// clients.
func IssueCSRF(forceSecure bool) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !hasCSRFCookie(r) && SessionFromContext(r.Context()) == nil {
				if token, err := auth.GenerateCSRFToken(); err == nil {
					SetCSRFCookie(w, token, IsSecure(r, forceSecure))
				}
			}
			next.ServeHTTP(w, r)
		})
	}
}

// CSRF enforces that mutating requests (POST, PUT, PATCH, DELETE) provide a valid
// X-CSRF-Token header. The header is valid when it equals the ytmdl_csrf
// cookie (double submit) or, for a signed-in request, the session-bound token
// of that session. The second form needs the HttpOnly session token to
// compute, so it is no easier to forge than the first; it lets a request pass
// whose cookie was replaced by a late response after the client read it.
func CSRF(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete:
			headerVal := r.Header.Get(CSRFHeaderName)
			if headerVal == "" {
				response.Fail(w, r, apperr.CodeCSRFInvalid, "Ungültiges oder fehlendes CSRF-Token.")
				return
			}
			if expected := sessionCSRFToken(r); expected != "" && auth.VerifyCSRFToken(expected, headerVal) {
				break
			}
			cookie, err := r.Cookie(CSRFCookieName)
			if err != nil || cookie.Value == "" {
				response.Fail(w, r, apperr.CodeCSRFInvalid, "Fehlendes CSRF-Cookie.")
				return
			}
			if !auth.VerifyCSRFToken(cookie.Value, headerVal) {
				response.Fail(w, r, apperr.CodeCSRFInvalid, "Ungültiges oder fehlendes CSRF-Token.")
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

// SetSessionCookie sets the secure HttpOnly session cookie on the response.
func SetSessionCookie(w http.ResponseWriter, token string, expiresAt time.Time, secure bool) {
	maxAge := int(time.Until(expiresAt).Seconds())
	if maxAge < 0 {
		maxAge = 0
	}
	cookie := &http.Cookie{
		Name:     SessionCookieName,
		Value:    token,
		Path:     "/",
		Expires:  expiresAt.UTC(),
		MaxAge:   maxAge,
		HttpOnly: true,
		Secure:   secure,
		SameSite: http.SameSiteLaxMode,
	}
	http.SetCookie(w, cookie)
}

// ClearSessionCookie removes the session cookie on the client.
func ClearSessionCookie(w http.ResponseWriter, secure bool) {
	cookie := &http.Cookie{
		Name:     SessionCookieName,
		Value:    "",
		Path:     "/",
		Expires:  time.Unix(0, 0).UTC(),
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   secure,
		SameSite: http.SameSiteLaxMode,
	}
	http.SetCookie(w, cookie)
}

// SetCSRFCookie sets the readable CSRF cookie for the frontend client.
func SetCSRFCookie(w http.ResponseWriter, token string, secure bool) {
	cookie := &http.Cookie{
		Name:     CSRFCookieName,
		Value:    token,
		Path:     "/",
		MaxAge:   30 * 24 * 3600, // 30 days
		HttpOnly: false,          // must be readable by JavaScript
		Secure:   secure,
		SameSite: http.SameSiteLaxMode,
	}
	http.SetCookie(w, cookie)
}
