package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"time"

	"ytdm/backend/internal/apperr"
	"ytdm/backend/internal/database"
	"ytdm/backend/internal/jobs"
	"ytdm/backend/internal/music"
)

// Jobs persists jobs and their items.
type Jobs struct {
	db *database.DB
}

// NewJobs returns a job repository.
func NewJobs(db *database.DB) *Jobs { return &Jobs{db: db} }

const jobColumns = `id, type, status, label, metadata_provider, media_provider, target_id,
	options_json, total, completed, failed, skipped, error_code, error_message,
	created_at, updated_at, started_at, finished_at, priority, paused`

func scanJob(scan func(dest ...any) error) (jobs.Job, error) {
	var (
		j            jobs.Job
		jobType      string
		status       string
		optionsJSON  string
		startedAt    sql.NullTime
		finishedAt   sql.NullTime
		priorityRank int
		paused       bool
	)
	err := scan(&j.ID, &jobType, &status, &j.Label, &j.MetadataProvider, &j.MediaProvider,
		&j.TargetID, &optionsJSON, &j.Total, &j.Completed, &j.Failed, &j.Skipped,
		&j.ErrorCode, &j.ErrorMessage, &j.CreatedAt, &j.UpdatedAt, &startedAt, &finishedAt,
		&priorityRank, &paused)
	if err != nil {
		return jobs.Job{}, err
	}
	j.Type = jobs.Type(jobType)
	j.Status = jobs.Status(status)
	j.Priority = jobs.PriorityFromRank(priorityRank)
	j.Paused = paused
	if err := json.Unmarshal([]byte(optionsJSON), &j.Options); err != nil {
		j.Options = jobs.DefaultOptions()
	}
	j.CreatedAt = j.CreatedAt.UTC()
	j.UpdatedAt = j.UpdatedAt.UTC()
	j.StartedAt = timePtr(startedAt)
	j.FinishedAt = timePtr(finishedAt)
	return j, nil
}

// Create stores a new job.
func (r *Jobs) Create(ctx context.Context, job *jobs.Job) error {
	if job.ID == "" {
		job.ID = music.NewID()
	}
	if job.Priority == "" {
		job.Priority = jobs.PriorityNormal
	}
	now := time.Now().UTC()
	job.CreatedAt, job.UpdatedAt = now, now

	options, err := json.Marshal(job.Options)
	if err != nil {
		return apperr.Wrap(apperr.CodeInternal, "The job options could not be encoded.", err)
	}

	_, err = r.db.ExecContext(ctx, `
		INSERT INTO jobs (id, type, status, label, metadata_provider, media_provider, target_id,
			options_json, total, completed, failed, skipped, error_code, error_message,
			created_at, updated_at, started_at, finished_at, priority, paused)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $15, $16, $17, $18, $19)`,
		job.ID, string(job.Type), string(job.Status), job.Label, job.MetadataProvider,
		job.MediaProvider, job.TargetID, string(options), job.Total, job.Completed,
		job.Failed, job.Skipped, job.ErrorCode, job.ErrorMessage,
		now, nullTime(job.StartedAt), nullTime(job.FinishedAt),
		job.Priority.Rank(), job.Paused)
	if err != nil {
		return wrapDB("create job", err)
	}
	return nil
}

// Get loads a job by id, deriving live progress counters from job_items if present.
func (r *Jobs) Get(ctx context.Context, id string) (*jobs.Job, error) {
	query := `
		SELECT
			j.id, j.type, j.status, j.label, j.metadata_provider, j.media_provider, j.target_id,
			j.options_json,
			CASE WHEN counts.total_count IS NOT NULL AND counts.total_count > 0 THEN counts.total_count ELSE j.total END,
			COALESCE(counts.completed_count, j.completed),
			COALESCE(counts.failed_count, j.failed),
			COALESCE(counts.skipped_count, j.skipped),
			j.error_code, j.error_message,
			j.created_at, j.updated_at, j.started_at, j.finished_at, j.priority, j.paused
		FROM jobs j
		LEFT JOIN (
			SELECT
				job_id,
				count(*) AS total_count,
				count(*) FILTER (WHERE status = $2) AS completed_count,
				count(*) FILTER (WHERE status = $3) AS failed_count,
				count(*) FILTER (WHERE status = $4) AS skipped_count
			FROM job_items
			WHERE job_id = $1
			GROUP BY job_id
		) counts ON counts.job_id = j.id
		WHERE j.id = $1`

	row := r.db.QueryRowContext(ctx, query, id,
		string(jobs.ItemCompleted), string(jobs.ItemFailed), string(jobs.ItemSkipped))
	job, err := scanJob(row.Scan)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, apperr.Newf(apperr.CodeJobNotFound, "Job %q does not exist.", id)
		}
		return nil, wrapDB("get job", err)
	}
	return &job, nil
}

