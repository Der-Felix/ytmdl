package dotenv_test

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"ytdm/backend/cmd/ytmdlctl/internal/dotenv"
)

func TestParseEnvFile(t *testing.T) {
	content := `
# Deployment settings
YTMDL_VERSION=0.15.0
YTMDL_HOST_PORT=8080

# Quoted values
MUSICDL_LIBRARY="/music"
YTMDL_DATA_PATH='./data'

# Whitespace and comments
  POSTGRES_DB = ytmdl  # inline comments ignored
POSTGRES_USER=ytmdl_user

# Duplicate keys: latest value must win deterministically
DUPLICATE_KEY=first
DUPLICATE_KEY=second

# Empty value
EMPTY_VAR=
`
	tmpDir := t.TempDir()
	envPath := filepath.Join(tmpDir, ".env")
	if err := os.WriteFile(envPath, []byte(content), 0600); err != nil {
		t.Fatalf("WriteFile failed: %v", err)
	}

	vars, err := dotenv.ParseFile(envPath)
	if err != nil {
		t.Fatalf("ParseFile failed: %v", err)
	}

	tests := []struct {
		key      string
		expected string
		exists   bool
	}{
		{"YTMDL_VERSION", "0.15.0", true},
		{"YTMDL_HOST_PORT", "8080", true},
		{"MUSICDL_LIBRARY", "/music", true},
		{"YTMDL_DATA_PATH", "./data", true},
		{"POSTGRES_DB", "ytmdl", true},
		{"POSTGRES_USER", "ytmdl_user", true},
		{"DUPLICATE_KEY", "second", true},
		{"EMPTY_VAR", "", true},
		{"NON_EXISTENT", "", false},
	}

	for _, tc := range tests {
		t.Run(tc.key, func(t *testing.T) {
			val, ok := vars[tc.key]
			if ok != tc.exists {
				t.Fatalf("key %s exists = %v, want %v", tc.key, ok, tc.exists)
			}
			if val != tc.expected {
				t.Errorf("vars[%s] = %q, want %q", tc.key, val, tc.expected)
			}
		})
	}
}

func TestParseMissingFileReturnsEmpty(t *testing.T) {
	tmpDir := t.TempDir()
	envPath := filepath.Join(tmpDir, ".env.nonexistent")

	vars, err := dotenv.ParseFile(envPath)
	if err != nil {
		t.Fatalf("ParseFile on nonexistent file should not error, got: %v", err)
	}
	if len(vars) != 0 {
		t.Fatalf("expected empty map, got: %+v", vars)
	}
}

func TestValidateForUpdateSuccess(t *testing.T) {
	tmpDir := t.TempDir()
	envPath := filepath.Join(tmpDir, ".env")
	content := "# Comment\nYTMDL_VERSION=0.15.0\nOTHER=foo\n"
	_ = os.WriteFile(envPath, []byte(content), 0600)

	ver, err := dotenv.ValidateForUpdate(envPath)
	if err != nil {
		t.Fatalf("ValidateForUpdate failed: %v", err)
	}
	if ver != "0.15.0" {
		t.Errorf("got version %q, want 0.15.0", ver)
	}
}

func TestValidateForUpdateRejectsLatest(t *testing.T) {
	tmpDir := t.TempDir()
	envPath := filepath.Join(tmpDir, ".env")
	content := "YTMDL_VERSION=latest\n"
	_ = os.WriteFile(envPath, []byte(content), 0600)

	_, err := dotenv.ValidateForUpdate(envPath)
	if err == nil || !strings.Contains(err.Error(), "pinned SemVer") {
		t.Fatalf("expected error for 'latest', got: %v", err)
	}
}

func TestValidateForUpdateRejectsMissing(t *testing.T) {
	tmpDir := t.TempDir()
	envPath := filepath.Join(tmpDir, ".env")
	content := "OTHER=foo\n"
	_ = os.WriteFile(envPath, []byte(content), 0600)

	_, err := dotenv.ValidateForUpdate(envPath)
	if !errors.Is(err, dotenv.ErrVersionNotFound) {
		t.Fatalf("expected ErrVersionNotFound, got: %v", err)
	}
}

func TestValidateForUpdateRejectsDuplicate(t *testing.T) {
	tmpDir := t.TempDir()
	envPath := filepath.Join(tmpDir, ".env")
	content := "YTMDL_VERSION=0.15.0\nYTMDL_VERSION=0.15.1\n"
	_ = os.WriteFile(envPath, []byte(content), 0600)

	_, err := dotenv.ValidateForUpdate(envPath)
	if !errors.Is(err, dotenv.ErrDuplicateVersionKey) {
		t.Fatalf("expected ErrDuplicateVersionKey, got: %v", err)
	}
}

func TestValidateForUpdateRejectsSymlink(t *testing.T) {
	tmpDir := t.TempDir()
	realPath := filepath.Join(tmpDir, ".env.real")
	symlinkPath := filepath.Join(tmpDir, ".env")
	_ = os.WriteFile(realPath, []byte("YTMDL_VERSION=0.15.0\n"), 0600)
	_ = os.Symlink(realPath, symlinkPath)

	_, err := dotenv.ValidateForUpdate(symlinkPath)
	if !errors.Is(err, dotenv.ErrEnvSymlink) {
		t.Fatalf("expected ErrEnvSymlink, got: %v", err)
	}
}

func TestUpdateVersionSurgical(t *testing.T) {
	tmpDir := t.TempDir()
	envPath := filepath.Join(tmpDir, ".env")
	original := `# Top comment
# Second comment
export YTMDL_VERSION=0.15.0  # inline comment
UNRELATED="value with spaces"
OTHER=123
`
	if err := os.WriteFile(envPath, []byte(original), 0640); err != nil {
		t.Fatalf("WriteFile failed: %v", err)
	}

	if err := dotenv.UpdateVersion(envPath, "0.16.0"); err != nil {
		t.Fatalf("UpdateVersion failed: %v", err)
	}

	// Verify permissions preserved
	fi, err := os.Stat(envPath)
	if err != nil {
		t.Fatalf("Stat failed: %v", err)
	}
	if fi.Mode().Perm() != 0640 {
		t.Errorf("permissions = %o, want 0640", fi.Mode().Perm())
	}

	updated, err := os.ReadFile(envPath)
	if err != nil {
		t.Fatalf("ReadFile failed: %v", err)
	}
	updatedStr := string(updated)

	// Check that YTMDL_VERSION was replaced
	if !strings.Contains(updatedStr, "export YTMDL_VERSION=0.16.0") {
		t.Errorf("expected updated version line, got:\n%s", updatedStr)
	}
	// Check that unrelated contents and comments are preserved
	if !strings.Contains(updatedStr, "# Top comment") || !strings.Contains(updatedStr, `UNRELATED="value with spaces"`) {
		t.Errorf("comments or unrelated keys were lost:\n%s", updatedStr)
	}
}
