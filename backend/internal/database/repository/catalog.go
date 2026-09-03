package repository

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"

	"ytdm/backend/internal/apperr"
	"ytdm/backend/internal/database"
	"ytdm/backend/internal/discography"
	"ytdm/backend/internal/matcher"
	"ytdm/backend/internal/music"
)

// Catalog stores artists, releases, tracks and their external sources.
type Catalog struct {
	db *database.DB
}

// NewCatalog returns a catalogue repository.
func NewCatalog(db *database.DB) *Catalog { return &Catalog{db: db} }

// UpsertArtist inserts or refreshes an artist identified by provider and
// source id, and returns the stored record including its internal id.
func (c *Catalog) UpsertArtist(ctx context.Context, artist music.Artist) (music.Artist, error) {
	return upsertArtist(ctx, c.db, artist)
}

func upsertArtist(ctx context.Context, exec executor, artist music.Artist) (music.Artist, error) {
	now := time.Now().UTC()
	id := artist.ID
	if id == "" {
		id = music.NewID()
	}

	row := exec.QueryRowContext(ctx, `
		INSERT INTO artists (id, name, sort_key, provider, source_id, source_url, image_url, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $8)
		ON CONFLICT (provider, source_id) DO UPDATE SET
			name       = excluded.name,
			sort_key   = excluded.sort_key,
			source_url = excluded.source_url,
			image_url  = excluded.image_url,
			updated_at = excluded.updated_at
		RETURNING id`,
		id, artist.DisplayName(), matcher.NormalizeArtist(artist.DisplayName()),
		artist.Provider, artist.SourceID, artist.SourceURL, artist.ImageURL, now)

	if err := row.Scan(&id); err != nil {
		return music.Artist{}, wrapDB("upsert artist", err)
	}
	artist.ID = id
	return artist, nil
}

// GetArtist loads an artist by internal id.
func (c *Catalog) GetArtist(ctx context.Context, id string) (*music.Artist, error) {
	row := c.db.QueryRowContext(ctx, `
		SELECT id, name, provider, source_id, source_url, image_url
		FROM artists WHERE id = $1`, id)

	var a music.Artist
	if err := row.Scan(&a.ID, &a.Name, &a.Provider, &a.SourceID, &a.SourceURL, &a.ImageURL); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, apperr.Newf(apperr.CodeArtistNotFound, "Artist %q is not in the library.", id)
		}
		return nil, wrapDB("get artist", err)
	}
	return &a, nil
}

// FindArtistBySource looks an artist up by its provider identity.
func (c *Catalog) FindArtistBySource(ctx context.Context, provider, sourceID string) (*music.Artist, error) {
	row := c.db.QueryRowContext(ctx, `
		SELECT id, name, provider, source_id, source_url, image_url
		FROM artists WHERE provider = $1 AND source_id = $2`, provider, sourceID)

	var a music.Artist
	if err := row.Scan(&a.ID, &a.Name, &a.Provider, &a.SourceID, &a.SourceURL, &a.ImageURL); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, wrapDB("find artist by source", err)
	}
	return &a, nil
}

// ListArtists returns the artists in the library ordered by name.
func (c *Catalog) ListArtists(ctx context.Context, limit, offset int) ([]music.Artist, error) {
	rows, err := c.db.QueryContext(ctx, `
		SELECT id, name, provider, source_id, source_url, image_url
		FROM artists ORDER BY sort_key, name LIMIT $1 OFFSET $2`,
		clampLimit(limit, 100, 500), clampOffset(offset))
	if err != nil {
		return nil, wrapDB("list artists", err)
	}
	defer rows.Close()

	out := make([]music.Artist, 0, 32)
	for rows.Next() {
		var a music.Artist
		if err := rows.Scan(&a.ID, &a.Name, &a.Provider, &a.SourceID, &a.SourceURL, &a.ImageURL); err != nil {
			return nil, wrapDB("scan artist", err)
		}
		out = append(out, a)
	}
	if err := rows.Err(); err != nil {
		return nil, wrapDB("list artists", err)
	}
	return out, nil
}

