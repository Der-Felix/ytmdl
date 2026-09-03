package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"ytdm/backend/internal/apperr"
	"ytdm/backend/internal/database"
	"ytdm/backend/internal/jobs"
	"ytdm/backend/internal/music"
	"ytdm/backend/internal/subscriptions"
)

// Subscriptions persists the watched artists.
type Subscriptions struct {
	db *database.DB
}

// NewSubscriptions returns a subscription repository.
func NewSubscriptions(db *database.DB) *Subscriptions { return &Subscriptions{db: db} }

const subscriptionColumns = `id, provider, artist_source_id, artist_name, artist_image_url,
	enabled, auto_download, last_sync_at, next_sync_at, last_sync_status, last_error,
	created_at, updated_at, release_filter, download_priority`

func scanSubscription(scan func(dest ...any) error) (subscriptions.Subscription, error) {
	var (
		s            subscriptions.Subscription
		lastSyncAt   sql.NullTime
		status       string
		filterJSON   []byte
		priorityRank int
	)
	err := scan(&s.ID, &s.Provider, &s.ArtistSourceID, &s.ArtistName, &s.ArtistImageURL,
		&s.Enabled, &s.AutoDownload, &lastSyncAt, &s.NextSyncAt, &status, &s.LastError,
		&s.CreatedAt, &s.UpdatedAt, &filterJSON, &priorityRank)
	if err != nil {
		return subscriptions.Subscription{}, err
	}
	s.LastSyncStatus = subscriptions.SyncStatus(status)
	s.LastSyncAt = timePtr(lastSyncAt)
	s.NextSyncAt = s.NextSyncAt.UTC()
	s.CreatedAt = s.CreatedAt.UTC()
	s.UpdatedAt = s.UpdatedAt.UTC()
	s.DownloadPriority = jobs.PriorityFromRank(priorityRank)
	if len(filterJSON) > 0 {
		if err := json.Unmarshal(filterJSON, &s.ReleaseFilter); err != nil {
			s.ReleaseFilter = music.DefaultReleaseFilter()
		}
	} else {
		s.ReleaseFilter = music.DefaultReleaseFilter()
	}
	return s, nil
}

// Create stores a new subscription.
//
// Subscribing twice is not an error. The insert therefore resolves the unique
// key onto the existing row instead of failing, which makes a double click,
// a retried request and two concurrent subscribes all end up with the same
// single subscription.
//
// A second subscribe only ever improves what is stored: the name and the image
// are refreshed when the request actually carries them, and left alone when it
// does not. Overwriting unconditionally would let a request that names only
// the artist id replace a good name with the "Unknown Artist" placeholder.
func (r *Subscriptions) Create(ctx context.Context, req subscriptions.NewSubscription) (*subscriptions.Subscription, error) {
	if err := req.Validate(); err != nil {
		return nil, err
	}

	now := time.Now().UTC()

	// The trimmed name is passed as it came in, so that the conflict branch can
	// tell "no name given" from "this is the name". The placeholder is applied
	// only to the inserted row.
	name := strings.TrimSpace(req.ArtistName)
	image := strings.TrimSpace(req.ArtistImageURL)

	filter := music.DefaultReleaseFilter()
	if req.ReleaseFilter != nil && req.ReleaseFilter.Any() {
		filter = *req.ReleaseFilter
	}
	filterJSON, err := json.Marshal(filter)
	if err != nil {
		return nil, wrapDB("encode release filter", err)
	}

	priority := jobs.PriorityLow
	if req.DownloadPriority != nil && req.DownloadPriority.Valid() {
		priority = *req.DownloadPriority
	}

	enabled := true
	if req.Enabled != nil {
		enabled = *req.Enabled
	}

	row := r.db.QueryRowContext(ctx, `
		INSERT INTO artist_subscriptions (id, provider, artist_source_id, artist_name,
			artist_image_url, enabled, auto_download, last_sync_at, next_sync_at,
			last_sync_status, last_error, created_at, updated_at, release_filter, download_priority)
		VALUES ($1, $2, $3, COALESCE(NULLIF($4::text, ''), $9), $5, $12, $6, NULL, $7, $8, '', $7, $7, $10, $11)
		ON CONFLICT (provider, artist_source_id) DO UPDATE SET
			artist_name      = CASE WHEN $4::text <> ''
			                        THEN $4::text ELSE artist_subscriptions.artist_name END,
			artist_image_url = CASE WHEN $5::text <> ''
			                        THEN $5::text ELSE artist_subscriptions.artist_image_url END,
			updated_at       = excluded.updated_at
		RETURNING `+subscriptionColumns,
		music.NewID(), strings.TrimSpace(req.Provider), strings.TrimSpace(req.ArtistSourceID),
		name, image, req.AutoDownload, now, string(subscriptions.StatusPending),
		music.UnknownArtist, filterJSON, priority.Rank(), enabled)

	sub, err := scanSubscription(row.Scan)
	if err != nil {
		return nil, wrapDB("create subscription", err)
	}
	return &sub, nil
}

