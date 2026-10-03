package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"math"
	"time"
	"ytdm/backend/internal/apperr"
	"ytdm/backend/internal/fingerprint"
	"ytdm/backend/internal/music"
)

type AudioAnalysisStatus struct {
	Pending      int      `json:"pending"`
	Ready        int      `json:"ready"`
	Inconclusive int      `json:"inconclusive"`
	Failed       int      `json:"failed"`
	NextIDs      []string `json:"next_ids"`
}

func (c *Catalog) AudioAnalysisStatus(ctx context.Context) (AudioAnalysisStatus, error) {
	var result AudioAnalysisStatus
	result.NextIDs = []string{}
	const source = ` FROM tracks t JOIN LATERAL(SELECT id,updated_at FROM files WHERE track_id=t.id ORDER BY id LIMIT 1) f ON true LEFT JOIN audio_fingerprints a ON a.track_id=t.id AND a.file_id=f.id AND a.file_updated_at=f.updated_at `
	err := c.db.QueryRowContext(ctx, `SELECT count(*) FILTER(WHERE a.track_id IS NULL),count(*) FILTER(WHERE a.state='ready'),count(*) FILTER(WHERE a.state='inconclusive'),count(*) FILTER(WHERE a.state='failed')`+source).Scan(&result.Pending, &result.Ready, &result.Inconclusive, &result.Failed)
	if err != nil {
		return result, err
	}
	rows, err := c.db.QueryContext(ctx, `SELECT t.id`+source+`WHERE a.track_id IS NULL ORDER BY t.id LIMIT 20`)
	if err != nil {
		return result, err
	}
	defer rows.Close()
	for rows.Next() {
		var id string
		if err = rows.Scan(&id); err != nil {
			return result, err
		}
		result.NextIDs = append(result.NextIDs, id)
	}
	return result, rows.Err()
}
func (c *Catalog) ResetAudioAnalysis(ctx context.Context) error {
	return c.db.WithTx(ctx, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, `DELETE FROM audio_duplicate_pairs`); err != nil {
			return err
		}
		_, err := tx.ExecContext(ctx, `DELETE FROM audio_fingerprints`)
		return err
	})
}
func (c *Catalog) SaveAudioFingerprint(ctx context.Context, trackID, fileID string, updated time.Time, size, mtime int64, words []uint32, state string) error {
	if state != "ready" && state != "failed" && state != "inconclusive" {
		return apperr.New(apperr.CodeInvalidRequest, "Ungültige Analyse.")
	}
	if len(words) > 1200 {
		return apperr.New(apperr.CodeInvalidRequest, "Analyse ist zu groß.")
	}
	if state == "ready" && !fingerprint.Usable(words) {
		state = "inconclusive"
	}
	raw, err := json.Marshal(words)
	if err != nil {
		return err
	}
	if words == nil {
		raw = []byte("[]")
	}
	return c.db.WithTx(ctx, func(tx *sql.Tx) error {
		var duration int64
		if err := tx.QueryRowContext(ctx, `SELECT t.duration_ms FROM tracks t JOIN files f ON f.track_id=t.id WHERE t.id=$1 AND f.id=$2 AND f.updated_at=$3 FOR SHARE OF t,f`, trackID, fileID, updated).Scan(&duration); err != nil {
			return apperr.New(apperr.CodeConflict, "Audiodatei wurde geändert. Bitte neu laden.")
		}
		generation := music.NewID()
		buckets := fingerprint.Buckets(words)
		if _, err = tx.ExecContext(ctx, `DELETE FROM audio_duplicate_pairs WHERE first_id=$1 OR second_id=$1`, trackID); err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx, `INSERT INTO audio_fingerprints(track_id,file_id,file_updated_at,generation,size_bytes,mtime_ns,state,words,buckets) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9::integer[]) ON CONFLICT(track_id) DO UPDATE SET file_id=EXCLUDED.file_id,file_updated_at=EXCLUDED.file_updated_at,generation=EXCLUDED.generation,size_bytes=EXCLUDED.size_bytes,mtime_ns=EXCLUDED.mtime_ns,state=EXCLUDED.state,words=EXCLUDED.words,buckets=EXCLUDED.buckets,analyzed_at=now()`, trackID, fileID, updated, generation, size, mtime, state, raw, buckets); err != nil {
			return err
		}
		if state != "ready" {
			return nil
		}
		// The inverted index bounds expensive comparisons. Reanalysis is explicit;
		// measured short songs remain downloadable and are marked inconclusive.
		rows, err := tx.QueryContext(ctx, `SELECT a.track_id,a.generation,a.words FROM audio_fingerprints a JOIN files f ON f.id=a.file_id AND f.updated_at=a.file_updated_at JOIN tracks t ON t.id=a.track_id WHERE a.track_id<>$1 AND a.state='ready' AND a.buckets&&$2::integer[] AND abs(t.duration_ms-$3)<=greatest(2000,$3*0.03) ORDER BY a.track_id LIMIT 300`, trackID, buckets, duration)
		if err != nil {
			return err
		}
		type match struct {
			id, generation string
			score          float64
		}
		matches := []match{}
		for rows.Next() {
			var id, g string
			var raw []byte
			if err = rows.Scan(&id, &g, &raw); err != nil {
				rows.Close()
				return err
			}
			var other []uint32
			if json.Unmarshal(raw, &other) != nil {
				continue
			}
			score := fingerprint.Similarity(words, other)
			if score >= 0.92 && !math.IsNaN(score) {
				matches = append(matches, match{id, g, score})
			}
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return err
		}
		for _, m := range matches {
			a, b, ag, bg := trackID, m.id, generation, m.generation
			if a > b {
				a, b, ag, bg = b, a, bg, ag
			}
			if _, err = tx.ExecContext(ctx, `INSERT INTO audio_duplicate_pairs(first_id,second_id,first_generation,second_generation,similarity) VALUES($1,$2,$3,$4,$5) ON CONFLICT(first_id,second_id) DO UPDATE SET first_generation=EXCLUDED.first_generation,second_generation=EXCLUDED.second_generation,similarity=EXCLUDED.similarity`, a, b, ag, bg, m.score); err != nil {
				return err
			}
		}
		return nil
	})
}

func (c *Catalog) FingerprintFile(ctx context.Context, id string) (*music.File, error) {
	f, err := scanFile(c.db.QueryRowContext(ctx, `SELECT `+fileColumns+` FROM files WHERE track_id=$1 ORDER BY id LIMIT 1`, id).Scan)
	if err != nil {
		return nil, apperr.New(apperr.CodeFileNotFound, "Audiodatei fehlt.")
	}
	return &f, nil
}
