package auth

import (
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

// VerifyCSRFToken compares two CSRF tokens in constant time.
func VerifyCSRFToken(tokenA, tokenB string) bool {
	if len(tokenA) == 0 || len(tokenB) == 0 {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(tokenA), []byte(tokenB)) == 1
}
