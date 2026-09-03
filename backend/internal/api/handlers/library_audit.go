package handlers

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	"ytdm/backend/internal/api/middleware"
	"ytdm/backend/internal/api/response"
	"ytdm/backend/internal/apperr"
	"ytdm/backend/internal/database/repository"
	"ytdm/backend/internal/library"
	"ytdm/backend/internal/music"
)

type StartAuditRequest struct {
	Mode music.AuditMode `json:"mode"`
}

type PreviewRepairsRequest struct {
	FindingIDs []string `json:"finding_ids"`
}

// StartLibraryAudit answers POST /library/audits.
func (h *Handlers) StartLibraryAudit(w http.ResponseWriter, r *http.Request) {
	if h.deps.LibraryService == nil {
		response.Fail(w, r, apperr.CodeInternal, "Library service is unavailable.")
		return
	}

	var req StartAuditRequest
	if err := decodeJSON(r, &req); err != nil {
		response.Error(w, r, err)
		return
	}

	mode := req.Mode
	if mode == "" {
		mode = music.AuditModeQuick
	}

	var userID *string
	if user := middleware.UserFromContext(r.Context()); user != nil {
		userID = &user.ID
	}

	run, err := h.deps.LibraryService.StartAudit(r.Context(), mode, userID)
	if err != nil {
		response.Error(w, r, err)
		return
	}

	response.Data(w, http.StatusAccepted, run)
}

// ListLibraryAudits answers GET /library/audits.
func (h *Handlers) ListLibraryAudits(w http.ResponseWriter, r *http.Request) {
	if h.deps.LibraryService == nil || h.deps.LibraryService.AuditRepo() == nil {
		response.Fail(w, r, apperr.CodeInternal, "Audit repository is unavailable.")
		return
	}

	limit, offset, err := pagination(r, 20, 100)
	if err != nil {
		response.Error(w, r, err)
		return
	}

	runs, total, err := h.deps.LibraryService.AuditRepo().ListRuns(r.Context(), limit, offset)
	if err != nil {
		response.Error(w, r, err)
		return
	}

	response.List(w, runs, response.Meta{Count: len(runs), Total: total, Limit: limit, Offset: offset})
}

// GetCurrentLibraryAudit answers GET /library/audits/current.
func (h *Handlers) GetCurrentLibraryAudit(w http.ResponseWriter, r *http.Request) {
	if h.deps.LibraryService == nil || h.deps.LibraryService.AuditRepo() == nil {
		response.Fail(w, r, apperr.CodeInternal, "Audit repository is unavailable.")
		return
	}

	active, err := h.deps.LibraryService.AuditRepo().GetActiveRun(r.Context())
	if err != nil {
		response.Error(w, r, err)
		return
	}
	if active != nil {
		response.Data(w, http.StatusOK, active)
		return
	}

	latest, err := h.deps.LibraryService.AuditRepo().GetLatestRun(r.Context())
	if err != nil {
		response.Error(w, r, err)
		return
	}

	response.Data(w, http.StatusOK, latest)
}

// GetLibraryAudit answers GET /library/audits/{id}.
func (h *Handlers) GetLibraryAudit(w http.ResponseWriter, r *http.Request) {
	if h.deps.LibraryService == nil || h.deps.LibraryService.AuditRepo() == nil {
		response.Fail(w, r, apperr.CodeInternal, "Audit repository is unavailable.")
		return
	}

	id := chi.URLParam(r, "id")
	if id == "" {
		response.Error(w, r, apperr.New(apperr.CodeInvalidRequest, "Audit run ID is required."))
		return
	}

	run, err := h.deps.LibraryService.AuditRepo().GetRun(r.Context(), id)
	if err != nil {
		response.Error(w, r, err)
		return
	}
	if run == nil {
		response.Fail(w, r, apperr.CodeFileNotFound, "Audit run not found.")
		return
	}

	response.Data(w, http.StatusOK, run)
}

// ListLibraryAuditFindings answers GET /library/audits/{id}/findings.
func (h *Handlers) ListLibraryAuditFindings(w http.ResponseWriter, r *http.Request) {
	if h.deps.LibraryService == nil || h.deps.LibraryService.AuditRepo() == nil {
		response.Fail(w, r, apperr.CodeInternal, "Audit repository is unavailable.")
		return
	}

	runID := chi.URLParam(r, "id")
	if runID == "" {
		response.Error(w, r, apperr.New(apperr.CodeInvalidRequest, "Audit run ID is required."))
		return
	}

	limit, offset, err := pagination(r, 50, 200)
	if err != nil {
		response.Error(w, r, err)
		return
	}

	opts := repository.ListFindingsOptions{
		Severity:    queryString(r, "severity"),
		FindingCode: queryString(r, "finding_code"),
		ArtistID:    queryString(r, "artist_id"),
		ReleaseID:   queryString(r, "release_id"),
		TrackID:     queryString(r, "track_id"),
		Limit:       limit,
		Offset:      offset,
	}

	findings, total, err := h.deps.LibraryService.AuditRepo().ListFindings(r.Context(), runID, opts)
	if err != nil {
		response.Error(w, r, err)
		return
	}

	response.List(w, findings, response.Meta{Count: len(findings), Total: total, Limit: limit, Offset: offset})
}

// CancelLibraryAudit answers POST /library/audits/{id}/cancel.
func (h *Handlers) CancelLibraryAudit(w http.ResponseWriter, r *http.Request) {
	if h.deps.LibraryService == nil {
		response.Fail(w, r, apperr.CodeInternal, "Library service is unavailable.")
		return
	}

	id := chi.URLParam(r, "id")
	if id == "" {
		response.Error(w, r, apperr.New(apperr.CodeInvalidRequest, "Audit run ID is required."))
		return
	}

	if err := h.deps.LibraryService.CancelAudit(r.Context(), id); err != nil {
		response.Error(w, r, err)
		return
	}

	response.Data(w, http.StatusOK, map[string]string{"status": "cancelled"})
}

// PreviewLibraryRepairs answers POST /library/repairs/preview.
func (h *Handlers) PreviewLibraryRepairs(w http.ResponseWriter, r *http.Request) {
	if h.deps.LibraryService == nil {
		response.Fail(w, r, apperr.CodeInternal, "Library service is unavailable.")
		return
	}

	var req PreviewRepairsRequest
	if err := decodeJSON(r, &req); err != nil {
		response.Error(w, r, err)
		return
	}

	if len(req.FindingIDs) == 0 {
		response.Error(w, r, apperr.New(apperr.CodeInvalidRequest, "At least one finding ID is required for preview."))
		return
	}

	previews, err := h.deps.LibraryService.PreviewRepairs(r.Context(), req.FindingIDs)
	if err != nil {
		response.Error(w, r, err)
		return
	}

	response.Data(w, http.StatusOK, previews)
}

// ApplyLibraryRepairs answers POST /library/repairs/apply.
func (h *Handlers) ApplyLibraryRepairs(w http.ResponseWriter, r *http.Request) {
	if h.deps.LibraryService == nil {
		response.Fail(w, r, apperr.CodeInternal, "Library service is unavailable.")
		return
	}

	var req library.RepairApplyRequest
	if err := decodeJSON(r, &req); err != nil {
		response.Error(w, r, err)
		return
	}

	result, err := h.deps.LibraryService.ApplyRepairs(r.Context(), req)
	if err != nil {
		response.Error(w, r, err)
		return
	}

	response.Data(w, http.StatusOK, result)
}
