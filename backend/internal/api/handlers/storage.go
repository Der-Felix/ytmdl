package handlers

import (
	"net/http"
	"strings"
	"time"

	"ytdm/backend/internal/api/response"
	"ytdm/backend/internal/storage"
)

// StorageLibraryStatus carries library filesystem and guard metrics.
type StorageLibraryStatus struct {
	Path            string    `json:"path"`
	GuardConfigured bool      `json:"guard_configured"`
	GuardStatus     string    `json:"guard_status"` // "disabled", "verified", "missing", "mismatch", "invalid"
	Status          string    `json:"status"`
	StatusDetail    string    `json:"status_detail,omitempty"`
	FSType          string    `json:"fs_type"`
	TotalBytes      int64     `json:"total_bytes"`
	FreeBytes       int64     `json:"free_bytes"`
	UsedBytes       int64     `json:"used_bytes"`
	FreePercent     float64   `json:"free_percent"`
	MinFreeBytes    int64     `json:"min_free_bytes"`
	IsNetworkFS     bool      `json:"is_network_fs"`
	LastCheckedAt   time.Time `json:"last_checked_at"`
}

// StorageStagingStatus carries staging directory metrics.
type StorageStagingStatus struct {
	Path               string `json:"path"`
	TotalBytes         int64  `json:"total_bytes"`
	FreeBytes          int64  `json:"free_bytes"`
	UsedBytes          int64  `json:"used_bytes"`
	MinFreeBytes       int64  `json:"min_free_bytes"`
	MaxBytes           int64  `json:"max_bytes"`
	CurrentStagedBytes int64  `json:"current_staged_bytes"`
	ActiveItems        int    `json:"active_items"`
	ActivePartials     int    `json:"active_partials"`
}

// StorageQueueStatus carries queue pause state and waiting item counts.
type StorageQueueStatus struct {
	Paused              bool `json:"paused"`
	WaitingStorageItems int  `json:"waiting_storage_items"`
	WaitingSpaceItems   int  `json:"waiting_space_items"`
	RetryWaitItems      int  `json:"retry_wait_items"`
}

// StorageStatusResponse is the aggregated response for GET /api/v1/storage/status.
type StorageStatusResponse struct {
	Library StorageLibraryStatus `json:"library"`
	Staging StorageStagingStatus `json:"staging"`
	Queue   StorageQueueStatus   `json:"queue"`
}

// StorageStatus renders storage metrics and queue health.
func (h *Handlers) StorageStatus(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	var libStatus StorageLibraryStatus
	if h.deps.Library != nil {
		lib := h.deps.Library
		libStatus.Path = lib.Root()

		if guard := lib.Guard(); guard != nil {
			health := guard.CheckHealth(ctx, false)
			libStatus.GuardConfigured = guard.GuardID() != ""
			libStatus.GuardStatus = string(health.GuardStatus)
			libStatus.Status = string(health.Status)
			libStatus.StatusDetail = health.LastError
			libStatus.FSType = health.Filesystem
			libStatus.TotalBytes = int64(health.TotalBytes)
			libStatus.FreeBytes = int64(health.FreeBytes)
			if health.TotalBytes > 0 {
				libStatus.UsedBytes = int64(health.TotalBytes - health.FreeBytes)
				libStatus.FreePercent = float64(health.FreeBytes) / float64(health.TotalBytes) * 100.0
			}
			libStatus.MinFreeBytes = guard.MinFreeBytes()
			libStatus.IsNetworkFS = strings.Contains(strings.ToLower(health.Filesystem), "nfs") ||
				strings.Contains(strings.ToLower(health.Filesystem), "cifs") ||
				strings.Contains(strings.ToLower(health.Filesystem), "smb")
			libStatus.LastCheckedAt = health.LastChecked
		} else {
			libStatus.Status = string(storage.HealthHealthy)
		}
	}

	var stagingStatus StorageStagingStatus
	if h.deps.Jobs != nil && h.deps.Jobs.Staging() != nil {
		stg := h.deps.Jobs.Staging()
		stagingStatus.Path = stg.Root()
		_, total, free, _, _ := storage.QueryFS(stg.Root())
		stagingStatus.TotalBytes = int64(total)
		stagingStatus.FreeBytes = int64(free)
		if total > 0 {
			stagingStatus.UsedBytes = int64(total - free)
		}
		stagingStatus.MinFreeBytes = stg.MinFreeBytes()
		stagingStatus.MaxBytes = stg.MaxBytes()
		stagingStatus.CurrentStagedBytes, _ = stg.UsedBytes()
		stagingStatus.ActivePartials, _ = stg.CountPartials()
	}

	var queueStatus StorageQueueStatus
	if h.deps.Jobs != nil {
		queueStatus.Paused = h.deps.Jobs.QueuePaused()
	}

	response.Data(w, http.StatusOK, StorageStatusResponse{
		Library: libStatus,
		Staging: stagingStatus,
		Queue:   queueStatus,
	})
}

// StorageProbe triggers an immediate storage health probe and returns updated status.
func (h *Handlers) StorageProbe(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	if h.deps.Library != nil && h.deps.Library.Guard() != nil {
		_ = h.deps.Library.Guard().CheckHealth(ctx, true)
	}
	h.StorageStatus(w, r)
}

// StorageQueuePause pauses the background download dispatcher and persists the state.
func (h *Handlers) StorageQueuePause(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	if h.deps.Settings != nil {
		_ = h.deps.Settings.SetQueuePaused(ctx, true)
	} else if h.deps.Jobs != nil {
		h.deps.Jobs.SetQueuePaused(true)
	}
	response.Data(w, http.StatusOK, map[string]any{"paused": true})
}

// StorageQueueResume resumes the background download dispatcher and persists the state.
func (h *Handlers) StorageQueueResume(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	if h.deps.Settings != nil {
		_ = h.deps.Settings.SetQueuePaused(ctx, false)
	} else if h.deps.Jobs != nil {
		h.deps.Jobs.SetQueuePaused(false)
	}
	response.Data(w, http.StatusOK, map[string]any{"paused": false})
}
