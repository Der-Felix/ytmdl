package auth

import (
	"testing"
	"time"
)

func TestPasswordHashAndVerify(t *testing.T) {
	password := "correct_horse_battery_staple_123"

	hash, err := HashPassword(password)
	if err != nil {
		t.Fatalf("hash password: %v", err)
	}
	if len(hash) == 0 {
		t.Fatal("expected non-empty hash")
	}

	// Positive verify
	match, err := VerifyPassword(password, hash)
	if err != nil {
		t.Fatalf("verify correct password: %v", err)
	}
	if !match {
		t.Fatal("expected correct password to match")
	}

	// Negative verify
	match, err = VerifyPassword("wrong_password_xyz", hash)
	if err != nil {
		t.Fatalf("verify wrong password: %v", err)
	}
	if match {
		t.Fatal("expected wrong password not to match")
	}
}

func TestPasswordValidation(t *testing.T) {
	// Short password
	if err := ValidatePassword("short"); err == nil {
		t.Fatal("expected short password (<8 chars) to fail validation")
	}

	// Valid password
	if err := ValidatePassword("valid_password_8chars"); err != nil {
		t.Fatalf("expected valid password, got %v", err)
	}
}

func TestPasswordHashRandomSalt(t *testing.T) {
	password := "same_password_for_both_runs_123"
	hash1, err := HashPassword(password)
	if err != nil {
		t.Fatalf("hash1: %v", err)
	}
	hash2, err := HashPassword(password)
	if err != nil {
		t.Fatalf("hash2: %v", err)
	}

	if hash1 == hash2 {
		t.Fatal("identical passwords must generate different hashes due to random salt")
	}

	match1, err := VerifyPassword(password, hash1)
	if err != nil || !match1 {
		t.Fatalf("verify hash1 failed: %v", err)
	}
	match2, err := VerifyPassword(password, hash2)
	if err != nil || !match2 {
		t.Fatalf("verify hash2 failed: %v", err)
	}
}

func TestVerifyPasswordCorruptedHashes(t *testing.T) {
	password := "correct_password_123"

	corruptedHashes := []string{
		"",
		"not_a_hash",
		"$argon2i$v=19$m=65536,t=3,p=2$c2FsdHNhbHRzYWx0MTY$a2V5a2V5a2V5a2V5a2V5a2V5a2V5a2V5MzI",
		"$argon2id$v=18$m=65536,t=3,p=2$c2FsdHNhbHRzYWx0MTY$a2V5a2V5a2V5a2V5a2V5a2V5a2V5a2V5MzI",
		"$argon2id$v=19$m=0,t=3,p=2$c2FsdHNhbHRzYWx0MTY$a2V5a2V5a2V5a2V5a2V5a2V5a2V5a2V5MzI",
		"$argon2id$v=19$m=65536,t=3,p=2$invalid_b64!$a2V5a2V5a2V5a2V5a2V5a2V5a2V5a2V5MzI",
		"$argon2id$v=19$m=65536,t=3,p=2$c2FsdHNhbHRzYWx0MTY$invalid_key_b64!",
		"$argon2id$v=19$m=65536,t=3,p=2$$",
	}

	for _, badHash := range corruptedHashes {
		match, err := VerifyPassword(password, badHash)
		if err == nil && match {
			t.Errorf("corrupted hash %q should not verify successfully", badHash)
		}
	}
}

func TestDummyVerifyTimingProtection(t *testing.T) {
	start := time.Now()
	DummyVerify("arbitrary_password")
	duration := time.Since(start)

	if duration < 5*time.Millisecond {
		t.Fatalf("dummy verify should take full Argon2id execution time, took %v", duration)
	}
}