// releaseTypeOf returns the stored release type. The column carries a CHECK
// constraint, so an unclassified release is filed as an album rather than
// written as an empty string.
func releaseTypeOf(t music.ReleaseType) string {
	if !t.Valid() {
		return string(music.ReleaseAlbum)
	}
	return string(t)
}

// UpsertRelease inserts or refreshes a release and returns it with its
// internal id. artistID may be empty when the artist is unknown locally.
func (c *Catalog) UpsertRelease(ctx context.Context, release music.Release, artistID string) (music.Release, error) {
	return upsertRelease(ctx, c.db, release, artistID)
}

func upsertRelease(ctx context.Context, exec executor, release music.Release, artistID string) (music.Release, error) {
	now := time.Now().UTC()
	id := release.ID
	if id == "" {
		id = music.NewID()
	}

	row := exec.QueryRowContext(ctx, `
		INSERT INTO releases (id, artist_id, title, artists_json, album_artist, release_type,
			year, release_date, track_count, cover_url, provider, source_id, source_url,
			compilation, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $15)
		ON CONFLICT (provider, source_id) DO UPDATE SET
			artist_id    = COALESCE(excluded.artist_id, releases.artist_id),
			title        = excluded.title,
			artists_json = excluded.artists_json,
			album_artist = excluded.album_artist,
			release_type = excluded.release_type,
			year         = excluded.year,
			release_date = excluded.release_date,
			track_count  = excluded.track_count,
			cover_url    = excluded.cover_url,
			source_url   = excluded.source_url,
			compilation  = excluded.compilation,
			updated_at   = excluded.updated_at
		RETURNING id`,
		id, nullString(artistID), release.DisplayTitle(), encodeStrings(release.Artists),
		release.DisplayAlbumArtist(), releaseTypeOf(release.ReleaseType), release.Year,
		release.ReleaseDate, release.TrackCount, release.CoverURL,
		release.Provider, release.SourceID, release.SourceURL, release.Compilation, now)

	if err := row.Scan(&id); err != nil {
		return music.Release{}, wrapDB("upsert release", err)
	}
	release.ID = id
	return release, nil
}

const releaseColumns = `id, title, artists_json, album_artist, release_type, year,
	release_date, track_count, cover_url, provider, source_id, source_url, compilation`

func scanRelease(scan func(dest ...any) error) (music.Release, error) {
	var (
		r           music.Release
		artistsJSON string
		releaseType string
	)
	err := scan(&r.ID, &r.Title, &artistsJSON, &r.AlbumArtist, &releaseType, &r.Year,
		&r.ReleaseDate, &r.TrackCount, &r.CoverURL, &r.Provider, &r.SourceID, &r.SourceURL,
		&r.Compilation)
	if err != nil {
		return music.Release{}, err
	}
	r.Artists = decodeStrings(artistsJSON)
	r.ReleaseType = music.ReleaseType(releaseType)
	return r, nil
}

// GetRelease loads a release by internal id.
func (c *Catalog) GetRelease(ctx context.Context, id string) (*music.Release, error) {
	row := c.db.QueryRowContext(ctx, `SELECT `+releaseColumns+` FROM releases WHERE id = $1`, id)
	release, err := scanRelease(row.Scan)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, apperr.Newf(apperr.CodeReleaseNotFound, "Release %q is not in the library.", id)
		}
		return nil, wrapDB("get release", err)
	}
	return &release, nil
}

// UpdateReleaseCover updates the cover URL for a release and cascades to its tracks.
func (c *Catalog) UpdateReleaseCover(ctx context.Context, releaseID string, coverURL string) error {
	now := time.Now().UTC()
	tx, err := c.db.BeginTx(ctx, nil)
	if err != nil {
		return wrapDB("update release cover begin", err)
	}
	defer tx.Rollback()

	if _, err := tx.ExecContext(ctx,
		`UPDATE releases SET cover_url = $2, updated_at = $3 WHERE id = $1`,
		releaseID, coverURL, now); err != nil {
		return wrapDB("update release cover", err)
	}

	if _, err := tx.ExecContext(ctx,
		`UPDATE tracks SET cover_url = $2, updated_at = $3 WHERE release_id = $1`,
		releaseID, coverURL, now); err != nil {
		return wrapDB("update tracks cover", err)
	}

	return tx.Commit()
}