// List returns jobs newest first with total matching count and live progress counters.
func (r *Jobs) List(ctx context.Context, filter jobs.ListFilter) ([]jobs.Job, int, error) {
	limit := clampLimit(filter.Limit, 25, 100)
	offset := clampOffset(filter.Offset)

	var (
		whereClauses []string
		args         []any
		argIdx       = 1
	)

	if filter.Status != "" {
		whereClauses = append(whereClauses, "status = $"+strconv.Itoa(argIdx))
		args = append(args, string(filter.Status))
		argIdx++
	}
	if filter.Type != "" {
		whereClauses = append(whereClauses, "type = $"+strconv.Itoa(argIdx))
		args = append(args, string(filter.Type))
		argIdx++
	}
	if filter.Priority != "" {
		whereClauses = append(whereClauses, "priority = $"+strconv.Itoa(argIdx))
		args = append(args, filter.Priority.Rank())
		argIdx++
	}

	whereSQL := ""
	if len(whereClauses) > 0 {
		whereSQL = " WHERE " + strings.Join(whereClauses, " AND ")
	}

	var total int
	countSQL := "SELECT COUNT(*) FROM jobs" + whereSQL
	if err := r.db.QueryRowContext(ctx, countSQL, args...).Scan(&total); err != nil {
		return nil, 0, wrapDB("count jobs", err)
	}

	limitArg := "$" + strconv.Itoa(argIdx)
	offsetArg := "$" + strconv.Itoa(argIdx+1)
	statusCompletedArg := "$" + strconv.Itoa(argIdx+2)
	statusFailedArg := "$" + strconv.Itoa(argIdx+3)
	statusSkippedArg := "$" + strconv.Itoa(argIdx+4)

	selectSQL := `
		WITH paged_jobs AS (
			SELECT ` + jobColumns + ` FROM jobs` + whereSQL + `
			ORDER BY created_at DESC, id DESC LIMIT ` + limitArg + ` OFFSET ` + offsetArg + `
		)
		SELECT
			j.id, j.type, j.status, j.label, j.metadata_provider, j.media_provider, j.target_id,
			j.options_json,
			CASE WHEN counts.total_count IS NOT NULL AND counts.total_count > 0 THEN counts.total_count ELSE j.total END,
			COALESCE(counts.completed_count, j.completed),
			COALESCE(counts.failed_count, j.failed),
			COALESCE(counts.skipped_count, j.skipped),
			j.error_code, j.error_message,
			j.created_at, j.updated_at, j.started_at, j.finished_at, j.priority, j.paused
		FROM paged_jobs j
		LEFT JOIN (
			SELECT
				job_id,
				count(*) AS total_count,
				count(*) FILTER (WHERE status = ` + statusCompletedArg + `) AS completed_count,
				count(*) FILTER (WHERE status = ` + statusFailedArg + `) AS failed_count,
				count(*) FILTER (WHERE status = ` + statusSkippedArg + `) AS skipped_count
			FROM job_items
			WHERE job_id IN (SELECT id FROM paged_jobs)
			GROUP BY job_id
		) counts ON counts.job_id = j.id
		ORDER BY j.created_at DESC, j.id DESC`

	pageArgs := append(args, limit, offset,
		string(jobs.ItemCompleted), string(jobs.ItemFailed), string(jobs.ItemSkipped))

	rows, err := r.db.QueryContext(ctx, selectSQL, pageArgs...)
	if err != nil {
		return nil, 0, wrapDB("list jobs", err)
	}
	defer rows.Close()

	list, err := collectJobs(rows, "list jobs")
	if err != nil {
		return nil, 0, err
	}
	return list, total, nil
}

