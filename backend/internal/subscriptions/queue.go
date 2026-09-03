package subscriptions

import (
	"context"

	"ytdm/backend/internal/jobs"
)

// JobQueue hands new material to the download pipeline that already exists.
//
// It is a thin adapter on purpose: the subscription sync must not gain a
// download path of its own, so everything goes through the same job manager,
// the same matcher and the same workers a manual download uses.
type JobQueue struct {
	manager *jobs.Manager
}

// NewJobQueue adapts the job manager to the Downloader interface.
func NewJobQueue(manager *jobs.Manager) *JobQueue { return &JobQueue{manager: manager} }

// EnqueueRelease queues one release. Skip-existing is always on: the sync
// queues a release because some of its recordings are missing, and the worker
// is what decides, track by track, which ones those are.
//
// A release an unfinished job already covers is not queued again. A sync that
// ended partial comes back early, and without this check the retry would put a
// second job on the same release while the first is still downloading it.
func (q *JobQueue) EnqueueRelease(ctx context.Context, metadataProvider, releaseID, label string) (bool, error) {
	return q.EnqueueReleaseWithPriority(ctx, metadataProvider, releaseID, label, jobs.PriorityNormal)
}

// EnqueueReleaseWithPriority queues one release with a specific download priority.
func (q *JobQueue) EnqueueReleaseWithPriority(ctx context.Context, metadataProvider, releaseID, label string, priority jobs.Priority) (bool, error) {
	running, err := q.manager.HasUnfinishedJob(ctx, jobs.TypeRelease, releaseID)
	if err != nil {
		return false, err
	}
	if running {
		return false, nil
	}

	skipExisting := true
	p := priority
	if !p.Valid() {
		p = jobs.PriorityNormal
	}
	_, err = q.manager.Enqueue(ctx, jobs.Request{
		Type:             jobs.TypeRelease,
		MetadataProvider: metadataProvider,
		TargetID:         releaseID,
		Label:            label,
		Options: jobs.RequestOptions{
			SkipExisting: &skipExisting,
			Priority:     &p,
		},
	})
	if err != nil {
		return false, err
	}
	return true, nil
}