// FindReleaseBySource looks a release up by its provider identity. A release
// the library does not hold yields nil rather than an error, because "not in
// the library yet" is the normal answer during a subscription sync.
func (c *Catalog) FindReleaseBySource(ctx context.Context, provider, sourceID string) (*music.Release, error) {
	row := c.db.QueryRowContext(ctx,
		`SELECT `+releaseColumns+` FROM releases WHERE provider = $1 AND source_id = $2`,
		provider, sourceID)

	release, err := scanRelease(row.Scan)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, wrapDB("find release by source", err)
	}
	return &release, nil
}

// ListReleases returns releases, optionally restricted to one artist.
func (c *Catalog) ListReleases(ctx context.Context, artistID string, limit, offset int) ([]music.Release, error) {
	query := `SELECT ` + releaseColumns + ` FROM releases WHERE ($1::text = '' OR artist_id = $1)
		ORDER BY year, title LIMIT $2 OFFSET $3`

	rows, err := c.db.QueryContext(ctx, query,
		artistID, clampLimit(limit, 100, 500), clampOffset(offset))
	if err != nil {
		return nil, wrapDB("list releases", err)
	}
	defer rows.Close()

	out := make([]music.Release, 0, 32)
	for rows.Next() {
		release, err := scanRelease(rows.Scan)
		if err != nil {
			return nil, wrapDB("scan release", err)
		}
		out = append(out, release)
	}
	if err := rows.Err(); err != nil {
		return nil, wrapDB("list releases", err)
	}
	return out, nil
}

// ListAllReleases returns all releases in the database for library-wide maintenance.
func (c *Catalog) ListAllReleases(ctx context.Context) ([]music.Release, error) {
	query := `SELECT ` + releaseColumns + ` FROM releases ORDER BY title, year`
	rows, err := c.db.QueryContext(ctx, query)
	if err != nil {
		return nil, wrapDB("list all releases", err)
	}
	defer rows.Close()

	var out []music.Release
	for rows.Next() {
		release, err := scanRelease(rows.Scan)
		if err != nil {
			return nil, wrapDB("scan release", err)
		}
		out = append(out, release)
	}
	if err := rows.Err(); err != nil {
		return nil, wrapDB("list all releases", err)
	}
	return out, nil
}

const trackColumns = `tracks.id, tracks.release_id, tracks.title, tracks.artists_json, tracks.album, tracks.album_artist,
	tracks.track_number, tracks.track_total, tracks.disc_number, tracks.disc_total, tracks.duration_ms, tracks.year, tracks.isrc,
	tracks.cover_url, tracks.identity_key, tracks.compilation, tracks.lyrics_state, tracks.lyrics_provider, tracks.lyrics_checked_at,
	COALESCE(releases.release_type, '')`

// StoredTrack is a track as it exists in the library, including the internal
// identity key it was stored under.
type StoredTrack struct {
	Track       music.Track
	IdentityKey string
}

