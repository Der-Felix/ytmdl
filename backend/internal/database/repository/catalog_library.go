package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"ytdm/backend/internal/apperr"
	"ytdm/backend/internal/discography"
	"ytdm/backend/internal/music"
)

// TrackListFilter parameters for filtering library tracks.
type TrackListFilter struct {
	Query       string
	ArtistID    string
	ReleaseID   string
	LyricsState string
	Sort        string
	Order       string
	Limit       int
	Offset      int
}

// ReleaseListFilter parameters for filtering library releases.
type ReleaseListFilter struct {
	Query       string
	ArtistID    string
	ReleaseType string
	Year        int
	Sort        string
	Order       string
	Limit       int
	Offset      int
}

// ArtistListFilter parameters for filtering library artists.
type ArtistListFilter struct {
	Query  string
	Sort   string
	Order  string
	Limit  int
	Offset int
}

func sanitizeTrackSort(sort, order string) (string, error) {
	sort = strings.TrimSpace(strings.ToLower(sort))
	order = strings.TrimSpace(strings.ToLower(order))

	if order != "" && order != "asc" && order != "desc" {
		return "", apperr.New(apperr.CodeInvalidRequest, "Invalid order: must be 'asc' or 'desc'.")
	}

	switch sort {
	case "", "recent":
		if order == "" {
			order = "desc"
		}
		return fmt.Sprintf("t.created_at %s, t.id %s", order, order), nil
	case "title":
		if order == "" {
			order = "asc"
		}
		return fmt.Sprintf("t.title %s, t.id %s", order, order), nil
	case "artist":
		if order == "" {
			order = "asc"
		}
		return fmt.Sprintf("t.album_artist %s, t.title %s, t.id %s", order, order, order), nil
	case "album":
		if order == "" {
			order = "asc"
		}
		return fmt.Sprintf("t.album %s, t.disc_number %s, t.track_number %s, t.id %s", order, order, order, order), nil
	case "year":
		if order == "" {
			order = "desc"
		}
		return fmt.Sprintf("t.year %s, t.title %s, t.id %s", order, order, order), nil
	case "duration":
		if order == "" {
			order = "desc"
		}
		return fmt.Sprintf("t.duration_ms %s, t.title %s, t.id %s", order, order, order), nil
	case "track_number":
		if order == "" {
			order = "asc"
		}
		return fmt.Sprintf("t.disc_number %s, t.track_number %s, t.title %s, t.id %s", order, order, order, order), nil
	default:
		return "", apperr.Newf(apperr.CodeInvalidRequest, "Invalid sort field: %q.", sort)
	}
}

func sanitizeReleaseSort(sort, order string) (string, error) {
	sort = strings.TrimSpace(strings.ToLower(sort))
	order = strings.TrimSpace(strings.ToLower(order))

	if order != "" && order != "asc" && order != "desc" {
		return "", apperr.New(apperr.CodeInvalidRequest, "Invalid order: must be 'asc' or 'desc'.")
	}

	switch sort {
	case "", "recent":
		if order == "" {
			order = "desc"
		}
		return fmt.Sprintf("r.created_at %s, r.id %s", order, order), nil
	case "year":
		if order == "" {
			order = "desc"
		}
		return fmt.Sprintf("r.year %s, r.title %s, r.id %s", order, order, order), nil
	case "title":
		if order == "" {
			order = "asc"
		}
		return fmt.Sprintf("r.title %s, r.id %s", order, order), nil
	case "artist":
		if order == "" {
			order = "asc"
		}
		return fmt.Sprintf("r.album_artist %s, r.title %s, r.id %s", order, order, order), nil
	default:
		return "", apperr.Newf(apperr.CodeInvalidRequest, "Invalid sort field: %q.", sort)
	}
}

func sanitizeArtistSort(sort, order string) (string, error) {
	sort = strings.TrimSpace(strings.ToLower(sort))
	order = strings.TrimSpace(strings.ToLower(order))

	if order != "" && order != "asc" && order != "desc" {
		return "", apperr.New(apperr.CodeInvalidRequest, "Invalid order: must be 'asc' or 'desc'.")
	}

	switch sort {
	case "", "name":
		if order == "" {
			order = "asc"
		}
		return fmt.Sprintf("a.sort_key %s, a.name %s, a.id %s", order, order, order), nil
	case "recent":
		if order == "" {
			order = "desc"
		}
		return fmt.Sprintf("a.created_at %s, a.id %s", order, order), nil
	case "release_count":
		if order == "" {
			order = "desc"
		}
		return fmt.Sprintf("release_count %s, a.sort_key %s, a.name %s, a.id %s", order, order, order, order), nil
	default:
		return "", apperr.Newf(apperr.CodeInvalidRequest, "Invalid sort field: %q.", sort)
	}
}