// Get loads a subscription by id.
func (r *Subscriptions) Get(ctx context.Context, id string) (*subscriptions.Subscription, error) {
	row := r.db.QueryRowContext(ctx,
		`SELECT `+subscriptionColumns+` FROM artist_subscriptions WHERE id = $1`, id)

	sub, err := scanSubscription(row.Scan)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, subscriptionNotFound(id)
		}
		return nil, wrapDB("get subscription", err)
	}
	return &sub, nil
}

// FindBySource resolves a subscription by its provider identity. An artist
// that is not watched yields nil rather than an error, because "not
// subscribed" is a normal answer for the artist page.
func (r *Subscriptions) FindBySource(ctx context.Context, provider, artistSourceID string) (*subscriptions.Subscription, error) {
	row := r.db.QueryRowContext(ctx,
		`SELECT `+subscriptionColumns+` FROM artist_subscriptions
		 WHERE provider = $1 AND artist_source_id = $2`, provider, artistSourceID)

	sub, err := scanSubscription(row.Scan)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, wrapDB("find subscription by source", err)
	}
	return &sub, nil
}

// List returns the subscriptions ordered by artist name.
func (r *Subscriptions) List(ctx context.Context, filter subscriptions.ListFilter) ([]subscriptions.Subscription, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT `+subscriptionColumns+`
		FROM artist_subscriptions
		WHERE ($1::text = '' OR provider = $1)
		  AND ($2::text = '' OR artist_source_id = $2)
		ORDER BY artist_name, provider LIMIT $3 OFFSET $4`,
		filter.Provider, filter.ArtistSourceID,
		clampLimit(filter.Limit, 100, 500), clampOffset(filter.Offset))
	if err != nil {
		return nil, wrapDB("list subscriptions", err)
	}
	defer rows.Close()

	return collectSubscriptions(rows, "list subscriptions")
}

// ListAll returns all subscriptions ordered by artist name and provider without limits.
func (r *Subscriptions) ListAll(ctx context.Context) ([]subscriptions.Subscription, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT `+subscriptionColumns+`
		FROM artist_subscriptions
		ORDER BY artist_name, provider`)
	if err != nil {
		return nil, wrapDB("list all subscriptions", err)
	}
	defer rows.Close()

	return collectSubscriptions(rows, "list all subscriptions")
}

