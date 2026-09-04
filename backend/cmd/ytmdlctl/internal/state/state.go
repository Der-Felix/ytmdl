// Package state tracks transactional update lifecycle state in <project-dir>/.ytmdl/update-state.json.
package state

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"ytdm/backend/cmd/ytmdlctl/internal/redact"
)

// CurrentStateVersion is the active schema version for update-state.json.
const CurrentStateVersion = 2

// Status defines valid lifecycle stages of an update operation.
type Status string

const (
	StatusIdle               Status = "idle"
	StatusQuiescing          Status = "quiescing"
	StatusPrepared           Status = "prepared"
	StatusMigrating          Status = "migrating"
	StatusMutating           Status = "mutating" // backward-compatible alias for v1
	StatusVerifying          Status = "verifying"
	StatusSuccess            Status = "success"
	StatusRollbackInProgress Status = "rollback_in_progress"
	StatusRolledBack         Status = "rolled_back"
	StatusRecoveryRequired   Status = "recovery_required"
	StatusRecoveryInProgress Status = "recovery_in_progress"
	StatusRecovered          Status = "recovered"
)

var (
	// ErrUnknownStatus is returned when update-state.json has an unhandled status string.
	ErrUnknownStatus = errors.New("unknown update state status")
	// ErrUnsupportedVersion is returned when state_version is unsupported.
	ErrUnsupportedVersion = errors.New("unsupported state version")
)

// State holds transactional metadata about update and rollback operations.
type State struct {
	StateVersion             int       `json:"state_version"`
	OperationID              string    `json:"operation_id"`
	Status                   Status    `json:"status"`
	StartedAt                time.Time `json:"started_at"`
	UpdatedAt                time.Time `json:"updated_at"`
	CurrentVersion           string    `json:"current_version"`
	TargetVersion            string    `json:"target_version"`
	ComposeFile              string    `json:"compose_file"`
	Engine                   string    `json:"engine"`
	BaseURL                  string    `json:"base_url,omitempty"`
	SchemaBefore             int       `json:"schema_before"`
	TargetSchema             int       `json:"target_schema,omitempty"`
	UpdateClassification     string    `json:"update_classification,omitempty"`
	RollbackClassification   string    `json:"rollback_classification,omitempty"`
	SupportedSourceSchemas   []int     `json:"supported_source_schemas,omitempty"`
	BackupPath               string    `json:"backup_path,omitempty"`
	RecoverySafetyBackupPath string    `json:"recovery_safety_backup_path,omitempty"`
	QuarantineDBName         string    `json:"quarantine_db_name,omitempty"`
	RestoredDBName           string    `json:"restored_db_name,omitempty"`
	PreviousBackendImage     string    `json:"previous_backend_image,omitempty"`
	PreviousBackendImageID   string    `json:"previous_backend_image_id,omitempty"`
	PreviousBackendDigest    string    `json:"previous_backend_digest,omitempty"`
	PreviousBackendDigests   []string  `json:"previous_backend_digests,omitempty"`
	PreviousFrontendImage    string    `json:"previous_frontend_image,omitempty"`
	PreviousFrontendImageID  string    `json:"previous_frontend_image_id,omitempty"`
	PreviousFrontendDigest   string    `json:"previous_frontend_digest,omitempty"`
	PreviousFrontendDigests  []string  `json:"previous_frontend_digests,omitempty"`
	TargetBackendImage       string    `json:"target_backend_image,omitempty"`
	TargetBackendDigest      string    `json:"target_backend_digest,omitempty"`
	TargetFrontendImage      string    `json:"target_frontend_image,omitempty"`
	TargetFrontendDigest     string    `json:"target_frontend_digest,omitempty"`
	LastError                string    `json:"last_error,omitempty"`
}

// ErrInvalidTransition is returned when an illegal status change is attempted.
var ErrInvalidTransition = errors.New("invalid state transition")