// ListTracksFiltered retrieves a paginated and filtered list of tracks from the library.
func (c *Catalog) ListTracksFiltered(ctx context.Context, filter TrackListFilter) ([]music.LibraryTrack, int, error) {
	orderBy, err := sanitizeTrackSort(filter.Sort, filter.Order)
	if err != nil {
		return nil, 0, err
	}

	limit := clampLimit(filter.Limit, 50, 100)
	offset := clampOffset(filter.Offset)

	var (
		whereClauses []string
		args         []any
		argIdx       = 1
	)

	if filter.ArtistID != "" {
		whereClauses = append(whereClauses, fmt.Sprintf("t.artist_id = $%d", argIdx))
		args = append(args, filter.ArtistID)
		argIdx++
	}
	if filter.ReleaseID != "" {
		whereClauses = append(whereClauses, fmt.Sprintf("t.release_id = $%d", argIdx))
		args = append(args, filter.ReleaseID)
		argIdx++
	}
	if filter.LyricsState != "" {
		if !music.ValidLyricsState(filter.LyricsState) {
			return nil, 0, apperr.Newf(apperr.CodeInvalidRequest, "Invalid lyrics_state filter: %q.", filter.LyricsState)
		}
		whereClauses = append(whereClauses, fmt.Sprintf("t.lyrics_state = $%d", argIdx))
		args = append(args, filter.LyricsState)
		argIdx++
	}
	if q := strings.TrimSpace(filter.Query); q != "" {
		isrcTerm := discography.NormalizeISRC(q)
		if isrcTerm == "" {
			isrcTerm = q
		}
		whereClauses = append(whereClauses, fmt.Sprintf(
			"(t.title ILIKE $%d OR t.album ILIKE $%d OR t.album_artist ILIKE $%d OR LOWER(t.isrc) = LOWER($%d))",
			argIdx, argIdx, argIdx, argIdx+1,
		))
		args = append(args, "%"+q+"%", isrcTerm)
		argIdx += 2
	}

	whereSQL := ""
	if len(whereClauses) > 0 {
		whereSQL = "WHERE " + strings.Join(whereClauses, " AND ")
	}

	countQuery := "SELECT COUNT(*) FROM tracks t " + whereSQL
	var total int
	if err := c.db.QueryRowContext(ctx, countQuery, args...).Scan(&total); err != nil {
		return nil, 0, wrapDB("count tracks", err)
	}

	dataQuery := fmt.Sprintf(`
		SELECT
			t.id, t.release_id, t.artist_id, t.title, t.artists_json, t.album, t.album_artist,
			t.track_number, t.track_total, t.disc_number, t.disc_total, t.duration_ms, t.year,
			t.isrc, t.cover_url, t.identity_key, t.compilation, t.lyrics_state, t.lyrics_provider,
			t.lyrics_checked_at, t.created_at,
			COALESCE(f.path, ''), COALESCE(f.size_bytes, 0), COALESCE(f.codec, ''), COALESCE(f.bitrate_kbps, 0)
		FROM tracks t
		LEFT JOIN files f ON f.track_id = t.id
		%s
		ORDER BY %s
		LIMIT $%d OFFSET $%d`, whereSQL, orderBy, argIdx, argIdx+1)

	queryArgs := append(args, limit, offset)
	rows, err := c.db.QueryContext(ctx, dataQuery, queryArgs...)
	if err != nil {
		return nil, 0, wrapDB("list tracks filtered", err)
	}
	defer rows.Close()

	out := make([]music.LibraryTrack, 0, limit)
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
			return nil, 0, wrapDB("scan track filtered", err)
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
		return nil, 0, wrapDB("list tracks filtered", err)
	}
	return out, total, nil
}