func scanTrack(scan func(dest ...any) error) (StoredTrack, error) {
	var (
		st          StoredTrack
		releaseID   sql.NullString
		artistsJSON string
		lyricsState string
		checkedAt   sql.NullTime
		releaseType sql.NullString
	)
	err := scan(&st.Track.ID, &releaseID, &st.Track.Title, &artistsJSON, &st.Track.Album,
		&st.Track.AlbumArtist, &st.Track.TrackNumber, &st.Track.TrackTotal,
		&st.Track.DiscNumber, &st.Track.DiscTotal, &st.Track.DurationMS, &st.Track.Year,
		&st.Track.ISRC, &st.Track.CoverURL, &st.IdentityKey, &st.Track.Compilation,
		&lyricsState, &st.Track.LyricsProvider, &checkedAt, &releaseType)
	if err != nil {
		return StoredTrack{}, err
	}
	st.Track.Artists = decodeStrings(artistsJSON)
	st.Track.ReleaseID = stringOf(releaseID)
	st.Track.LyricsState = music.LyricsState(lyricsState)
	if checkedAt.Valid {
		when := checkedAt.Time.UTC()
		st.Track.LyricsCheckedAt = &when
	}
	st.Track.ReleaseType = music.ReleaseType(stringOf(releaseType))
	return st, nil
}

// UpsertTrack stores a track. An existing recording is recognised by its ISRC
// first and by identity key plus runtime second, mirroring the deduplication
// rules so that the library never holds the same recording twice.
func (c *Catalog) UpsertTrack(ctx context.Context, track music.Track, releaseID, artistID string, toleranceMS int) (music.Track, error) {
	var stored music.Track
	err := c.db.WithTx(ctx, func(tx *sql.Tx) error {
		var err error
		stored, err = upsertTrack(ctx, tx, track, releaseID, artistID, toleranceMS)
		return err
	})
	if err != nil {
		return music.Track{}, err
	}
	return stored, nil
}

// upsertTrack must run inside a transaction: it takes advisory locks that are
// released when that transaction ends.
func upsertTrack(ctx context.Context, tx executor, track music.Track, releaseID, artistID string, toleranceMS int) (music.Track, error) {
	identity := discography.IdentityKey(track)
	isrc := discography.NormalizeISRC(track.ISRC)
	now := time.Now().UTC()

	// Looking a recording up and then inserting it is a read-modify-write that
	// two workers can run at the same moment for the same track. The advisory
	// locks below serialise exactly those two workers — they are keyed by the
	// identifiers the lookup uses and are released with the transaction, so no
	// unrelated write is blocked.
	if track.ID != "" {
		if err := lockTrackIdentity(ctx, tx, "id:", track.ID); err != nil {
			return music.Track{}, err
		}
	}
	if err := lockTrackIdentity(ctx, tx, "isrc:", isrc); err != nil {
		return music.Track{}, err
	}
	if err := lockTrackIdentity(ctx, tx, "identity:", identity); err != nil {
		return music.Track{}, err
	}

	existingID, err := findTrackID(ctx, tx, track.ID, identity, isrc, track.DurationMS, toleranceMS)
	if err != nil {
		return music.Track{}, err
	}

	if existingID == "" {
		existingID = track.ID
		if existingID == "" {
			existingID = music.NewID()
		}
		_, err = tx.ExecContext(ctx, `
			INSERT INTO tracks (id, release_id, artist_id, title, artists_json, album,
				album_artist, track_number, track_total, disc_number, disc_total,
				duration_ms, year, isrc, cover_url, identity_key, compilation,
				lyrics_state, lyrics_provider, lyrics_checked_at, created_at, updated_at)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16,
				$17, $18, $19, $20, $21, $21)`,
			existingID, nullString(releaseID), nullString(artistID), track.DisplayTitle(),
			encodeStrings(track.Artists), track.Album, track.DisplayAlbumArtist(),
			track.TrackNumber, track.TrackTotal, track.DiscNumber, track.DiscTotal,
			track.DurationMS, track.Year, isrc, track.CoverURL, identity, track.Compilation,
			string(track.DisplayLyricsState()), track.LyricsProvider,
			nullTime(track.LyricsCheckedAt), now)
		if err != nil {
			return music.Track{}, wrapDB("insert track", err)
		}
	} else {
		_, err = tx.ExecContext(ctx, `
			UPDATE tracks SET
				release_id   = COALESCE(release_id, $1),
				artist_id    = COALESCE(artist_id, $2),
				title        = $3,
				artists_json = $4,
				album        = $5,
				album_artist = $6,
				track_number = $7,
				track_total  = $8,
				disc_number  = $9,
				disc_total   = $10,
				duration_ms  = $11,
				year         = $12,
				isrc         = CASE WHEN $13::text <> '' THEN $13::text ELSE isrc END,
				cover_url    = $14,
				compilation  = $15,
				-- A caller that knows nothing about lyrics must not erase what
				-- a lookup already established, so "unknown" leaves the stored
				-- state and its timestamp alone.
				lyrics_state = CASE WHEN $16::text = 'unknown' THEN lyrics_state ELSE $16::text END,
				lyrics_provider = CASE WHEN $16::text = 'unknown' THEN lyrics_provider ELSE $17::text END,
				lyrics_checked_at = CASE WHEN $16::text = 'unknown' THEN lyrics_checked_at ELSE $18::timestamptz END,
				updated_at   = $19
			WHERE id = $20`,
			nullString(releaseID), nullString(artistID), track.DisplayTitle(),
			encodeStrings(track.Artists), track.Album, track.DisplayAlbumArtist(),
			track.TrackNumber, track.TrackTotal, track.DiscNumber, track.DiscTotal,
			track.DurationMS, track.Year, isrc, track.CoverURL, track.Compilation,
			string(track.DisplayLyricsState()), track.LyricsProvider,
			nullTime(track.LyricsCheckedAt), now, existingID)
		if err != nil {
			return music.Track{}, wrapDB("update track", err)
		}
	}

	stored := track
	stored.ID = existingID
	stored.ReleaseID = releaseID
	return stored, nil
}

