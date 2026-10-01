package jobs

import (
	"context"
	"regexp"
	"time"

	"ytdm/backend/internal/logging"
)

// itemStatusReader answers the stored status of items. The job repository
// implements it; a store that does not leaves staging untouched.
type itemStatusReader interface {
	ItemStatuses(ctx context.Context, ids []string) (map[string]ItemStatus, error)
}

// itemIDPattern is the shape of an item id (music.NewID): 32 lower case hex
// characters. A staging entry of any other shape was not created for an item
// and is never removed.
var itemIDPattern = regexp.MustCompile(`^[0-9a-f]{32}$`)

// stagingPruneBatch bounds how many ids one status query carries.
const stagingPruneBatch = 500

// stagingPruneTimeout bounds how long the status queries may take in total.
const stagingPruneTimeout = 2 * time.Minute

// pruneStaging removes the staging directories of items that have reached a
// final state. It runs once at start, after recovery and before any worker,
// so no directory it looks at can be in use.
//
// A directory is removed only when all of this holds: its name is an item id,
// it is a real directory directly below the staging root, and the database
// reports its item as completed, failed, skipped or cancelled. Directories of
// active, waiting or retryable items are kept - a retry may continue a partial
// download. So are directories whose item the database does not know: nothing
// proves they are expendable. If the status cannot be read for every
// candidate, nothing is removed at all.
func (m *Manager) pruneStaging(ctx context.Context) {
	if m.staging == nil {
		return
	}
	reader, ok := m.store.(itemStatusReader)
	if !ok {
		return
	}
	logger := m.logger.With(logging.KeyOperation, "staging_prune")

	names, err := m.staging.ItemDirNames()
	if err != nil {
		logger.Warn("staging not pruned: the staging root could not be listed", logging.KeyError, err.Error())
		return
	}
	ids := make([]string, 0, len(names))
	for _, name := range names {
		if itemIDPattern.MatchString(name) {
			ids = append(ids, name)
		}
	}
	unrecognised := len(names) - len(ids)

	queryCtx, cancel := context.WithTimeout(ctx, stagingPruneTimeout)
	defer cancel()
	statuses := make(map[string]ItemStatus, len(ids))
	for start := 0; start < len(ids); start += stagingPruneBatch {
		end := min(start+stagingPruneBatch, len(ids))
		batch, err := reader.ItemStatuses(queryCtx, ids[start:end])
		if err != nil {
			logger.Warn("staging not pruned: item states could not be read", logging.KeyError, err.Error())
			return
		}
		for id, status := range batch {
			statuses[id] = status
		}
	}

	var removed, active, unknown, failed int
	for _, id := range ids {
		if ctx.Err() != nil {
			break
		}
		status, known := statuses[id]
		switch {
		case !known:
			unknown++
		case !status.Terminal():
			active++
		default:
			if err := m.staging.RemoveItemDir(id); err != nil {
				failed++
				continue
			}
			removed++
		}
	}
	if len(names) == 0 {
		return
	}
	logger.Info("staging pruned",
		"removed", removed,
		"kept_active", active,
		"kept_unknown_item", unknown,
		"kept_unrecognised", unrecognised,
		"not_removable", failed,
	)
}
