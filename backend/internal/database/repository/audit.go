package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"ytdm/backend/internal/apperr"
	"ytdm/backend/internal/database"
	"ytdm/backend/internal/music"
)

// Audit persists library audit runs and their detected findings.
type Audit struct {
	db *database.DB
}

// NewAudit returns a new Audit repository instance.
func NewAudit(db *database.DB) *Audit {
	return &Audit{db: db}
}

// ListFindingsOptions holds query filters and pagination for finding listings.
type ListFindingsOptions struct {
	Severity    string
	FindingCode string
	ArtistID    string
	ReleaseID   string
	TrackID     string
	Limit       int
	Offset      int
}

const auditRunColumns = `id, mode, status, started_at, finished_at, scanned, total, findings_count, error_summary, created_by, created_at`

func scanAuditRun(scan func(dest ...any) error) (music.AuditRun, error) {
	var (
		r            music.AuditRun
		mode         string
		status       string
		finishedAt   sql.NullTime
		errorSummary sql.NullString
		createdBy    sql.NullString
	)
	err := scan(
		&r.ID,
		&mode,
		&status,
		&r.StartedAt,
		&finishedAt,
		&r.Scanned,
		&r.Total,
		&r.FindingsCount,
		&errorSummary,
		&createdBy,
		&r.CreatedAt,
	)
	if err != nil {
		return music.AuditRun{}, err
	}
	r.Mode = music.AuditMode(mode)
	r.Status = music.AuditRunStatus(status)
	if finishedAt.Valid {
		r.FinishedAt = &finishedAt.Time
		r.DurationMS = finishedAt.Time.Sub(r.StartedAt).Milliseconds()
	}
	if errorSummary.Valid {
		r.ErrorSummary = errorSummary.String
	}
	if createdBy.Valid {
		r.CreatedBy = &createdBy.String
	}
	return r, nil
}

// CreateRun inserts a new library audit run record.
func (a *Audit) CreateRun(ctx context.Context, run music.AuditRun) error {
	var createdBy sql.NullString
	if run.CreatedBy != nil && *run.CreatedBy != "" {
		createdBy = sql.NullString{String: *run.CreatedBy, Valid: true}
	}

	_, err := a.db.ExecContext(ctx, `
		INSERT INTO library_audit_runs (
			id, mode, status, started_at, scanned, total, findings_count, created_by, created_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
	`, run.ID, string(run.Mode), string(run.Status), run.StartedAt, run.Scanned, run.Total, run.FindingsCount, createdBy, run.CreatedAt)

	if err != nil {
		return apperr.Wrap(apperr.CodeInternal, "Failed to create audit run.", err)
	}
	return nil
}

// UpdateRunProgress updates the progress counters of an active audit run.
func (a *Audit) UpdateRunProgress(ctx context.Context, id string, scanned, total, findingsCount int) error {
	_, err := a.db.ExecContext(ctx, `
		UPDATE library_audit_runs
		SET scanned = $2, total = $3, findings_count = $4
		WHERE id = $1 AND status = 'running'
	`, id, scanned, total, findingsCount)

	if err != nil {
		return apperr.Wrap(apperr.CodeInternal, "Failed to update audit run progress.", err)
	}
	return nil
}

// CompleteRun transitions an audit run to a terminal state (completed, failed, cancelled).
func (a *Audit) CompleteRun(ctx context.Context, id string, status music.AuditRunStatus, scanned, total, findingsCount int, errorSummary string) error {
	now := time.Now().UTC()
	var errSum sql.NullString
	if errorSummary != "" {
		errSum = sql.NullString{String: errorSummary, Valid: true}
	}

	_, err := a.db.ExecContext(ctx, `
		UPDATE library_audit_runs
		SET status = $2, finished_at = $3, scanned = $4, total = $5, findings_count = $6, error_summary = $7
		WHERE id = $1
	`, id, string(status), now, scanned, total, findingsCount, errSum)

	if err != nil {
		return apperr.Wrap(apperr.CodeInternal, "Failed to complete audit run.", err)
	}
	return nil
}

// GetRun returns an audit run by ID.
func (a *Audit) GetRun(ctx context.Context, id string) (*music.AuditRun, error) {
	row := a.db.QueryRowContext(ctx, `
		SELECT `+auditRunColumns+`
		FROM library_audit_runs
		WHERE id = $1
	`, id)

	r, err := scanAuditRun(row.Scan)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, apperr.Wrap(apperr.CodeInternal, "Failed to fetch audit run.", err)
	}
	return &r, nil
}

