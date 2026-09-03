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
	ip := net.ParseIP(host)
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

// isTrustedPeer checks if the direct connection originates from a trusted peer.
func isTrustedPeer(remoteAddr string) bool {
	trustedProxyMu.RLock()
	checker := globalProxyChecker
	trustedProxyMu.RUnlock()
	return checker.IsTrusted(remoteAddr)
}

// ClientIP resolves the client IP. It only trusts X-Forwarded-For if the peer
// is an explicitly trusted proxy (loopback or ytmdl-frontend container).
func ClientIP(r *http.Request) string {
	if isTrustedPeer(r.RemoteAddr) {
		if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
			parts := strings.Split(xff, ",")
			if len(parts) > 0 {
				client := strings.TrimSpace(parts[0])
				if client != "" {
					return client
				}
			}
		}
	}
	return clientIP(r)
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

// EnsureCSRF ensures that a readable CSRF cookie is set on any incoming request.
func EnsureCSRF(forceSecure bool) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if cookie, err := r.Cookie(CSRFCookieName); err != nil || cookie.Value == "" {
				token, err := auth.GenerateCSRFToken()
				if err == nil {
					SetCSRFCookie(w, token, IsSecure(r, forceSecure))
				}
			}
			next.ServeHTTP(w, r)
		})
	}
}

// CSRF enforces that mutating requests (POST, PUT, PATCH, DELETE) provide a valid
// X-CSRF-Token header matching the ytmdl_csrf cookie.
func CSRF(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete:
			cookie, err := r.Cookie(CSRFCookieName)
			if err != nil || cookie.Value == "" {
				response.Fail(w, r, apperr.CodeCSRFInvalid, "Fehlendes CSRF-Cookie.")
				return
			}
			headerVal := r.Header.Get(CSRFHeaderName)
			if headerVal == "" || !auth.VerifyCSRFToken(cookie.Value, headerVal) {
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
