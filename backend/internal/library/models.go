package library

import (
	"time"

	"ytdm/backend/internal/music"
)

// HealthStatus categorises a track or file in the library reconciliation.
type HealthStatus string

const (
	StatusHealthy          HealthStatus = "healthy"
	StatusMissingFile      HealthStatus = "missing_file"
	StatusOrphanFile       HealthStatus = "orphan_file"
	StatusInvalidFile      HealthStatus = "invalid_file"
	StatusMetadataMismatch HealthStatus = "metadata_mismatch"
	StatusDuplicateFile    HealthStatus = "duplicate_file"
)

// ScanIssue represents a single discrepancy detected during a library scan.
type ScanIssue struct {
	ID           string       `json:"id"`
	Status       HealthStatus `json:"status"`
	TrackID      string       `json:"track_id,omitempty"`
	TrackTitle   string       `json:"track_title,omitempty"`
	ArtistName   string       `json:"artist_name,omitempty"`
	ReleaseID    string       `json:"release_id,omitempty"`
	ReleaseTitle string       `json:"release_title,omitempty"`
	Path         string       `json:"path"`
	Expected     string       `json:"expected,omitempty"`
	Actual       string       `json:"actual,omitempty"`
	Details      string       `json:"details,omitempty"`
}

// ScanSummary contains aggregate issue counts of a library scan.
type ScanSummary struct {
	TotalFilesScanned  int `json:"total_files_scanned"`
	Healthy            int `json:"healthy"`
	MissingFiles       int `json:"missing_files"`
	OrphanFiles        int `json:"orphan_files"`
	InvalidFiles       int `json:"invalid_files"`
	MetadataMismatches int `json:"metadata_mismatches"`
	DuplicateFiles     int `json:"duplicate_files"`
}

// ScanStatus represents the lifecycle state of a scan.
type ScanStatus string

const (
	ScanRunning   ScanStatus = "running"
	ScanCompleted ScanStatus = "completed"
	ScanFailed    ScanStatus = "failed"
)

// ScanResult is the comprehensive result report of a library reconciliation run.
type ScanResult struct {
	ID           string      `json:"id"`
	Status       ScanStatus  `json:"status"`
	StartedAt    time.Time   `json:"started_at"`
	FinishedAt   *time.Time  `json:"finished_at,omitempty"`
	DurationMS   int64       `json:"duration_ms"`
	FilesScanned int         `json:"files_scanned"`
	Summary      ScanSummary `json:"summary"`
	Issues       []ScanIssue `json:"issues"`
	Warnings     []string    `json:"warnings"`
}

// StorageStats reports aggregate storage metrics and library health.
type StorageStats struct {
	TotalArtists   int                       `json:"total_artists"`
	TotalReleases  int                       `json:"total_releases"`
	TotalTracks    int                       `json:"total_tracks"`
	TotalFiles     int                       `json:"total_files"`
	TotalBytes     int64                     `json:"total_bytes"`
	HealthyCount   int                       `json:"healthy_count"`
	IssueCount     int                       `json:"issue_count"`
	CodecBreakdown map[string]int            `json:"codec_breakdown"`
	LyricsCoverage map[music.LyricsState]int `json:"lyrics_coverage"`
}

// AuditMode specifies whether an audit is a fast existence check or deep probe.
type AuditMode = music.AuditMode

const (
	AuditModeQuick = music.AuditModeQuick
	AuditModeDeep  = music.AuditModeDeep
)

// AuditRunStatus represents the lifecycle state of a library audit run.
type AuditRunStatus = music.AuditRunStatus

const (
	AuditRunRunning   = music.AuditRunRunning
	AuditRunCompleted = music.AuditRunCompleted
	AuditRunFailed    = music.AuditRunFailed
	AuditRunCancelled = music.AuditRunCancelled
)

// Severity indicates the impact level of a detected finding.
type Severity = music.Severity

const (
	SeverityError   = music.SeverityError
	SeverityWarning = music.SeverityWarning
	SeverityInfo    = music.SeverityInfo
)

// FindingCode represents the precise classification of a library discrepancy.
type FindingCode = music.FindingCode

const (
	FindingFileMissing       = music.FindingFileMissing
	FindingFileUntracked     = music.FindingFileUntracked
	FindingLegacyDuplicate   = music.FindingLegacyDuplicate
	FindingFileDuplicate     = music.FindingFileDuplicate
	FindingAudioInvalid      = music.FindingAudioInvalid
	FindingTagMismatch       = music.FindingTagMismatch
	FindingPathMismatch      = music.FindingPathMismatch
	FindingCoverMissing      = music.FindingCoverMissing
	FindingCoverInvalid      = music.FindingCoverInvalid
	FindingLyricsMissing     = music.FindingLyricsMissing
	FindingLyricsOrphaned    = music.FindingLyricsOrphaned
	FindingReleaseIncomplete = music.FindingReleaseIncomplete
)

// EvidenceLevel classifies the certainty of a match or finding without pseudo-percentages.
type EvidenceLevel = music.EvidenceLevel

const (
	EvidenceExactContent   = music.EvidenceExactContent
	EvidenceExactCatalogID = music.EvidenceExactCatalogID
	EvidenceStrongMetadata = music.EvidenceStrongMetadata
	EvidenceWeakMetadata   = music.EvidenceWeakMetadata
	EvidenceUnknown        = music.EvidenceUnknown
)

// RepairAction defines the explicit repair operation that can resolve a finding.
type RepairAction = music.RepairAction

const (
	ActionMoveCanonical  = music.ActionMoveCanonical
	ActionRestoreTags    = music.ActionRestoreTags
	ActionAdoptFile      = music.ActionAdoptFile
	ActionQuarantineFile = music.ActionQuarantineFile
	ActionRelinkFile     = music.ActionRelinkFile
)

// AuditRun represents a persisted library audit execution.
type AuditRun = music.AuditRun

// FindingEvidence holds the structured technical evidence for a finding.
type FindingEvidence = music.FindingEvidence

// AuditFinding is a single persisted finding from an audit run.
type AuditFinding = music.AuditFinding