// GetActiveRun returns the currently running audit run, if one exists.
func (a *Audit) GetActiveRun(ctx context.Context) (*music.AuditRun, error) {
	row := a.db.QueryRowContext(ctx, `
		SELECT `+auditRunColumns+`
		FROM library_audit_runs
		WHERE status = 'running'
		ORDER BY started_at DESC
		LIMIT 1
	`)

	r, err := scanAuditRun(row.Scan)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, apperr.Wrap(apperr.CodeInternal, "Failed to fetch active audit run.", err)
	}
	return &r, nil
}

// GetLatestRun returns the most recently started audit run.
func (a *Audit) GetLatestRun(ctx context.Context) (*music.AuditRun, error) {
	row := a.db.QueryRowContext(ctx, `
		SELECT `+auditRunColumns+`
		FROM library_audit_runs
		ORDER BY started_at DESC
		LIMIT 1
	`)

	r, err := scanAuditRun(row.Scan)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, apperr.Wrap(apperr.CodeInternal, "Failed to fetch latest audit run.", err)
	}
	return &r, nil
}

// RecoverRunningRuns marks any stale 'running' audit runs as failed upon backend startup.
func (a *Audit) RecoverRunningRuns(ctx context.Context) (int64, error) {
	now := time.Now().UTC()
	res, err := a.db.ExecContext(ctx, `
		UPDATE library_audit_runs
		SET status = 'failed', finished_at = $1, error_summary = 'Audit was interrupted by a server restart.'
		WHERE status = 'running'
	`, now)
	if err != nil {
		return 0, apperr.Wrap(apperr.CodeInternal, "Failed to recover running audit runs.", err)
	}
	return res.RowsAffected()
}

// RecoverInterruptedRuns is an alias for RecoverRunningRuns.
func (a *Audit) RecoverInterruptedRuns(ctx context.Context) (int64, error) {
	return a.RecoverRunningRuns(ctx)
}

