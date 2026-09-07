package repository

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"

	"ytdm/backend/internal/apperr"
	"ytdm/backend/internal/database"
	"ytdm/backend/internal/music"
)

// Playlist represents a user-created collection of tracks.
type Playlist struct {
	ID          string    `json:"id"`
	UserID      string    `json:"user_id"`
	Name        string    `json:"name"`
	Description string    `json:"description"`
	TrackCount  int       `json:"track_count"`
	DurationMS  int       `json:"duration_ms"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// PlaylistTrack represents a track membership within a playlist, preserving position.
type PlaylistTrack struct {
	music.LibraryTrack
	Position int       `json:"position"`
	AddedAt  time.Time `json:"added_at"`
}

// PlaylistDetail bundles a playlist with its ordered tracks.
type PlaylistDetail struct {
	Playlist
	Tracks []PlaylistTrack `json:"tracks"`
}

// Playlists manages storage and ordering of user playlists and track favorites.
type Playlists struct {
	db *database.DB
}

// NewPlaylists returns a new playlists repository.
func NewPlaylists(db *database.DB) *Playlists {
	return &Playlists{db: db}
}

// CreatePlaylist creates a new playlist for the given user.
func (r *Playlists) CreatePlaylist(ctx context.Context, userID, name, description string) (Playlist, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return Playlist{}, apperr.New(apperr.CodeInvalidRequest, "Playlist-Name darf nicht leer sein.")
	}
	if len(name) > 128 {
		return Playlist{}, apperr.New(apperr.CodeInvalidRequest, "Playlist-Name ist zu lang (maximal 128 Zeichen).")
	}

	id := music.NewID()
	now := time.Now().UTC()

	query := `
		INSERT INTO playlists (id, user_id, name, description, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $5)`
	if _, err := r.db.ExecContext(ctx, query, id, userID, name, strings.TrimSpace(description), now); err != nil {
		return Playlist{}, wrapDB("create playlist", err)
	}

	return Playlist{
		ID:          id,
		UserID:      userID,
		Name:        name,
		Description: strings.TrimSpace(description),
		TrackCount:  0,
		DurationMS:  0,
		CreatedAt:   now,
		UpdatedAt:   now,
	}, nil
}

// ListPlaylistsForUser returns all playlists belonging to the user.
func (r *Playlists) ListPlaylistsForUser(ctx context.Context, userID string) ([]Playlist, error) {
	query := `
		SELECT
			p.id, p.user_id, p.name, p.description, p.created_at, p.updated_at,
			COUNT(pt.track_id) AS track_count,
			COALESCE(SUM(t.duration_ms), 0) AS duration_ms
		FROM playlists p
		LEFT JOIN playlist_tracks pt ON pt.playlist_id = p.id
		LEFT JOIN tracks t ON t.id = pt.track_id
		WHERE p.user_id = $1
		GROUP BY p.id
		ORDER BY p.updated_at DESC, p.created_at DESC`

	rows, err := r.db.QueryContext(ctx, query, userID)
	if err != nil {
		return nil, wrapDB("list playlists", err)
	}
	defer rows.Close()

	out := make([]Playlist, 0)
	for rows.Next() {
		var p Playlist
		if err := rows.Scan(
			&p.ID, &p.UserID, &p.Name, &p.Description,
			&p.CreatedAt, &p.UpdatedAt,
			&p.TrackCount, &p.DurationMS,
		); err != nil {
			return nil, wrapDB("scan playlist", err)
		}
		p.CreatedAt = p.CreatedAt.UTC()
		p.UpdatedAt = p.UpdatedAt.UTC()
		out = append(out, p)
	}
	if err := rows.Err(); err != nil {
		return nil, wrapDB("list playlists rows", err)
	}
	return out, nil
}

// GetPlaylistForUser returns the playlist metadata and ordered tracks.
func (r *Playlists) GetPlaylistForUser(ctx context.Context, userID, playlistID string) (PlaylistDetail, error) {
	plQuery := `
		SELECT
			p.id, p.user_id, p.name, p.description, p.created_at, p.updated_at,
			COUNT(pt.track_id) AS track_count,
			COALESCE(SUM(t.duration_ms), 0) AS duration_ms
		FROM playlists p
		LEFT JOIN playlist_tracks pt ON pt.playlist_id = p.id
		LEFT JOIN tracks t ON t.id = pt.track_id
		WHERE p.id = $1 AND p.user_id = $2
		GROUP BY p.id`

	var p Playlist
	err := r.db.QueryRowContext(ctx, plQuery, playlistID, userID).Scan(
		&p.ID, &p.UserID, &p.Name, &p.Description,
		&p.CreatedAt, &p.UpdatedAt,
		&p.TrackCount, &p.DurationMS,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return PlaylistDetail{}, apperr.New(apperr.CodePlaylistNotFound, "Playlist nicht gefunden.")
		}
		return PlaylistDetail{}, wrapDB("get playlist", err)
	}
	p.CreatedAt = p.CreatedAt.UTC()
	p.UpdatedAt = p.UpdatedAt.UTC()

	trkQuery := `
		SELECT
			t.id, t.release_id, t.artist_id, t.title, t.artists_json, t.album, t.album_artist,
			t.track_number, t.track_total, t.disc_number, t.disc_total, t.duration_ms, t.year,
			t.isrc, t.cover_url, t.identity_key, t.compilation, t.lyrics_state, t.lyrics_provider,
			t.lyrics_checked_at, t.created_at,
			COALESCE(f.path, ''), COALESCE(f.size_bytes, 0), COALESCE(f.codec, ''), COALESCE(f.bitrate_kbps, 0),
			ROW_NUMBER() OVER (ORDER BY pt.position ASC)::integer AS position, pt.added_at
		FROM playlist_tracks pt
		JOIN tracks t ON t.id = pt.track_id
		LEFT JOIN files f ON f.track_id = t.id
		WHERE pt.playlist_id = $1
		ORDER BY pt.position ASC`

	rows, err := r.db.QueryContext(ctx, trkQuery, playlistID)
	if err != nil {
		return PlaylistDetail{}, wrapDB("get playlist tracks", err)
	}
	defer rows.Close()

	tracks := make([]PlaylistTrack, 0, p.TrackCount)
	for rows.Next() {
		var (
			pt          PlaylistTrack
			releaseID   sql.NullString
			artistID    sql.NullString
			artistsJSON string
			identityKey string
			lyricsState string
			checkedAt   sql.NullTime
			createdAt   time.Time
			addedAt     time.Time
		)
		if err := rows.Scan(
			&pt.ID, &releaseID, &artistID, &pt.Title, &artistsJSON, &pt.Album, &pt.AlbumArtist,
			&pt.TrackNumber, &pt.TrackTotal, &pt.DiscNumber, &pt.DiscTotal, &pt.DurationMS, &pt.Year,
			&pt.ISRC, &pt.CoverURL, &identityKey, &pt.Compilation, &lyricsState, &pt.LyricsProvider,
			&checkedAt, &createdAt,
			&pt.FilePath, &pt.FileSizeBytes, &pt.Codec, &pt.BitrateKbps,
			&pt.Position, &addedAt,
		); err != nil {
			return PlaylistDetail{}, wrapDB("scan playlist track", err)
		}
		pt.ReleaseID = stringOf(releaseID)
		pt.Artists = decodeStrings(artistsJSON)
		pt.LyricsState = music.LyricsState(lyricsState)
		if checkedAt.Valid {
			t := checkedAt.Time.UTC()
			pt.LyricsCheckedAt = &t
		}
		pt.CreatedAt = createdAt.UTC()
		pt.AddedAt = addedAt.UTC()
		tracks = append(tracks, pt)
	}
	if err := rows.Err(); err != nil {
		return PlaylistDetail{}, wrapDB("get playlist tracks rows", err)
	}

	return PlaylistDetail{
		Playlist: p,
		Tracks:   tracks,
	}, nil
}

// UpdatePlaylist updates name and description for a user's playlist.
func (r *Playlists) UpdatePlaylist(ctx context.Context, userID, playlistID, name, description string) (Playlist, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return Playlist{}, apperr.New(apperr.CodeInvalidRequest, "Playlist-Name darf nicht leer sein.")
	}
	if len(name) > 128 {
		return Playlist{}, apperr.New(apperr.CodeInvalidRequest, "Playlist-Name ist zu lang (maximal 128 Zeichen).")
	}

	now := time.Now().UTC()
	query := `
		UPDATE playlists
		SET name = $3, description = $4, updated_at = $5
		WHERE id = $1 AND user_id = $2`

	res, err := r.db.ExecContext(ctx, query, playlistID, userID, name, strings.TrimSpace(description), now)
	if err != nil {
		return Playlist{}, wrapDB("update playlist", err)
	}
	rows, err := res.RowsAffected()
	if err != nil {
		return Playlist{}, wrapDB("update playlist rows", err)
	}
	if rows == 0 {
		return Playlist{}, apperr.New(apperr.CodePlaylistNotFound, "Playlist nicht gefunden.")
	}

	detail, err := r.GetPlaylistForUser(ctx, userID, playlistID)
	if err != nil {
		return Playlist{}, err
	}
	return detail.Playlist, nil
}

// DeletePlaylist removes a playlist and its track memberships.
// Library tracks and audio files remain completely untouched.
func (r *Playlists) DeletePlaylist(ctx context.Context, userID, playlistID string) error {
	query := `DELETE FROM playlists WHERE id = $1 AND user_id = $2`
	res, err := r.db.ExecContext(ctx, query, playlistID, userID)
	if err != nil {
		return wrapDB("delete playlist", err)
	}
	rows, err := res.RowsAffected()
	if err != nil {
		return wrapDB("delete playlist rows", err)
	}
	if rows == 0 {
		return apperr.New(apperr.CodePlaylistNotFound, "Playlist nicht gefunden.")
	}
	return nil
}

// AddTrack adds a library track to the playlist at the end.
func (r *Playlists) AddTrack(ctx context.Context, userID, playlistID, trackID string) (PlaylistDetail, error) {
	trackID = strings.TrimSpace(trackID)
	if trackID == "" {
		return PlaylistDetail{}, apperr.New(apperr.CodeInvalidRequest, "Track-ID ist erforderlich.")
	}

	err := r.db.WithTx(ctx, func(tx *sql.Tx) error {
		var ownerID string
		err := tx.QueryRowContext(ctx, `SELECT user_id FROM playlists WHERE id = $1 FOR UPDATE`, playlistID).Scan(&ownerID)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return apperr.New(apperr.CodePlaylistNotFound, "Playlist nicht gefunden.")
			}
			return wrapDB("lock playlist", err)
		}
		if ownerID != userID {
			return apperr.New(apperr.CodePlaylistNotFound, "Playlist nicht gefunden.")
		}

		var trackExists bool
		if err := tx.QueryRowContext(ctx, `SELECT EXISTS (SELECT 1 FROM tracks WHERE id = $1)`, trackID).Scan(&trackExists); err != nil {
			return wrapDB("check track exists", err)
		}
		if !trackExists {
			return apperr.New(apperr.CodeTrackNotFound, "Track nicht gefunden.")
		}

		var alreadyPresent bool
		if err := tx.QueryRowContext(ctx, `SELECT EXISTS (SELECT 1 FROM playlist_tracks WHERE playlist_id = $1 AND track_id = $2)`, playlistID, trackID).Scan(&alreadyPresent); err != nil {
			return wrapDB("check track in playlist", err)
		}
		if alreadyPresent {
			return apperr.New(apperr.CodeAlreadyExists, "Der Track ist bereits in dieser Playlist vorhanden.")
		}

		// Compact any gaps (e.g. from foreign key cascade track deletion) to guarantee 1..N contiguous positions
		if _, err := tx.ExecContext(ctx, `
			WITH numbered AS (
				SELECT track_id, ROW_NUMBER() OVER (ORDER BY position ASC)::integer AS new_pos
				FROM playlist_tracks
				WHERE playlist_id = $1
			)
			UPDATE playlist_tracks pt
			SET position = n.new_pos
			FROM numbered n
			WHERE pt.playlist_id = $1 AND pt.track_id = n.track_id AND pt.position != n.new_pos
		`, playlistID); err != nil {
			return wrapDB("compact positions", err)
		}

		var nextPos int
		if err := tx.QueryRowContext(ctx, `SELECT COALESCE(MAX(position), 0) + 1 FROM playlist_tracks WHERE playlist_id = $1`, playlistID).Scan(&nextPos); err != nil {
			return wrapDB("get next position", err)
		}

		now := time.Now().UTC()
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO playlist_tracks (playlist_id, track_id, position, added_at)
			VALUES ($1, $2, $3, $4)
		`, playlistID, trackID, nextPos, now); err != nil {
			return wrapDB("insert playlist track", err)
		}

		if _, err := tx.ExecContext(ctx, `UPDATE playlists SET updated_at = $1 WHERE id = $2`, now, playlistID); err != nil {
			return wrapDB("update playlist timestamp", err)
		}

		return nil
	})
	if err != nil {
		return PlaylistDetail{}, err
	}

	return r.GetPlaylistForUser(ctx, userID, playlistID)
}

