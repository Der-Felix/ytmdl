package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"ytdm/backend/internal/apperr"
)

func (r *Playlists) SetSmartRules(ctx context.Context, userID, id string, rules *SmartRules) error {
	var data any
	if rules != nil {
		if err := rules.Validate(); err != nil {
			return err
		}
		b, _ := json.Marshal(rules)
		data = string(b)
	}
	res, err := r.db.ExecContext(ctx, "UPDATE playlists SET smart_rules=$3::jsonb,updated_at=now() WHERE id=$1 AND user_id=$2", id, userID, data)
	if err != nil {
		return wrapDB("save playlist rules", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return apperr.New(apperr.CodePlaylistNotFound, "Playlist nicht gefunden.")
	}
	return nil
}
func (r *Playlists) EnrichSmart(ctx context.Context, p *Playlist) ([]PlaylistTrack, error) {
	var raw sql.NullString
	if err := r.db.QueryRowContext(ctx, "SELECT smart_rules FROM playlists WHERE id=$1 AND user_id=$2", p.ID, p.UserID).Scan(&raw); err != nil {
		return nil, err
	}
	if !raw.Valid {
		return nil, nil
	}
	var rules SmartRules
	if err := json.Unmarshal([]byte(raw.String), &rules); err != nil {
		return nil, err
	}
	p.SmartRules = &rules
	tracks, err := NewCatalog(r.db).SmartTracks(ctx, p.UserID, rules)
	if err != nil {
		return nil, err
	}
	out := make([]PlaylistTrack, 0, len(tracks))
	p.TrackCount = len(tracks)
	p.DurationMS = 0
	for i, t := range tracks {
		p.DurationMS += t.DurationMS
		out = append(out, PlaylistTrack{LibraryTrack: t, Position: i + 1, AddedAt: t.CreatedAt})
	}
	return out, nil
}
func (r *Playlists) AddTracks(ctx context.Context, userID, id string, ids []string) (PlaylistDetail, error) {
	if len(ids) < 1 || len(ids) > 100 {
		return PlaylistDetail{}, apperr.New(apperr.CodeInvalidRequest, "Bitte 1–100 Titel auswählen.")
	}
	err := r.db.WithTx(ctx, func(tx *sql.Tx) error {
		var owner string
		var smart bool
		err := tx.QueryRowContext(ctx, "SELECT user_id,smart_rules IS NOT NULL FROM playlists WHERE id=$1 FOR UPDATE", id).Scan(&owner, &smart)
		if errors.Is(err, sql.ErrNoRows) || owner != userID {
			return apperr.New(apperr.CodePlaylistNotFound, "Playlist nicht gefunden.")
		}
		if err != nil {
			return err
		}
		if smart {
			return apperr.New(apperr.CodeInvalidRequest, "Intelligente Playlists werden automatisch gepflegt.")
		}
		for _, track := range ids {
			var exists bool
			if err := tx.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM tracks WHERE id=$1)", track).Scan(&exists); err != nil {
				return err
			}
			if !exists {
				return apperr.New(apperr.CodeTrackNotFound, "Titel nicht gefunden; keine Titel hinzugefügt.")
			}
			_, err = tx.ExecContext(ctx, `INSERT INTO playlist_tracks(playlist_id,track_id,position,added_at) SELECT $1,$2,COALESCE(MAX(position),0)+1,now() FROM playlist_tracks WHERE playlist_id=$1 ON CONFLICT(playlist_id,track_id) DO NOTHING`, id, track)
			if err != nil {
				return err
			}
		}
		_, err = tx.ExecContext(ctx, "UPDATE playlists SET updated_at=now() WHERE id=$1", id)
		return err
	})
	if err != nil {
		return PlaylistDetail{}, err
	}
	return r.GetPlaylistForUser(ctx, userID, id)
}
