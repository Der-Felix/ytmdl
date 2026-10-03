package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"
	"ytdm/backend/internal/apperr"
	"ytdm/backend/internal/music"
)

type TrashMove struct {
	Original string `json:"original"`
	Stored   string `json:"stored"`
	Size     int64  `json:"size"`
	MTimeNS  int64  `json:"mtime_ns"`
}
type TrashEntry struct {
	ID        string          `json:"id"`
	TrackID   string          `json:"track_id"`
	Title     string          `json:"title"`
	CreatedAt time.Time       `json:"created_at"`
	ExpiresAt time.Time       `json:"expires_at"`
	State     string          `json:"state"`
	Payload   json.RawMessage `json:"-"`
	Moves     []TrashMove     `json:"-"`
}

const trashPayloadSQL = `SELECT t.title,jsonb_build_object(
   'track',to_jsonb(t),
   'files',COALESCE((SELECT jsonb_agg(to_jsonb(f)) FROM files f WHERE f.track_id=t.id),'[]'),
   'sources',COALESCE((SELECT jsonb_agg(to_jsonb(s)) FROM track_sources s WHERE s.track_id=t.id),'[]'),
   'overrides',COALESCE((SELECT jsonb_agg(to_jsonb(o)) FROM track_overrides o WHERE o.track_id=t.id),'[]'),
   'favorites',COALESCE((SELECT jsonb_agg(to_jsonb(f)) FROM favorite_tracks f WHERE f.track_id=t.id),'[]'),
   'playlists',COALESCE((SELECT jsonb_agg(to_jsonb(p)||jsonb_build_object('next_ids',ARRAY(SELECT n.track_id FROM playlist_tracks n WHERE n.playlist_id=p.playlist_id AND n.position>p.position ORDER BY n.position),'previous_ids',ARRAY(SELECT n.track_id FROM playlist_tracks n WHERE n.playlist_id=p.playlist_id AND n.position<p.position ORDER BY n.position DESC))) FROM playlist_tracks p WHERE p.track_id=t.id),'[]'))
   FROM tracks t WHERE t.id=$1 FOR UPDATE`

