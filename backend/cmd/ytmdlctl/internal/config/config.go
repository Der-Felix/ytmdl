// Package config manages local operator settings in <project-dir>/.ytmdl/config.json.
package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// CurrentConfigVersion is the expected config_version schema version.
const CurrentConfigVersion = 1

var (
	// ErrUnsupportedVersion indicates the config file schema version is unhandled.
	ErrUnsupportedVersion = errors.New("unsupported config version")
)

// Config holds persistent operator choices.
type Config struct {
	ConfigVersion int    `json:"config_version"`
	ComposeFile   string `json:"compose_file,omitempty"`
	Engine        string `json:"engine,omitempty"`
	BaseURL       string `json:"base_url,omitempty"`
}

// Load reads and parses <projectDir>/.ytmdl/config.json.
// If the file does not exist, it returns nil, nil.
func Load(projectDir string) (*Config, error) {
	path := filepath.Join(projectDir, ".ytmdl", "config.json")
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("config: failed to read %s: %w", path, err)
	}

	var cfg Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("config: invalid JSON in %s: %w", path, err)
	}

	if cfg.ConfigVersion != CurrentConfigVersion {
		return nil, fmt.Errorf("%w: %d (expected %d)", ErrUnsupportedVersion, cfg.ConfigVersion, CurrentConfigVersion)
	}

	return &cfg, nil
}

// Save atomically persists the configuration to <projectDir>/.ytmdl/config.json.
func (c *Config) Save(projectDir string) error {
	if c.ConfigVersion == 0 {
		c.ConfigVersion = CurrentConfigVersion
	}

	dotDir := filepath.Join(projectDir, ".ytmdl")
	if err := os.MkdirAll(dotDir, 0700); err != nil {
		return fmt.Errorf("config: failed to create directory %s: %w", dotDir, err)
	}
	_ = os.Chmod(dotDir, 0700)

	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return fmt.Errorf("config: failed to marshal JSON: %w", err)
	}
	data = append(data, '\n')

	targetPath := filepath.Join(dotDir, "config.json")
	tmpPath := filepath.Join(dotDir, fmt.Sprintf("config.json.tmp.%d", os.Getpid()))

	tmpFile, err := os.OpenFile(tmpPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0600)
	if err != nil {
		return fmt.Errorf("config: failed to create temp file %s: %w", tmpPath, err)
	}

	if _, err := tmpFile.Write(data); err != nil {
		tmpFile.Close()
		_ = os.Remove(tmpPath)
		return fmt.Errorf("config: failed to write temp file: %w", err)
	}

	if err := tmpFile.Sync(); err != nil {
		tmpFile.Close()
		_ = os.Remove(tmpPath)
		return fmt.Errorf("config: failed to sync temp file: %w", err)
	}

	if err := tmpFile.Close(); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("config: failed to close temp file: %w", err)
	}

	if err := os.Rename(tmpPath, targetPath); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("config: failed to rename %s to %s: %w", tmpPath, targetPath, err)
	}

	return nil
}
