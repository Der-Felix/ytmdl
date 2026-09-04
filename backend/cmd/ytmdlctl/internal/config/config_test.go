package config_test

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"ytdm/backend/cmd/ytmdlctl/internal/config"
)

func TestLoadNonExistentConfig(t *testing.T) {
	tmpDir := t.TempDir()

	cfg, err := config.Load(tmpDir)
	if err != nil {
		t.Fatalf("Load on empty dir failed: %v", err)
	}
	if cfg != nil {
		t.Errorf("expected nil config for non-existent file, got %+v", cfg)
	}
}

func TestSaveAndLoadValidConfig(t *testing.T) {
	tmpDir := t.TempDir()

	orig := &config.Config{
		ConfigVersion: 1,
		ComposeFile:   "compose.ghcr.yaml",
		Engine:        "podman",
		BaseURL:       "http://127.0.0.1:8080",
	}

	if err := orig.Save(tmpDir); err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	// Verify permissions
	dotDir := filepath.Join(tmpDir, ".ytmdl")
	dInfo, err := os.Stat(dotDir)
	if err != nil {
		t.Fatalf("stat %s failed: %v", dotDir, err)
	}
	if dInfo.Mode().Perm() != 0700 {
		t.Errorf(".ytmdl perm = %o, want 0700", dInfo.Mode().Perm())
	}

	cfgPath := filepath.Join(dotDir, "config.json")
	fInfo, err := os.Stat(cfgPath)
	if err != nil {
		t.Fatalf("stat %s failed: %v", cfgPath, err)
	}
	if fInfo.Mode().Perm() != 0600 {
		t.Errorf("config.json perm = %o, want 0600", fInfo.Mode().Perm())
	}

	// Load back
	loaded, err := config.Load(tmpDir)
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}
	if loaded == nil {
		t.Fatal("expected non-nil config")
	}

	if loaded.ConfigVersion != 1 {
		t.Errorf("ConfigVersion = %d, want 1", loaded.ConfigVersion)
	}
	if loaded.ComposeFile != "compose.ghcr.yaml" {
		t.Errorf("ComposeFile = %q, want compose.ghcr.yaml", loaded.ComposeFile)
	}
	if loaded.Engine != "podman" {
		t.Errorf("Engine = %q, want podman", loaded.Engine)
	}
	if loaded.BaseURL != "http://127.0.0.1:8080" {
		t.Errorf("BaseURL = %q, want http://127.0.0.1:8080", loaded.BaseURL)
	}
}

func TestLoadMalformedConfig(t *testing.T) {
	tmpDir := t.TempDir()
	dotDir := filepath.Join(tmpDir, ".ytmdl")
	_ = os.MkdirAll(dotDir, 0700)
	_ = os.WriteFile(filepath.Join(dotDir, "config.json"), []byte("{broken json:"), 0600)

	_, err := config.Load(tmpDir)
	if err == nil {
		t.Fatal("expected error on malformed json, got nil")
	}
}

func TestLoadUnsupportedConfigVersion(t *testing.T) {
	tmpDir := t.TempDir()
	dotDir := filepath.Join(tmpDir, ".ytmdl")
	_ = os.MkdirAll(dotDir, 0700)
	_ = os.WriteFile(filepath.Join(dotDir, "config.json"), []byte(`{"config_version": 999}`), 0600)

	_, err := config.Load(tmpDir)
	if !errors.Is(err, config.ErrUnsupportedVersion) {
		t.Fatalf("got %v, want ErrUnsupportedVersion", err)
	}
}