// Update changes the flags of a subscription. A field the update does not name
// keeps its value, which is what makes a PATCH that only flips auto download
// leave the enabled state alone.
func (r *Subscriptions) Update(ctx context.Context, id string, update subscriptions.Update) (*subscriptions.Subscription, error) {
	var (
		enabled      any
		autoDownload any
		filterJSON   any
		priorityRank any
	)
	if update.Enabled != nil {
		enabled = *update.Enabled
	}
	if update.AutoDownload != nil {
		autoDownload = *update.AutoDownload
	}
	if update.ReleaseFilter != nil {
		b, err := json.Marshal(update.ReleaseFilter)
		if err != nil {
			return nil, wrapDB("marshal release filter", err)
		}
		filterJSON = b
	}
	if update.DownloadPriority != nil && update.DownloadPriority.Valid() {
		priorityRank = update.DownloadPriority.Rank()
	}

	row := r.db.QueryRowContext(ctx, `
		UPDATE artist_subscriptions SET
			enabled           = COALESCE($1::boolean, enabled),
			auto_download     = COALESCE($2::boolean, auto_download),
			release_filter    = COALESCE($3::jsonb, release_filter),
			download_priority = COALESCE($4::integer, download_priority),
			updated_at        = $5
		WHERE id = $6
		RETURNING `+subscriptionColumns,
		enabled, autoDownload, filterJSON, priorityRank, time.Now().UTC(), id)

	sub, err := scanSubscription(row.Scan)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, subscriptionNotFound(id)
		}
		return nil, wrapDB("update subscription", err)
	}
	return &sub, nil
}

