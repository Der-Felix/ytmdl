package subscriptions

import (
	"context"
	"time"

	"ytdm/backend/internal/jobs"
	"ytdm/backend/internal/music"
)

// The interfaces below describe what the subscription service needs from the
// rest of the backend. They are declared here so that this package depends on
// behaviour rather than on the concrete repositories, which also keeps the
// sync testable without a database.

// Store persists subscriptions.
type Store interface {
	// Create stores a new subscription. Subscribing to an artist that is
	// already watched is not an error: the existing subscription is returned
	// unchanged, so a double click cannot produce a second row.
	Create(ctx context.Context, req NewSubscription) (*Subscription, error)
	Get(ctx context.Context, id string) (*Subscription, error)
	// FindBySource resolves a subscription by its provider identity and
	// returns nil when the artist is not watched.
	FindBySource(ctx context.Context, provider, artistSourceID string) (*Subscription, error)
	List(ctx context.Context, filter ListFilter) ([]Subscription, error)
	ListAll(ctx context.Context) ([]Subscription, error)
	Update(ctx context.Context, id string, update Update) (*Subscription, error)
	Delete(ctx context.Context, id string) error
	ApplyImport(ctx context.Context, newSubs []NewSubscription, updates []ImportUpdate) (*ImportResult, error)

	// ListDueForSync returns the enabled subscriptions whose next run is due.
	ListDueForSync(ctx context.Context, now time.Time, limit int) ([]Subscription, error)
	// RecordSync stores the outcome of a run.
	RecordSync(ctx context.Context, id string, outcome SyncOutcome) error
}

// SyncOutcome is what a finished run writes back to the subscription.
type SyncOutcome struct {
	At     time.Time
	NextAt time.Time
	Status SyncStatus
	// Error is the reason a failed run gives. It is cleared on every run that
	// did finish, so a stale message never outlives the problem.
	Error string
}

// Catalog answers what the library already holds. Both methods already exist
// for the download pipeline; the sync only reads through them.
type Catalog interface {
	// FindReleaseBySource resolves a release by its provider identity and
	// returns nil when the library does not hold it.
	FindReleaseBySource(ctx context.Context, provider, sourceID string) (*music.Release, error)
	// FindTrack resolves a recording by ISRC first and by identity key plus
	// runtime second — the same rules the deduplication uses.
	FindTrack(ctx context.Context, track music.Track, toleranceMS int) (*music.Track, error)
}

// FileStore answers whether a recording was actually downloaded. A track that
// is known to the catalogue but has no file still has to be fetched.
type FileStore interface {
	ListByTrack(ctx context.Context, trackID string) ([]music.File, error)
}

// Downloader hands new material to the existing download queue.
type Downloader interface {
	// EnqueueRelease queues one release and reports whether a job was created.
	// A false without an error means an unfinished job already covers that
	// release: a sync repeated after a partial run must not produce a second
	// job for work that is still on its way.
	EnqueueRelease(ctx context.Context, metadataProvider, releaseID, label string) (queued bool, err error)
	EnqueueReleaseWithPriority(ctx context.Context, metadataProvider, releaseID, label string, priority jobs.Priority) (queued bool, err error)
}
