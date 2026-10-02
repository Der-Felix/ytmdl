package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"ytdm/backend/internal/apperr"
	"ytdm/backend/internal/music"
)

type SmartRules struct {
	Genre     string `json:"genre,omitempty"`
	ArtistID  string `json:"artist_id,omitempty"`
	Favorites bool   `json:"favorites,omitempty"`
	AddedDays int    `json:"added_days,omitempty"`
	Sort      string `json:"sort"`
	Limit     int    `json:"limit"`
}

func (r *SmartRules) Validate() error {
	r.Genre = strings.TrimSpace(r.Genre)
	r.ArtistID = strings.TrimSpace(r.ArtistID)
	if utf8.RuneCountInString(r.Genre) > 80 || len(r.ArtistID) > 200 || r.AddedDays < 0 || r.AddedDays > 3650 || r.Limit < 1 || r.Limit > 500 {
		return apperr.New(apperr.CodeInvalidRequest, "Ungültige Playlist-Regeln (1–500 Titel, maximal 3650 Tage).")
	}
	if r.Sort != "recent" && r.Sort != "title" && r.Sort != "frequent" && r.Sort != "last_played" {
		return apperr.New(apperr.CodeInvalidRequest, "Ungültige Playlist-Sortierung.")
	}
	return nil
}

// Shared filtered catalog query keeps artwork and manual metadata consistent.
func (c *Catalog) TracksByIDs(ctx context.Context, ids []string) ([]music.LibraryTrack, error) {
	out := make([]music.LibraryTrack, 0, len(ids))
	byID := map[string]music.LibraryTrack{}
	for start := 0; start < len(ids); start += 100 {
		end := min(start+100, len(ids))
		tracks, _, err := c.ListTracksFiltered(ctx, TrackListFilter{IDs: ids[start:end], Limit: 100})
		if err != nil {
			return nil, err
		}
		for _, t := range tracks {
			byID[t.ID] = t
		}
	}
	for _, id := range ids {
		if t, ok := byID[id]; ok {
			out = append(out, t)
		}
	}
	return out, nil
}
func (c *Catalog) SmartTracks(ctx context.Context, userID string, rules SmartRules) ([]music.LibraryTrack, error) {
	if err := rules.Validate(); err != nil {
		return nil, err
	}
	args := []any{userID}
	where := []string{"EXISTS(SELECT 1 FROM files f WHERE f.track_id=t.id)"}
	idx := 2
	if rules.ArtistID != "" {
		where = append(where, fmt.Sprintf("t.artist_id=$%d", idx))
		args = append(args, rules.ArtistID)
		idx++
	}
	if err := appendGenreFilter(rules.Genre, false, "t.artist_id", &where, &args, &idx); err != nil {
		return nil, err
	}
	if rules.Favorites {
		where = append(where, "EXISTS(SELECT 1 FROM favorite_tracks f WHERE f.track_id=t.id AND f.user_id=$1)")
	}
	if rules.AddedDays > 0 {
		where = append(where, fmt.Sprintf("t.created_at >= now()-($%d::int * interval '1 day')", idx))
		args = append(args, rules.AddedDays)
		idx++
	}
	order := "t.created_at DESC,t.id"
	switch rules.Sort {
	case "title":
		order = "lower(t.title),t.id"
	case "frequent":
		order = "COALESCE(l.play_count,0) DESC,t.id"
	case "last_played":
		order = "l.last_played_at DESC NULLS LAST,t.id"
	}
	rows, err := c.db.QueryContext(ctx, `SELECT t.id FROM tracks t LEFT JOIN listening_history l ON l.track_id=t.id AND l.user_id=$1 WHERE `+strings.Join(where, " AND ")+` ORDER BY `+order+fmt.Sprintf(" LIMIT $%d", idx), append(args, rules.Limit)...)
	if err != nil {
		return nil, wrapDB("smart playlist", err)
	}
	ids := []string{}
	for rows.Next() {
		var id string
		if err = rows.Scan(&id); err != nil {
			rows.Close()
			return nil, err
		}
		ids = append(ids, id)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	return c.TracksByIDs(ctx, ids)
}

type HistoryTrack struct {
	music.LibraryTrack
	PlayCount    int64     `json:"play_count"`
	LastPlayedAt time.Time `json:"last_played_at"`
}

func (c *Catalog) RecordPlayback(ctx context.Context, userID, eventID, trackID string) error {
	if len(eventID) < 16 || len(eventID) > 100 || len(trackID) > 200 {
		return apperr.New(apperr.CodeInvalidRequest, "Ungültiger Wiedergabeeintrag.")
	}
	_, err := c.db.ExecContext(ctx, `WITH inserted AS (INSERT INTO playback_events(user_id,event_id,track_id) SELECT $1,$2,id FROM tracks WHERE id=$3 ON CONFLICT DO NOTHING RETURNING track_id) INSERT INTO listening_history(user_id,track_id) SELECT $1,track_id FROM inserted ON CONFLICT(user_id,track_id) DO UPDATE SET play_count=listening_history.play_count+1,last_played_at=now()`, userID, eventID, trackID)
	if err != nil {
		return wrapDB("record playback", err)
	}
	return nil
}
func (c *Catalog) ListeningHistory(ctx context.Context, userID, sort string) ([]HistoryTrack, error) {
	order := "last_played_at DESC,track_id"
	if sort == "frequent" {
		order = "play_count DESC,last_played_at DESC,track_id"
	} else if sort != "" && sort != "recent" {
		return nil, apperr.New(apperr.CodeInvalidRequest, "Ungültige Verlaufssortierung.")
	}
	rows, err := c.db.QueryContext(ctx, "SELECT track_id,play_count,last_played_at FROM listening_history WHERE user_id=$1 ORDER BY "+order+" LIMIT 100", userID)
	if err != nil {
		return nil, wrapDB("history", err)
	}
	ids := []string{}
	details := map[string]HistoryTrack{}
	for rows.Next() {
		var id string
		var t HistoryTrack
		if err = rows.Scan(&id, &t.PlayCount, &t.LastPlayedAt); err != nil {
			rows.Close()
			return nil, err
		}
		ids = append(ids, id)
		details[id] = t
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	tracks, err := c.TracksByIDs(ctx, ids)
	if err != nil {
		return nil, err
	}
	out := make([]HistoryTrack, 0, len(tracks))
	for _, t := range tracks {
		entry := details[t.ID]
		entry.LibraryTrack = t
		out = append(out, entry)
	}
	return out, nil
}
func (c *Catalog) ClearListeningHistory(ctx context.Context, userID string) error {
	tx, err := c.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, "DELETE FROM playback_events WHERE user_id=$1", userID); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, "DELETE FROM listening_history WHERE user_id=$1", userID); err != nil {
		return err
	}
	return tx.Commit()
}