// lockTrackIdentity takes a transaction scoped advisory lock on one lookup key.
// An empty key is ignored, because there is nothing to serialise on.
func lockTrackIdentity(ctx context.Context, tx executor, prefix, key string) error {
	if key == "" {
		return nil
	}
	if _, err := tx.ExecContext(ctx,
		`SELECT pg_advisory_xact_lock(hashtext($1))`, prefix+key); err != nil {
		return wrapDB("lock track identity", err)
	}
	return nil
}

// findTrackID resolves an existing track by ID, ISRC, or by identity key and
// runtime. It returns an empty string when the recording is unknown.
func findTrackID(ctx context.Context, exec executor, trackID, identity, isrc string, durationMS, toleranceMS int) (string, error) {
	if trackID != "" {
		var id string
		err := exec.QueryRowContext(ctx, `SELECT id FROM tracks WHERE id = $1 LIMIT 1`, trackID).Scan(&id)
		switch {
		case err == nil:
			return id, nil
		case !errors.Is(err, sql.ErrNoRows):
			return "", wrapDB("find track by id", err)
		}
	}

	if isrc != "" {
		var id string
		err := exec.QueryRowContext(ctx, `SELECT id FROM tracks WHERE isrc = $1 LIMIT 1`, isrc).Scan(&id)
		switch {
		case err == nil:
			return id, nil
		case !errors.Is(err, sql.ErrNoRows):
			return "", wrapDB("find track by isrc", err)
		}
	}

	if toleranceMS <= 0 {
		toleranceMS = discography.DefaultDurationToleranceMS
	}
	rows, err := exec.QueryContext(ctx,
		`SELECT id, duration_ms FROM tracks WHERE identity_key = $1`, identity)
	if err != nil {
		return "", wrapDB("find track by identity", err)
	}
	defer rows.Close()

	for rows.Next() {
		var (
			id       string
			duration int
		)
		if err := rows.Scan(&id, &duration); err != nil {
			return "", wrapDB("scan track candidate", err)
		}
		if durationMS <= 0 || duration <= 0 {
			return id, nil
		}
		diff := durationMS - duration
		if diff < 0 {
			diff = -diff
		}
		if diff <= toleranceMS {
			return id, nil
		}
	}
	if err := rows.Err(); err != nil {
		return "", wrapDB("find track by identity", err)
	}
	return "", nil
}