func (c *Catalog) PrepareTrash(ctx context.Context, trackID, userID string, moves []TrashMove) (*TrashEntry, error) {
	entry := &TrashEntry{ID: music.NewID(), TrackID: trackID, Moves: moves}
	err := c.db.WithTx(ctx, func(tx *sql.Tx) error {
		var payload []byte
		err := tx.QueryRowContext(ctx, trashPayloadSQL, trackID).Scan(&entry.Title, &payload)
		if errors.Is(err, sql.ErrNoRows) {
			return apperr.New(apperr.CodeTrackNotFound, "Titel fehlt.")
		}
		if err != nil {
			return err
		}
		if len(payload) > 8<<20 {
			return apperr.New(apperr.CodeInvalidRequest, "Zu viele Zuordnungen für eine sichere Wiederherstellung.")
		}
		raw, err := json.Marshal(moves)
		if err != nil {
			return err
		}
		entry.Payload = payload
		return tx.QueryRowContext(ctx, `INSERT INTO library_trash(id,track_id,title,deleted_by,state,payload,moves) VALUES($1,$2,$3,NULLIF($4,''),'preparing',$5,$6) RETURNING created_at,expires_at,state`, entry.ID, trackID, entry.Title, userID, payload, raw).Scan(&entry.CreatedAt, &entry.ExpiresAt, &entry.State)
	})
	return entry, err
}
func (c *Catalog) CommitTrash(ctx context.Context, id string) error {
	return c.db.WithTx(ctx, func(tx *sql.Tx) error {
		var track string
		var payload []byte
		if err := tx.QueryRowContext(ctx, `SELECT track_id,payload FROM library_trash WHERE id=$1 AND state='preparing' FOR UPDATE`, id).Scan(&track, &payload); err != nil {
			return err
		}
		var locked string
		if err := tx.QueryRowContext(ctx, `SELECT id FROM tracks WHERE id=$1 FOR UPDATE`, track).Scan(&locked); err != nil {
			return err
		}
		lists, err := tx.QueryContext(ctx, `SELECT id FROM playlists WHERE id IN (SELECT playlist_id FROM playlist_tracks WHERE track_id=$1) ORDER BY id FOR UPDATE`, track)
		if err != nil {
			return err
		}
		for lists.Next() {
			var id string
			if err = lists.Scan(&id); err != nil {
				lists.Close()
				return err
			}
		}
		err = lists.Err()
		lists.Close()
		if err != nil {
			return err
		}
		// File generations cannot change between the snapshot and catalog removal.
		var same bool
		if err := tx.QueryRowContext(ctx, `SELECT COALESCE((SELECT jsonb_agg(to_jsonb(f) ORDER BY f.id) FROM files f WHERE track_id=$1),'[]')=(SELECT COALESCE(jsonb_agg(f ORDER BY f->>'id'),'[]') FROM jsonb_array_elements($2::jsonb->'files') f)`, track, payload).Scan(&same); err != nil {
			return err
		}
		if !same {
			return apperr.New(apperr.CodeConflict, "Eine Datei wurde geändert. Bitte neu laden.")
		}
		// Capture memberships after locking, so changes made while files moved
		// are preserved as well. Anchors make multi-song restore order independent.
		var title string
		var current []byte
		if err := tx.QueryRowContext(ctx, trashPayloadSQL, track).Scan(&title, &current); err != nil {
			return err
		}
		if len(current) > 8<<20 {
			return apperr.New(apperr.CodeInvalidRequest, "Zu viele Zuordnungen für eine sichere Wiederherstellung.")
		}
		if _, err := tx.ExecContext(ctx, `UPDATE library_trash SET title=$2,payload=$3 WHERE id=$1`, id, title, current); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM files WHERE track_id=$1`, track); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM tracks WHERE id=$1`, track); err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx, `UPDATE library_trash SET state='ready' WHERE id=$1`, id)
		return err
	})
}
func (c *Catalog) ListTrash(ctx context.Context, limit int, maintenance bool) ([]TrashEntry, error) {
	return c.ListTrashPage(ctx, limit, 0, maintenance)
}
func (c *Catalog) ListTrashPage(ctx context.Context, limit, offset int, maintenance bool) ([]TrashEntry, error) {
	limit = min(max(limit, 1), 100)
	offset = max(offset, 0)
	rows, err := c.db.QueryContext(ctx, `SELECT id,track_id,title,created_at,expires_at,state FROM library_trash WHERE ($1 OR state<>'preparing') ORDER BY CASE WHEN $1 THEN CASE WHEN state IN ('preparing','restoring') THEN '-infinity'::timestamptz ELSE expires_at END END ASC,created_at DESC,id DESC LIMIT $2 OFFSET $3`, maintenance, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []TrashEntry{}
	for rows.Next() {
		var e TrashEntry
		if err = rows.Scan(&e.ID, &e.TrackID, &e.Title, &e.CreatedAt, &e.ExpiresAt, &e.State); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}
func (c *Catalog) GetTrash(ctx context.Context, id string) (*TrashEntry, error) {
	var e TrashEntry
	var moves []byte
	err := c.db.QueryRowContext(ctx, `SELECT id,track_id,title,created_at,expires_at,state,payload,moves FROM library_trash WHERE id=$1`, id).Scan(&e.ID, &e.TrackID, &e.Title, &e.CreatedAt, &e.ExpiresAt, &e.State, &e.Payload, &moves)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, apperr.New(apperr.CodeTrackNotFound, "Papierkorbeintrag fehlt.")
	}
	if err != nil {
		return nil, err
	}
	err = json.Unmarshal(moves, &e.Moves)
	return &e, err
}
func (c *Catalog) SetTrashState(ctx context.Context, id, from, to string) error {
	r, err := c.db.ExecContext(ctx, `UPDATE library_trash SET state=$3 WHERE id=$1 AND state=$2`, id, from, to)
	if err != nil {
		return err
	}
	n, err := r.RowsAffected()
	if err != nil {
		return err
	}
	if n != 1 {
		return apperr.New(apperr.CodeConflict, "Der Papierkorbeintrag wird bereits verarbeitet.")
	}
	return nil
}
func (c *Catalog) ForgetTrash(ctx context.Context, id string) error {
	_, err := c.db.ExecContext(ctx, `DELETE FROM library_trash WHERE id=$1`, id)
	return err
}
func (c *Catalog) CheckTrashRestore(ctx context.Context, e *TrashEntry) error {
	var conflict bool
	err := c.db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM tracks WHERE id=$1) OR EXISTS(SELECT 1 FROM files f JOIN jsonb_array_elements($2::jsonb->'files') a ON f.id=a->>'id' OR f.path=a->>'path')`, e.TrackID, []byte(e.Payload)).Scan(&conflict)
	if err != nil {
		return err
	}
	if conflict {
		return apperr.New(apperr.CodeConflict, "Titel oder Dateipfad wurde erneut verwendet. Wiederherstellung überschreibt nichts.")
	}
	return nil
}
func (c *Catalog) RestoreTrashMetadata(ctx context.Context, e *TrashEntry) error {
	return c.db.WithTx(ctx, func(tx *sql.Tx) error {
		var payload []byte
		if err := tx.QueryRowContext(ctx, `SELECT payload FROM library_trash WHERE id=$1 AND state='restoring' FOR UPDATE`, e.ID).Scan(&payload); err != nil {
			return err
		}
		for _, q := range []string{
			`INSERT INTO tracks SELECT * FROM jsonb_populate_record(NULL::tracks,$1::jsonb->'track')`,
			`INSERT INTO files SELECT * FROM jsonb_populate_recordset(NULL::files,$1::jsonb->'files')`,
			`INSERT INTO track_sources SELECT * FROM jsonb_populate_recordset(NULL::track_sources,$1::jsonb->'sources')`,
			`INSERT INTO track_overrides SELECT * FROM jsonb_populate_recordset(NULL::track_overrides,$1::jsonb->'overrides')`,
			`INSERT INTO favorite_tracks SELECT f.* FROM jsonb_populate_recordset(NULL::favorite_tracks,$1::jsonb->'favorites') f JOIN users u ON u.id=f.user_id ON CONFLICT DO NOTHING`,
		} {
			if _, err := tx.ExecContext(ctx, q, payload); err != nil {
				return err
			}
		}
		rows, err := tx.QueryContext(ctx, `SELECT p->>'playlist_id',(p->>'position')::integer,(p->>'added_at')::timestamptz,p->'next_ids',p->'previous_ids' FROM jsonb_array_elements($1::jsonb->'playlists') p JOIN playlists l ON l.id=p->>'playlist_id' ORDER BY p->>'playlist_id'`, payload)
		if err != nil {
			return err
		}
		type membership struct {
			id                   string
			pos                  int
			at                   time.Time
			nextRaw, previousRaw []byte
		}
		members := []membership{}
		for rows.Next() {
			var m membership
			if err = rows.Scan(&m.id, &m.pos, &m.at, &m.nextRaw, &m.previousRaw); err != nil {
				rows.Close()
				return err
			}
			members = append(members, m)
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return err
		}
		for _, m := range members {
			var id string
			if err = tx.QueryRowContext(ctx, `SELECT id FROM playlists WHERE id=$1 FOR UPDATE`, m.id).Scan(&id); errors.Is(err, sql.ErrNoRows) {
				continue
			}
			if err != nil {
				return err
			}
			var count int
			if err = tx.QueryRowContext(ctx, `SELECT count(*) FROM playlist_tracks WHERE playlist_id=$1`, id).Scan(&count); err != nil {
				return err
			}
			pos := min(max(m.pos, 1), count+1)
			next, previous := []string{}, []string{}
			if err = json.Unmarshal(m.nextRaw, &next); err != nil {
				return err
			}
			if err = json.Unmarshal(m.previousRaw, &previous); err != nil {
				return err
			}
			anchored := false
			for _, n := range next {
				var at int
				err = tx.QueryRowContext(ctx, `SELECT position FROM playlist_tracks WHERE playlist_id=$1 AND track_id=$2`, id, n).Scan(&at)
				if errors.Is(err, sql.ErrNoRows) {
					continue
				}
				if err != nil {
					return err
				}
				pos = at
				anchored = true
				break
			}
			if !anchored {
				for _, n := range previous {
					var at int
					err = tx.QueryRowContext(ctx, `SELECT position FROM playlist_tracks WHERE playlist_id=$1 AND track_id=$2`, id, n).Scan(&at)
					if errors.Is(err, sql.ErrNoRows) {
						continue
					}
					if err != nil {
						return err
					}
					pos = at + 1
					break
				}
			}

			if _, err = tx.ExecContext(ctx, `UPDATE playlist_tracks SET position=position+1 WHERE playlist_id=$1 AND position>=$2`, id, pos); err != nil {
				return err
			}
			if _, err = tx.ExecContext(ctx, `INSERT INTO playlist_tracks(playlist_id,track_id,position,added_at) VALUES($1,$2,$3,$4)`, id, e.TrackID, pos, m.at); err != nil {
				return err
			}
		}
		_, err = tx.ExecContext(ctx, `DELETE FROM library_trash WHERE id=$1`, e.ID)
		return err
	})
}