// ListReleasesFiltered retrieves a paginated and filtered list of releases from the library.
func (c *Catalog) ListReleasesFiltered(ctx context.Context, filter ReleaseListFilter) ([]music.LibraryRelease, int, error) {
	orderBy, err := sanitizeReleaseSort(filter.Sort, filter.Order)
	if err != nil {
		return nil, 0, err
	}

	limit := clampLimit(filter.Limit, 24, 120)
	offset := clampOffset(filter.Offset)

	var (
		whereClauses []string
		args         []any
		argIdx       = 1
	)

	if filter.ArtistID != "" {
		whereClauses = append(whereClauses, fmt.Sprintf("r.artist_id = $%d", argIdx))
		args = append(args, filter.ArtistID)
		argIdx++
	}
	if filter.ReleaseType != "" {
		if !music.ReleaseType(filter.ReleaseType).Valid() {
			return nil, 0, apperr.Newf(apperr.CodeInvalidRequest, "Invalid release_type filter: %q.", filter.ReleaseType)
		}
		whereClauses = append(whereClauses, fmt.Sprintf("r.release_type = $%d", argIdx))
		args = append(args, filter.ReleaseType)
		argIdx++
	}
	if filter.Year > 0 {
		whereClauses = append(whereClauses, fmt.Sprintf("r.year = $%d", argIdx))
		args = append(args, filter.Year)
		argIdx++
	}
	if q := strings.TrimSpace(filter.Query); q != "" {
		whereClauses = append(whereClauses, fmt.Sprintf("(r.title ILIKE $%d OR r.album_artist ILIKE $%d)", argIdx, argIdx))
		args = append(args, "%"+q+"%")
		argIdx++
	}

	whereSQL := ""
	if len(whereClauses) > 0 {
		whereSQL = "WHERE " + strings.Join(whereClauses, " AND ")
	}

	countQuery := "SELECT COUNT(*) FROM releases r " + whereSQL
	var total int
	if err := c.db.QueryRowContext(ctx, countQuery, args...).Scan(&total); err != nil {
		return nil, 0, wrapDB("count releases", err)
	}

	dataQuery := fmt.Sprintf(`
		SELECT
			r.id, r.title, r.artists_json, r.album_artist, r.release_type, r.year,
			r.release_date, r.track_count, r.cover_url, r.provider, r.source_id, r.source_url,
			r.compilation, r.created_at,
			COUNT(DISTINCT t.id) AS track_count_in_lib,
			COALESCE(SUM(f.size_bytes), 0) AS total_size
		FROM releases r
		LEFT JOIN tracks t ON t.release_id = r.id
		LEFT JOIN files f ON f.track_id = t.id
		%s
		GROUP BY r.id
		ORDER BY %s
		LIMIT $%d OFFSET $%d`, whereSQL, orderBy, argIdx, argIdx+1)

	queryArgs := append(args, limit, offset)
	rows, err := c.db.QueryContext(ctx, dataQuery, queryArgs...)
	if err != nil {
		return nil, 0, wrapDB("list releases filtered", err)
	}
	defer rows.Close()

	out := make([]music.LibraryRelease, 0, limit)
	for rows.Next() {
		var (
			lr          music.LibraryRelease
			artistsJSON string
			releaseType string
			createdAt   time.Time
		)
		if err := rows.Scan(
			&lr.ID, &lr.Title, &artistsJSON, &lr.AlbumArtist, &releaseType, &lr.Year,
			&lr.ReleaseDate, &lr.TrackCount, &lr.CoverURL, &lr.Provider, &lr.SourceID, &lr.SourceURL,
			&lr.Compilation, &createdAt,
			&lr.TrackCountInLibrary, &lr.TotalSizeBytes,
		); err != nil {
			return nil, 0, wrapDB("scan release filtered", err)
		}
		lr.Artists = decodeStrings(artistsJSON)
		lr.ReleaseType = music.ReleaseType(releaseType)
		lr.CreatedAt = createdAt.UTC()
		out = append(out, lr)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, wrapDB("list releases filtered", err)
	}
	return out, total, nil
}