// ListUnfinished returns every job that has not reached a terminal state and is not paused.
func (r *Jobs) ListUnfinished(ctx context.Context) ([]jobs.Job, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT `+jobColumns+`
		FROM jobs WHERE status NOT IN ($1, $2, $3) AND NOT paused ORDER BY priority DESC, created_at ASC, id ASC`,
		string(jobs.StatusCompleted), string(jobs.StatusFailed), string(jobs.StatusCancelled))
	if err != nil {
		return nil, wrapDB("list unfinished jobs", err)
	}
	defer rows.Close()

	return collectJobs(rows, "list unfinished jobs")
}

func collectJobs(rows *sql.Rows, operation string) ([]jobs.Job, error) {
	out := make([]jobs.Job, 0, 16)
	for rows.Next() {
		job, err := scanJob(rows.Scan)
		if err != nil {
			return nil, wrapDB("scan job", err)
		}
		out = append(out, job)
	}
	if err := rows.Err(); err != nil {
		return nil, wrapDB(operation, err)
	}
	return out, nil
}

// SetStatus moves a job into a new state. The transition is validated against
// the state machine; an illegal move is rejected instead of silently applied.
//
// The row is locked while the current state is read, so that a cancellation and
// a worker's progress update cannot both act on the same stale status and undo
// each other.
func (r *Jobs) SetStatus(ctx context.Context, id string, status jobs.Status, errorCode, errorMessage string) error {
	return r.db.WithTx(ctx, func(tx *sql.Tx) error {
		var current string
		err := tx.QueryRowContext(ctx, `SELECT status FROM jobs WHERE id = $1 FOR UPDATE`, id).Scan(&current)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return apperr.Newf(apperr.CodeJobNotFound, "Job %q does not exist.", id)
			}
			return wrapDB("read job status", err)
		}
		from := jobs.Status(current)
		if !from.CanTransitionTo(status) {
			return apperr.Newf(apperr.CodeInvalidRequest,
				"A job cannot move from %q to %q.", from, status)
		}

		now := time.Now().UTC()
		var startedAt any
		if from == jobs.StatusQueued && status != jobs.StatusQueued {
			startedAt = now
		}
		var finishedAt any
		if status.Terminal() {
			finishedAt = now
		}

		_, err = tx.ExecContext(ctx, `
			UPDATE jobs SET
				status        = $1,
				error_code    = $2,
				error_message = $3,
				updated_at    = $4,
				started_at    = COALESCE(started_at, $5),
				finished_at   = COALESCE($6, finished_at)
			WHERE id = $7`,
			string(status), errorCode, errorMessage, now, startedAt, finishedAt, id)
		if err != nil {
			return wrapDB("update job status", err)
		}
		return nil
	})
}

// SetLabel stores the human readable name a job got while it was resolved.
// Until the catalogue is read the label is only the target id, which is what a
// client would otherwise keep showing for the whole run.
func (r *Jobs) SetLabel(ctx context.Context, id, label string) error {
	_, err := r.db.ExecContext(ctx,
		`UPDATE jobs SET label = $1, updated_at = $2 WHERE id = $3`,
		label, time.Now().UTC(), id)
	if err != nil {
		return wrapDB("set job label", err)
	}
	return nil
}

// SetTotal records how many items a job contains.
func (r *Jobs) SetTotal(ctx context.Context, id string, total int) error {
	_, err := r.db.ExecContext(ctx,
		`UPDATE jobs SET total = $1, updated_at = $2 WHERE id = $3`,
		total, time.Now().UTC(), id)
	if err != nil {
		return wrapDB("set job total", err)
	}
	return nil
}

// RefreshCounters recomputes the completed, failed and skipped counters from
// the item table and returns the updated job. Recomputing and reading back in
// one statement keeps the returned counters consistent with the row that was
// just written, even when several workers finish at the same moment.
func (r *Jobs) RefreshCounters(ctx context.Context, id string) (*jobs.Job, error) {
	row := r.db.QueryRowContext(ctx, `
		UPDATE jobs SET
			completed  = counts.completed_count,
			failed     = counts.failed_count,
			skipped    = counts.skipped_count,
			updated_at = $2
		FROM (
			SELECT
				count(*) FILTER (WHERE status = $3) AS completed_count,
				count(*) FILTER (WHERE status = $4) AS failed_count,
				count(*) FILTER (WHERE status = $5) AS skipped_count
			FROM job_items WHERE job_id = $1
		) AS counts
		WHERE jobs.id = $1
		RETURNING `+jobColumns,
		id, time.Now().UTC(),
		string(jobs.ItemCompleted), string(jobs.ItemFailed), string(jobs.ItemSkipped))

	job, err := scanJob(row.Scan)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, apperr.Newf(apperr.CodeJobNotFound, "Job %q does not exist.", id)
		}
		return nil, wrapDB("refresh job counters", err)
	}
	return &job, nil
}

const itemColumns = `id, job_id, position, status, track_id, track_json, label,
	media_provider, media_id, media_url, match_score, file_id, attempts,
	max_attempts, next_retry_at, staging_relpath, staged_size, staged_sha256,
	error_code, error_message, created_at, updated_at, started_at, finished_at`

func scanItem(scan func(dest ...any) error) (jobs.Item, error) {
	var (
		it          jobs.Item
		status      string
		trackID     sql.NullString
		trackJSON   string
		fileID      sql.NullString
		nextRetryAt sql.NullTime
		startedAt   sql.NullTime
		finishedAt  sql.NullTime
	)
	err := scan(&it.ID, &it.JobID, &it.Position, &status, &trackID, &trackJSON, &it.Label,
		&it.MediaProvider, &it.MediaID, &it.MediaURL, &it.MatchScore, &fileID, &it.Attempts,
		&it.MaxAttempts, &nextRetryAt, &it.StagingRelPath, &it.StagedSize, &it.StagedSHA256,
		&it.ErrorCode, &it.ErrorMessage, &it.CreatedAt, &it.UpdatedAt, &startedAt, &finishedAt)
	if err != nil {
		return jobs.Item{}, err
	}
	it.Status = jobs.ItemStatus(status)
	it.TrackID = stringOf(trackID)
	it.FileID = stringOf(fileID)
	if nextRetryAt.Valid {
		t := nextRetryAt.Time.UTC()
		it.NextRetryAt = &t
	}
	if err := json.Unmarshal([]byte(trackJSON), &it.Track); err != nil {
		return jobs.Item{}, apperr.Wrap(apperr.CodeInternal, "The stored track could not be decoded.", err)
	}
	it.CreatedAt = it.CreatedAt.UTC()
	it.UpdatedAt = it.UpdatedAt.UTC()
	it.StartedAt = timePtr(startedAt)
	it.FinishedAt = timePtr(finishedAt)
	return it, nil
}

// AddItems stores the items of a job in one transaction and updates the total,
// so that a job never has a partially written item list.
func (r *Jobs) AddItems(ctx context.Context, jobID string, items []jobs.Item) error {
	if len(items) == 0 {
		return r.SetTotal(ctx, jobID, 0)
	}
	now := time.Now().UTC()

	return r.db.WithTx(ctx, func(tx *sql.Tx) error {
		stmt, err := tx.PrepareContext(ctx, `
			INSERT INTO job_items (id, job_id, position, status, track_id, track_json, label,
				media_provider, media_id, media_url, match_score, file_id, attempts,
				max_attempts, next_retry_at, staging_relpath, staged_size, staged_sha256,
				error_code, error_message, created_at, updated_at)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18, $19, $20, $21, $21)`)
		if err != nil {
			return wrapDB("prepare item insert", err)
		}
		defer stmt.Close()

		for i := range items {
			item := &items[i]
			if item.ID == "" {
				item.ID = music.NewID()
			}
			item.JobID = jobID
			if item.Status == "" {
				item.Status = jobs.ItemPending
			}
			if item.MaxAttempts <= 0 {
				item.MaxAttempts = 5
			}
			trackJSON, err := json.Marshal(item.Track)
			if err != nil {
				return apperr.Wrap(apperr.CodeInternal, "The track could not be encoded.", err)
			}
			if _, err := stmt.ExecContext(ctx,
				item.ID, jobID, item.Position, string(item.Status), nullString(item.TrackID),
				string(trackJSON), item.Label, item.MediaProvider, item.MediaID, item.MediaURL,
				item.MatchScore, nullString(item.FileID), item.Attempts,
				item.MaxAttempts, nullTime(item.NextRetryAt), item.StagingRelPath, item.StagedSize, item.StagedSHA256,
				item.ErrorCode, item.ErrorMessage, now); err != nil {
				return wrapDB("insert job item", err)
			}
		}

		if _, err := tx.ExecContext(ctx,
			`UPDATE jobs SET total = $1, updated_at = $2 WHERE id = $3`,
			len(items), now, jobID); err != nil {
			return wrapDB("update job total", err)
		}
		return nil
	})
}

// ListItems returns the items of a job in processing order.
func (r *Jobs) ListItems(ctx context.Context, jobID string) ([]jobs.Item, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT `+itemColumns+` FROM job_items WHERE job_id = $1 ORDER BY position`, jobID)
	if err != nil {
		return nil, wrapDB("list job items", err)
	}
	defer rows.Close()

	return collectItems(rows, "list job items")
}

