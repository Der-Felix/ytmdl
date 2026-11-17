// Package bgmigration holds what the background data migrations of the backend
// share: when they run, how they are retried and where they keep their marker.
// A data migration rewrites stored data after an update (file names, identity
// keys) without holding up the start: the schema migrations run before the
// backend serves, these run next to it.
package bgmigration

import (
	"context"
	"encoding/json"
	"log/slog"
	"strconv"
	"strings"
	"sync"
	"time"

	"ytdm/backend/internal/logging"
)

// Settings is the key value store a migration keeps its marker, status and
// journal in. The settings table implements it, so the state survives a
// restart and a second backend process sees the same state.
type Settings interface {
	Get(ctx context.Context, key string) (string, bool, error)
	Set(ctx context.Context, key, value string) error
}

// Schedule runs run in the background: once after delay, and again after
// retry for as long as it returns an error. A nil error means the migration
// reached a state that only the next start can change (done, or left rows for
// review), so it is not repeated. wg is incremented for the goroutine, so a
// shutdown can wait for a run in progress.
func Schedule(ctx context.Context, wg *sync.WaitGroup, logger *slog.Logger, name string, delay, retry time.Duration, run func(context.Context) error) {
	if retry <= 0 {
		retry = 10 * time.Minute
	}
	if logger == nil {
		logger = slog.Default()
	}
	wg.Add(1)
	go func() {
		defer wg.Done()
		if !wait(ctx, delay) {
			return
		}
		for {
			err := run(ctx)
			if err == nil || ctx.Err() != nil {
				return
			}
			logger.Warn(name+" will be retried",
				"retry_in", retry.String(), logging.KeyError, err.Error())
			if !wait(ctx, retry) {
				return
			}
		}
	}()
}

// wait reports false when ctx ended first.
func wait(ctx context.Context, d time.Duration) bool {
	if d <= 0 {
		return ctx.Err() == nil
	}
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}

// ReadScheme reads a positive scheme number stored under key. ok is false
// when there is none or it cannot be read; callers treat that as the oldest
// scheme, so a damaged marker leads to a (harmless) second run, never to a
// skipped one.
func ReadScheme(ctx context.Context, settings Settings, key string) (int, bool) {
	if settings == nil {
		return 0, false
	}
	raw, ok, err := settings.Get(ctx, key)
	if err != nil || !ok {
		return 0, false
	}
	n, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil || n <= 0 {
		return 0, false
	}
	return n, true
}

// WriteScheme stores the scheme marker.
func WriteScheme(ctx context.Context, settings Settings, key string, scheme int) error {
	return settings.Set(ctx, key, strconv.Itoa(scheme))
}

// LoadStatus decodes the JSON status stored under key into dst. It reports
// false when there is none or it cannot be read.
func LoadStatus(ctx context.Context, settings Settings, key string, dst any) bool {
	if settings == nil {
		return false
	}
	raw, ok, err := settings.Get(ctx, key)
	if err != nil || !ok {
		return false
	}
	return json.Unmarshal([]byte(raw), dst) == nil
}

// SaveStatus stores status as JSON under key. It is written even when ctx has
// ended, so that a run stopped by a shutdown leaves its last state behind.
func SaveStatus(ctx context.Context, settings Settings, key string, status any) error {
	raw, err := json.Marshal(status)
	if err != nil {
		return err
	}
	return settings.Set(context.WithoutCancel(ctx), key, string(raw))
}
