package music

import (
	"time"
)

// AuditMode specifies whether an audit is a fast existence check or deep probe.
type AuditMode string

const (
	AuditModeQuick AuditMode = "quick"
	AuditModeDeep  AuditMode = "deep"
)

// AuditRunStatus represents the lifecycle state of a library audit run.
type AuditRunStatus string

const (
	AuditRunRunning   AuditRunStatus = "running"
	AuditRunCompleted AuditRunStatus = "completed"
	AuditRunFailed    AuditRunStatus = "failed"
	AuditRunCancelled AuditRunStatus = "cancelled"
)

// Severity indicates the impact level of a detected finding.
type Severity string

const (
	SeverityError   Severity = "error"
	SeverityWarning Severity = "warning"
	SeverityInfo    Severity = "info"
)

// FindingCode represents the precise classification of a library discrepancy.
type FindingCode string

const (
	FindingFileMissing       FindingCode = "FILE_MISSING"
	FindingFileUntracked     FindingCode = "FILE_UNTRACKED"
	FindingLegacyDuplicate   FindingCode = "LEGACY_DUPLICATE"
	FindingFileDuplicate     FindingCode = "FILE_DUPLICATE"
	FindingAudioInvalid      FindingCode = "AUDIO_INVALID"
	FindingTagMismatch       FindingCode = "TAG_MISMATCH"
	FindingPathMismatch      FindingCode = "PATH_MISMATCH"
	FindingCoverMissing      FindingCode = "COVER_MISSING"
	FindingCoverInvalid      FindingCode = "COVER_INVALID"
	FindingLyricsMissing     FindingCode = "LYRICS_MISSING"
	FindingLyricsOrphaned    FindingCode = "LYRICS_ORPHANED"
	FindingReleaseIncomplete FindingCode = "RELEASE_INCOMPLETE"
)

// EvidenceLevel classifies the certainty of a match or finding without pseudo-percentages.
type EvidenceLevel string

const (
	EvidenceExactContent   EvidenceLevel = "EXACT_CONTENT"
	EvidenceExactCatalogID EvidenceLevel = "EXACT_CATALOG_ID"
	EvidenceStrongMetadata EvidenceLevel = "STRONG_METADATA"
	EvidenceWeakMetadata   EvidenceLevel = "WEAK_METADATA"
	EvidenceUnknown        EvidenceLevel = "UNKNOWN"
)

// RepairAction defines the explicit repair operation that can resolve a finding.
type RepairAction string

const (
	ActionMoveCanonical  RepairAction = "MOVE_CANONICAL"
	ActionRestoreTags    RepairAction = "RESTORE_TAGS"
	ActionAdoptFile      RepairAction = "ADOPT_FILE"
	ActionQuarantineFile RepairAction = "QUARANTINE_FILE"
	ActionRelinkFile     RepairAction = "RELINK_FILE"
)

// AuditRun represents a persisted library audit execution.
type AuditRun struct {
	ID            string         `json:"id"`
	Mode          AuditMode      `json:"mode"`
	Status        AuditRunStatus `json:"status"`
	StartedAt     time.Time      `json:"started_at"`
	FinishedAt    *time.Time     `json:"finished_at,omitempty"`
	DurationMS    int64          `json:"duration_ms"`
	Scanned       int            `json:"scanned"`
	Total         int            `json:"total"`
	FindingsCount int            `json:"findings_count"`
	ErrorSummary  string         `json:"error_summary,omitempty"`
	CreatedBy     *string        `json:"created_by,omitempty"`
	CreatedAt     time.Time      `json:"created_at"`
}

// FindingEvidence holds the structured technical evidence for a finding.
type FindingEvidence struct {
	Level           EvidenceLevel `json:"level,omitempty"`
	ExpectedPath    string        `json:"expected_path,omitempty"`
	ActualPath      string        `json:"actual_path,omitempty"`
	ExpectedTag     string        `json:"expected_tag,omitempty"`
	ActualTag       string        `json:"actual_tag,omitempty"`
	MismatchedTags  []string      `json:"mismatched_tags,omitempty"`
	SizeBytes       int64         `json:"size_bytes,omitempty"`
	DurationMS      int64         `json:"duration_ms,omitempty"`
	Codec           string        `json:"codec,omitempty"`
	SourceProvider  string        `json:"source_provider,omitempty"`
	SourceID        string        `json:"source_id,omitempty"`
	ISRC            string        `json:"isrc,omitempty"`
	SHA256          string        `json:"sha256,omitempty"`
	CanonicalPath   string        `json:"canonical_path,omitempty"`
	CanonicalFileID string        `json:"canonical_file_id,omitempty"`
	Details         string        `json:"details,omitempty"`
}

// AuditFinding is a single persisted finding from an audit run.
type AuditFinding struct {
	ID              string          `json:"id"`
	RunID           string          `json:"run_id"`
	FindingCode     FindingCode     `json:"finding_code"`
	Severity        Severity        `json:"severity"`
	RelativePath    string          `json:"relative_path"`
	ArtistID        string          `json:"artist_id,omitempty"`
	ReleaseID       string          `json:"release_id,omitempty"`
	TrackID         string          `json:"track_id,omitempty"`
	ArtistName      string          `json:"artist_name,omitempty"`
	ReleaseTitle    string          `json:"release_title,omitempty"`
	TrackTitle      string          `json:"track_title,omitempty"`
	SuggestedAction *RepairAction   `json:"suggested_action,omitempty"`
	Evidence        FindingEvidence `json:"evidence"`
	CreatedAt       time.Time       `json:"created_at"`
}
