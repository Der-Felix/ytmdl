package storage

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"ytdm/backend/internal/apperr"
)

func TestStorageGuard_DisabledWhenUnset(t *testing.T) {
	tmp := t.TempDir()
	guard := NewStorageGuard(tmp, "", 0)

	status, err := guard.ValidateIdentity()
	if err != nil {
		t.Fatalf("expected nil error when guard unset, got: %v", err)
	}
	if status != GuardDisabled {
		t.Fatalf("expected GuardDisabled, got %v", status)
	}

	health := guard.CheckHealth(context.Background(), true)
	if health.Status != HealthHealthy {
		t.Fatalf("expected HealthHealthy, got %v (%s)", health.Status, health.LastError)
	}
	if health.GuardStatus != GuardDisabled {
		t.Fatalf("expected GuardDisabled, got %v", health.GuardStatus)
	}
}

func TestStorageGuard_MissingMarker(t *testing.T) {
	tmp := t.TempDir()
	guard := NewStorageGuard(tmp, "nas-guard-uuid-1234", 0)

	status, err := guard.ValidateIdentity()
	if err == nil {
		t.Fatal("expected error when marker missing, got nil")
	}
	if status != GuardMissing {
		t.Fatalf("expected GuardMissing, got %v", status)
	}

	health := guard.CheckHealth(context.Background(), true)
	if health.Status != HealthGuardMissing {
		t.Fatalf("expected HealthGuardMissing, got %v", health.Status)
	}
}

func TestStorageGuard_ValidMarker(t *testing.T) {
	tmp := t.TempDir()
	guardID := "nas-guard-uuid-1234"
	markerPath := filepath.Join(tmp, MarkerFileName)
	if err := os.WriteFile(markerPath, []byte("ytmdl-storage:nas-guard-uuid-1234\n"), 0o644); err != nil {
		t.Fatalf("write marker: %v", err)
	}

	guard := NewStorageGuard(tmp, guardID, 0)
	status, err := guard.ValidateIdentity()
	if err != nil {
		t.Fatalf("expected nil error on valid marker, got: %v", err)
	}
	if status != GuardVerified {
		t.Fatalf("expected GuardVerified, got %v", status)
	}

	health := guard.CheckHealth(context.Background(), true)
	if health.Status != HealthHealthy {
		t.Fatalf("expected HealthHealthy, got %v (%s)", health.Status, health.LastError)
	}
	if health.GuardStatus != GuardVerified {
		t.Fatalf("expected GuardVerified in health, got %v", health.GuardStatus)
	}
	if !health.Writable {
		t.Fatal("expected Writable = true")
	}
}

func TestStorageGuard_MismatchedMarker(t *testing.T) {
	tmp := t.TempDir()
	markerPath := filepath.Join(tmp, MarkerFileName)
	if err := os.WriteFile(markerPath, []byte("ytmdl-storage:wrong-id\n"), 0o644); err != nil {
		t.Fatalf("write marker: %v", err)
	}

	guard := NewStorageGuard(tmp, "nas-guard-uuid-1234", 0)
	status, err := guard.ValidateIdentity()
	if err == nil {
		t.Fatal("expected error on mismatched marker, got nil")
	}
	if status != GuardMismatch {
		t.Fatalf("expected GuardMismatch, got %v", status)
	}

	health := guard.CheckHealth(context.Background(), true)
	if health.Status != HealthGuardMismatch {
		t.Fatalf("expected HealthGuardMismatch, got %v", health.Status)
	}
}

func TestStorageGuard_RejectsSymlinkMarker(t *testing.T) {
	tmp := t.TempDir()
	targetFile := filepath.Join(tmp, "real-marker.txt")
	if err := os.WriteFile(targetFile, []byte("ytmdl-storage:nas-guard-uuid-1234\n"), 0o644); err != nil {
		t.Fatalf("write real marker: %v", err)
	}

	markerPath := filepath.Join(tmp, MarkerFileName)
	if err := os.Symlink(targetFile, markerPath); err != nil {
		t.Fatalf("symlink marker: %v", err)
	}

	guard := NewStorageGuard(tmp, "nas-guard-uuid-1234", 0)
	status, err := guard.ValidateIdentity()
	if err == nil {
		t.Fatal("expected error on symlink marker, got nil")
	}
	if status != GuardMismatch {
		t.Fatalf("expected GuardMismatch, got %v", status)
	}
}

func TestStorageGuard_RejectsDirectoryMarker(t *testing.T) {
	tmp := t.TempDir()
	markerPath := filepath.Join(tmp, MarkerFileName)
	if err := os.Mkdir(markerPath, 0o755); err != nil {
		t.Fatalf("mkdir marker: %v", err)
	}

	guard := NewStorageGuard(tmp, "nas-guard-uuid-1234", 0)
	status, err := guard.ValidateIdentity()
	if err == nil {
		t.Fatal("expected error on directory marker, got nil")
	}
	if status != GuardMismatch {
		t.Fatalf("expected GuardMismatch, got %v", status)
	}
}