// RemoveTrack removes a track from the playlist and normalizes subsequent positions.
func (r *Playlists) RemoveTrack(ctx context.Context, userID, playlistID, trackID string) (PlaylistDetail, error) {
	trackID = strings.TrimSpace(trackID)
	if trackID == "" {
		return PlaylistDetail{}, apperr.New(apperr.CodeInvalidRequest, "Track-ID ist erforderlich.")
	}

	err := r.db.WithTx(ctx, func(tx *sql.Tx) error {
		var ownerID string
		err := tx.QueryRowContext(ctx, `SELECT user_id FROM playlists WHERE id = $1 FOR UPDATE`, playlistID).Scan(&ownerID)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return apperr.New(apperr.CodePlaylistNotFound, "Playlist nicht gefunden.")
			}
			return wrapDB("lock playlist", err)
		}
		if ownerID != userID {
			return apperr.New(apperr.CodePlaylistNotFound, "Playlist nicht gefunden.")
		}

		res, err := tx.ExecContext(ctx, `
			DELETE FROM playlist_tracks
			WHERE playlist_id = $1 AND track_id = $2
		`, playlistID, trackID)
		if err != nil {
			return wrapDB("delete playlist track", err)
		}
		rows, err := res.RowsAffected()
		if err != nil {
			return wrapDB("delete playlist track rows", err)
		}
		if rows == 0 {
			return apperr.New(apperr.CodeTrackNotFound, "Track ist nicht in dieser Playlist.")
		}

		now := time.Now().UTC()
		if _, err := tx.ExecContext(ctx, `UPDATE playlists SET updated_at = $1 WHERE id = $2`, now, playlistID); err != nil {
			return wrapDB("update playlist timestamp", err)
		}

		return nil
	})
	if err != nil {
		return PlaylistDetail{}, err
	}

	return r.GetPlaylistForUser(ctx, userID, playlistID)
}