// ListArtistsFiltered retrieves a paginated and filtered list of artists from the library.
func (c *Catalog) ListArtistsFiltered(ctx context.Context, filter ArtistListFilter) ([]music.LibraryArtist, int, error) {
	orderBy, err := sanitizeArtistSort(filter.Sort, filter.Order)
	if err != nil {
		return nil, 0, err
	}

	limit := clampLimit(filter.Limit, 24, 120)
	offset := clampOffset(filter.Offset)

	var (
		whereClauses []string
		args         []any
		argIdx       = 1
	)

	if q := strings.TrimSpace(filter.Query); q != "" {
		whereClauses = append(whereClauses, fmt.Sprintf("a.name ILIKE $%d", argIdx))
		args = append(args, "%"+q+"%")
		argIdx++
	}

	whereSQL := ""
	if len(whereClauses) > 0 {
		whereSQL = "WHERE " + strings.Join(whereClauses, " AND ")
	}

	countQuery := "SELECT COUNT(*) FROM artists a " + whereSQL
	var total int
	if err := c.db.QueryRowContext(ctx, countQuery, args...).Scan(&total); err != nil {
		return nil, 0, wrapDB("count artists", err)
	}

	dataQuery := fmt.Sprintf(`
		SELECT
			a.id, a.name, a.provider, a.source_id, a.source_url, a.image_url, a.created_at,
			COUNT(DISTINCT r.id) AS release_count,
			COALESCE(ts.track_count, 0) AS track_count,
			COALESCE(ts.total_size, 0) AS total_size
		FROM artists a
		LEFT JOIN releases r ON r.artist_id = a.id
		LEFT JOIN (
			SELECT t.artist_id, COUNT(t.id) AS track_count, COALESCE(SUM(f.size_bytes), 0) AS total_size
			FROM tracks t
			LEFT JOIN files f ON f.track_id = t.id
			GROUP BY t.artist_id
		) ts ON ts.artist_id = a.id
		%s
		GROUP BY a.id, ts.track_count, ts.total_size
		ORDER BY %s
		LIMIT $%d OFFSET $%d`, whereSQL, orderBy, argIdx, argIdx+1)

	queryArgs := append(args, limit, offset)
	rows, err := c.db.QueryContext(ctx, dataQuery, queryArgs...)
	if err != nil {
		return nil, 0, wrapDB("list artists filtered", err)
	}
	defer rows.Close()

	out := make([]music.LibraryArtist, 0, limit)
	for rows.Next() {
		var (
			la        music.LibraryArtist
			createdAt time.Time
		)
		if err := rows.Scan(
			&la.ID, &la.Name, &la.Provider, &la.SourceID, &la.SourceURL, &la.ImageURL, &createdAt,
			&la.ReleaseCount, &la.TrackCount, &la.TotalSizeBytes,
		); err != nil {
			return nil, 0, wrapDB("scan artist filtered", err)
		}
		la.CreatedAt = createdAt.UTC()
		out = append(out, la)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, wrapDB("list artists filtered", err)
	}
	return out, total, nil
}

// GetLibraryArtistDetail returns full details of an artist with their local releases and tracks.
func (c *Catalog) GetLibraryArtistDetail(ctx context.Context, id string) (*music.LibraryArtistDetail, error) {
	artist, err := c.GetArtist(ctx, id)
	if err != nil {
		return nil, err
	}

	releases, _, err := c.ListReleasesFiltered(ctx, ReleaseListFilter{
		ArtistID: id,
		Limit:    120,
		Sort:     "year",
		Order:    "desc",
	})
	if err != nil {
		return nil, err
	}

	tracks, _, err := c.ListTracksFiltered(ctx, TrackListFilter{
		ArtistID: id,
		Limit:    100,
		Sort:     "recent",
		Order:    "desc",
	})
	if err != nil {
		return nil, err
	}

	var totalSizeBytes int64
	for _, r := range releases {
		totalSizeBytes += r.TotalSizeBytes
	}

	var (
		subID      string
		subImage   string
		subscribed bool
	)
	// 1. Authoritative match via artist_sources (Schema 9+)
	err = c.db.QueryRowContext(ctx,
		`SELECT s.id, s.artist_image_url
		 FROM artist_subscriptions s
		 JOIN artist_sources src ON src.provider = s.provider AND src.source_id = s.artist_source_id
		 WHERE src.artist_id = $1
		 LIMIT 1`, id).Scan(&subID, &subImage)
	if err == nil {
		subscribed = true
		if artist.ImageURL == "" && strings.TrimSpace(subImage) != "" {
			artist.ImageURL = strings.TrimSpace(subImage)
		}
	} else if !errors.Is(err, sql.ErrNoRows) {
		return nil, wrapDB("check artist subscription via sources", err)
	} else {
		// 2. Direct match on (provider, source_id)
		err = c.db.QueryRowContext(ctx,
			`SELECT id, artist_image_url FROM artist_subscriptions
			 WHERE provider = $1 AND artist_source_id = $2`,
			artist.Provider, artist.SourceID).Scan(&subID, &subImage)
		if err == nil {
			subscribed = true
			if artist.ImageURL == "" && strings.TrimSpace(subImage) != "" {
				artist.ImageURL = strings.TrimSpace(subImage)
			}
		} else if !errors.Is(err, sql.ErrNoRows) {
			return nil, wrapDB("check artist subscription exact", err)
		} else if strings.HasPrefix(artist.SourceID, "artist:") && artist.Provider != "" {
			// 3. Restricted safe legacy fallback:
			// ONLY for synthetic legacy source IDs, ONLY within the same provider,
			// and ONLY if the pairing is demonstrably unique (no same-name ambiguities).
			var subCount, artistCount int
			_ = c.db.QueryRowContext(ctx,
				`SELECT COUNT(*) FROM artist_subscriptions
				 WHERE provider = $1 AND LOWER(artist_name) = LOWER($2)`,
				artist.Provider, artist.Name).Scan(&subCount)

			_ = c.db.QueryRowContext(ctx,
				`SELECT COUNT(*) FROM artists
				 WHERE provider = $1 AND LOWER(name) = LOWER($2)`,
				artist.Provider, artist.Name).Scan(&artistCount)

			if subCount == 1 && artistCount == 1 {
				err = c.db.QueryRowContext(ctx,
					`SELECT id, artist_image_url FROM artist_subscriptions
					 WHERE provider = $1 AND LOWER(artist_name) = LOWER($2)`,
					artist.Provider, artist.Name).Scan(&subID, &subImage)
				if err == nil {
					subscribed = true
					if artist.ImageURL == "" && strings.TrimSpace(subImage) != "" {
						artist.ImageURL = strings.TrimSpace(subImage)
					}
				}
			}
		}
	}

	return &music.LibraryArtistDetail{
		Artist:         *artist,
		Releases:       releases,
		Tracks:         tracks,
		ReleaseCount:   len(releases),
		TrackCount:     len(tracks),
		TotalSizeBytes: totalSizeBytes,
		Subscribed:     subscribed,
		SubscriptionID: subID,
	}, nil
}