// CanTransition returns whether changing from status 'from' to 'to' is legally allowed.
func CanTransition(from, to Status) bool {
	switch from {
	case "", StatusIdle, StatusRolledBack, StatusRecovered:
		return to == StatusQuiescing || to == StatusPrepared
	case StatusSuccess:
		return to == StatusQuiescing || to == StatusPrepared || to == StatusRollbackInProgress
	case StatusQuiescing:
		return to == StatusPrepared || to == StatusRollbackInProgress || to == StatusRolledBack
	case StatusPrepared:
		return to == StatusMigrating || to == StatusMutating || to == StatusRolledBack || to == StatusRollbackInProgress
	case StatusMigrating, StatusMutating:
		return to == StatusVerifying || to == StatusRollbackInProgress || to == StatusRecoveryRequired
	case StatusVerifying:
		return to == StatusSuccess || to == StatusRollbackInProgress || to == StatusRecoveryRequired
	case StatusRollbackInProgress:
		return to == StatusRolledBack || to == StatusRecoveryRequired
	case StatusRecoveryRequired:
		return to == StatusRecoveryInProgress || to == StatusRollbackInProgress
	case StatusRecoveryInProgress:
		return to == StatusSuccess || to == StatusRecovered || to == StatusRecoveryRequired
	default:
		return false
	}
}

// TransitionTo validates and sets the new status on s.
func (s *State) TransitionTo(to Status) error {
	if !CanTransition(s.Status, to) {
		return fmt.Errorf("%w: cannot transition from %q to %q", ErrInvalidTransition, s.Status, to)
	}
	s.Status = to
	return nil
}

// IsValidStatus returns whether s is a recognized status.
func IsValidStatus(s Status) bool {
	switch s {
	case StatusIdle, StatusQuiescing, StatusPrepared, StatusMigrating, StatusMutating, StatusVerifying,
		StatusSuccess, StatusRollbackInProgress, StatusRolledBack, StatusRecoveryRequired,
		StatusRecoveryInProgress, StatusRecovered:
		return true
	default:
		return false
	}
}

// IsInterrupted returns true if the transaction was interrupted mid-flight.
func (s *State) IsInterrupted() bool {
	if s == nil {
		return false
	}
	switch s.Status {
	case StatusQuiescing, StatusPrepared, StatusMigrating, StatusMutating, StatusVerifying,
		StatusRollbackInProgress, StatusRecoveryInProgress:
		return true
	default:
		return false
	}
}

// Load reads and validates <projectDir>/.ytmdl/update-state.json.
// If the file does not exist, it returns nil, nil.
func Load(projectDir string) (*State, error) {
	path := filepath.Join(projectDir, ".ytmdl", "update-state.json")
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("state: failed to read %s: %w", path, err)
	}

	var st State
	if err := json.Unmarshal(data, &st); err != nil {
		return nil, fmt.Errorf("state: invalid JSON in %s: %w", path, err)
	}

	if st.StateVersion != 1 && st.StateVersion != CurrentStateVersion {
		return nil, fmt.Errorf("%w: %d (expected 1 or %d)", ErrUnsupportedVersion, st.StateVersion, CurrentStateVersion)
	}

	if !IsValidStatus(st.Status) {
		return nil, fmt.Errorf("%w: %q", ErrUnknownStatus, st.Status)
	}

	return &st, nil
}

// Save atomically writes the state record to <projectDir>/.ytmdl/update-state.json.
func (s *State) Save(projectDir string) error {
	if s.StateVersion == 0 {
		s.StateVersion = CurrentStateVersion
	}
	s.UpdatedAt = time.Now().UTC()

	// Redact sensitive strings before persistence
	if s.LastError != "" {
		s.LastError = redact.String(s.LastError)
	}

	dotDir := filepath.Join(projectDir, ".ytmdl")
	if err := os.MkdirAll(dotDir, 0700); err != nil {
		return fmt.Errorf("state: failed to create directory %s: %w", dotDir, err)
	}
	_ = os.Chmod(dotDir, 0700)

	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return fmt.Errorf("state: failed to marshal JSON: %w", err)
	}
	data = append(data, '\n')

	targetPath := filepath.Join(dotDir, "update-state.json")
	tmpPath := filepath.Join(dotDir, fmt.Sprintf("update-state.json.tmp.%d", os.Getpid()))

	tmpFile, err := os.OpenFile(tmpPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0600)
	if err != nil {
		return fmt.Errorf("state: failed to create temp file %s: %w", tmpPath, err)
	}

	if _, err := tmpFile.Write(data); err != nil {
		tmpFile.Close()
		_ = os.Remove(tmpPath)
		return fmt.Errorf("state: failed to write temp file: %w", err)
	}

	if err := tmpFile.Sync(); err != nil {
		tmpFile.Close()
		_ = os.Remove(tmpPath)
		return fmt.Errorf("state: failed to sync temp file: %w", err)
	}

	if err := tmpFile.Close(); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("state: failed to close temp file: %w", err)
	}

	if err := os.Rename(tmpPath, targetPath); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("state: failed to rename %s to %s: %w", tmpPath, targetPath, err)
	}

	return nil
}