// ReorderTracks atomically updates the track positions within a playlist.
// Submitted trackIDs must contain every track of the playlist with zero omissions or foreign tracks.
func (r *Playlists) ReorderTracks(ctx context.Context, userID, playlistID string, trackIDs []string) (PlaylistDetail, error) {
	if len(trackIDs) == 0 {
		return PlaylistDetail{}, apperr.New(apperr.CodeInvalidRequest, "Track-IDs dürfen nicht leer sein.")
	}

	// Validate no duplicates in input
	seen := make(map[string]struct{}, len(trackIDs))
	for _, id := range trackIDs {
		id = strings.TrimSpace(id)
		if id == "" {
			return PlaylistDetail{}, apperr.New(apperr.CodeInvalidRequest, "Ungültige Track-ID.")
		}
		if _, exists := seen[id]; exists {
			return PlaylistDetail{}, apperr.New(apperr.CodeInvalidRequest, "Doppelte Track-ID in Reorder-Anfrage.")
		}
		seen[id] = struct{}{}
	}

	err := r.db.WithTx(ctx, func(tx *sql.Tx) error {
		var ownerID string
		err := tx.QueryRowContext(ctx, `SELECT user_id FROM playlists WHERE id = $1 FOR UPDATE`, playlistID).Scan(&ownerID)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return apperr.New(apperr.CodePlaylistNotFound, "Playlist nicht gefunden.")
			}
			return wrapDB("lock playlist", err)
		}
		if ownerID != userID {
			return apperr.New(apperr.CodePlaylistNotFound, "Playlist nicht gefunden.")
		}

		// Retrieve all existing track IDs in this playlist
		rows, err := tx.QueryContext(ctx, `SELECT track_id FROM playlist_tracks WHERE playlist_id = $1`, playlistID)
		if err != nil {
			return wrapDB("query existing playlist tracks", err)
		}
		defer rows.Close()

		existingMap := make(map[string]struct{})
		for rows.Next() {
			var tid string
			if err := rows.Scan(&tid); err != nil {
				return wrapDB("scan existing track id", err)
			}
			existingMap[tid] = struct{}{}
		}
		if err := rows.Err(); err != nil {
			return wrapDB("rows existing track ids", err)
		}

		if len(existingMap) != len(trackIDs) {
			return apperr.New(apperr.CodeInvalidRequest, "Reorder-Anfrage muss alle Tracks der Playlist enthalten.")
		}

		for _, tid := range trackIDs {
			if _, ok := existingMap[tid]; !ok {
				return apperr.New(apperr.CodeInvalidRequest, "Reorder enthält Track, der nicht zur Playlist gehört.")
			}
		}

		// Update positions atomically (uq_playlist_tracks_position is DEFERRABLE INITIALLY DEFERRED)
		for idx, tid := range trackIDs {
			newPos := idx + 1
			if _, err := tx.ExecContext(ctx, `
				UPDATE playlist_tracks
				SET position = $1
				WHERE playlist_id = $2 AND track_id = $3
			`, newPos, playlistID, tid); err != nil {
				return wrapDB("update track position", err)
			}
		}

		now := time.Now().UTC()
		if _, err := tx.ExecContext(ctx, `UPDATE playlists SET updated_at = $1 WHERE id = $2`, now, playlistID); err != nil {
			return wrapDB("update playlist timestamp", err)
		}

		return nil
	})
	if err != nil {
		return PlaylistDetail{}, err
	}

	return r.GetPlaylistForUser(ctx, userID, playlistID)
}

