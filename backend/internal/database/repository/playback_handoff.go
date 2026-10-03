package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"math"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
	"ytdm/backend/internal/apperr"
	"ytdm/backend/internal/music"
)

type PlaybackHandoff struct {
	ID              string               `json:"id"`
	QueueIDs        []string             `json:"queue_ids"`
	Queue           []music.LibraryTrack `json:"queue"`
	QueueIndex      int                  `json:"queue_index"`
	PositionSeconds float64              `json:"position_seconds"`
	RepeatMode      string               `json:"repeat_mode"`
	SourceName      string               `json:"source_name"`
	CreatedAt       time.Time            `json:"created_at"`
	ExpiresAt       time.Time            `json:"expires_at"`
}

func (c *Catalog) SavePlaybackHandoff(ctx context.Context, user string, h PlaybackHandoff) (*PlaybackHandoff, error) {
	h.SourceName = strings.TrimSpace(h.SourceName)
	if len(h.QueueIDs) < 1 || len(h.QueueIDs) > 500 || h.QueueIndex < 0 || h.QueueIndex >= len(h.QueueIDs) || math.IsNaN(h.PositionSeconds) || math.IsInf(h.PositionSeconds, 0) || h.PositionSeconds < 0 || h.PositionSeconds > 86400 || utf8.RuneCountInString(h.SourceName) > 80 || strings.ContainsFunc(h.SourceName, unicode.IsControl) || (h.RepeatMode != "off" && h.RepeatMode != "queue" && h.RepeatMode != "track") {
		return nil, apperr.New(apperr.CodeInvalidRequest, "Übergabe benötigt 1–500 Titel, eine gültige Position und höchstens 80 Zeichen Gerätename.")
	}
	for _, id := range h.QueueIDs {
		if len(id) < 1 || len(id) > 200 {
			return nil, apperr.New(apperr.CodeInvalidRequest, "Ungültige Warteschlange.")
		}
	}
	raw, err := json.Marshal(h.QueueIDs)
	if err != nil {
		return nil, err
	}
	h.ID = music.NewID()
	err = c.db.WithTx(ctx, func(tx *sql.Tx) error {
		var missing int
		if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM jsonb_array_elements_text($1::jsonb) q WHERE NOT EXISTS(SELECT 1 FROM files f WHERE f.track_id=q)`, raw).Scan(&missing); err != nil {
			return err
		}
		if missing > 0 {
			return apperr.New(apperr.CodeConflict, "Die Warteschlange enthält nicht mehr verfügbare Titel. Bitte entfernen oder eine neue Liste starten.")
		}
		return tx.QueryRowContext(ctx, `INSERT INTO playback_handoffs(user_id,id,queue_ids,queue_index,position_seconds,repeat_mode,source_name)VALUES($1,$2,$3,$4,$5,$6,$7) ON CONFLICT(user_id) DO UPDATE SET id=EXCLUDED.id,queue_ids=EXCLUDED.queue_ids,queue_index=EXCLUDED.queue_index,position_seconds=EXCLUDED.position_seconds,repeat_mode=EXCLUDED.repeat_mode,source_name=EXCLUDED.source_name,created_at=now(),expires_at=now()+interval '15 minutes' RETURNING created_at,expires_at`, user, h.ID, raw, h.QueueIndex, h.PositionSeconds, h.RepeatMode, h.SourceName).Scan(&h.CreatedAt, &h.ExpiresAt)
	})
	return &h, err
}
func (c *Catalog) PlaybackHandoff(ctx context.Context, user string) (*PlaybackHandoff, error) {
	var h PlaybackHandoff
	var raw []byte
	err := c.db.QueryRowContext(ctx, `SELECT id,queue_ids,queue_index,position_seconds,repeat_mode,source_name,created_at,expires_at FROM playback_handoffs WHERE user_id=$1 AND expires_at>now()`, user).Scan(&h.ID, &raw, &h.QueueIndex, &h.PositionSeconds, &h.RepeatMode, &h.SourceName, &h.CreatedAt, &h.ExpiresAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if err = json.Unmarshal(raw, &h.QueueIDs); err != nil {
		return nil, err
	}
	var missing int
	if err = c.db.QueryRowContext(ctx, `SELECT count(*) FROM jsonb_array_elements_text($1::jsonb) q WHERE NOT EXISTS(SELECT 1 FROM files f WHERE f.track_id=q)`, raw).Scan(&missing); err != nil {
		return nil, err
	}
	if missing > 0 {
		return nil, apperr.New(apperr.CodeConflict, "Eine übergebene Datei fehlt inzwischen. Bitte neu übertragen.")
	}
	h.Queue, err = c.TracksByIDs(ctx, h.QueueIDs)
	if err != nil {
		return nil, err
	}
	if len(h.Queue) != len(h.QueueIDs) || h.QueueIndex >= len(h.Queue) {
		return nil, apperr.New(apperr.CodeConflict, "Eine übergebene Version fehlt inzwischen. Bitte am Ausgangsgerät neu übertragen.")
	}
	if duration := h.Queue[h.QueueIndex].DurationMS; duration > 0 {
		h.PositionSeconds = math.Min(h.PositionSeconds, math.Max(0, float64(duration)/1000-0.25))
	}
	return &h, nil
}
func (c *Catalog) DeletePlaybackHandoff(ctx context.Context, user, id string) error {
	result, err := c.db.ExecContext(ctx, `DELETE FROM playback_handoffs WHERE user_id=$1 AND id=$2`, user, id)
	if err != nil {
		return err
	}
	n, _ := result.RowsAffected()
	if n != 1 {
		return apperr.New(apperr.CodeConflict, "Die Übergabe hat sich geändert. Bitte neu laden.")
	}
	return nil
}