// ListPendingItems returns the items of a job that still have to be processed.
func (r *Jobs) ListPendingItems(ctx context.Context, jobID string) ([]jobs.Item, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT `+itemColumns+` FROM job_items
		 WHERE job_id = $1 AND status NOT IN ($2, $3, $4, $5) ORDER BY position`,
		jobID, string(jobs.ItemCompleted), string(jobs.ItemFailed),
		string(jobs.ItemSkipped), string(jobs.ItemCancelled))
	if err != nil {
		return nil, wrapDB("list pending items", err)
	}
	defer rows.Close()

	return collectItems(rows, "list pending items")
}

func collectItems(rows *sql.Rows, operation string) ([]jobs.Item, error) {
	out := make([]jobs.Item, 0, 32)
	for rows.Next() {
		item, err := scanItem(rows.Scan)
		if err != nil {
			return nil, wrapDB("scan job item", err)
		}
		out = append(out, item)
	}
	if err := rows.Err(); err != nil {
		return nil, wrapDB(operation, err)
	}
	return out, nil
}

// GetItem loads a single item.
func (r *Jobs) GetItem(ctx context.Context, id string) (*jobs.Item, error) {
	row := r.db.QueryRowContext(ctx, `SELECT `+itemColumns+` FROM job_items WHERE id = $1`, id)
	item, err := scanItem(row.Scan)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, apperr.Newf(apperr.CodeJobNotFound, "Job item %q does not exist.", id)
		}
		return nil, wrapDB("get job item", err)
	}
	return &item, nil
}

// UpdateItem applies a worker update to an item, validating the state
// transition. The row is locked for the read-modify-write so that a job
// cancellation and a worker update cannot cross each other.
func (r *Jobs) UpdateItem(ctx context.Context, id string, update jobs.ItemUpdate) error {
	return r.db.WithTx(ctx, func(tx *sql.Tx) error {
		var current string
		err := tx.QueryRowContext(ctx,
			`SELECT status FROM job_items WHERE id = $1 FOR UPDATE`, id).Scan(&current)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return apperr.Newf(apperr.CodeJobNotFound, "Job item %q does not exist.", id)
			}
			return wrapDB("read item status", err)
		}
		from := jobs.ItemStatus(current)
		if update.Status != "" && !from.CanTransitionTo(update.Status) {
			return apperr.Newf(apperr.CodeInvalidRequest,
				"A job item cannot move from %q to %q.", from, update.Status)
		}

		now := time.Now().UTC()
		status := update.Status
		if status == "" {
			status = from
		}

		var startedAt any
		if from == jobs.ItemPending && status != jobs.ItemPending {
			startedAt = now
		}
		var finishedAt any
		if status.Terminal() {
			finishedAt = now
		}
		var attempts any
		if update.Attempts != nil {
			attempts = *update.Attempts
		}
		var maxAttempts any
		if update.MaxAttempts != nil {
			maxAttempts = *update.MaxAttempts
		}
		var stagingRelPath any
		if update.StagingRelPath != nil {
			stagingRelPath = *update.StagingRelPath
		}
		var stagedSize any
		if update.StagedSize != nil {
			stagedSize = *update.StagedSize
		}
		var stagedSHA256 any
		if update.StagedSHA256 != nil {
			stagedSHA256 = *update.StagedSHA256
		}

		_, err = tx.ExecContext(ctx, `
			UPDATE job_items SET
				status          = $1,
				media_provider  = CASE WHEN $2::text <> '' THEN $2::text ELSE media_provider END,
				media_id        = CASE WHEN $3::text <> '' THEN $3::text ELSE media_id END,
				media_url       = CASE WHEN $4::text <> '' THEN $4::text ELSE media_url END,
				match_score     = CASE WHEN $5::double precision > 0 THEN $5::double precision ELSE match_score END,
				file_id         = COALESCE($6, file_id),
				track_id        = COALESCE($7, track_id),
				attempts        = COALESCE($8, attempts),
				max_attempts    = COALESCE($9, max_attempts),
				next_retry_at   = CASE WHEN $10::boolean THEN NULL WHEN $11::timestamptz IS NOT NULL THEN $11::timestamptz ELSE next_retry_at END,
				staging_relpath = COALESCE($12, staging_relpath),
				staged_size     = COALESCE($13, staged_size),
				staged_sha256   = COALESCE($14, staged_sha256),
				error_code      = $15,
				error_message   = $16,
				updated_at      = $17,
				started_at      = COALESCE(started_at, $18),
				finished_at     = COALESCE($19, finished_at)
			WHERE id = $20`,
			string(status),
			update.MediaProvider, update.MediaID, update.MediaURL, update.MatchScore,
			nullString(update.FileID), nullString(update.TrackID), attempts,
			maxAttempts, update.ClearNextRetry, nullTime(update.NextRetryAt),
			stagingRelPath, stagedSize, stagedSHA256,
			update.ErrorCode, update.ErrorMessage,
			now, startedAt, finishedAt, id)
		if err != nil {
			return wrapDB("update job item", err)
		}
		return nil
	})
}

// ListRetryDueItems returns items in retry_wait that are ready to be retried.
func (r *Jobs) ListRetryDueItems(ctx context.Context, jobID string, now time.Time) ([]jobs.Item, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT `+itemColumns+` FROM job_items
		 WHERE job_id = $1 AND status = $2 AND (next_retry_at IS NULL OR next_retry_at <= $3)
		 ORDER BY position`,
		jobID, string(jobs.ItemRetryWait), now.UTC())
	if err != nil {
		return nil, wrapDB("list retry due items", err)
	}
	defer rows.Close()

	return collectItems(rows, "list retry due items")
}