// CompactPositions ensures persisted track positions in a playlist are strictly contiguous 1..N without gaps.
func (r *Playlists) CompactPositions(ctx context.Context, playlistID string) error {
	_, err := r.db.ExecContext(ctx, `
		WITH numbered AS (
			SELECT track_id, ROW_NUMBER() OVER (ORDER BY position ASC)::integer AS new_pos
			FROM playlist_tracks
			WHERE playlist_id = $1
		)
		UPDATE playlist_tracks pt
		SET position = n.new_pos
		FROM numbered n
		WHERE pt.playlist_id = $1 AND pt.track_id = n.track_id AND pt.position != n.new_pos
	`, playlistID)
	if err != nil {
		return wrapDB("compact positions", err)
	}
	return nil
}

// FavoriteTrack adds a track to the user's favorites (idempotent).
func (r *Playlists) FavoriteTrack(ctx context.Context, userID, trackID string) error {
	trackID = strings.TrimSpace(trackID)
	if trackID == "" {
		return apperr.New(apperr.CodeInvalidRequest, "Track-ID ist erforderlich.")
	}

	var trackExists bool
	if err := r.db.QueryRowContext(ctx, `SELECT EXISTS (SELECT 1 FROM tracks WHERE id = $1)`, trackID).Scan(&trackExists); err != nil {
		return wrapDB("check track exists", err)
	}
	if !trackExists {
		return apperr.New(apperr.CodeTrackNotFound, "Track nicht gefunden.")
	}

	now := time.Now().UTC()
	query := `
		INSERT INTO favorite_tracks (user_id, track_id, created_at)
		VALUES ($1, $2, $3)
		ON CONFLICT (user_id, track_id) DO NOTHING`
	if _, err := r.db.ExecContext(ctx, query, userID, trackID, now); err != nil {
		return wrapDB("favorite track", err)
	}
	return nil
}

