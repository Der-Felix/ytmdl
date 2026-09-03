package repository

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"ytdm/backend/internal/apperr"
	"ytdm/backend/internal/database"
	"ytdm/backend/internal/music"
)

// Files records the audio files that exist in the library.
type Files struct {
	db *database.DB
}

// NewFiles returns a file repository.
func NewFiles(db *database.DB) *Files { return &Files{db: db} }

const fileColumns = `id, track_id, path, size_bytes, codec, container, bitrate_kbps,
	sample_rate, channels, duration_ms, source_provider, source_id, source_url,
	created_at, updated_at`

func scanFile(scan func(dest ...any) error) (music.File, error) {
	var (
		f       music.File
		trackID sql.NullString
	)
	err := scan(&f.ID, &trackID, &f.Path, &f.SizeBytes, &f.Codec, &f.Container,
		&f.BitrateKbps, &f.SampleRate, &f.Channels, &f.DurationMS,
		&f.SourceProvider, &f.SourceID, &f.SourceURL, &f.CreatedAt, &f.UpdatedAt)
	if err != nil {
		return music.File{}, err
	}
	f.TrackID = stringOf(trackID)
	f.CreatedAt = f.CreatedAt.UTC()
	f.UpdatedAt = f.UpdatedAt.UTC()
	return f, nil
}

// Upsert stores a library file. The path is the unique key, so re-downloading
// the same target updates the existing record instead of creating a second one.
func (r *Files) Upsert(ctx context.Context, file music.File) (music.File, error) {
	return upsertFile(ctx, r.db, file)
}

// upsertFile is the statement behind Upsert. It takes an executor so that the
// same write can take part in a larger transaction.
func upsertFile(ctx context.Context, exec executor, file music.File) (music.File, error) {
	if file.Path == "" {
		return music.File{}, apperr.New(apperr.CodeInvalidRequest, "A library file needs a path.")
	}
	id := file.ID
	if id == "" {
		id = music.NewID()
	}
	now := time.Now().UTC()

	row := exec.QueryRowContext(ctx, `
		INSERT INTO files (id, track_id, path, size_bytes, codec, container, bitrate_kbps,
			sample_rate, channels, duration_ms, source_provider, source_id, source_url,
			created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $14)
		ON CONFLICT (path) DO UPDATE SET
			track_id        = COALESCE(excluded.track_id, files.track_id),
			size_bytes      = excluded.size_bytes,
			codec           = excluded.codec,
			container       = excluded.container,
			bitrate_kbps    = excluded.bitrate_kbps,
			sample_rate     = excluded.sample_rate,
			channels        = excluded.channels,
			duration_ms     = excluded.duration_ms,
			source_provider = excluded.source_provider,
			source_id       = excluded.source_id,
			source_url      = excluded.source_url,
			updated_at      = excluded.updated_at
		RETURNING id`,
		id, nullString(file.TrackID), file.Path, file.SizeBytes, file.Codec, file.Container,
		file.BitrateKbps, file.SampleRate, file.Channels, file.DurationMS,
		file.SourceProvider, file.SourceID, file.SourceURL, now)

	if err := row.Scan(&id); err != nil {
		return music.File{}, wrapDB("upsert file", err)
	}
	file.ID = id
	file.CreatedAt, file.UpdatedAt = now, now
	return file, nil
}

// FindByID looks a file up by its unique ID.
func (r *Files) FindByID(ctx context.Context, id string) (*music.File, error) {
	row := r.db.QueryRowContext(ctx, `SELECT `+fileColumns+` FROM files WHERE id = $1`, id)
	file, err := scanFile(row.Scan)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, wrapDB("find file by id", err)
	}
	return &file, nil
}

// FindByPath looks a file up by its library relative path.
func (r *Files) FindByPath(ctx context.Context, path string) (*music.File, error) {
	row := r.db.QueryRowContext(ctx, `SELECT `+fileColumns+` FROM files WHERE path = $1`, path)
	file, err := scanFile(row.Scan)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, wrapDB("find file by path", err)
	}
	return &file, nil
}

// ListByTrack returns every file that belongs to a track.
func (r *Files) ListByTrack(ctx context.Context, trackID string) ([]music.File, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT `+fileColumns+` FROM files WHERE track_id = $1 ORDER BY path`, trackID)
	if err != nil {
		return nil, wrapDB("list files by track", err)
	}
	defer rows.Close()

	out := make([]music.File, 0, 2)
	for rows.Next() {
		file, err := scanFile(rows.Scan)
		if err != nil {
			return nil, wrapDB("scan file", err)
		}
		out = append(out, file)
	}
	if err := rows.Err(); err != nil {
		return nil, wrapDB("list files by track", err)
	}
	return out, nil
}

// ListAll returns every file record in the library.
func (r *Files) ListAll(ctx context.Context) ([]music.File, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT `+fileColumns+` FROM files ORDER BY path`)
	if err != nil {
		return nil, wrapDB("list all files", err)
	}
	defer rows.Close()

	out := make([]music.File, 0, 64)
	for rows.Next() {
		file, err := scanFile(rows.Scan)
		if err != nil {
			return nil, wrapDB("scan file", err)
		}
		out = append(out, file)
	}
	if err := rows.Err(); err != nil {
		return nil, wrapDB("list all files", err)
	}
	return out, nil
}

// Delete removes a file record.
func (r *Files) Delete(ctx context.Context, id string) error {
	if _, err := r.db.ExecContext(ctx, `DELETE FROM files WHERE id = $1`, id); err != nil {
		return wrapDB("delete file", err)
	}
	return nil
}

// DeleteByTrack removes all file records associated with a track ID.
func (r *Files) DeleteByTrack(ctx context.Context, trackID string) error {
	if _, err := r.db.ExecContext(ctx, `DELETE FROM files WHERE track_id = $1`, trackID); err != nil {
		return wrapDB("delete files by track", err)
	}
	return nil
}

// DeleteByPath removes a file record by its library-relative path.
func (r *Files) DeleteByPath(ctx context.Context, path string) error {
	if _, err := r.db.ExecContext(ctx, `DELETE FROM files WHERE path = $1`, path); err != nil {
		return wrapDB("delete file by path", err)
	}
	return nil
}