// ListWaitingStorageItems returns all items that are waiting for library or staging storage.
func (r *Jobs) ListWaitingStorageItems(ctx context.Context) ([]jobs.Item, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT `+itemColumns+` FROM job_items
		 WHERE status IN ($1, $2)
		 ORDER BY created_at`,
		string(jobs.ItemWaitingStorage), string(jobs.ItemWaitingSpace))
	if err != nil {
		return nil, wrapDB("list waiting storage items", err)
	}
	defer rows.Close()

	return collectItems(rows, "list waiting storage items")
}

// CancelPendingItems marks every item of a job that has not finished yet as
// cancelled and returns how many were affected.
func (r *Jobs) CancelPendingItems(ctx context.Context, jobID string) (int, error) {
	now := time.Now().UTC()
	result, err := r.db.ExecContext(ctx, `
		UPDATE job_items SET status = $1, updated_at = $2, finished_at = COALESCE(finished_at, $2)
		WHERE job_id = $3 AND status NOT IN ($4, $5, $6, $1)`,
		string(jobs.ItemCancelled), now, jobID,
		string(jobs.ItemCompleted), string(jobs.ItemFailed), string(jobs.ItemSkipped))
	if err != nil {
		return 0, wrapDB("cancel job items", err)
	}
	return rowsAffected(result, "cancel job items")
}

// ResetInFlightItems returns items that a previous process left in an active
// working state (matching, downloading, tagging, finalizing) back to pending so they are
// resumed. Staged partials, hashes, and attempts are preserved. Items in
// retry_wait, waiting_for_storage, waiting_for_space remain untouched.
func (r *Jobs) ResetInFlightItems(ctx context.Context) (int, error) {
	result, err := r.db.ExecContext(ctx, `
		UPDATE job_items SET status = $1, updated_at = $2, started_at = NULL
		WHERE status IN ($3, $4, $5, $6)`,
		string(jobs.ItemPending), time.Now().UTC(),
		string(jobs.ItemMatching), string(jobs.ItemDownloading), string(jobs.ItemTagging),
		string(jobs.ItemFinalizing))
	if err != nil {
		return 0, wrapDB("reset in flight items", err)
	}
	return rowsAffected(result, "reset in flight items")
}

// ResetInterruptedJobs puts every job that a previous process left in an active
// state back into the queue. It deliberately bypasses the forward-only state
// machine: after a crash the recorded state describes work that is no longer
// running, and returning it to "queued" is the one transition that makes the
// job start again in a defined way. Terminal jobs are never touched.
func (r *Jobs) ResetInterruptedJobs(ctx context.Context) (int, error) {
	result, err := r.db.ExecContext(ctx, `
		UPDATE jobs SET status = $1, updated_at = $2, error_code = '', error_message = ''
		WHERE status NOT IN ($1, $3, $4, $5)`,
		string(jobs.StatusQueued), time.Now().UTC(),
		string(jobs.StatusCompleted), string(jobs.StatusFailed), string(jobs.StatusCancelled))
	if err != nil {
		return 0, wrapDB("reset interrupted jobs", err)
	}
	return rowsAffected(result, "reset interrupted jobs")
}

// HasItems reports whether a job already has items.
func (r *Jobs) HasItems(ctx context.Context, jobID string) (bool, error) {
	var exists bool
	if err := r.db.QueryRowContext(ctx,
		`SELECT EXISTS (SELECT 1 FROM job_items WHERE job_id = $1)`, jobID).Scan(&exists); err != nil {
		return false, wrapDB("count job items", err)
	}
	return exists, nil
}

// SetPriority updates a job's priority rank.
func (r *Jobs) SetPriority(ctx context.Context, id string, priority jobs.Priority) error {
	if !priority.Valid() {
		return apperr.Newf(apperr.CodeInvalidRequest, "Invalid priority: %s", priority)
	}
	res, err := r.db.ExecContext(ctx, `
		UPDATE jobs SET priority = $2, updated_at = $3 WHERE id = $1`,
		id, priority.Rank(), time.Now().UTC())
	if err != nil {
		return wrapDB("set job priority", err)
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return wrapDB("check rows affected", err)
	}
	if affected == 0 {
		return apperr.Newf(apperr.CodeJobNotFound, "Job %q does not exist.", id)
	}
	return nil
}

// SetPaused sets or clears the paused flag on a job.
func (r *Jobs) SetPaused(ctx context.Context, id string, paused bool) error {
	res, err := r.db.ExecContext(ctx, `
		UPDATE jobs SET paused = $2, updated_at = $3 WHERE id = $1`,
		id, paused, time.Now().UTC())
	if err != nil {
		return wrapDB("set job paused", err)
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return wrapDB("check rows affected", err)
	}
	if affected == 0 {
		return apperr.Newf(apperr.CodeJobNotFound, "Job %q does not exist.", id)
	}
	return nil
}

// DeleteHistory removes completed or cancelled jobs older than the given cutoff time.
// job_items are cascade deleted by PostgreSQL foreign key constraint.
// Library tracks and files remain completely untouched.
func (r *Jobs) DeleteHistory(ctx context.Context, olderThan time.Time, allowedStatuses []jobs.Status) (int, int, error) {
	if len(allowedStatuses) == 0 {
		allowedStatuses = []jobs.Status{jobs.StatusCompleted}
	}
	statusPlaceholders := make([]string, len(allowedStatuses))
	args := []any{olderThan.UTC()}
	for i, s := range allowedStatuses {
		statusPlaceholders[i] = "$" + strconv.Itoa(i+2)
		args = append(args, string(s))
	}

	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, 0, wrapDB("begin delete history tx", err)
	}
	defer tx.Rollback()

	countQuery := "SELECT COUNT(ji.id), COUNT(DISTINCT j.id) FROM jobs j LEFT JOIN job_items ji ON ji.job_id = j.id WHERE j.created_at < $1 AND j.status IN (" + strings.Join(statusPlaceholders, ", ") + ")"

	var (
		itemCount int
		jobCount  int
	)
	if err := tx.QueryRowContext(ctx, countQuery, args...).Scan(&itemCount, &jobCount); err != nil {
		return 0, 0, wrapDB("count history to delete", err)
	}

	if jobCount == 0 {
		return 0, 0, nil
	}

	deleteQuery := "DELETE FROM jobs WHERE created_at < $1 AND status IN (" + strings.Join(statusPlaceholders, ", ") + ")"

	if _, err := tx.ExecContext(ctx, deleteQuery, args...); err != nil {
		return 0, 0, wrapDB("delete history jobs", err)
	}

	if err := tx.Commit(); err != nil {
		return 0, 0, wrapDB("commit delete history tx", err)
	}

	return jobCount, itemCount, nil
}

// ResetItemForRetry resets an item that is in a failed or retry_wait state so it can be picked up fresh.
func (r *Jobs) ResetItemForRetry(ctx context.Context, jobID, itemID string) error {
	now := time.Now().UTC()
	res, err := r.db.ExecContext(ctx, `
		UPDATE job_items
		SET status = $3,
		    attempts = 0,
		    error_code = '',
		    error_message = '',
		    next_retry_at = NULL,
		    updated_at = $4
		WHERE id = $1 AND job_id = $2
		  AND status IN ($5, $6)`,
		itemID, jobID, string(jobs.ItemPending), now,
		string(jobs.ItemFailed), string(jobs.ItemRetryWait))
	if err != nil {
		return wrapDB("reset item for retry", err)
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return wrapDB("check rows affected", err)
	}
	if affected == 0 {
		return apperr.New(apperr.CodeInvalidRequest, "The item is not in a retryable state (failed or retry_wait).")
	}

	// Also make sure parent job is not stuck in a terminal failed state
	_, _ = r.db.ExecContext(ctx, `
		UPDATE jobs
		SET status = $2, error_code = '', error_message = '', updated_at = $3
		WHERE id = $1 AND status IN ($4, $5)`,
		jobID, string(jobs.StatusQueued), now,
		string(jobs.StatusFailed), string(jobs.StatusCompleted))

	return nil
}

// ResetFailedItemsInJob resets all items with status 'failed' in a job to 'pending'.
func (r *Jobs) ResetFailedItemsInJob(ctx context.Context, jobID string) (int, int, error) {
	now := time.Now().UTC()

	var totalFailed int
	err := r.db.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM job_items
		WHERE job_id = $1 AND status = $2`,
		jobID, string(jobs.ItemFailed)).Scan(&totalFailed)
	if err != nil {
		return 0, 0, wrapDB("count failed items", err)
	}

	if totalFailed == 0 {
		return 0, 0, nil
	}

	res, err := r.db.ExecContext(ctx, `
		UPDATE job_items
		SET status = $2,
		    attempts = 0,
		    error_code = '',
		    error_message = '',
		    next_retry_at = NULL,
		    updated_at = $3
		WHERE job_id = $1 AND status = $4`,
		jobID, string(jobs.ItemPending), now, string(jobs.ItemFailed))
	if err != nil {
		return 0, 0, wrapDB("reset failed items in job", err)
	}
	retried, err := res.RowsAffected()
	if err != nil {
		return 0, 0, wrapDB("check rows affected", err)
	}

	// Reset parent job status if it was failed or completed
	_, _ = r.db.ExecContext(ctx, `
		UPDATE jobs
		SET status = $2, error_code = '', error_message = '', updated_at = $3
		WHERE id = $1 AND status IN ($4, $5)`,
		jobID, string(jobs.StatusQueued), now,
		string(jobs.StatusFailed), string(jobs.StatusCompleted))

	return int(retried), 0, nil
}

func rowsAffected(result sql.Result, operation string) (int, error) {
	affected, err := result.RowsAffected()
	if err != nil {
		return 0, wrapDB(operation, err)
	}
	return int(affected), nil
}
