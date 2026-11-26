package library

import (
	"context"
	"path/filepath"
	"strings"
	"time"

	"ytdm/backend/internal/apperr"
	"ytdm/backend/internal/database/repository"
	"ytdm/backend/internal/logging"
	"ytdm/backend/internal/lyrics"
)

// LyricsIndexStore keeps the lyrics search index (track_lyrics_search). It
// is optional: a catalog without it has no lyrics search, and the indexer
// does nothing.
type LyricsIndexStore interface {
	ListTracksNeedingLyricsIndex(ctx context.Context, limit int) ([]repository.LyricsIndexCandidate, error)
	UpsertLyricsIndex(ctx context.Context, trackID, body string, checkedAt *time.Time) error
	DeleteLyricsIndex(ctx context.Context, trackID string) error
	PruneLyricsIndex(ctx context.Context) error
}

const (
	// lyricsIndexBatch is how many sidecars one indexer pass reads.
	lyricsIndexBatch = 200
	// lyricsIndexIdle is the pause once the index has caught up; a full
	// batch is followed by lyricsIndexBusy instead, so a large library is
	// indexed in one sweep without hogging the disk.
	lyricsIndexIdle  = 10 * time.Minute
	lyricsIndexBusy  = 2 * time.Second
	lyricsIndexStart = 45 * time.Second
)

func (s *Service) lyricsIndex() (LyricsIndexStore, bool) {
	store, ok := s.catalog.(LyricsIndexStore)
	return store, ok
}

// StartLyricsIndex keeps the lyrics search index in step with the sidecars
// in the background: every track whose lyrics are marked available and
// whose index row is missing or older than its last lyrics check is read
// and indexed, a batch at a time.
func (s *Service) StartLyricsIndex() {
	if _, ok := s.lyricsIndex(); !ok {
		return
	}
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		timer := time.NewTimer(lyricsIndexStart)
		defer timer.Stop()
		for {
			select {
			case <-s.ctx.Done():
				return
			case <-timer.C:
				ctx, cancel := context.WithTimeout(s.ctx, 5*time.Minute)
				n, err := s.IndexLyricsBatch(ctx, lyricsIndexBatch)
				cancel()
				next := lyricsIndexIdle
				if err != nil {
					s.logger.Warn("lyrics search index pass failed",
						logging.KeyOperation, "lyrics_index", logging.KeyError, apperr.MessageOf(err))
				} else if n == lyricsIndexBatch {
					next = lyricsIndexBusy
				}
				timer.Reset(next)
			}
		}
	}()
}

// IndexLyricsBatch indexes up to limit tracks whose lyrics need it and
// returns how many it looked at. A track whose sidecar cannot be found or
// read is indexed with no text, so it is not read again until its lyrics
// change. Once the index has caught up, rows of tracks without lyrics are
// pruned.
func (s *Service) IndexLyricsBatch(ctx context.Context, limit int) (int, error) {
	store, ok := s.lyricsIndex()
	if !ok {
		return 0, nil
	}
	cands, err := store.ListTracksNeedingLyricsIndex(ctx, limit)
	if err != nil {
		return 0, err
	}
	for _, cand := range cands {
		if err := ctx.Err(); err != nil {
			return 0, err
		}
		body := ""
		if _, audioPath, err := s.trackAudio(ctx, cand.TrackID); err == nil {
			if path, text, err := s.library.ReadLyrics(audioPath); err == nil && path != "" {
				body = plainLyrics(path, text)
			}
		}
		if err := store.UpsertLyricsIndex(ctx, cand.TrackID, body, cand.CheckedAt); err != nil {
			return 0, err
		}
	}
	if len(cands) < limit {
		if err := store.PruneLyricsIndex(ctx); err != nil {
			return len(cands), err
		}
	}
	return len(cands), nil
}

// plainLyrics is the searchable text of a sidecar: synchronised lyrics
// without their timestamps and tags.
func plainLyrics(path, text string) string {
	if strings.EqualFold(filepath.Ext(path), ".lrc") {
		return lyrics.StripTimestamps(text)
	}
	return strings.TrimSpace(text)
}

// indexLyricsNow updates the index right after the lyrics of a track changed
// through this service, so a search finds them before the next indexer
// pass. It is best effort: the indexer repairs anything missed.
func (s *Service) indexLyricsNow(ctx context.Context, trackID, path, body string, checkedAt *time.Time) {
	store, ok := s.lyricsIndex()
	if !ok {
		return
	}
	var err error
	if path == "" {
		err = store.DeleteLyricsIndex(ctx, trackID)
	} else {
		err = store.UpsertLyricsIndex(ctx, trackID, plainLyrics(path, body), checkedAt)
	}
	if err != nil {
		s.logger.Warn("lyrics search index update failed",
			logging.KeyOperation, "lyrics_index", logging.KeyError, apperr.MessageOf(err))
	}
}
