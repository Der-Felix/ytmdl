package jobs

import (
	"context"
	"time"

	"ytdm/backend/internal/downloader"
	"ytdm/backend/internal/metadata"
	"ytdm/backend/internal/music"
)

// The interfaces below describe what the job pipeline needs from the rest of
// the backend. They are declared here so that the jobs package depends on
// behaviour rather than on the concrete repository implementations, which also
// keeps the pipeline testable with fakes.

// Store persists jobs and their items.
type Store interface {
	Create(ctx context.Context, job *Job) error
	Get(ctx context.Context, id string) (*Job, error)
	List(ctx context.Context, filter ListFilter) ([]Job, int, error)
	ListUnfinished(ctx context.Context) ([]Job, error)
	SetStatus(ctx context.Context, id string, status Status, errorCode, errorMessage string) error
	SetLabel(ctx context.Context, id, label string) error
	SetTotal(ctx context.Context, id string, total int) error
	RefreshCounters(ctx context.Context, id string) (*Job, error)
	SetPriority(ctx context.Context, id string, priority Priority) error
	SetPaused(ctx context.Context, id string, paused bool) error
	DeleteHistory(ctx context.Context, olderThan time.Time, allowedStatuses []Status) (int, int, error)
	ResetItemForRetry(ctx context.Context, jobID, itemID string) error
	ResetFailedItemsInJob(ctx context.Context, jobID string) (int, int, error)

	AddItems(ctx context.Context, jobID string, items []Item) error
	ListItems(ctx context.Context, jobID string) ([]Item, error)
	ListPendingItems(ctx context.Context, jobID string) ([]Item, error)
	GetItem(ctx context.Context, id string) (*Item, error)
	UpdateItem(ctx context.Context, id string, update ItemUpdate) error
	CancelPendingItems(ctx context.Context, jobID string) (int, error)
	HasItems(ctx context.Context, jobID string) (bool, error)

	// ResetInFlightItems and ResetInterruptedJobs implement the recovery after
	// a crash or a SIGTERM: everything a previous process left mid-flight is
	// returned to a state the queue can start from again.
	ResetInFlightItems(ctx context.Context) (int, error)
	ResetInterruptedJobs(ctx context.Context) (int, error)
}

// Catalog persists the resolved catalogue.
type Catalog interface {
	// FindTrack answers whether a recording is already known, which is what
	// the skip-existing check is built on.
	FindTrack(ctx context.Context, track music.Track, toleranceMS int) (*music.Track, error)
	// PersistDownload writes artist, release, recording, sources and file as
	// one atomic unit once a download has finished.
	PersistDownload(ctx context.Context, entry music.LibraryEntry, toleranceMS int) (music.StoredEntry, error)
}

// FileStore answers which files a recording already has in the library. The
// files themselves are written as part of Catalog.PersistDownload, so this
// interface only reads.
type FileStore interface {
	ListByTrack(ctx context.Context, trackID string) ([]music.File, error)
	// FindByPath answers who owns a library path. It is what lets the worker
	// tell "this is my own file from a previous run" apart from "this is
	// somebody else's file", which decides whether a target may be replaced.
	FindByPath(ctx context.Context, path string) (*music.File, error)
}

// Tagger writes tags and cover art onto a finished file.
type Tagger interface {
	Apply(ctx context.Context, path string, tags metadata.Tags, artwork *metadata.Artwork) error
}

// ArtworkFetcher downloads cover images.
type ArtworkFetcher interface {
	Fetch(ctx context.Context, url string) (*metadata.Artwork, error)
}

// Downloader fetches a resolved media source.
type Downloader = downloader.Downloader