// GetTrack loads a track by internal id.
func (c *Catalog) GetTrack(ctx context.Context, id string) (*music.Track, error) {
	row := c.db.QueryRowContext(ctx, `SELECT `+trackColumns+` FROM tracks LEFT JOIN releases ON releases.id = tracks.release_id WHERE tracks.id = $1`, id)
	stored, err := scanTrack(row.Scan)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, apperr.Newf(apperr.CodeTrackNotFound, "Track %q is not in the library.", id)
		}
		return nil, wrapDB("get track", err)
	}
	return &stored.Track, nil
}

// ListTracks returns tracks, optionally restricted to one release.
func (c *Catalog) ListTracks(ctx context.Context, releaseID string, limit, offset int) ([]music.Track, error) {
	query := `SELECT ` + trackColumns + ` FROM tracks LEFT JOIN releases ON releases.id = tracks.release_id WHERE ($1::text = '' OR tracks.release_id = $1)
		ORDER BY tracks.album, tracks.disc_number, tracks.track_number, tracks.title LIMIT $2 OFFSET $3`

	rows, err := c.db.QueryContext(ctx, query,
		releaseID, clampLimit(limit, 100, 500), clampOffset(offset))
	if err != nil {
		return nil, wrapDB("list tracks", err)
	}
	defer rows.Close()

	out := make([]music.Track, 0, 32)
	for rows.Next() {
		stored, err := scanTrack(rows.Scan)
		if err != nil {
			return nil, wrapDB("scan track", err)
		}
		out = append(out, stored.Track)
	}
	if err := rows.Err(); err != nil {
		return nil, wrapDB("list tracks", err)
	}
	return out, nil
}

// FindTrack resolves a recording the way the download history needs it: by
// ID first, ISRC second, by identity key and runtime third.
func (c *Catalog) FindTrack(ctx context.Context, track music.Track, toleranceMS int) (*music.Track, error) {
	id, err := findTrackID(ctx, c.db, track.ID, discography.IdentityKey(track),
		discography.NormalizeISRC(track.ISRC), track.DurationMS, toleranceMS)
	if err != nil {
		return nil, err
	}
	if id == "" {
		return nil, nil
	}
	return c.GetTrack(ctx, id)
}

// AddSource records an external origin of a track. Re-registering the same
// source is a no-op.
func (c *Catalog) AddSource(ctx context.Context, source music.Source) error {
	return addSource(ctx, c.db, source)
}

func addSource(ctx context.Context, exec executor, source music.Source) error {
	if source.TrackID == "" || source.Provider == "" || source.SourceID == "" {
		return apperr.New(apperr.CodeInvalidRequest, "A track source needs a track, a provider and a source id.")
	}
	switch source.Kind {
	case music.SourceMetadata, music.SourceMedia:
	default:
		return apperr.Newf(apperr.CodeInvalidRequest, "%q is not a valid track source kind.", source.Kind)
	}
	id := source.ID
	if id == "" {
		id = music.NewID()
	}
	_, err := exec.ExecContext(ctx, `
		INSERT INTO track_sources (id, track_id, provider, kind, source_id, source_url, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		ON CONFLICT (track_id, provider, kind, source_id) DO UPDATE SET
			source_url = excluded.source_url`,
		id, source.TrackID, source.Provider, string(source.Kind),
		source.SourceID, source.SourceURL, time.Now().UTC())
	if err != nil {
		return wrapDB("add track source", err)
	}
	return nil
}

// ListSources returns every external source of a track.
func (c *Catalog) ListSources(ctx context.Context, trackID string) ([]music.Source, error) {
	rows, err := c.db.QueryContext(ctx, `
		SELECT id, track_id, provider, kind, source_id, source_url
		FROM track_sources WHERE track_id = $1 ORDER BY provider, kind`, trackID)
	if err != nil {
		return nil, wrapDB("list track sources", err)
	}
	defer rows.Close()

	out := make([]music.Source, 0, 4)
	for rows.Next() {
		var (
			s    music.Source
			kind string
		)
		if err := rows.Scan(&s.ID, &s.TrackID, &s.Provider, &kind, &s.SourceID, &s.SourceURL); err != nil {
			return nil, wrapDB("scan track source", err)
		}
		s.Kind = music.SourceKind(kind)
		out = append(out, s)
	}
	if err := rows.Err(); err != nil {
		return nil, wrapDB("list track sources", err)
	}
	return out, nil
}

