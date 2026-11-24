// Package artistgenres gives library artists the genres their metadata
// provider knows. Downloads store an artist under a synthetic source without
// genres, so the genres are looked up afterwards: in a slow background pass
// over the library, and right after a subscription sync for the subscribed
// artist. Genres an administrator set by hand, or that are already there,
// are never replaced.
package artistgenres

import (
	"context"
	"errors"
	"log/slog"
	"slices"
	"strings"
	"sync"
	"time"

	"ytdm/backend/internal/apperr"
	"ytdm/backend/internal/database/repository"
	"ytdm/backend/internal/logging"
	"ytdm/backend/internal/matcher"
)

// Source answers genre questions at one metadata provider.
type Source interface {
	Name() string
	// ArtistGenres returns the provider's genres of an artist, most
	// significant first; none is a valid answer.
	ArtistGenres(ctx context.Context, artistID string) ([]string, error)
	// AlbumArtist returns the main artist of an album at the provider.
	AlbumArtist(ctx context.Context, albumID string) (id, name string, err error)
}

// Store is the catalogue side.
type Store interface {
	ArtistsMissingGenres(ctx context.Context, provider string, recheck repository.GenreRecheck, limit int) ([]repository.GenreCandidate, error)
	StoreArtistGenreLookup(ctx context.Context, artistID string, genres []string) error
	RecordArtistGenreFailure(ctx context.Context, artistID string) error
	ArtistMissingGenresBySource(ctx context.Context, provider, sourceID, name string, recheck repository.GenreRecheck) (string, error)
}

// Options configures the backfill. Zero durations and counts take the
// defaults below.
type Options struct {
	Store  Store
	Source Source
	Logger *slog.Logger
	// StartDelay keeps the first pass away from the start of the backend.
	StartDelay time.Duration
	// Pace is the pause between two artists. The provider's own limiter
	// paces the requests on top; this keeps the backfill a trickle beside
	// downloads and syncs.
	Pace time.Duration
	// Idle is the pause between passes; a pass picks up artists added since.
	Idle time.Duration
	// Retry is the pause after a pass the provider failed.
	Retry time.Duration
	// Recheck is how long an artist without provider genres is left alone.
	Recheck time.Duration
	// FailedRecheck is how long an artist the provider failed on is left alone.
	FailedRecheck time.Duration
	// MaxPerPass bounds the artists one pass looks up.
	MaxPerPass int
}

const (
	defaultStartDelay = 2 * time.Minute
	defaultPace       = 2 * time.Second
	defaultIdle       = 6 * time.Hour
	defaultRetry      = 15 * time.Minute
	defaultRecheck    = 30 * 24 * time.Hour
	defaultFailed     = 24 * time.Hour
	defaultMaxPerPass = 1000
	batchSize         = 100
	// maxConsecutiveFailures failed artists in a row mean the provider is
	// down rather than one artist being broken.
	maxConsecutiveFailures = 3
)

// Backfill looks up genres for library artists in the background.
type Backfill struct {
	opts   Options
	cancel context.CancelFunc
	wg     sync.WaitGroup
	stop   sync.Once
}

// New returns a backfill; Start runs it.
func New(opts Options) *Backfill {
	if opts.StartDelay <= 0 {
		opts.StartDelay = defaultStartDelay
	}
	if opts.Pace < 0 {
		opts.Pace = 0
	} else if opts.Pace == 0 {
		opts.Pace = defaultPace
	}
	if opts.Idle <= 0 {
		opts.Idle = defaultIdle
	}
	if opts.Retry <= 0 {
		opts.Retry = defaultRetry
	}
	if opts.Recheck <= 0 {
		opts.Recheck = defaultRecheck
	}
	if opts.FailedRecheck <= 0 {
		opts.FailedRecheck = defaultFailed
	}
	if opts.MaxPerPass <= 0 {
		opts.MaxPerPass = defaultMaxPerPass
	}
	if opts.Logger == nil {
		opts.Logger = slog.Default()
	}
	return &Backfill{opts: opts}
}

// Start runs passes in the background until Stop: the first after
// StartDelay, then one every Idle, or after Retry when the provider failed.
// It returns at once and never delays the start of the backend.
func (b *Backfill) Start(ctx context.Context) {
	ctx, b.cancel = context.WithCancel(ctx)
	b.wg.Add(1)
	go func() {
		defer b.wg.Done()
		wait := b.opts.StartDelay
		for sleep(ctx, wait) {
			n, err := b.RunPass(ctx)
			if ctx.Err() != nil {
				return
			}
			wait = b.opts.Idle
			if err != nil {
				wait = b.opts.Retry
				b.opts.Logger.Warn("artist genre backfill paused",
					logging.KeyProvider, b.opts.Source.Name(), "artists", n,
					"retry_in", wait.String(), logging.KeyError, err.Error())
			} else if n > 0 {
				b.opts.Logger.Info("artist genre backfill pass finished",
					logging.KeyProvider, b.opts.Source.Name(), "artists", n)
			}
		}
	}()
}

// Stop ends a pass at the next artist and waits for it.
func (b *Backfill) Stop() {
	b.stop.Do(func() {
		if b.cancel != nil {
			b.cancel()
		}
		b.wg.Wait()
	})
}

