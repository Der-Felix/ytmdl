package repository

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/jackc/pgx/v5/pgconn"

	"ytdm/backend/internal/apperr"
	"ytdm/backend/internal/database"
	"ytdm/backend/internal/music"
)

// Placements records the further releases a recording is placed on. The home
// release of a recording is tracks.release_id with its files; a placement is
// a tagged copy of one of those files in another release's folder.
type Placements struct {
	db *database.DB
}

// NewPlacements returns a placement repository.
func NewPlacements(db *database.DB) *Placements { return &Placements{db: db} }

const placementColumns = `p.id, p.track_id, p.release_id, p.path, p.size_bytes, p.sha256,
	p.disc_number, p.track_number, p.title, p.source_file_id, r.provider, r.source_id,
	p.created_at, p.updated_at`

const placementFrom = ` FROM track_placements p JOIN releases r ON r.id = p.release_id`

func scanPlacement(scan func(dest ...any) error) (music.Placement, error) {
	var (
		p            music.Placement
		sourceFileID sql.NullString
	)
	err := scan(&p.ID, &p.TrackID, &p.ReleaseID, &p.Path, &p.SizeBytes, &p.SHA256,
		&p.DiscNumber, &p.TrackNumber, &p.Title, &sourceFileID, &p.ReleaseProvider,
		&p.ReleaseSourceID, &p.CreatedAt, &p.UpdatedAt)
	if err != nil {
		return music.Placement{}, err
	}
	p.SourceFileID = stringOf(sourceFileID)
	p.CreatedAt = p.CreatedAt.UTC()
	p.UpdatedAt = p.UpdatedAt.UTC()
	return p, nil
}

// Persist writes the artist, the release and the placement in one
// transaction, exactly as PersistDownload does for a download, so a placement
// never points at a release that is missing. Placing the same recording on
// the same release position again updates the existing row.
func (r *Placements) Persist(ctx context.Context, entry music.PlacementEntry) (music.Placement, error) {
	placement := entry.Placement
	if placement.TrackID == "" || placement.Path == "" {
		return music.Placement{}, apperr.New(apperr.CodeInvalidRequest, "A placement needs a track and a path.")
	}
	if entry.Release.SourceID == "" {
		return music.Placement{}, apperr.New(apperr.CodeInvalidRequest, "A placement needs a release with a source id.")
	}

	err := r.db.WithTx(ctx, func(tx *sql.Tx) error {
		var artistID string
		if entry.Artist != nil {
			artist, err := upsertArtist(ctx, tx, *entry.Artist)
			if err != nil {
				return err
			}
			artistID = artist.ID
		}
		release, err := upsertRelease(ctx, tx, entry.Release, artistID)
		if err != nil {
			return err
		}
		placement.ReleaseID = release.ID
		placement.ReleaseProvider = release.Provider
		placement.ReleaseSourceID = release.SourceID

		id := placement.ID
		if id == "" {
			id = music.NewID()
		}
		now := time.Now().UTC()
		row := tx.QueryRowContext(ctx, `
			INSERT INTO track_placements (id, track_id, release_id, path, size_bytes, sha256,
				disc_number, track_number, title, source_file_id, created_at, updated_at)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $11)
			ON CONFLICT (track_id, release_id, disc_number, track_number) DO UPDATE SET
				path           = excluded.path,
				size_bytes     = excluded.size_bytes,
				sha256         = excluded.sha256,
				title          = excluded.title,
				source_file_id = excluded.source_file_id,
				updated_at     = excluded.updated_at
			RETURNING id, created_at`,
			id, placement.TrackID, placement.ReleaseID, placement.Path, placement.SizeBytes,
			placement.SHA256, placement.DiscNumber, placement.TrackNumber, placement.Title,
			nullString(placement.SourceFileID), now)
		if err := row.Scan(&placement.ID, &placement.CreatedAt); err != nil {
			var pgErr *pgconn.PgError
			if errors.As(err, &pgErr) && pgErr.Code == "23505" {
				return apperr.Newf(apperr.CodePathConflict, "Another placement already uses %q.", placement.Path)
			}
			return wrapDB("upsert placement", err)
		}
		placement.CreatedAt = placement.CreatedAt.UTC()
		placement.UpdatedAt = now
		return nil
	})
	if err != nil {
		return music.Placement{}, err
	}
	return placement, nil
}

// FindByTrackAndRelease returns the placements of a recording on one release.
// A recording can sit on a release more than once (a bonus disc repeating a
// track), so the answer is a list.
func (r *Placements) FindByTrackAndRelease(ctx context.Context, trackID, releaseID string) ([]music.Placement, error) {
	return r.list(ctx, "find placements by track and release",
		` WHERE p.track_id = $1 AND p.release_id = $2`, trackID, releaseID)
}

// ListByRelease returns every placement that lives in a release.
func (r *Placements) ListByRelease(ctx context.Context, releaseID string) ([]music.Placement, error) {
	return r.list(ctx, "list placements by release", ` WHERE p.release_id = $1`, releaseID)
}

// ListByTrack returns every further release a recording is placed on.
func (r *Placements) ListByTrack(ctx context.Context, trackID string) ([]music.Placement, error) {
	return r.list(ctx, "list placements by track", ` WHERE p.track_id = $1`, trackID)
}

// ListAll returns every placement, for library-wide maintenance.
func (r *Placements) ListAll(ctx context.Context) ([]music.Placement, error) {
	return r.list(ctx, "list all placements", ``)
}

// FindByPath answers whether a library path is a placement.
func (r *Placements) FindByPath(ctx context.Context, path string) (*music.Placement, error) {
	row := r.db.QueryRowContext(ctx, `SELECT `+placementColumns+placementFrom+` WHERE p.path = $1`, path)
	placement, err := scanPlacement(row.Scan)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, wrapDB("find placement by path", err)
	}
	return &placement, nil
}

// Delete removes a placement row. The file on disk is the caller's business.
func (r *Placements) Delete(ctx context.Context, id string) error {
	if _, err := r.db.ExecContext(ctx, `DELETE FROM track_placements WHERE id = $1`, id); err != nil {
		return wrapDB("delete placement", err)
	}
	return nil
}

// HomeRelease returns the release a recording's own files belong to
// (tracks.release_id), or nil when the recording has none. The worker compares
// it with a requested release to tell "this release already holds the
// recording" apart from "this is a further release".
func (r *Placements) HomeRelease(ctx context.Context, trackID string) (*music.Release, error) {
	row := r.db.QueryRowContext(ctx, `SELECT `+releaseColumns+` FROM releases
		WHERE id = (SELECT release_id FROM tracks WHERE id = $1)`, trackID)
	release, err := scanRelease(row.Scan)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, wrapDB("find home release", err)
	}
	return &release, nil
}

func (r *Placements) list(ctx context.Context, op, where string, args ...any) ([]music.Placement, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT `+placementColumns+placementFrom+where+` ORDER BY p.path`, args...)
	if err != nil {
		return nil, wrapDB(op, err)
	}
	defer rows.Close()

	out := make([]music.Placement, 0, 4)
	for rows.Next() {
		placement, err := scanPlacement(rows.Scan)
		if err != nil {
			return nil, wrapDB(op, err)
		}
		out = append(out, placement)
	}
	if err := rows.Err(); err != nil {
		return nil, wrapDB(op, err)
	}
	return out, nil
}
