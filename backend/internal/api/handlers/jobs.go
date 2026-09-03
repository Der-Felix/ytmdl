package handlers

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"

	"ytdm/backend/internal/api/response"
	"ytdm/backend/internal/apperr"
	"ytdm/backend/internal/jobs"
)

// ListJobs answers GET /jobs.
func (h *Handlers) ListJobs(w http.ResponseWriter, r *http.Request) {
	limit, offset, err := pagination(r, 50, 200)
	if err != nil {
		response.Error(w, r, err)
		return
	}

	filter := jobs.ListFilter{Limit: limit, Offset: offset}
	if raw := queryString(r, "status"); raw != "" {
		status := jobs.Status(raw)
		if !status.Valid() {
			response.Fail(w, r, apperr.CodeInvalidRequest, "The status filter is not a known job status.")
			return
		}
		filter.Status = status
	}
	if raw := queryString(r, "type"); raw != "" {
		jobType := jobs.Type(raw)
		if !jobType.Valid() {
			response.Fail(w, r, apperr.CodeInvalidRequest, "The type filter is not a known job type.")
			return
		}
		filter.Type = jobType
	}
	if raw := queryString(r, "priority"); raw != "" {
		priority := jobs.Priority(raw)
		if !priority.Valid() {
			response.Fail(w, r, apperr.CodeInvalidRequest, "The priority filter is not a known priority.")
			return
		}
		filter.Priority = priority
	}

	list, total, err := h.deps.Jobs.List(r.Context(), filter)
	if err != nil {
		response.Error(w, r, err)
		return
	}
	response.List(w, list, response.Meta{Count: len(list), Total: total, Limit: limit, Offset: offset})
}

// GetJob answers GET /jobs/{id}, including the item list and the summary.
func (h *Handlers) GetJob(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	job, err := h.deps.Jobs.Get(r.Context(), id)
	if err != nil {
		response.Error(w, r, err)
		return
	}
	items, err := h.deps.Jobs.Items(r.Context(), id)
	if err != nil {
		response.Error(w, r, err)
		return
	}

	response.Data(w, http.StatusOK, map[string]any{
		"job":     job,
		"items":   items,
		"summary": job.Summary(),
	})
}

// UpdateJobRequest carries fields for PATCH /jobs/{id}.
type UpdateJobRequest struct {
	Priority *jobs.Priority `json:"priority"`
	Paused   *bool          `json:"paused"`
}

// UpdateJob answers PATCH /jobs/{id}.
func (h *Handlers) UpdateJob(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	var req UpdateJobRequest
	if err := decodeJSON(r, &req); err != nil {
		response.Error(w, r, err)
		return
	}

	if req.Priority == nil && req.Paused == nil {
		response.Fail(w, r, apperr.CodeInvalidRequest, "The request changes no field.")
		return
	}

	var job *jobs.Job
	var err error

	if req.Priority != nil {
		job, err = h.deps.Jobs.SetPriority(r.Context(), id, *req.Priority)
		if err != nil {
			response.Error(w, r, err)
			return
		}
	}

	if req.Paused != nil {
		if *req.Paused {
			job, err = h.deps.Jobs.Pause(r.Context(), id)
		} else {
			job, err = h.deps.Jobs.Resume(r.Context(), id)
		}
		if err != nil {
			response.Error(w, r, err)
			return
		}
	}

	response.Data(w, http.StatusOK, job)
}

// PauseJob answers POST /jobs/{id}/pause.
func (h *Handlers) PauseJob(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	job, err := h.deps.Jobs.Pause(r.Context(), id)
	if err != nil {
		response.Error(w, r, err)
		return
	}
	response.Data(w, http.StatusOK, job)
}

// ResumeJob answers POST /jobs/{id}/resume.
func (h *Handlers) ResumeJob(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	job, err := h.deps.Jobs.Resume(r.Context(), id)
	if err != nil {
		response.Error(w, r, err)
		return
	}
	response.Data(w, http.StatusOK, job)
}

// RetryFailedJob answers POST /jobs/{id}/retry-failed.
func (h *Handlers) RetryFailedJob(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	job, retried, skipped, err := h.deps.Jobs.RetryFailed(r.Context(), id)
	if err != nil {
		response.Error(w, r, err)
		return
	}
	response.Data(w, http.StatusOK, map[string]any{
		"job":     job,
		"retried": retried,
		"skipped": skipped,
	})
}

// RetryJobItem answers POST /jobs/{job_id}/items/{item_id}/retry.
func (h *Handlers) RetryJobItem(w http.ResponseWriter, r *http.Request) {
	jobID := chi.URLParam(r, "job_id")
	itemID := chi.URLParam(r, "item_id")

	item, err := h.deps.Jobs.RetryItem(r.Context(), jobID, itemID)
	if err != nil {
		response.Error(w, r, err)
		return
	}
	response.Data(w, http.StatusOK, item)
}

// CancelJob answers DELETE /jobs/{id}. The job is cancelled, not deleted: its
// history stays readable.
func (h *Handlers) CancelJob(w http.ResponseWriter, r *http.Request) {
	job, err := h.deps.Jobs.Cancel(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		response.Error(w, r, err)
		return
	}
	response.Data(w, http.StatusOK, job)
}

// DeleteJobHistoryRequest carries options for DELETE /jobs/history.
type DeleteJobHistoryRequest struct {
	OlderThanDays int           `json:"older_than_days"`
	Statuses      []jobs.Status `json:"statuses"`
}

// DeleteJobHistory answers DELETE /jobs/history (Admin only).
func (h *Handlers) DeleteJobHistory(w http.ResponseWriter, r *http.Request) {
	days := 30
	var req DeleteJobHistoryRequest
	if r.Body != nil && r.ContentLength > 0 {
		if err := decodeJSON(r, &req); err != nil {
			response.Error(w, r, err)
			return
		}
	}
	if req.OlderThanDays > 0 {
		if req.OlderThanDays < 7 {
			response.Fail(w, r, apperr.CodeInvalidRequest, "older_than_days must be at least 7.")
			return
		}
		days = req.OlderThanDays
	}

	if raw := queryString(r, "older_than_days"); raw != "" {
		if d, err := strconv.Atoi(raw); err == nil && d >= 7 {
			days = d
		} else if err != nil || d < 7 {
			response.Fail(w, r, apperr.CodeInvalidRequest, "older_than_days must be at least 7.")
			return
		}
	}

	statuses := []jobs.Status{jobs.StatusCompleted, jobs.StatusCancelled}
	if len(req.Statuses) > 0 {
		statuses = req.Statuses
	} else if rawStatuses := queryString(r, "statuses"); rawStatuses != "" {
		parts := strings.Split(rawStatuses, ",")
		statuses = make([]jobs.Status, 0, len(parts))
		for _, p := range parts {
			s := jobs.Status(strings.TrimSpace(p))
			if s == jobs.StatusCompleted || s == jobs.StatusCancelled || s == jobs.StatusFailed {
				statuses = append(statuses, s)
			}
		}
	}

	deletedJobs, deletedItems, err := h.deps.Jobs.DeleteHistory(r.Context(), days, statuses)
	if err != nil {
		response.Error(w, r, err)
		return
	}

	response.Data(w, http.StatusOK, map[string]any{
		"deleted_jobs":  deletedJobs,
		"deleted_items": deletedItems,
	})
}
