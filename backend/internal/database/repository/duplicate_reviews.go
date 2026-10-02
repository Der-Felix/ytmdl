package repository

import (
	"context"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"

	"ytdm/backend/internal/apperr"
)

// Hash JSON tuples rather than concatenating title/artist strings with a delimiter.
// The snapshot covers all members, file generations and effective comparison metadata.
const duplicateCTE = `WITH members AS (
 SELECT t.id,t.created_at,lower(btrim(t.title)) AS title_key,
 lower(btrim(CASE WHEN COALESCE(o.album_artist,t.album_artist)<>''
 THEN COALESCE(o.album_artist,t.album_artist) ELSE COALESCE(o.artists_json,t.artists_json)::text END)) AS artist_key,
 jsonb_build_array(t.id,t.updated_at,t.duration_ms,COALESCE(o.album,t.album),COALESCE(o.year,t.year),
  (SELECT jsonb_agg(jsonb_build_array(f.id,f.path,f.updated_at,f.size_bytes,f.codec,f.bitrate_kbps) ORDER BY f.id) FROM files f WHERE f.track_id=t.id)) AS snapshot
 FROM tracks t LEFT JOIN track_overrides o ON o.track_id=t.id
 WHERE EXISTS(SELECT 1 FROM files f WHERE f.track_id=t.id)
), grouped AS (
 SELECT encode(sha256(convert_to(jsonb_build_array(title_key,artist_key)::text,'UTF8')),'hex') AS key,
 count(*) AS count,array_agg(id ORDER BY created_at,id) AS ids,
 encode(sha256(convert_to(jsonb_agg(snapshot ORDER BY id)::text,'UTF8')),'hex') AS fingerprint
 FROM members GROUP BY title_key,artist_key HAVING count(*)>1
) `

type DuplicateReview struct {
	GroupKey         string `json:"group_key"`
	Fingerprint      string `json:"fingerprint"`
	Outcome          string `json:"outcome"`
	PreferredTrackID string `json:"preferred_track_id"`
}

func validDuplicateHash(s string) bool {
	if len(s) != 64 {
		return false
	}
	_, err := hex.DecodeString(s)
	return err == nil
}

func (c *Catalog) DuplicateGroupsForUser(ctx context.Context, userID string, offset int, includeReviewed bool, after string) ([]DuplicateGroup, error) {
	if after != "" && !validDuplicateHash(after) {
		return nil, apperr.New(apperr.CodeInvalidRequest, "Ungültige nächste Seite.")
	}
	rows, err := c.db.QueryContext(ctx, duplicateCTE+`SELECT g.key,g.count,to_json(g.ids[1:100]),g.fingerprint,
 CASE WHEN r.fingerprint=g.fingerprint THEN r.outcome ELSE '' END,COALESCE(r.preferred_track_id,'')
 FROM grouped g LEFT JOIN duplicate_reviews r ON r.user_id=$1 AND r.group_key=g.key
 WHERE ($2 OR r.group_key IS NULL OR r.fingerprint<>g.fingerprint) AND ($4='' OR g.key>$4)
 ORDER BY g.key LIMIT 20 OFFSET $3`, userID, includeReviewed, clampOffset(offset), after)
	if err != nil {
		return nil, wrapDB("duplicate review candidates", err)
	}
	out := []DuplicateGroup{}
	ids := [][]string{}
	for rows.Next() {
		var g DuplicateGroup
		var raw []byte
		if err = rows.Scan(&g.Key, &g.Count, &raw, &g.Fingerprint, &g.Outcome, &g.PreferredTrackID); err != nil {
			rows.Close()
			return nil, err
		}
		var list []string
		if err = json.Unmarshal(raw, &list); err != nil {
			rows.Close()
			return nil, err
		}
		out = append(out, g)
		ids = append(ids, list)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	for i, list := range ids {
		out[i].Tracks, err = c.TracksByIDs(ctx, list)
		if err != nil {
			return nil, err
		}
	}
	return out, nil
}

func duplicateSnapshot(ctx context.Context, tx *sql.Tx, key string) (string, []string, error) {
	var fingerprint string
	var raw []byte
	err := tx.QueryRowContext(ctx, duplicateCTE+`SELECT fingerprint,to_json(ids) FROM grouped WHERE key=$1`, key).Scan(&fingerprint, &raw)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil, apperr.New(apperr.CodeConflict, "Die Gruppe hat sich geändert. Bitte neu laden.")
	}
	if err != nil {
		return "", nil, err
	}
	var ids []string
	err = json.Unmarshal(raw, &ids)
	return fingerprint, ids, err
}

