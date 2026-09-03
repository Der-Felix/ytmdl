package auth

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"golang.org/x/crypto/argon2"
	"ytdm/backend/internal/apperr"
)

// Default Argon2id parameters aligned with OWASP recommendations.
const (
	argonMemory      uint32 = 64 * 1024 // 64 MB
	argonIterations  uint32 = 3
	argonParallelism uint8  = 2
	argonSaltLength  uint32 = 16
	argonKeyLength   uint32 = 32
)

// staticDummyHash is a pre-generated valid Argon2id hash used to prevent
// timing leaks when a login is attempted for a non-existent username.
var staticDummyHash string
var staticDummySalt []byte

func init() {
	staticDummySalt = make([]byte, argonSaltLength)
	// Deterministic salt for dummy hash so init is fast
	for i := range staticDummySalt {
		staticDummySalt[i] = byte(i)
	}
	key := argon2.IDKey([]byte("dummy_password_for_timing_protection"), staticDummySalt,
		argonIterations, argonMemory, argonParallelism, argonKeyLength)
	b64Salt := base64.RawStdEncoding.EncodeToString(staticDummySalt)
	b64Key := base64.RawStdEncoding.EncodeToString(key)
	staticDummyHash = fmt.Sprintf("$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s",
		argon2.Version, argonMemory, argonIterations, argonParallelism, b64Salt, b64Key)
}

// HashPassword hashes a plain text password using Argon2id with a fresh random salt.
func HashPassword(password string) (string, error) {
	if err := ValidatePassword(password); err != nil {
		return "", err
	}

	salt := make([]byte, argonSaltLength)
	if _, err := rand.Read(salt); err != nil {
		return "", apperr.Wrap(apperr.CodeInternal, "failed to generate password salt", err)
	}

	key := argon2.IDKey([]byte(password), salt, argonIterations, argonMemory, argonParallelism, argonKeyLength)

	b64Salt := base64.RawStdEncoding.EncodeToString(salt)
	b64Key := base64.RawStdEncoding.EncodeToString(key)

	encoded := fmt.Sprintf("$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s",
		argon2.Version, argonMemory, argonIterations, argonParallelism, b64Salt, b64Key)
	return encoded, nil
}

// VerifyPassword verifies a plain text password against an Argon2id encoded hash string.
func VerifyPassword(password, encodedHash string) (bool, error) {
	parts := strings.Split(encodedHash, "$")
	if len(parts) != 6 {
		return false, errors.New("invalid hash format")
	}

	if parts[1] != "argon2id" {
		return false, errors.New("unsupported algorithm: " + parts[1])
	}

	var version int
	if _, err := fmt.Sscanf(parts[2], "v=%d", &version); err != nil || version != argon2.Version {
		return false, errors.New("incompatible argon2 version")
	}

	var memory, iterations uint32
	var parallelism uint8
	params := strings.Split(parts[3], ",")
	for _, p := range params {
		kv := strings.Split(p, "=")
		if len(kv) != 2 {
			continue
		}
		val, err := strconv.ParseUint(kv[1], 10, 32)
		if err != nil {
			return false, errors.New("invalid parameter value in hash")
		}
		switch kv[0] {
		case "m":
			memory = uint32(val)
		case "t":
			iterations = uint32(val)
		case "p":
			parallelism = uint8(val)
		}
	}

	if memory == 0 || iterations == 0 || parallelism == 0 {
		return false, errors.New("missing argon2 parameters")
	}

	salt, err := base64.RawStdEncoding.DecodeString(parts[4])
	if err != nil || len(salt) == 0 {
		return false, errors.New("invalid salt encoding")
	}

	expectedKey, err := base64.RawStdEncoding.DecodeString(parts[5])
	if err != nil || len(expectedKey) == 0 {
		return false, errors.New("invalid key encoding")
	}

	key := argon2.IDKey([]byte(password), salt, iterations, memory, parallelism, uint32(len(expectedKey)))

	if subtle.ConstantTimeCompare(key, expectedKey) == 1 {
		return true, nil
	}
	return false, nil
}

// DummyVerify runs the full Argon2id computation against the static dummy hash
// to ensure timing consistency when a user account does not exist.
func DummyVerify(password string) {
	_, _ = VerifyPassword(password, staticDummyHash)
}
