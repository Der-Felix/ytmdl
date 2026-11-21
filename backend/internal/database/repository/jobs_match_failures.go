package repository

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"ytdm/backend/internal/apperr"
	"ytdm/backend/internal/jobs"
)

// ReleaseMatchFailures reports the tracks the most recent finished job for a
// release could not find media for, and how many finished jobs for that
// release ended that way.
//
// The answer is zero unless every failed item of that job failed with
// MATCH_FAILED. A job that also lost a track to a network error or a full
// disk says nothing reliable about the match, and must not make a caller
// wait.
func (r *Jobs) ReleaseMatchFailures(ctx context.Context, releaseID string) (jobs.MatchFailures, error) {
	var none jobs.MatchFailures
	if releaseID == "" {
		return none, nil
	}

	var (
		jobID      string
		finishedAt time.Time
	)
	err := r.db.QueryRowContext(ctx, `
		SELECT id, COALESCE(finished_at, updated_at)
		FROM jobs
		WHERE type = $1 AND target_id = $2 AND status IN ($3, $4, $5)
		ORDER BY created_at DESC
		LIMIT 1`,
		string(jobs.TypeRelease), releaseID,
		string(jobs.StatusCompleted), string(jobs.StatusFailed), string(jobs.StatusCancelled),
	).Scan(&jobID, &finishedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return none, nil
	}
	if err != nil {
		return none, wrapDB("find last finished release job", err)
	}

	rows, err := r.db.QueryContext(ctx, `
		SELECT COALESCE(error_code, ''), COALESCE(track_json->>'source_id', '')
		FROM job_items
		WHERE job_id = $1 AND status = $2
		ORDER BY position`, jobID, string(jobs.ItemFailed))
	if err != nil {
		return none, wrapDB("list failed items of release job", err)
	}
	defer rows.Close()

	ids := make([]string, 0, 4)
	for rows.Next() {
		var code, sourceID string
		if err := rows.Scan(&code, &sourceID); err != nil {
			return none, wrapDB("scan failed item of release job", err)
		}
		if code != string(apperr.CodeMatchFailed) || sourceID == "" {
			return none, nil
		}
		ids = append(ids, sourceID)
	}
	if err := rows.Err(); err != nil {
		return none, wrapDB("list failed items of release job", err)
	}
	if len(ids) == 0 {
		return none, nil
	}

	var attempts int
	err = r.db.QueryRowContext(ctx, `
		SELECT COUNT(DISTINCT j.id)
		FROM jobs j
		JOIN job_items i ON i.job_id = j.id
		WHERE j.type = $1 AND j.target_id = $2 AND j.status IN ($3, $4, $5)
		  AND i.status = $6 AND i.error_code = $7`,
		string(jobs.TypeRelease), releaseID,
		string(jobs.StatusCompleted), string(jobs.StatusFailed), string(jobs.StatusCancelled),
		string(jobs.ItemFailed), string(apperr.CodeMatchFailed),
	).Scan(&attempts)
	if err != nil {
		return none, wrapDB("count unmatched release jobs", err)
	}

	return jobs.MatchFailures{Attempts: attempts, LastAt: finishedAt, TrackSourceIDs: ids}, nil
}