// RunPass looks up at most MaxPerPass artists and reports how many it
// handled. One artist the provider fails on (a broken answer, an error
// Deezer gives for that artist only) does not hold up the rest: it is
// recorded as a failed lookup, asked again after FailedRecheck, and the pass
// goes on. The pass stops on a rate limit, on a database error, or after
// maxConsecutiveFailures failures in a row - then the provider itself is
// down, and those artists are not marked, so the next pass asks them first.
func (b *Backfill) RunPass(ctx context.Context) (int, error) {
	done := 0
	var failed []string
	flush := func() error {
		for _, id := range failed {
			if err := b.opts.Store.RecordArtistGenreFailure(ctx, id); err != nil {
				return err
			}
		}
		failed = failed[:0]
		return nil
	}
	for done < b.opts.MaxPerPass {
		batch, err := b.opts.Store.ArtistsMissingGenres(ctx, b.opts.Source.Name(), b.recheck(), min(batchSize, b.opts.MaxPerPass-done))
		if err != nil {
			return done, err
		}
		if len(batch) == 0 {
			break
		}
		progressed := false
		for _, cand := range batch {
			if slices.Contains(failed, cand.ArtistID) {
				continue // not recorded yet; asked again by the next pass
			}
			if done > 0 && !sleep(ctx, b.opts.Pace) {
				return done, ctx.Err()
			}
			done++
			err := b.lookup(ctx, cand)
			switch {
			case err == nil:
				progressed = true
				if err := flush(); err != nil {
					return done, err
				}
			case stopsPass(err):
				return done, err
			default:
				failed = append(failed, cand.ArtistID)
				if len(failed) >= maxConsecutiveFailures {
					return done, err
				}
			}
			if done >= b.opts.MaxPerPass {
				break
			}
		}
		if !progressed && len(failed) > 0 {
			// Only failures left in this batch: record them so the next
			// batch query moves past them.
			if err := flush(); err != nil {
				return done, err
			}
		}
	}
	return done, flush()
}

// EnrichSubscribed looks up genres for the library artist a subscription
// stands for, right after its sync. Best effort: without a library artist
// that still needs genres it asks the provider nothing.
func (b *Backfill) EnrichSubscribed(ctx context.Context, provider, sourceID, name string) {
	if provider != b.opts.Source.Name() || strings.TrimSpace(sourceID) == "" {
		return
	}
	artistID, err := b.opts.Store.ArtistMissingGenresBySource(ctx, provider, sourceID, name, b.recheck())
	if err != nil || artistID == "" {
		return
	}
	if err := b.lookup(ctx, repository.GenreCandidate{ArtistID: artistID, Name: name, SourceArtistID: sourceID}); err != nil {
		if !stopsPass(err) {
			_ = b.opts.Store.RecordArtistGenreFailure(ctx, artistID)
		}
		b.opts.Logger.Debug("artist genres after subscription sync failed",
			logging.KeyProvider, provider, logging.KeyError, err.Error())
	}
}

// lookup asks the provider for one artist and records the answer. An artist
// the provider does not know is recorded as having no genres; any other
// error leaves the artist unrecorded for the caller to classify.
func (b *Backfill) lookup(ctx context.Context, cand repository.GenreCandidate) error {
	artistID := cand.SourceArtistID
	if artistID == "" && cand.SourceAlbumID != "" {
		id, name, err := b.opts.Source.AlbumArtist(ctx, cand.SourceAlbumID)
		if err != nil && !perArtist(err) {
			return err
		}
		// The album's main artist only counts when it is this artist; a
		// compilation or a feature would lend another artist's genres.
		if err == nil && matcher.NormalizeArtist(name) == matcher.NormalizeArtist(cand.Name) {
			artistID = id
		}
	}
	var genres []string
	if artistID != "" {
		found, err := b.opts.Source.ArtistGenres(ctx, artistID)
		if err != nil && !perArtist(err) {
			return err
		}
		genres = found
	}
	return b.opts.Store.StoreArtistGenreLookup(ctx, cand.ArtistID, genres)
}

func (b *Backfill) recheck() repository.GenreRecheck {
	return repository.GenreRecheck{NoGenres: b.opts.Recheck, Failed: b.opts.FailedRecheck}
}

// stopsPass reports an error that is not about one artist: the provider
// rate limits, the run was cancelled, or the database failed.
func stopsPass(err error) bool {
	switch apperr.CodeOf(err) {
	case apperr.CodeProviderRateLimited, apperr.CodeJobCancelled, apperr.CodeInternal:
		return true
	}
	return errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded)
}

// perArtist reports an error about one artist or album rather than the
// provider: it is recorded and the pass goes on.
func perArtist(err error) bool {
	switch apperr.CodeOf(err) {
	case apperr.CodeArtistNotFound, apperr.CodeReleaseNotFound, apperr.CodeProviderNotFound, apperr.CodeInvalidRequest:
		return true
	}
	return false
}

func sleep(ctx context.Context, d time.Duration) bool {
	if d <= 0 {
		return ctx.Err() == nil
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-t.C:
		return true
	}
}