func TestStorageGuard_NoWriteBeforeGuardVerification(t *testing.T) {
	tmp := t.TempDir()
	healthDir := filepath.Join(tmp, ".ytmdl-health")

	guard := NewStorageGuard(tmp, "guard-missing-test", 0)
	// Execute health check on missing guard
	health := guard.CheckHealth(context.Background(), true)
	if health.Status != HealthGuardMissing {
		t.Fatalf("expected HealthGuardMissing, got %v", health.Status)
	}

	// Verify that .ytmdl-health was NOT created on disk because guard failed
	if _, err := os.Stat(healthDir); !os.IsNotExist(err) {
		t.Fatalf("expected .ytmdl-health to NOT exist when guard fails, but it was created")
	}
}

func TestStorageGuard_CachingAndSingleFlight(t *testing.T) {
	tmp := t.TempDir()
	guard := NewStorageGuard(tmp, "", 0)

	h1 := guard.CheckHealth(context.Background(), false)
	time.Sleep(10 * time.Millisecond)
	h2 := guard.CheckHealth(context.Background(), false)

	if !h1.LastChecked.Equal(h2.LastChecked) {
		t.Fatalf("expected cached health check result, got h1=%v h2=%v", h1.LastChecked, h2.LastChecked)
	}

	// Force probe bypasses cache
	h3 := guard.CheckHealth(context.Background(), true)
	if !h3.LastChecked.After(h1.LastChecked) {
		t.Fatalf("expected forced probe to have newer LastChecked, got h3=%v h1=%v", h3.LastChecked, h1.LastChecked)
	}
}

func TestStorageGuard_RequireWritable(t *testing.T) {
	tmp := t.TempDir()
	guard := NewStorageGuard(tmp, "test-guard", 0)

	// Marker missing -> RequireWritable fails
	if err := guard.RequireWritable(); err == nil {
		t.Fatal("expected RequireWritable to fail when marker missing, got nil")
	}

	// Create valid marker
	markerPath := filepath.Join(tmp, MarkerFileName)
	if err := os.WriteFile(markerPath, []byte("ytmdl-storage:test-guard\n"), 0o644); err != nil {
		t.Fatalf("write marker: %v", err)
	}

	if err := guard.RequireWritable(); err != nil {
		t.Fatalf("expected RequireWritable to pass, got: %v", err)
	}

	// Test with huge minFreeBytes threshold
	hugeGuard := NewStorageGuard(tmp, "test-guard", 1<<60) // 1 Exabyte
	if err := hugeGuard.RequireWritable(); err == nil {
		t.Fatal("expected huge minFreeBytes to trigger LowSpace error, got nil")
	} else if apperr.CodeOf(err) != apperr.CodeStorageLowSpace {
		t.Fatalf("expected CodeStorageLowSpace, got %v", apperr.CodeOf(err))
	}
}

func TestStorageGuard_RejectsEmptyAndOversizedMarker(t *testing.T) {
	tmp := t.TempDir()
	markerPath := filepath.Join(tmp, MarkerFileName)

	guard := NewStorageGuard(tmp, "nas-guard-uuid-1234", 0)

	// 1. Empty marker file
	if err := os.WriteFile(markerPath, []byte(""), 0o644); err != nil {
		t.Fatalf("write empty marker: %v", err)
	}
	status, err := guard.ValidateIdentity()
	if err == nil || status != GuardMismatch {
		t.Fatalf("expected GuardMismatch on empty marker, got status=%v err=%v", status, err)
	}

	// 2. Oversized marker file (> 4096 bytes)
	largeData := make([]byte, 5000)
	copy(largeData, []byte("ytmdl-storage:nas-guard-uuid-1234\n"))
	if err := os.WriteFile(markerPath, largeData, 0o644); err != nil {
		t.Fatalf("write large marker: %v", err)
	}
	status, err = guard.ValidateIdentity()
	if err == nil || status != GuardMismatch {
		t.Fatalf("expected GuardMismatch on oversized marker, got status=%v err=%v", status, err)
	}
}

func TestStorageGuard_FreshValidationOnMutationBypassesCache(t *testing.T) {
	tmp := t.TempDir()
	markerPath := filepath.Join(tmp, MarkerFileName)
	if err := os.WriteFile(markerPath, []byte("ytmdl-storage:nas-guard-uuid-1234\n"), 0o644); err != nil {
		t.Fatalf("write marker: %v", err)
	}

	guard := NewStorageGuard(tmp, "nas-guard-uuid-1234", 0)

	// t=0: Health check succeeds and caches healthy state for 30s
	health := guard.CheckHealth(context.Background(), false)
	if health.Status != HealthHealthy {
		t.Fatalf("expected HealthHealthy, got %v", health.Status)
	}

	// t=1: NAS unmounts / marker is removed
	if err := os.Remove(markerPath); err != nil {
		t.Fatalf("remove marker: %v", err)
	}

	// Health check with cache still returns cached status (for UI)
	cachedHealth := guard.CheckHealth(context.Background(), false)
	if cachedHealth.Status != HealthHealthy {
		t.Fatalf("expected cached HealthHealthy, got %v", cachedHealth.Status)
	}

	// BUT write authorization (RequireWritable) is ALWAYS FRESH and immediately fails!
	if err := guard.RequireWritable(); err == nil {
		t.Fatal("CRITICAL SECURITY FLAW: RequireWritable allowed write after marker removal due to cache!")
	}
}