func (c *Catalog) SaveDuplicateReview(ctx context.Context, userID string, r DuplicateReview) error {
	if !validDuplicateHash(r.GroupKey) || !validDuplicateHash(r.Fingerprint) || (r.Outcome != "preferred" && r.Outcome != "distinct") || (r.Outcome == "preferred" && r.PreferredTrackID == "") || (r.Outcome == "distinct" && r.PreferredTrackID != "") {
		return apperr.New(apperr.CodeInvalidRequest, "Ungültige Vergleichsentscheidung.")
	}
	return c.db.WithTx(ctx, func(tx *sql.Tx) error {
		fingerprint, ids, err := duplicateSnapshot(ctx, tx, r.GroupKey)
		if err != nil {
			return err
		}
		if fingerprint != r.Fingerprint {
			return apperr.New(apperr.CodeConflict, "Die Versionen haben sich geändert. Bitte neu vergleichen.")
		}
		if len(ids) > 100 {
			return apperr.New(apperr.CodeInvalidRequest, "Für mehr als 100 Versionen bitte die Tabellenansicht verwenden.")
		}
		found := r.Outcome == "distinct"
		for _, id := range ids {
			if id == r.PreferredTrackID {
				found = true
			}
		}
		if !found {
			return apperr.New(apperr.CodeInvalidRequest, "Die bevorzugte Version gehört nicht zu dieser Gruppe.")
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO duplicate_reviews(user_id,group_key,fingerprint,outcome,preferred_track_id) VALUES($1,$2,$3,$4,NULLIF($5,'')) ON CONFLICT(user_id,group_key) DO UPDATE SET fingerprint=EXCLUDED.fingerprint,outcome=EXCLUDED.outcome,preferred_track_id=EXCLUDED.preferred_track_id,updated_at=now()`, userID, r.GroupKey, r.Fingerprint, r.Outcome, r.PreferredTrackID)
		return err
	})
}

func (c *Catalog) ResetDuplicateReview(ctx context.Context, userID, key string) error {
	if !validDuplicateHash(key) {
		return apperr.New(apperr.CodeInvalidRequest, "Ungültige Gruppe.")
	}
	_, err := c.db.ExecContext(ctx, `DELETE FROM duplicate_reviews WHERE user_id=$1 AND group_key=$2`, userID, key)
	return err
}

// Validate the entire confirmed deletion set before any file is touched.
func (c *Catalog) ValidateDuplicateRemoval(ctx context.Context, userID string, r DuplicateReview, remove []string) error {
	if !validDuplicateHash(r.GroupKey) || !validDuplicateHash(r.Fingerprint) || r.PreferredTrackID == "" || len(remove) < 1 || len(remove) > 99 {
		return apperr.New(apperr.CodeInvalidRequest, "Bitte 1–99 andere Versionen auswählen.")
	}
	return c.db.WithTx(ctx, func(tx *sql.Tx) error {
		fingerprint, ids, err := duplicateSnapshot(ctx, tx, r.GroupKey)
		if err != nil {
			return err
		}
		if fingerprint != r.Fingerprint {
			return apperr.New(apperr.CodeConflict, "Die Gruppe hat sich geändert. Bitte vor dem Löschen neu vergleichen.")
		}
		var preferred string
		err = tx.QueryRowContext(ctx, `SELECT preferred_track_id FROM duplicate_reviews WHERE user_id=$1 AND group_key=$2 AND fingerprint=$3 AND outcome='preferred'`, userID, r.GroupKey, r.Fingerprint).Scan(&preferred)
		if errors.Is(err, sql.ErrNoRows) || err == nil && preferred != r.PreferredTrackID {
			return apperr.New(apperr.CodeInvalidRequest, "Bitte zuerst die bevorzugte Version speichern.")
		}
		if err != nil {
			return err
		}
		members := map[string]bool{}
		for _, id := range ids {
			members[id] = true
		}
		if !members[preferred] {
			return apperr.New(apperr.CodeConflict, "Die bevorzugte Version fehlt. Bitte neu vergleichen.")
		}
		seen := map[string]bool{}
		for _, id := range remove {
			if id == preferred || !members[id] || seen[id] {
				return apperr.New(apperr.CodeInvalidRequest, "Die Löschliste enthält eine ungültige oder bevorzugte Version.")
			}
			seen[id] = true
		}
		return nil
	})
}