// ListRuns returns a paginated list of audit runs ordered by started_at DESC.
func (a *Audit) ListRuns(ctx context.Context, limit, offset int) ([]music.AuditRun, int, error) {
	if limit <= 0 {
		limit = 20
	}
	if limit > 100 {
		limit = 100
	}
	if offset < 0 {
		offset = 0
	}

	var total int
	if err := a.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM library_audit_runs`).Scan(&total); err != nil {
		return nil, 0, apperr.Wrap(apperr.CodeInternal, "Failed to count audit runs.", err)
	}

	rows, err := a.db.QueryContext(ctx, `
		SELECT `+auditRunColumns+`
		FROM library_audit_runs
		ORDER BY started_at DESC, id DESC
		LIMIT $1 OFFSET $2
	`, limit, offset)
	if err != nil {
		return nil, 0, apperr.Wrap(apperr.CodeInternal, "Failed to list audit runs.", err)
	}
	defer rows.Close()

	var runs []music.AuditRun
	for rows.Next() {
		r, err := scanAuditRun(rows.Scan)
		if err != nil {
			return nil, 0, apperr.Wrap(apperr.CodeInternal, "Failed to scan audit run.", err)
		}
		runs = append(runs, r)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, apperr.Wrap(apperr.CodeInternal, "Error iterating audit runs.", err)
	}
	return runs, total, nil
}

// DeleteRun deletes an audit run and cascades to its findings.
func (a *Audit) DeleteRun(ctx context.Context, id string) error {
	_, err := a.db.ExecContext(ctx, `DELETE FROM library_audit_runs WHERE id = $1`, id)
	if err != nil {
		return apperr.Wrap(apperr.CodeInternal, "Failed to delete audit run.", err)
	}
	return nil
}

// InsertFindings batch-inserts findings in chunks.
func (a *Audit) InsertFindings(ctx context.Context, findings []music.AuditFinding) error {
	if len(findings) == 0 {
		return nil
	}

	const batchSize = 200
	for i := 0; i < len(findings); i += batchSize {
		end := i + batchSize
		if end > len(findings) {
			end = len(findings)
		}
		chunk := findings[i:end]

		valueStrings := make([]string, 0, len(chunk))
		valueArgs := make([]any, 0, len(chunk)*9)

		for idx, f := range chunk {
			base := idx * 9
			valueStrings = append(valueStrings, fmt.Sprintf("($%d, $%d, $%d, $%d, $%d, $%d, $%d, $%d, $%d)",
				base+1, base+2, base+3, base+4, base+5, base+6, base+7, base+8, base+9))

			evidenceBytes, _ := json.Marshal(f.Evidence)

			var artistID, releaseID, trackID sql.NullString
			if f.ArtistID != "" {
				artistID = sql.NullString{String: f.ArtistID, Valid: true}
			}
			if f.ReleaseID != "" {
				releaseID = sql.NullString{String: f.ReleaseID, Valid: true}
			}
			if f.TrackID != "" {
				trackID = sql.NullString{String: f.TrackID, Valid: true}
			}

			valueArgs = append(valueArgs, f.ID, f.RunID, string(f.FindingCode), string(f.Severity),
				f.RelativePath, artistID, releaseID, trackID, string(evidenceBytes))
		}

		query := fmt.Sprintf(`
			INSERT INTO library_audit_findings (
				id, run_id, finding_code, severity, relative_path, artist_id, release_id, track_id, evidence
			) VALUES %s
		`, strings.Join(valueStrings, ", "))

		if _, err := a.db.ExecContext(ctx, query, valueArgs...); err != nil {
			return apperr.Wrap(apperr.CodeInternal, "Failed to insert audit findings batch.", err)
		}
	}
	return nil
}

// ListFindings queries findings for a specific run with optional filters and stable pagination.
func (a *Audit) ListFindings(ctx context.Context, runID string, opts ListFindingsOptions) ([]music.AuditFinding, int, error) {
	var (
		whereClauses = []string{"f.run_id = $1"}
		args         = []any{runID}
		argIdx       = 2
	)

	if opts.Severity != "" {
		whereClauses = append(whereClauses, fmt.Sprintf("f.severity = $%d", argIdx))
		args = append(args, opts.Severity)
		argIdx++
	}
	if opts.FindingCode != "" {
		whereClauses = append(whereClauses, fmt.Sprintf("f.finding_code = $%d", argIdx))
		args = append(args, opts.FindingCode)
		argIdx++
	}
	if opts.ArtistID != "" {
		whereClauses = append(whereClauses, fmt.Sprintf("f.artist_id = $%d", argIdx))
		args = append(args, opts.ArtistID)
		argIdx++
	}
	if opts.ReleaseID != "" {
		whereClauses = append(whereClauses, fmt.Sprintf("f.release_id = $%d", argIdx))
		args = append(args, opts.ReleaseID)
		argIdx++
	}
	if opts.TrackID != "" {
		whereClauses = append(whereClauses, fmt.Sprintf("f.track_id = $%d", argIdx))
		args = append(args, opts.TrackID)
		argIdx++
	}

	whereSQL := strings.Join(whereClauses, " AND ")

	var total int
	countQuery := fmt.Sprintf(`SELECT COUNT(*) FROM library_audit_findings f WHERE %s`, whereSQL)
	if err := a.db.QueryRowContext(ctx, countQuery, args...).Scan(&total); err != nil {
		return nil, 0, apperr.Wrap(apperr.CodeInternal, "Failed to count audit findings.", err)
	}

	limit := opts.Limit
	if limit <= 0 {
		limit = 50
	}
	if limit > 500 {
		limit = 500
	}
	offset := opts.Offset
	if offset < 0 {
		offset = 0
	}

	selectQuery := fmt.Sprintf(`
		SELECT
			f.id, f.run_id, f.finding_code, f.severity, f.relative_path,
			f.artist_id, f.release_id, f.track_id, f.evidence, f.created_at,
			COALESCE(a.name, '') AS artist_name,
			COALESCE(r.title, '') AS release_title,
			COALESCE(t.title, '') AS track_title
		FROM library_audit_findings f
		LEFT JOIN artists a ON f.artist_id = a.id
		LEFT JOIN releases r ON f.release_id = r.id
		LEFT JOIN tracks t ON f.track_id = t.id
		WHERE %s
		ORDER BY
			CASE f.severity
				WHEN 'error' THEN 1
				WHEN 'warning' THEN 2
				WHEN 'info' THEN 3
				ELSE 4
			END ASC,
			f.relative_path ASC,
			f.id ASC
		LIMIT $%d OFFSET $%d
	`, whereSQL, argIdx, argIdx+1)

	args = append(args, limit, offset)

	rows, err := a.db.QueryContext(ctx, selectQuery, args...)
	if err != nil {
		return nil, 0, apperr.Wrap(apperr.CodeInternal, "Failed to list audit findings.", err)
	}
	defer rows.Close()

	var findings []music.AuditFinding
	for rows.Next() {
		var (
			f            music.AuditFinding
			code         string
			sev          string
			artistID     sql.NullString
			releaseID    sql.NullString
			trackID      sql.NullString
			evidenceJSON []byte
		)

		err := rows.Scan(
			&f.ID,
			&f.RunID,
			&code,
			&sev,
			&f.RelativePath,
			&artistID,
			&releaseID,
			&trackID,
			&evidenceJSON,
			&f.CreatedAt,
			&f.ArtistName,
			&f.ReleaseTitle,
			&f.TrackTitle,
		)
		if err != nil {
			return nil, 0, apperr.Wrap(apperr.CodeInternal, "Failed to scan audit finding.", err)
		}

		f.FindingCode = music.FindingCode(code)
		f.Severity = music.Severity(sev)
		if artistID.Valid {
			f.ArtistID = artistID.String
		}
		if releaseID.Valid {
			f.ReleaseID = releaseID.String
		}
		if trackID.Valid {
			f.TrackID = trackID.String
		}
		if len(evidenceJSON) > 0 {
			_ = json.Unmarshal(evidenceJSON, &f.Evidence)
		}

		// Attach SuggestedAction based on code and evidence
		attachSuggestedAction(&f)

		findings = append(findings, f)
	}

	if err := rows.Err(); err != nil {
		return nil, 0, apperr.Wrap(apperr.CodeInternal, "Error iterating audit findings.", err)
	}

	return findings, total, nil
}

func attachSuggestedAction(f *music.AuditFinding) {
	switch f.FindingCode {
	case music.FindingPathMismatch:
		action := music.ActionMoveCanonical
		f.SuggestedAction = &action
	case music.FindingTagMismatch:
		action := music.ActionRestoreTags
		f.SuggestedAction = &action
	case music.FindingFileUntracked:
		if f.Evidence.Level == music.EvidenceExactCatalogID || f.Evidence.Level == music.EvidenceExactContent {
			action := music.ActionAdoptFile
			f.SuggestedAction = &action
		}
	case music.FindingLegacyDuplicate:
		action := music.ActionQuarantineFile
		f.SuggestedAction = &action
	}
}

// GetFinding retrieves a single finding by ID.
func (a *Audit) GetFinding(ctx context.Context, id string) (*music.AuditFinding, error) {
	row := a.db.QueryRowContext(ctx, `
		SELECT
			f.id, f.run_id, f.finding_code, f.severity, f.relative_path,
			f.artist_id, f.release_id, f.track_id, f.evidence, f.created_at,
			COALESCE(a.name, '') AS artist_name,
			COALESCE(r.title, '') AS release_title,
			COALESCE(t.title, '') AS track_title
		FROM library_audit_findings f
		LEFT JOIN artists a ON f.artist_id = a.id
		LEFT JOIN releases r ON f.release_id = r.id
		LEFT JOIN tracks t ON f.track_id = t.id
		WHERE f.id = $1
	`, id)

	var (
		f            music.AuditFinding
		code         string
		sev          string
		artistID     sql.NullString
		releaseID    sql.NullString
		trackID      sql.NullString
		evidenceJSON []byte
	)

	err := row.Scan(
		&f.ID,
		&f.RunID,
		&code,
		&sev,
		&f.RelativePath,
		&artistID,
		&releaseID,
		&trackID,
		&evidenceJSON,
		&f.CreatedAt,
		&f.ArtistName,
		&f.ReleaseTitle,
		&f.TrackTitle,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, apperr.Wrap(apperr.CodeInternal, "Failed to fetch audit finding.", err)
	}

	f.FindingCode = music.FindingCode(code)
	f.Severity = music.Severity(sev)
	if artistID.Valid {
		f.ArtistID = artistID.String
	}
	if releaseID.Valid {
		f.ReleaseID = releaseID.String
	}
	if trackID.Valid {
		f.TrackID = trackID.String
	}
	if len(evidenceJSON) > 0 {
		_ = json.Unmarshal(evidenceJSON, &f.Evidence)
	}

	attachSuggestedAction(&f)
	return &f, nil
}

// DeleteFinding removes a specific finding record by ID.
func (a *Audit) DeleteFinding(ctx context.Context, id string) error {
	_, err := a.db.ExecContext(ctx, `DELETE FROM library_audit_findings WHERE id = $1`, id)
	if err != nil {
		return apperr.Wrap(apperr.CodeInternal, "Failed to delete audit finding.", err)
	}
	return nil
}

// UpdateFindingEvidence updates the structured evidence payload of a finding.
func (a *Audit) UpdateFindingEvidence(ctx context.Context, id string, evidence music.FindingEvidence) error {
	evidenceJSON, err := json.Marshal(evidence)
	if err != nil {
		return apperr.Wrap(apperr.CodeInternal, "Failed to marshal finding evidence.", err)
	}
	_, err = a.db.ExecContext(ctx, `UPDATE library_audit_findings SET evidence = $2 WHERE id = $1`, id, evidenceJSON)
	if err != nil {
		return apperr.Wrap(apperr.CodeInternal, "Failed to update finding evidence.", err)
	}
	return nil
}