// UnfavoriteTrack removes a track from the user's favorites (idempotent).
func (r *Playlists) UnfavoriteTrack(ctx context.Context, userID, trackID string) error {
	trackID = strings.TrimSpace(trackID)
	if trackID == "" {
		return apperr.New(apperr.CodeInvalidRequest, "Track-ID ist erforderlich.")
	}

	query := `DELETE FROM favorite_tracks WHERE user_id = $1 AND track_id = $2`
	if _, err := r.db.ExecContext(ctx, query, userID, trackID); err != nil {
		return wrapDB("unfavorite track", err)
	}
	return nil
}

// IsFavorite checks if a track is in the user's favorites.
func (r *Playlists) IsFavorite(ctx context.Context, userID, trackID string) (bool, error) {
	var fav bool
	err := r.db.QueryRowContext(ctx, `
		SELECT EXISTS (SELECT 1 FROM favorite_tracks WHERE user_id = $1 AND track_id = $2)
	`, userID, trackID).Scan(&fav)
	if err != nil {
		return false, wrapDB("check is favorite", err)
	}
	return fav, nil
}

// ListFavoriteTracks returns all favorite tracks for the user with full library metadata.
func (r *Playlists) ListFavoriteTracks(ctx context.Context, userID string) ([]music.LibraryTrack, error) {
	query := `
		SELECT
			t.id, t.release_id, t.artist_id, t.title, t.artists_json, t.album, t.album_artist,
			t.track_number, t.track_total, t.disc_number, t.disc_total, t.duration_ms, t.year,
			t.isrc, t.cover_url, t.identity_key, t.compilation, t.lyrics_state, t.lyrics_provider,
			t.lyrics_checked_at, t.created_at,
			COALESCE(f.path, ''), COALESCE(f.size_bytes, 0), COALESCE(f.codec, ''), COALESCE(f.bitrate_kbps, 0)
		FROM favorite_tracks ft
		JOIN tracks t ON t.id = ft.track_id
		LEFT JOIN files f ON f.track_id = t.id
		WHERE ft.user_id = $1
		ORDER BY ft.created_at DESC`

	rows, err := r.db.QueryContext(ctx, query, userID)
	if err != nil {
		return nil, wrapDB("list favorite tracks", err)
	}
	defer rows.Close()

	out := make([]music.LibraryTrack, 0)
	for rows.Next() {
		var (
			lt          music.LibraryTrack
			releaseID   sql.NullString
			artistID    sql.NullString
			artistsJSON string
			identityKey string
			lyricsState string
			checkedAt   sql.NullTime
			createdAt   time.Time
		)
		if err := rows.Scan(
			&lt.ID, &releaseID, &artistID, &lt.Title, &artistsJSON, &lt.Album, &lt.AlbumArtist,
			&lt.TrackNumber, &lt.TrackTotal, &lt.DiscNumber, &lt.DiscTotal, &lt.DurationMS, &lt.Year,
			&lt.ISRC, &lt.CoverURL, &identityKey, &lt.Compilation, &lyricsState, &lt.LyricsProvider,
			&checkedAt, &createdAt,
			&lt.FilePath, &lt.FileSizeBytes, &lt.Codec, &lt.BitrateKbps,
		); err != nil {
			return nil, wrapDB("scan favorite track", err)
		}
		lt.ReleaseID = stringOf(releaseID)
		lt.Artists = decodeStrings(artistsJSON)
		lt.LyricsState = music.LyricsState(lyricsState)
		if checkedAt.Valid {
			t := checkedAt.Time.UTC()
			lt.LyricsCheckedAt = &t
		}
		lt.CreatedAt = createdAt.UTC()
		out = append(out, lt)
	}
	if err := rows.Err(); err != nil {
		return nil, wrapDB("list favorite tracks rows", err)
	}
	return out, nil
}

// ListFavoriteTrackIDs returns a slice of all favorited track IDs for the given user.
func (r *Playlists) ListFavoriteTrackIDs(ctx context.Context, userID string) ([]string, error) {
	query := `SELECT track_id FROM favorite_tracks WHERE user_id = $1 ORDER BY created_at DESC`
	rows, err := r.db.QueryContext(ctx, query, userID)
	if err != nil {
		return nil, wrapDB("list favorite track ids", err)
	}
	defer rows.Close()

	out := make([]string, 0)
	for rows.Next() {
		var tid string
		if err := rows.Scan(&tid); err != nil {
			return nil, wrapDB("scan favorite track id", err)
		}
		out = append(out, tid)
	}
	if err := rows.Err(); err != nil {
		return nil, wrapDB("list favorite track ids rows", err)
	}
	return out, nil
}
