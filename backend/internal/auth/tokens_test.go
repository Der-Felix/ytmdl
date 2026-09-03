package auth

import (
	"testing"
)

func TestSessionTokens(t *testing.T) {
	raw1, hash1, err := GenerateSessionToken()
	if err != nil {
		t.Fatalf("generate session token: %v", err)
	}
	if len(raw1) != 64 {
		t.Fatalf("expected 64 hex characters, got %d", len(raw1))
	}
	if len(hash1) != 64 {
		t.Fatalf("expected 64 hex characters for SHA-256 hash, got %d", len(hash1))
	}

	raw2, hash2, err := GenerateSessionToken()
	if err != nil {
		t.Fatalf("generate second session token: %v", err)
	}
	if raw1 == raw2 || hash1 == hash2 {
		t.Fatal("session tokens must be unique and non-deterministic")
	}

	// Verify HashToken function produces identical hash
	computedHash := HashToken(raw1)
	if computedHash != hash1 {
		t.Fatalf("expected HashToken(%q) == %q, got %q", raw1, hash1, computedHash)
	}
}

func TestCSRFTokens(t *testing.T) {
	csrf1, err := GenerateCSRFToken()
	if err != nil {
		t.Fatalf("generate csrf token: %v", err)
	}
	if len(csrf1) != 64 {
		t.Fatalf("expected 64 hex characters for CSRF token, got %d", len(csrf1))
	}

	csrf2, err := GenerateCSRFToken()
	if err != nil {
		t.Fatalf("generate second csrf token: %v", err)
	}
	if csrf1 == csrf2 {
		t.Fatal("CSRF tokens must be distinct")
	}

	if !VerifyCSRFToken(csrf1, csrf1) {
		t.Fatal("expected identical CSRF tokens to verify")
	}
	if VerifyCSRFToken(csrf1, csrf2) {
		t.Fatal("expected different CSRF tokens to fail verification")
	}
	if VerifyCSRFToken("", csrf1) || VerifyCSRFToken(csrf1, "") {
		t.Fatal("empty CSRF token must not verify")
	}
}
