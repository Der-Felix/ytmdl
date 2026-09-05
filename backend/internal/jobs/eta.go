package jobs

import (
	"math"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// ActiveWorkerPreview represents a worker currently processing a track.
type ActiveWorkerPreview struct {
	JobID           string     `json:"job_id"`
	ItemID          string     `json:"item_id"`
	Artist          string     `json:"artist"`
	Release         string     `json:"release"`
	Track           string     `json:"track"`
	TrackNumber     int        `json:"track_number"`
	Phase           ItemStatus `json:"phase"`
	ProgressPercent float64    `json:"progress_percent"`
	StartedAt       time.Time  `json:"started_at"`
}

// NextUpJob represents an upcoming job in dispatcher order.
type NextUpJob struct {
	JobID       string `json:"job_id"`
	Artist      string `json:"artist"`
	Release     string `json:"release"`
	OpenTracks  int    `json:"open_tracks"`
	TotalTracks int    `json:"total_tracks"`
	CoverURL    string `json:"cover_url,omitempty"`
}

// QueueCounts contains raw database metrics for queue summary computation.
type QueueCounts struct {
	RunnableItems     int
	ActiveItems       int
	RetryWaitItems    int
	PausedJobs        int
	CompletedLast1h   int
	CompletedLast6h   int
	TotalRelevant     int
	CompletedRelevant int
}

// QueueSummary holds the aggregated live queue preview and ETA statistics.
type QueueSummary struct {
	ActiveItems            int                   `json:"active_items"`
	RemainingItems         int                   `json:"remaining_items"`
	PausedJobs             int                   `json:"paused_jobs"`
	RetryWaitItems         int                   `json:"retry_wait_items"`
	CompletedLastHour      int                   `json:"completed_last_hour"`
	ThroughputItemsPerHour float64               `json:"throughput_items_per_hour"`
	ETASeconds             *int64                `json:"eta_seconds"`
	ETAConfidence          string                `json:"eta_confidence"`
	ETAText                string                `json:"eta_text"`
	TotalRelevant          int                   `json:"total_relevant"`
	CompletedRelevant      int                   `json:"completed_relevant"`
	StorageHealthy         bool                  `json:"storage_healthy"`
	Current                []ActiveWorkerPreview `json:"current"`
	Next                   []NextUpJob           `json:"next"`
}

// CalculateETA computes the estimated remaining time for runnable items based on recent throughput.
// If storageHealthy is false, it returns "Auf Speicher warten".
// If remainingItems is 0, it returns "Keine ausstehenden Downloads" (or "Queue pausiert" if pausedJobs > 0).
// If all remaining items are in retry_wait, it returns "Wartet auf erneuten Versuch".
// If recent completed items < 5, it returns "Berechnung läuft …".
func CalculateETA(remainingItems int, retryWaitItems int, activeItems int, pausedJobs int, completed1h int, completed6h int, storageHealthy bool) (*int64, string, string, float64) {
	if !storageHealthy {
		return nil, "waiting_for_storage", "Auf Speicher warten", 0
	}
	if remainingItems <= 0 {
		if pausedJobs > 0 {
			return nil, "paused", "Queue pausiert", 0
		}
		return nil, "idle", "Keine ausstehenden Downloads", 0
	}

	if activeItems == 0 && remainingItems > 0 && remainingItems == retryWaitItems {
		return nil, "retry_wait", "Wartet auf erneuten Versuch", 0
	}

	var rate float64
	var samples int

	if completed1h >= 5 {
		rate = float64(completed1h)
		samples = completed1h
	} else if completed6h >= 10 {
		rate = float64(completed6h) / 6.0
		samples = completed6h
	} else {
		return nil, "none", "Berechnung läuft …", 0
	}

	if rate <= 0 {
		return nil, "none", "Berechnung läuft …", 0
	}

	var confidence string
	switch {
	case samples < 20:
		confidence = "low"
	case samples < 100:
		confidence = "medium"
	default:
		confidence = "high"
	}

	// ETA in seconds = (remainingItems / rate) * 3600
	etaSec := int64(math.Round(float64(remainingItems) / rate * 3600.0))
	text := FormatETA(etaSec, confidence)
	return &etaSec, confidence, text, rate
}

// FormatETA converts seconds into a human-friendly German relative duration string.
func FormatETA(etaSec int64, confidence string) string {
	if etaSec < 60 {
		return "< 1 Minute"
	}

	if etaSec < 3600 {
		mins := int(math.Round(float64(etaSec) / 60.0))
		if mins <= 1 {
			return "< 1 Minute"
		}
		return "ca. " + strconv.Itoa(mins) + " Min."
	}

	if etaSec < 86400 {
		hours := etaSec / 3600
		remSec := etaSec % 3600
		mins := int(math.Round(float64(remSec) / 60.0))
		if mins >= 60 {
			hours++
			mins = 0
		}
		if mins < 5 {
			return "ca. " + strconv.FormatInt(hours, 10) + " Std."
		}
		return "ca. " + strconv.FormatInt(hours, 10) + " Std. " + strconv.Itoa(mins) + " Min."
	}

	days := int(math.Round(float64(etaSec) / 86400.0))
	if days <= 1 {
		return "ca. 1 Tag"
	}
	return "ca. " + strconv.Itoa(days) + " Tage"
}

// WorkerTracker maintains in-memory live progress of currently running items.
type WorkerTracker struct {
	mu      sync.RWMutex
	workers map[string]ActiveWorkerPreview
}

// NewWorkerTracker initializes a WorkerTracker.
func NewWorkerTracker() *WorkerTracker {
	return &WorkerTracker{
		workers: make(map[string]ActiveWorkerPreview),
	}
}

// RecordProgress registers or updates progress for an active worker.
func (wt *WorkerTracker) RecordProgress(job Job, item Item, phase ItemStatus, percent float64) {
	wt.mu.Lock()
	defer wt.mu.Unlock()

	artist := ""
	release := job.Label
	track := item.Label
	trackNumber := item.Position + 1

	if len(item.Track.Artists) > 0 {
		artist = strings.Join(item.Track.Artists, ", ")
	} else if item.Track.AlbumArtist != "" {
		artist = item.Track.AlbumArtist
	}
	if item.Track.Album != "" {
		release = item.Track.Album
	}
	if item.Track.Title != "" {
		track = item.Track.Title
	}
	if item.Track.TrackNumber > 0 {
		trackNumber = item.Track.TrackNumber
	}

	if artist == "" && strings.Contains(item.Label, " - ") {
		parts := strings.SplitN(item.Label, " - ", 2)
		artist = strings.TrimSpace(parts[0])
		track = strings.TrimSpace(parts[1])
	}

	existing, ok := wt.workers[item.ID]
	startedAt := time.Now().UTC()
	if ok && !existing.StartedAt.IsZero() {
		startedAt = existing.StartedAt
	}

	if percent <= 0 && ok && phase == existing.Phase {
		percent = existing.ProgressPercent
	}

	wt.workers[item.ID] = ActiveWorkerPreview{
		JobID:           job.ID,
		ItemID:          item.ID,
		Artist:          artist,
		Release:         release,
		Track:           track,
		TrackNumber:     trackNumber,
		Phase:           phase,
		ProgressPercent: percent,
		StartedAt:       startedAt,
	}
}

// Clear removes a finished worker from tracking.
func (wt *WorkerTracker) Clear(itemID string) {
	wt.mu.Lock()
	defer wt.mu.Unlock()
	delete(wt.workers, itemID)
}

// List returns currently active workers sorted by started time.
func (wt *WorkerTracker) List() []ActiveWorkerPreview {
	wt.mu.RLock()
	defer wt.mu.RUnlock()

	list := make([]ActiveWorkerPreview, 0, len(wt.workers))
	for _, w := range wt.workers {
		list = append(list, w)
	}

	sort.Slice(list, func(i, j int) bool {
		if !list[i].StartedAt.Equal(list[j].StartedAt) {
			return list[i].StartedAt.Before(list[j].StartedAt)
		}
		return list[i].ItemID < list[j].ItemID
	})

	return list
}