// GetLibraryReleaseDetail returns full details of a release with its local tracks and files.
func (c *Catalog) GetLibraryReleaseDetail(ctx context.Context, id string) (*music.LibraryReleaseDetail, error) {
	release, err := c.GetRelease(ctx, id)
	if err != nil {
		return nil, err
	}

	var artist *music.Artist
	row := c.db.QueryRowContext(ctx, `
		SELECT a.id, a.name, a.provider, a.source_id, a.source_url, a.image_url
		FROM artists a
		JOIN releases r ON r.artist_id = a.id
		WHERE r.id = $1`, id)
	var a music.Artist
	if err := row.Scan(&a.ID, &a.Name, &a.Provider, &a.SourceID, &a.SourceURL, &a.ImageURL); err == nil {
		artist = &a
	} else if !errors.Is(err, sql.ErrNoRows) {
		return nil, wrapDB("get release artist", err)
	}

	tracks, _, err := c.ListTracksFiltered(ctx, TrackListFilter{
		ReleaseID: id,
		Limit:     100,
		Sort:      "track_number",
		Order:     "asc",
	})
	if err != nil {
		return nil, err
	}

	var totalSize int64
	for _, t := range tracks {
		totalSize += t.FileSizeBytes
	}

	return &music.LibraryReleaseDetail{
		Release:        *release,
		Artist:         artist,
		Tracks:         tracks,
		TotalSizeBytes: totalSize,
	}, nil
}

// GetLibraryTrackDetail returns full metadata, associated file, and lyrics sidecar path for a track.
func (c *Catalog) GetLibraryTrackDetail(ctx context.Context, id string) (*music.LibraryTrackDetail, error) {
	track, err := c.GetTrack(ctx, id)
	if err != nil {
		return nil, err
	}

	var (
		file       *music.File
		release    *music.Release
		artist     *music.Artist
		lyricsPath string
	)

	row := c.db.QueryRowContext(ctx, `SELECT `+fileColumns+` FROM files WHERE track_id = $1`, id)
	f, err := scanFile(row.Scan)
	if err == nil {
		file = &f
		if track.LyricsState == music.LyricsAvailableSynced {
			ext := filepath.Ext(f.Path)
			lyricsPath = strings.TrimSuffix(f.Path, ext) + ".lrc"
		} else if track.LyricsState == music.LyricsAvailablePlain {
			ext := filepath.Ext(f.Path)
			lyricsPath = strings.TrimSuffix(f.Path, ext) + ".txt"
		}
	} else if !errors.Is(err, sql.ErrNoRows) {
		return nil, wrapDB("get track file", err)
	}

	if track.ReleaseID != "" {
		rel, err := c.GetRelease(ctx, track.ReleaseID)
		if err == nil {
			release = rel
		}
	}

	var artistID string
	if err := c.db.QueryRowContext(ctx, `SELECT COALESCE(artist_id, '') FROM tracks WHERE id = $1`, id).Scan(&artistID); err == nil && artistID != "" {
		art, err := c.GetArtist(ctx, artistID)
		if err == nil {
			artist = art
		}
	}

	return &music.LibraryTrackDetail{
		Track:      *track,
		File:       file,
		Release:    release,
		Artist:     artist,
		LyricsPath: lyricsPath,
	}, nil
}

