package library

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"time"

	"ytdm/backend/internal/apperr"
	"ytdm/backend/internal/lyrics"
	"ytdm/backend/internal/music"
)

// TrackLyricsResult describes the lyrics of one track.
//
// State is what the caller should believe. It is reconciled with the
// filesystem on every read, because the sidecar file is the source of truth
// for the text and the catalogue can drift away from it — a file removed by
// hand, a library restored from a backup, a sidecar written by another tool.
type TrackLyricsResult struct {
	TrackID   string            `json:"track_id"`
	State     music.LyricsState `json:"state"`
	Provider  string            `json:"provider,omitempty"`
	Synced    bool              `json:"synced"`
	Path      string            `json:"path,omitempty"`
	Content   string            `json:"content,omitempty"`
	CheckedAt *time.Time        `json:"checked_at,omitempty"`
	// Drifted reports that the catalogue and the filesystem disagreed and the
	// filesystem won.
	Drifted bool `json:"drifted,omitempty"`
}

// TrackLyrics reads the lyrics sidecar of a track.
func (s *Service) TrackLyrics(ctx context.Context, trackID string) (*TrackLyricsResult, error) {
	track, audioPath, err := s.trackAudio(ctx, trackID)
	if err != nil {
		return nil, err
	}

	result := &TrackLyricsResult{
		TrackID:   trackID,
		State:     track.DisplayLyricsState(),
		Provider:  track.LyricsProvider,
		CheckedAt: track.LyricsCheckedAt,
	}

	path, body, err := s.library.ReadLyrics(audioPath)
	if err != nil {
		return nil, err
	}

	if path == "" {
		// The catalogue claims a sidecar that is not there. Reporting the
		// claim would tell the user lyrics exist when nothing can show them,
		// so the filesystem wins and the drift is made visible.
		if result.State.HasSidecar() {
			result.Drifted = true
			result.State = music.LyricsUnknown
			result.Provider = ""
			result.CheckedAt = nil
		}
		return result, nil
	}

	result.Path = s.library.RelPath(path)
	result.Content = body
	result.Synced = strings.EqualFold(filepath.Ext(path), ".lrc")

	fileState := music.LyricsAvailablePlain
	if result.Synced {
		fileState = music.LyricsAvailableSynced
	}
	if result.State != fileState {
		result.Drifted = true
		result.State = fileState
	}
	return result, nil
}

// RefreshTrackLyrics re-resolves the lyrics of one track and rewrites its
// sidecar.
//
// A miss is a normal outcome and is recorded so the cooldown can start. A
// failed lookup is not recorded at all: nothing was learned about the track,
// and writing a negative result would both lie and suppress the next attempt.
func (s *Service) RefreshTrackLyrics(ctx context.Context, trackID string) (*TrackLyricsResult, error) {
	if s.lyrics == nil {
		return nil, apperr.New(apperr.CodeInternal, "No lyrics resolver is configured.")
	}

	unlock := s.locks.Lock("track:" + strings.TrimSpace(trackID))
	defer unlock()

	track, audioPath, err := s.trackAudio(ctx, trackID)
	if err != nil {
		return nil, err
	}

	// Existing lyrics priority: existing valid .lrc or .txt must never be overwritten.
	if existingPath, existingBody, _ := s.library.ReadLyrics(audioPath); existingPath != "" && strings.TrimSpace(existingBody) != "" {
		return s.TrackLyrics(ctx, trackID)
	}

	resolved, resolveErr := s.lyrics.Resolve(ctx, *track, s.mediaSourceID(ctx, trackID))

	switch {
	case resolveErr != nil && errors.Is(resolveErr, lyrics.ErrNoLyrics):
		if err := s.library.RemoveLyrics(audioPath); err != nil {
			return nil, err
		}
		checkedAt := time.Now().UTC()
		if err := s.catalog.SetLyricsState(ctx, trackID, music.LyricsNotFound, "", checkedAt); err != nil {
			return nil, err
		}
		return &TrackLyricsResult{TrackID: trackID, State: music.LyricsNotFound, CheckedAt: &checkedAt}, nil

	case resolveErr != nil:
		// Includes rate limits, which the caller has to see so it can honour
		// the Retry-After the provider asked for.
		return nil, resolveErr

	case resolved == nil:
		if err := s.library.RemoveLyrics(audioPath); err != nil {
			return nil, err
		}
		checkedAt := time.Now().UTC()
		if err := s.catalog.SetLyricsState(ctx, trackID, music.LyricsNotFound, "", checkedAt); err != nil {
			return nil, err
		}
		return &TrackLyricsResult{TrackID: trackID, State: music.LyricsNotFound, CheckedAt: &checkedAt}, nil
	}

	state := resolved.State()
	result := &TrackLyricsResult{TrackID: trackID, State: state, Provider: resolved.Provider}

	path, err := s.library.WriteLyrics(audioPath, *resolved)
	if err != nil {
		return nil, err
	}
	if path != "" {
		result.Path = s.library.RelPath(path)
		result.Content = resolved.Body()
		result.Synced = state == music.LyricsAvailableSynced
	}

	checkedAt := time.Now().UTC()
	if err := s.catalog.SetLyricsState(ctx, trackID, state, resolved.Provider, checkedAt); err != nil {
		return nil, err
	}
	result.CheckedAt = &checkedAt
	return result, nil
}

// DeleteTrackLyrics removes the sidecar of a track and forgets the recorded
// state, so a later refresh asks the providers again.
func (s *Service) DeleteTrackLyrics(ctx context.Context, trackID string) error {
	unlock := s.locks.Lock("track:" + strings.TrimSpace(trackID))
	defer unlock()

	_, audioPath, err := s.trackAudio(ctx, trackID)
	if err != nil {
		return err
	}
	if err := s.library.RemoveLyrics(audioPath); err != nil {
		return err
	}
	return s.catalog.SetLyricsState(ctx, trackID, music.LyricsUnknown, "", time.Time{})
}

// trackAudio resolves a track and the absolute path of its first existing
// audio file.
func (s *Service) trackAudio(ctx context.Context, trackID string) (*music.Track, string, error) {
	trackID = strings.TrimSpace(trackID)
	if trackID == "" {
		return nil, "", apperr.New(apperr.CodeInvalidRequest, "track_id is required.")
	}
	track, err := s.catalog.GetTrack(ctx, trackID)
	if err != nil {
		return nil, "", err
	}
	files, err := s.files.ListByTrack(ctx, trackID)
	if err != nil {
		return nil, "", err
	}
	for _, file := range files {
		if !IsSupportedAudio(filepath.Ext(file.Path)) {
			continue
		}
		abs, _, confErr := VerifyPathConfinement(s.library.Root(), file.Path, false)
		if confErr == nil {
			return track, abs, nil
		}
	}
	return nil, "", apperr.New(apperr.CodeFileNotFound, "No library file exists for this track.")
}

// mediaSourceID returns the media provider's own id for a track, which the
// YouTube Music lyrics fallback needs in order to look up the very recording
// that was downloaded rather than a similarly named one.
func (s *Service) mediaSourceID(ctx context.Context, trackID string) string {
	sources, err := s.catalog.ListSources(ctx, trackID)
	if err != nil {
		return ""
	}
	for _, source := range sources {
		if source.Kind == music.SourceMedia {
			return source.SourceID
		}
	}
	return ""
}
