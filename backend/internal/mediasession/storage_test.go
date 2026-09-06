package mediasession

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestCookieStorage_CreateAndRead(t *testing.T) {
	tempDir := t.TempDir()
	storage, err := NewCookieStorage(tempDir, nil)
	if err != nil {
		t.Fatalf("NewCookieStorage failed: %v", err)
	}

	sessionID := "11111111-2222-3333-4444-555555555555"
	syntheticContent := []byte("# Netscape HTTP Cookie File\n.youtube.com\tTRUE\t/\tTRUE\t2147483647\tSID\tfake-cookie-secret\n")

	ref, err := storage.Store(sessionID, syntheticContent)
	if err != nil {
		t.Fatalf("Store failed: %v", err)
	}

	expectedRef := CookieRefPrefix + sessionID
	if ref != expectedRef {
		t.Errorf("ref = %q, want %q", ref, expectedRef)
	}

	// Read through storage
	got, err := storage.Read(ref)
	if err != nil {
		t.Fatalf("Read failed: %v", err)
	}
	if string(got) != string(syntheticContent) {
		t.Errorf("content mismatch: got %q, want %q", string(got), string(syntheticContent))
	}

	// Verify permissions (on POSIX systems)
	if runtime.GOOS != "windows" {
		path, err := storage.ResolvePath(ref)
		if err != nil {
			t.Fatalf("ResolvePath failed: %v", err)
		}
		fi, err := os.Stat(path)
		if err != nil {
			t.Fatalf("Stat failed: %v", err)
		}
		mode := fi.Mode().Perm()
		if mode != 0600 {
			t.Errorf("file mode = %04o, want 0600", mode)
		}
	}
}

func TestCookieStorage_AtomicReplace(t *testing.T) {
	tempDir := t.TempDir()
	storage, err := NewCookieStorage(tempDir, nil)
	if err != nil {
		t.Fatalf("NewCookieStorage failed: %v", err)
	}

	sessionID := "session-atomic-test"
	initial := []byte("initial-synthetic-cookie")
	replaced := []byte("replaced-synthetic-cookie-updated")

	ref, err := storage.Store(sessionID, initial)
	if err != nil {
		t.Fatalf("initial Store failed: %v", err)
	}

	if err := storage.Replace(sessionID, replaced); err != nil {
		t.Fatalf("Replace failed: %v", err)
	}

	got, err := storage.Read(ref)
	if err != nil {
		t.Fatalf("Read after replace failed: %v", err)
	}
	if string(got) != string(replaced) {
		t.Errorf("content after replace = %q, want %q", string(got), string(replaced))
	}

	// Verify no temporary files remain in the directory
	entries, err := os.ReadDir(tempDir)
	if err != nil {
		t.Fatalf("ReadDir failed: %v", err)
	}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".") {
			t.Errorf("temporary file was not cleaned up: %s", entry.Name())
		}
	}
}

func TestCookieStorage_Delete(t *testing.T) {
	tempDir := t.TempDir()
	storage, err := NewCookieStorage(tempDir, nil)
	if err != nil {
		t.Fatalf("NewCookieStorage failed: %v", err)
	}

	sessionID := "session-delete-test"
	ref, err := storage.Store(sessionID, []byte("temp-data"))
	if err != nil {
		t.Fatalf("Store failed: %v", err)
	}

	if err := storage.Delete(ref); err != nil {
		t.Fatalf("Delete failed: %v", err)
	}

	// Verify path no longer resolves to an existing file
	_, err = storage.ResolvePath(ref)
	if err == nil {
		t.Fatal("expected ResolvePath to fail after delete, got nil")
	}

	// Idempotent delete should succeed without error
	if err := storage.Delete(ref); err != nil {
		t.Errorf("second Delete should be idempotent, got: %v", err)
	}
}

func TestCookieStorage_Security_PathTraversalAndInjection(t *testing.T) {
	tempDir := t.TempDir()
	storage, err := NewCookieStorage(tempDir, nil)
	if err != nil {
		t.Fatalf("NewCookieStorage failed: %v", err)
	}

	maliciousIDs := []string{
		"../escape",
		"../../etc/passwd",
		"sub/folder",
		"session\x00null",
		"session with spaces",
		"session;rm -rf /",
		"/absolute/path",
		"",
	}

	for _, id := range maliciousIDs {
		_, err := storage.Store(id, []byte("malicious-test"))
		if err == nil {
			t.Errorf("expected Store to reject malicious ID %q, got nil", id)
		}

		err = storage.Replace(id, []byte("malicious-test"))
		if err == nil {
			t.Errorf("expected Replace to reject malicious ID %q, got nil", id)
		}

		ref := CookieRefPrefix + id
		_, err = storage.ResolvePath(ref)
		if err == nil {
			t.Errorf("expected ResolvePath to reject malicious ref %q, got nil", ref)
		}
	}

	// Test raw filesystem path injection in ResolvePath
	rawPaths := []string{
		"/etc/passwd",
		filepath.Join(tempDir, "manual.txt"),
		"relative/path/cookies.txt",
	}
	for _, rp := range rawPaths {
		_, err := storage.ResolvePath(rp)
		if err == nil {
			t.Errorf("expected ResolvePath to reject raw path %q, got nil", rp)
		}
	}
}

func TestCookieStorage_Security_SymlinkEscape(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("skipping symlink escape test on Windows")
	}

	tempDir := t.TempDir()
	outsideDir := t.TempDir()
	targetOutside := filepath.Join(outsideDir, "outside_file.txt")
	_ = os.WriteFile(targetOutside, []byte("outside-secret"), 0600)

	storage, err := NewCookieStorage(tempDir, nil)
	if err != nil {
		t.Fatalf("NewCookieStorage failed: %v", err)
	}

	// Create symlink inside storage baseDir pointing outside
	symlinkPath := filepath.Join(tempDir, "escaped-session.cookies.txt")
	if err := os.Symlink(targetOutside, symlinkPath); err != nil {
		t.Fatalf("failed to create test symlink: %v", err)
	}

	// Store or Replace targeting this session must fail with security violation
	err = storage.Replace("escaped-session", []byte("override-outside"))
	if err == nil {
		t.Error("expected Replace on symlink to fail with security violation, got nil")
	}

	_, err = storage.ResolvePath(CookieRefPrefix + "escaped-session")
	if err == nil {
		t.Error("expected ResolvePath on symlink to fail with security violation, got nil")
	}
}

func TestCookieStorage_Security_NoSecretInErrors(t *testing.T) {
	tempDir := t.TempDir()
	storage, err := NewCookieStorage(tempDir, nil)
	if err != nil {
		t.Fatalf("NewCookieStorage failed: %v", err)
	}

	secretCookie := "super_secret_cookie_token_987654321"

	// Trigger an error by attempting to store with invalid ID containing parts of secret
	_, err = storage.Store("invalid/id", []byte(secretCookie))
	if err != nil {
		if strings.Contains(err.Error(), secretCookie) {
			t.Fatalf("error message leaked secret cookie contents: %v", err)
		}
	}
}