// PersistDownload writes the artist, the release, the recording, its sources
// and the library file in one transaction. A download either appears in the
// catalogue completely or not at all; a half written state — a track without
// its file, or a file whose recording is missing — can never be observed.
func (c *Catalog) PersistDownload(ctx context.Context, entry music.LibraryEntry, toleranceMS int) (music.StoredEntry, error) {
	var out music.StoredEntry
	err := c.db.WithTx(ctx, func(tx *sql.Tx) error {
		if entry.Artist != nil {
			artist, err := upsertArtist(ctx, tx, *entry.Artist)
			if err != nil {
				return err
			}
			out.ArtistID = artist.ID
		}

		if entry.Release != nil {
			release, err := upsertRelease(ctx, tx, *entry.Release, out.ArtistID)
			if err != nil {
				return err
			}
			out.ReleaseID = release.ID
		}

		track, err := upsertTrack(ctx, tx, entry.Track, out.ReleaseID, out.ArtistID, toleranceMS)
		if err != nil {
			return err
		}
		out.TrackID = track.ID

		for _, source := range entry.Sources {
			source.TrackID = track.ID
			if err := addSource(ctx, tx, source); err != nil {
				return err
			}
		}

		file := entry.File
		file.TrackID = track.ID
		if out.File, err = upsertFile(ctx, tx, file); err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		return music.StoredEntry{}, err
	}
	return out, nil
}

// ListAllTracks returns every track in the catalog.
func (c *Catalog) ListAllTracks(ctx context.Context) ([]StoredTrack, error) {
	rows, err := c.db.QueryContext(ctx, `SELECT `+trackColumns+` FROM tracks LEFT JOIN releases ON releases.id = tracks.release_id ORDER BY tracks.title`)
	if err != nil {
		return nil, wrapDB("list all tracks", err)
	}
	defer rows.Close()

	out := make([]StoredTrack, 0, 64)
	for rows.Next() {
		stored, err := scanTrack(rows.Scan)
		if err != nil {
			return nil, wrapDB("scan track", err)
		}
		out = append(out, stored)
	}
	if err := rows.Err(); err != nil {
		return nil, wrapDB("list all tracks", err)
	}
	return out, nil
}

// DeleteTrack removes a track from the catalog. Associated track_sources are
// removed by foreign key cascade.
func (c *Catalog) DeleteTrack(ctx context.Context, id string) error {
	if _, err := c.db.ExecContext(ctx, `DELETE FROM tracks WHERE id = $1`, id); err != nil {
		return wrapDB("delete track", err)
	}
	return nil
}

// DeleteRelease removes a release from the catalog.
func (c *Catalog) DeleteRelease(ctx context.Context, id string) error {
	if _, err := c.db.ExecContext(ctx, `DELETE FROM releases WHERE id = $1`, id); err != nil {
		return wrapDB("delete release", err)
	}
	return nil
}

// SetLyricsState records the outcome of a lyrics lookup.
//
// Only a definitive answer is written: a caller that hit a timeout, a rate
// limit or an unparsable response must not call this at all, so that a
// transient failure never looks like "this track has no lyrics" and never
// starts the cooldown that keeps a backfill from trying again.
//
// The lyrics text is deliberately not stored. The sidecar file next to the
// audio is the single source of truth, because it is also what the media
// servers read.
func (c *Catalog) SetLyricsState(ctx context.Context, trackID string,
	state music.LyricsState, providerName string, checkedAt time.Time) error {

	if strings.TrimSpace(trackID) == "" {
		return apperr.New(apperr.CodeInvalidRequest, "A track id is required.")
	}
	if !music.ValidLyricsState(string(state)) {
		return apperr.Newf(apperr.CodeInvalidRequest, "Unknown lyrics state %q.", state)
	}

	var checked any
	if !checkedAt.IsZero() {
		checked = checkedAt.UTC()
	}

	result, err := c.db.ExecContext(ctx, `
		UPDATE tracks
		   SET lyrics_state = $2, lyrics_provider = $3, lyrics_checked_at = $4, updated_at = $5
		 WHERE id = $1`,
		trackID, string(state), providerName, checked, time.Now().UTC())
	if err != nil {
		return wrapDB("set lyrics state", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return wrapDB("set lyrics state", err)
	}
	if affected == 0 {
		return apperr.Newf(apperr.CodeTrackNotFound, "Track %q is not in the library.", trackID)
	}
	return nil
}

// ListTracksNeedingLyrics returns the tracks a backfill should ask about,
// oldest check first.
//
// A track that already has synchronised lyrics is never returned: there is
// nothing better to find. A track whose last definitive check is younger than
// the cutoff is skipped, which is what keeps repeated runs from asking a free
// public service about the same missing track over and over.
func (c *Catalog) ListTracksNeedingLyrics(ctx context.Context, before time.Time, limit int) ([]StoredTrack, error) {
	if limit <= 0 {
		limit = 100
	}
	rows, err := c.db.QueryContext(ctx, `
		SELECT `+trackColumns+`
		  FROM tracks
		  LEFT JOIN releases ON releases.id = tracks.release_id
		 WHERE tracks.lyrics_state <> 'available_synced'
		   AND (tracks.lyrics_checked_at IS NULL OR tracks.lyrics_checked_at < $1)
		 ORDER BY tracks.lyrics_checked_at NULLS FIRST, tracks.created_at
		 LIMIT $2`, before.UTC(), limit)
	if err != nil {
		return nil, wrapDB("list lyrics candidates", err)
	}
	defer rows.Close()

	out := make([]StoredTrack, 0, limit)
	for rows.Next() {
		track, err := scanTrack(rows.Scan)
		if err != nil {
			return nil, wrapDB("scan lyrics candidate", err)
		}
		out = append(out, track)
	}
	if err := rows.Err(); err != nil {
		return nil, wrapDB("list lyrics candidates", err)
	}
	return out, nil
}

// LyricsStats reports count aggregations of tracks by lyrics state.
type LyricsStats struct {
	TracksScanned int `json:"tracks_scanned"`
	AlreadyLRC    int `json:"already_lrc"`
	AlreadyTXT    int `json:"already_txt"`
	Instrumental  int `json:"instrumental"`
	Missing       int `json:"missing"`
	Eligible      int `json:"eligible"`
}

// LyricsStats returns the count breakdown of tracks by lyrics state.
func (c *Catalog) LyricsStats(ctx context.Context, cutoff time.Time) (LyricsStats, error) {
	var stats LyricsStats
	err := c.db.QueryRowContext(ctx, `
		SELECT
			COUNT(*),
			COUNT(*) FILTER (WHERE lyrics_state = 'available_synced'),
			COUNT(*) FILTER (WHERE lyrics_state = 'available_plain'),
			COUNT(*) FILTER (WHERE lyrics_state = 'instrumental'),
			COUNT(*) FILTER (WHERE lyrics_state IN ('not_found', 'unknown')),
			COUNT(*) FILTER (WHERE lyrics_state IN ('not_found', 'unknown') AND (lyrics_checked_at IS NULL OR lyrics_checked_at < $1))
		  FROM tracks`, cutoff.UTC()).Scan(
		&stats.TracksScanned,
		&stats.AlreadyLRC,
		&stats.AlreadyTXT,
		&stats.Instrumental,
		&stats.Missing,
		&stats.Eligible,
	)
	if err != nil {
		return LyricsStats{}, wrapDB("aggregate lyrics stats", err)
	}
	return stats, nil
}