// Delete removes a subscription. Deleting one that does not exist reports the
// missing subscription rather than silently succeeding, so that a client
// cannot mistake a typo for a completed deletion.
func (r *Subscriptions) Delete(ctx context.Context, id string) error {
	result, err := r.db.ExecContext(ctx, `DELETE FROM artist_subscriptions WHERE id = $1`, id)
	if err != nil {
		return wrapDB("delete subscription", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return wrapDB("delete subscription", err)
	}
	if affected == 0 {
		return subscriptionNotFound(id)
	}
	return nil
}

// ListDueForSync returns the enabled subscriptions whose next run is due,
// oldest first so that a backlog is worked off in order.
func (r *Subscriptions) ListDueForSync(ctx context.Context, now time.Time, limit int) ([]subscriptions.Subscription, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT `+subscriptionColumns+`
		FROM artist_subscriptions
		WHERE enabled AND next_sync_at <= $1
		ORDER BY next_sync_at LIMIT $2`,
		now.UTC(), clampLimit(limit, 25, 200))
	if err != nil {
		return nil, wrapDB("list due subscriptions", err)
	}
	defer rows.Close()

	return collectSubscriptions(rows, "list due subscriptions")
}

// RecordSync writes the outcome of a run back to the subscription. The error
// column is always overwritten, so a run that finished clears the message of
// the run that failed before it.
func (r *Subscriptions) RecordSync(ctx context.Context, id string, outcome subscriptions.SyncOutcome) error {
	if !outcome.Status.Valid() {
		return apperr.Newf(apperr.CodeInternal, "%q is not a valid sync status.", outcome.Status)
	}
	result, err := r.db.ExecContext(ctx, `
		UPDATE artist_subscriptions SET
			last_sync_at     = $1,
			next_sync_at     = $2,
			last_sync_status = $3,
			last_error       = $4,
			updated_at       = $5
		WHERE id = $6`,
		outcome.At.UTC(), outcome.NextAt.UTC(), string(outcome.Status),
		outcome.Error, time.Now().UTC(), id)
	if err != nil {
		return wrapDB("record subscription sync", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return wrapDB("record subscription sync", err)
	}
	if affected == 0 {
		return subscriptionNotFound(id)
	}
	return nil
}

// ApplyImport applies the imported new subscriptions and updates in a single transaction.
func (r *Subscriptions) ApplyImport(ctx context.Context, newSubs []subscriptions.NewSubscription, updates []subscriptions.ImportUpdate) (*subscriptions.ImportResult, error) {
	result := &subscriptions.ImportResult{}
	now := time.Now().UTC()

	err := r.db.WithTx(ctx, func(tx *sql.Tx) error {
		for i, item := range newSubs {
			filter := music.DefaultReleaseFilter()
			if item.ReleaseFilter != nil && item.ReleaseFilter.Any() {
				filter = *item.ReleaseFilter
			}
			filterJSON, err := json.Marshal(filter)
			if err != nil {
				return wrapDB("marshal release filter", err)
			}
			priority := jobs.PriorityLow
			if item.DownloadPriority != nil && item.DownloadPriority.Valid() {
				priority = *item.DownloadPriority
			}
			enabled := true
			if item.Enabled != nil {
				enabled = *item.Enabled
			}
			name := strings.TrimSpace(item.ArtistName)
			image := strings.TrimSpace(item.ArtistImageURL)

			res, err := tx.ExecContext(ctx, `
				INSERT INTO artist_subscriptions (id, provider, artist_source_id, artist_name,
					artist_image_url, enabled, auto_download, last_sync_at, next_sync_at,
					last_sync_status, last_error, created_at, updated_at, release_filter, download_priority)
				VALUES ($1, $2, $3, COALESCE(NULLIF($4::text, ''), $9), $5, $6, $7, NULL, $8, 'pending', '', $8, $8, $10, $11)
				ON CONFLICT (provider, artist_source_id) DO UPDATE SET
					enabled           = excluded.enabled,
					auto_download     = excluded.auto_download,
					release_filter    = excluded.release_filter,
					download_priority = excluded.download_priority,
					artist_name       = CASE WHEN $4::text <> '' THEN $4::text ELSE artist_subscriptions.artist_name END,
					artist_image_url  = CASE WHEN $5::text <> '' THEN $5::text ELSE artist_subscriptions.artist_image_url END,
					updated_at        = excluded.updated_at`,
				music.NewID(), strings.TrimSpace(item.Provider), strings.TrimSpace(item.ArtistSourceID),
				name, image, enabled, item.AutoDownload, now, music.UnknownArtist, filterJSON, priority.Rank())
			if err != nil {
				result.Failed++
				result.Errors = append(result.Errors, subscriptions.ImportError{
					Index:          i,
					ArtistName:     item.ArtistName,
					Provider:       item.Provider,
					ArtistSourceID: item.ArtistSourceID,
					Error:          err.Error(),
				})
				return wrapDB("insert subscription import", err)
			}
			affected, _ := res.RowsAffected()
			if affected > 0 {
				result.Created++
			}
		}

		for _, up := range updates {
			filter := up.ReleaseFilter
			if !filter.Any() {
				filter = music.DefaultReleaseFilter()
			}
			filterJSON, err := json.Marshal(filter)
			if err != nil {
				return wrapDB("marshal release filter", err)
			}
			name := strings.TrimSpace(up.ArtistName)
			image := strings.TrimSpace(up.ArtistImageURL)

			res, err := tx.ExecContext(ctx, `
				UPDATE artist_subscriptions SET
					enabled           = $1,
					auto_download     = $2,
					release_filter    = $3,
					download_priority = $4,
					artist_name       = CASE WHEN $5::text <> '' THEN $5::text ELSE artist_subscriptions.artist_name END,
					artist_image_url  = CASE WHEN $6::text <> '' THEN $6::text ELSE artist_subscriptions.artist_image_url END,
					updated_at        = $7
				WHERE id = $8`,
				up.Enabled, up.AutoDownload, filterJSON, up.DownloadPriority.Rank(), name, image, now, up.ID)
			if err != nil {
				result.Failed++
				result.Errors = append(result.Errors, subscriptions.ImportError{
					ArtistName: up.ArtistName,
					Error:      err.Error(),
				})
				return wrapDB("update subscription import", err)
			}
			affected, _ := res.RowsAffected()
			if affected > 0 {
				result.Updated++
			} else {
				result.Unchanged++
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}

func collectSubscriptions(rows *sql.Rows, operation string) ([]subscriptions.Subscription, error) {
	out := make([]subscriptions.Subscription, 0, 16)
	for rows.Next() {
		sub, err := scanSubscription(rows.Scan)
		if err != nil {
			return nil, wrapDB("scan subscription", err)
		}
		out = append(out, sub)
	}
	if err := rows.Err(); err != nil {
		return nil, wrapDB(operation, err)
	}
	return out, nil
}

func subscriptionNotFound(id string) error {
	return apperr.Newf(apperr.CodeSubscriptionNotFound, "Subscription %q does not exist.", id)
}
