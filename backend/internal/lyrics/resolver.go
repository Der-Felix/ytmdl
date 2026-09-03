package lyrics

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"ytdm/backend/internal/apperr"
	"ytdm/backend/internal/logging"
	"ytdm/backend/internal/music"
	"ytdm/backend/internal/provider"
)

// ErrNoLyrics reports that every provider answered and none had an entry. It
// is a definitive result: the caller may record it and start the cooldown.
var ErrNoLyrics = errors.New("no lyrics available")

// ErrLookupFailed reports that at least one provider could not answer at all —
// a timeout, an outage, an unparsable response.
//
// It is deliberately distinct from ErrNoLyrics. A caller must not record it as
// "this track has no lyrics", because it says nothing about the track; it says
// something about the network. Recording it would poison the catalogue with a
// negative result and, worse, start a cooldown that keeps the backfill from
// ever trying again.
var ErrLookupFailed = errors.New("the lyrics lookup failed")

// DefaultRatePerSecond is the sustained lookup rate: 400 ms between requests,
// inside the 200 to 500 ms range LRCLIB asks batch clients to use.
const DefaultRatePerSecond = 2.5

// DefaultTimeout bounds one full resolve across every provider.
const DefaultTimeout = 15 * time.Second

// ResolverOptions configures the resolver.
type ResolverOptions struct {
	// Providers are tried in order. The first definitive answer wins.
	Providers     []provider.LyricsProvider
	RatePerSecond float64
	Timeout       time.Duration
	Logger        *slog.Logger
}

// Resolver asks the configured lyrics providers in order and returns the first
// usable answer.
type Resolver struct {
	providers []provider.LyricsProvider
	limiter   *Limiter
	timeout   time.Duration
	logger    *slog.Logger
}

// NewResolver builds a resolver.
func NewResolver(opts ResolverOptions) *Resolver {
	rate := opts.RatePerSecond
	if rate <= 0 {
		rate = DefaultRatePerSecond
	}
	timeout := opts.Timeout
	if timeout <= 0 {
		timeout = DefaultTimeout
	}
	logger := opts.Logger
	if logger == nil {
		logger = slog.Default()
	}
	return &Resolver{
		providers: opts.Providers,
		limiter:   NewLimiter(rate, 1),
		timeout:   timeout,
		logger:    logger,
	}
}

// Resolve returns the lyrics of a track.
//
// Synchronised lyrics win over plain ones, and a provider that reports a track
// as instrumental ends the search: that is a positive answer, and asking the
// next provider could only produce a worse one.
//
// The three outcomes are kept apart on purpose, because the catalogue records
// them differently:
//
//   - a result and no error: lyrics were found
//   - ErrNoLyrics: every provider answered and none had the track
//   - anything else: nothing was learned about the track
//
// A rate limit is returned unwrapped so the caller can honour its Retry-After.
func (r *Resolver) Resolve(ctx context.Context, track music.Track, mediaID string) (*music.Lyrics, error) {
	if len(r.providers) == 0 {
		return nil, apperr.Wrap(apperr.CodeInternal, "No lyrics provider is configured.", ErrLookupFailed)
	}

	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()

	failed := false
	for _, p := range r.providers {
		if err := ctx.Err(); err != nil {
			return nil, apperr.Wrap(apperr.CodeProviderUnavailable,
				"The lyrics lookup ran out of time.", errors.Join(ErrLookupFailed, err))
		}
		if err := r.limiter.Wait(ctx); err != nil {
			return nil, apperr.Wrap(apperr.CodeProviderUnavailable,
				"The lyrics lookup was cut short.", errors.Join(ErrLookupFailed, err))
		}

		result, err := p.Lyrics(ctx, track, mediaID)
		if err != nil {
			if wait, limited := RetryAfter(err); limited {
				r.logger.Warn("lyrics provider is rate limiting",
					logging.KeyProvider, p.Name(),
					logging.KeyOperation, "lyrics",
					"retry_after_ms", wait.Milliseconds())
				return nil, err
			}
			failed = true
			r.logger.Warn("lyrics provider failed",
				logging.KeyProvider, p.Name(),
				logging.KeyOperation, "lyrics",
				logging.KeyErrorCode, string(apperr.CodeOf(err)),
				logging.KeyError, err.Error())
			continue
		}
		if result == nil {
			continue
		}
		switch result.State() {
		case music.LyricsAvailableSynced, music.LyricsAvailablePlain, music.LyricsInstrumental:
			return result, nil
		}
	}

	if failed {
		return nil, apperr.Wrapf(apperr.CodeProviderUnavailable, ErrLookupFailed,
			"The lyrics of %q could not be looked up.", track.Label())
	}
	return nil, apperr.Wrapf(apperr.CodeFileNotFound, ErrNoLyrics,
		"No lyrics were found for %q.", track.Label())
}
