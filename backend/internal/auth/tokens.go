package auth

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"

	"ytdm/backend/internal/apperr"
)

const (
	tokenBytesLen = 32
)

// GenerateSessionToken generates a cryptographically secure random session token
// and its SHA-256 hash. The raw token is returned to the client in a cookie,
// while only the token hash is stored in PostgreSQL.
func GenerateSessionToken() (rawToken string, tokenHash string, err error) {
	buf := make([]byte, tokenBytesLen)
	if _, err := rand.Read(buf); err != nil {
		return "", "", apperr.Wrap(apperr.CodeInternal, "failed to generate session token", err)
	}
	rawToken = hex.EncodeToString(buf)
	tokenHash = HashToken(rawToken)
	return rawToken, tokenHash, nil
}

// HashToken computes the SHA-256 hex digest of a raw token.
func HashToken(rawToken string) string {
	h := sha256.Sum256([]byte(rawToken))
	return hex.EncodeToString(h[:])
}

// GenerateCSRFToken generates a 32-byte cryptographically secure random CSRF token.
func GenerateCSRFToken() (string, error) {
	buf := make([]byte, tokenBytesLen)
	if _, err := rand.Read(buf); err != nil {
		return "", apperr.Wrap(apperr.CodeInternal, "failed to generate csrf token", err)
	}
	return hex.EncodeToString(buf), nil
}

// sessionCSRFLabel separates the session-bound CSRF token from every other
// value derived from a session token. Changing it invalidates every issued
// session-bound CSRF token.
const sessionCSRFLabel = "ytmdl csrf token v1"

// SessionCSRFToken derives the CSRF token for a signed-in session from the raw
// session token: HMAC-SHA256 keyed with the session token over a fixed label.
//
// Every response for one session therefore carries the same CSRF token, so
// parallel requests cannot leave a client with a header token that differs
// from its cookie. The value is as secret as the HttpOnly session token it is
// keyed with: a page that cannot read that cookie cannot compute it, and the
// stored session hash (SHA-256 of the raw token) does not reveal it either.
func SessionCSRFToken(rawSessionToken string) string {
	if rawSessionToken == "" {
		return ""
	}
	mac := hmac.New(sha256.New, []byte(rawSessionToken))
	mac.Write([]byte(sessionCSRFLabel))
	return hex.EncodeToString(mac.Sum(nil))
}

// VerifyCSRFToken compares two CSRF tokens in constant time.
func VerifyCSRFToken(tokenA, tokenB string) bool {
	if len(tokenA) == 0 || len(tokenB) == 0 {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(tokenA), []byte(tokenB)) == 1
}