type MetadataPatch struct {
	Album       *string   `json:"album,omitempty"`
	AlbumArtist *string   `json:"album_artist,omitempty"`
	Artists     *[]string `json:"artists,omitempty"`
	Year        *int      `json:"year,omitempty"`
}

func (c *Catalog) UpdateTrackMetadata(ctx context.Context, ids []string, patch MetadataPatch) error {
	if len(ids) < 1 || len(ids) > 100 {
		return apperr.New(apperr.CodeInvalidRequest, "Bitte 1–100 Titel auswählen.")
	}
	for _, s := range []*string{patch.Album, patch.AlbumArtist} {
		if s != nil {
			*s = strings.TrimSpace(*s)
			if utf8.RuneCountInString(*s) > 300 {
				return apperr.New(apperr.CodeInvalidRequest, "Metadaten dürfen maximal 300 Zeichen enthalten.")
			}
		}
	}
	if patch.Year != nil && (*patch.Year < 0 || *patch.Year > 9999) {
		return apperr.New(apperr.CodeInvalidRequest, "Ungültiges Jahr.")
	}
	var artists any
	if patch.Artists != nil {
		a := []string{}
		if len(*patch.Artists) > 20 {
			return apperr.New(apperr.CodeInvalidRequest, "Maximal 20 Künstler.")
		}
		for _, v := range *patch.Artists {
			v = strings.TrimSpace(v)
			if v == "" || utf8.RuneCountInString(v) > 200 {
				return apperr.New(apperr.CodeInvalidRequest, "Ungültiger Künstlername.")
			}
			a = append(a, v)
		}
		artists = encodeStrings(a)
	}
	if patch.Album == nil && patch.AlbumArtist == nil && patch.Artists == nil && patch.Year == nil {
		return apperr.New(apperr.CodeInvalidRequest, "Keine Änderungen angegeben.")
	}
	tx, err := c.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, id := range ids {
		var exists bool
		if err = tx.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM tracks WHERE id=$1)", id).Scan(&exists); err != nil {
			return err
		}
		if !exists {
			return apperr.New(apperr.CodeTrackNotFound, "Titel nicht gefunden; keine Änderungen gespeichert.")
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO track_overrides(track_id,album,album_artist,artists_json,year) VALUES($1,$2,$3,$4::jsonb,$5) ON CONFLICT(track_id) DO UPDATE SET album=COALESCE(EXCLUDED.album,track_overrides.album),album_artist=COALESCE(EXCLUDED.album_artist,track_overrides.album_artist),artists_json=COALESCE(EXCLUDED.artists_json,track_overrides.artists_json),year=COALESCE(EXCLUDED.year,track_overrides.year)`, id, patch.Album, patch.AlbumArtist, artists, patch.Year)
		if err != nil {
			return wrapDB("metadata patch", err)
		}
	}
	return tx.Commit()
}
func (c *Catalog) ApplyTrackOverride(ctx context.Context, t *music.Track) error {
	var album, artist, artists sql.NullString
	var year sql.NullInt64
	err := c.db.QueryRowContext(ctx, "SELECT album,album_artist,artists_json,year FROM track_overrides WHERE track_id=$1", t.ID).Scan(&album, &artist, &artists, &year)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	if album.Valid {
		t.Album = album.String
	}
	if artist.Valid {
		t.AlbumArtist = artist.String
	}
	if artists.Valid {
		t.Artists = decodeStrings(artists.String)
	}
	if year.Valid {
		t.Year = int(year.Int64)
	}
	return nil
}

type DuplicateGroup struct {
	Key              string               `json:"key"`
	Count            int                  `json:"count"`
	Tracks           []music.LibraryTrack `json:"tracks"`
	Fingerprint      string               `json:"fingerprint"`
	Outcome          string               `json:"outcome"`
	PreferredTrackID string               `json:"preferred_track_id"`
}

func (c *Catalog) DuplicateGroups(ctx context.Context, offset int) ([]DuplicateGroup, error) {
	return c.DuplicateGroupsForUser(ctx, "", offset, true, "")
}
func (c *Catalog) CustomArtwork(ctx context.Context, kind, id string) ([]byte, time.Time, error) {
	var data []byte
	var updated time.Time
	err := c.db.QueryRowContext(ctx, "SELECT image,updated_at FROM library_artwork WHERE kind=$1 AND entity_id=$2", kind, id).Scan(&data, &updated)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, updated, nil
	}
	return data, updated, err
}
func (c *Catalog) SetCustomArtwork(ctx context.Context, kind, id string, data []byte) error {
	table := ""
	switch kind {
	case "artists":
		table = "artists"
	case "releases":
		table = "releases"
	default:
		return apperr.New(apperr.CodeInvalidRequest, "Ungültiger Bildtyp.")
	}
	return c.db.WithTx(ctx, func(tx *sql.Tx) error {
		var found string
		err := tx.QueryRowContext(ctx, "SELECT id FROM "+table+" WHERE id=$1 FOR KEY SHARE", id).Scan(&found)
		if errors.Is(err, sql.ErrNoRows) {
			return apperr.New(apperr.CodeArtistNotFound, "Eintrag nicht gefunden.")
		}
		if err != nil {
			return err
		}
		if data == nil {
			_, err = tx.ExecContext(ctx, "DELETE FROM library_artwork WHERE kind=$1 AND entity_id=$2", kind, id)
			return err
		}
		_, err = tx.ExecContext(ctx, "INSERT INTO library_artwork(kind,entity_id,image) VALUES($1,$2,$3) ON CONFLICT(kind,entity_id) DO UPDATE SET image=EXCLUDED.image,updated_at=now()", kind, id, data)
		return err
	})
}

type Loudness struct {
	GainDB         float64 `json:"gain_db"`
	IntegratedLUFS float64 `json:"integrated_lufs"`
	TruePeakDB     float64 `json:"true_peak_db"`
}

func (c *Catalog) Loudness(ctx context.Context, trackID, fileID string, size, mtime int64) (*Loudness, error) {
	var l Loudness
	err := c.db.QueryRowContext(ctx, "SELECT gain_db,integrated_lufs,true_peak_db FROM track_loudness WHERE track_id=$1 AND file_id=$2 AND size_bytes=$3 AND mtime_ns=$4", trackID, fileID, size, mtime).Scan(&l.GainDB, &l.IntegratedLUFS, &l.TruePeakDB)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return &l, err
}
func (c *Catalog) SetLoudness(ctx context.Context, trackID, fileID string, size, mtime int64, l Loudness) error {
	_, err := c.db.ExecContext(ctx, "INSERT INTO track_loudness(track_id,file_id,size_bytes,mtime_ns,gain_db,integrated_lufs,true_peak_db) VALUES($1,$2,$3,$4,$5,$6,$7) ON CONFLICT(track_id) DO UPDATE SET file_id=EXCLUDED.file_id,size_bytes=EXCLUDED.size_bytes,mtime_ns=EXCLUDED.mtime_ns,gain_db=EXCLUDED.gain_db,integrated_lufs=EXCLUDED.integrated_lufs,true_peak_db=EXCLUDED.true_peak_db,analyzed_at=now()", trackID, fileID, size, mtime, l.GainDB, l.IntegratedLUFS, l.TruePeakDB)
	return err
}