// SearchLibrary performs a combined search across artists, releases, and tracks.
func (c *Catalog) SearchLibrary(ctx context.Context, query string, limit int) (*music.LibrarySearchResults, error) {
	q := strings.TrimSpace(query)
	if q == "" {
		return &music.LibrarySearchResults{
			Artists:  []music.LibraryArtist{},
			Releases: []music.LibraryRelease{},
			Tracks:   []music.LibraryTrack{},
		}, nil
	}

	if limit <= 0 || limit > 20 {
		limit = 5
	}

	artists, _, err := c.ListArtistsFiltered(ctx, ArtistListFilter{
		Query: q,
		Limit: limit,
		Sort:  "name",
		Order: "asc",
	})
	if err != nil {
		return nil, err
	}

	releases, _, err := c.ListReleasesFiltered(ctx, ReleaseListFilter{
		Query: q,
		Limit: limit,
		Sort:  "recent",
		Order: "desc",
	})
	if err != nil {
		return nil, err
	}

	tracks, _, err := c.ListTracksFiltered(ctx, TrackListFilter{
		Query: q,
		Limit: limit,
		Sort:  "recent",
		Order: "desc",
	})
	if err != nil {
		return nil, err
	}

	return &music.LibrarySearchResults{
		Artists:  artists,
		Releases: releases,
		Tracks:   tracks,
	}, nil
}

// GetLibraryAggregates computes high-level library statistics in fast database queries.
func (c *Catalog) GetLibraryAggregates(ctx context.Context) (
	artistCount, releaseCount, trackCount, fileCount int,
	totalBytes int64,
	lyricsCoverage map[music.LyricsState]int,
	codecBreakdown map[string]int,
	err error,
) {
	lyricsCoverage = make(map[music.LyricsState]int)
	codecBreakdown = make(map[string]int)

	err = c.db.QueryRowContext(ctx, `
		SELECT
			(SELECT COUNT(*) FROM artists),
			(SELECT COUNT(*) FROM releases),
			(SELECT COUNT(*) FROM tracks),
			(SELECT COUNT(*) FROM files),
			(SELECT COALESCE(SUM(size_bytes), 0) FROM files)
	`).Scan(&artistCount, &releaseCount, &trackCount, &fileCount, &totalBytes)
	if err != nil {
		return 0, 0, 0, 0, 0, nil, nil, wrapDB("get library counts", err)
	}

	lRows, err := c.db.QueryContext(ctx, `SELECT lyrics_state, COUNT(*) FROM tracks GROUP BY lyrics_state`)
	if err != nil {
		return 0, 0, 0, 0, 0, nil, nil, wrapDB("get lyrics coverage", err)
	}
	defer lRows.Close()
	for lRows.Next() {
		var (
			st  string
			cnt int
		)
		if err := lRows.Scan(&st, &cnt); err != nil {
			return 0, 0, 0, 0, 0, nil, nil, wrapDB("scan lyrics coverage", err)
		}
		lyricsCoverage[music.LyricsState(st)] = cnt
	}
	if err := lRows.Err(); err != nil {
		return 0, 0, 0, 0, 0, nil, nil, wrapDB("get lyrics coverage", err)
	}

	cRows, err := c.db.QueryContext(ctx, `SELECT codec, COUNT(*) FROM files WHERE codec != '' GROUP BY codec`)
	if err != nil {
		return 0, 0, 0, 0, 0, nil, nil, wrapDB("get codec breakdown", err)
	}
	defer cRows.Close()
	for cRows.Next() {
		var (
			codec string
			cnt   int
		)
		if err := cRows.Scan(&codec, &cnt); err != nil {
			return 0, 0, 0, 0, 0, nil, nil, wrapDB("scan codec breakdown", err)
		}
		codecBreakdown[codec] = cnt
	}
	if err := cRows.Err(); err != nil {
		return 0, 0, 0, 0, 0, nil, nil, wrapDB("get codec breakdown", err)
	}

	return artistCount, releaseCount, trackCount, fileCount, totalBytes, lyricsCoverage, codecBreakdown, nil
}
