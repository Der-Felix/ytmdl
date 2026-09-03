package library

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"ytdm/backend/internal/apperr"
	"ytdm/backend/internal/music"
)

// TrashDirName is the reserved internal quarantine directory on library storage.
const TrashDirName = ".ytmdl-trash"

// RepairPreview describes the dry-run effect of a repair action on files and database.
type RepairPreview struct {
	FindingID       string             `json:"finding_id"`
	FindingCode     music.FindingCode  `json:"finding_code"`
	Action          music.RepairAction `json:"action"`
	SourcePath      string             `json:"source_path"`
	DestinationPath string             `json:"destination_path,omitempty"`
	Allowed         bool               `json:"allowed"`
	Message         string             `json:"message,omitempty"`
	DBChanges       []string           `json:"db_changes"`
	FileChanges     []string           `json:"file_changes"`
	Warnings        []string           `json:"warnings,omitempty"`
}

// RepairItemAction pairs a specific finding with the intended repair action.
type RepairItemAction struct {
	FindingID string             `json:"finding_id"`
	Action    music.RepairAction `json:"action"`
}

// RepairApplyRequest contains the explicit actions to be applied with confirmation.
type RepairApplyRequest struct {
	Confirm bool               `json:"confirm"`
	Actions []RepairItemAction `json:"actions"`
}

// RepairApplyResult summarizes the outcome of executing repair actions.
type RepairApplyResult struct {
	Requested   int      `json:"requested"`
	Applied     int      `json:"applied"`
	Quarantined int      `json:"quarantined"`
	Failed      int      `json:"failed"`
	Warnings    []string `json:"warnings,omitempty"`
}

// PreviewRepairs performs a read-only evaluation of proposed repair actions without mutating storage or DB.
func (s *Service) PreviewRepairs(ctx context.Context, findingIDs []string) ([]RepairPreview, error) {
	if s.auditRepo == nil {
		return nil, apperr.New(apperr.CodeInternal, "Audit repository is not configured.")
	}

	previews := make([]RepairPreview, 0, len(findingIDs))

	for _, id := range findingIDs {
		finding, err := s.auditRepo.GetFinding(ctx, id)
		if err != nil {
			return nil, err
		}
		if finding == nil {
			previews = append(previews, RepairPreview{
				FindingID: id,
				Allowed:   false,
				Message:   "Finding not found or already resolved.",
			})
			continue
		}

		action := music.ActionQuarantineFile
		if finding.SuggestedAction != nil {
			action = *finding.SuggestedAction
		} else {
			switch finding.FindingCode {
			case music.FindingPathMismatch:
				action = music.ActionMoveCanonical
			case music.FindingTagMismatch:
				action = music.ActionRestoreTags
			case music.FindingFileUntracked, music.FindingLegacyDuplicate, music.FindingFileDuplicate:
				action = music.ActionQuarantineFile
			}
		}

		prev := s.previewFinding(ctx, finding, action)
		previews = append(previews, prev)
	}

	return previews, nil
}

func computeFileSHA256(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func (s *Service) previewFinding(ctx context.Context, f *music.AuditFinding, action music.RepairAction) RepairPreview {
	prev := RepairPreview{
		FindingID:   f.ID,
		FindingCode: f.FindingCode,
		Action:      action,
		SourcePath:  f.RelativePath,
	}

	absSource, _, err := VerifyPathConfinement(s.library.Root(), f.RelativePath, true)
	if err != nil {
		prev.Allowed = false
		prev.Message = "Source path outside library root: " + err.Error()
		return prev
	}

	// Compute SHA256 of candidate source file for deterministic TOCTOU revalidation
	if s.library.Exists(absSource) {
		if hash, hashErr := computeFileSHA256(absSource); hashErr == nil {
			f.Evidence.SHA256 = hash
			if s.auditRepo != nil {
				_ = s.auditRepo.UpdateFindingEvidence(ctx, f.ID, f.Evidence)
			}
		}
	}

	switch action {
	case music.ActionMoveCanonical:
		if f.TrackID == "" {
			prev.Allowed = false
			prev.Message = "No track ID associated with finding."
			return prev
		}
		track, err := s.catalog.GetTrack(ctx, f.TrackID)
		if err != nil || track == nil {
			prev.Allowed = false
			prev.Message = "Track not found in catalog."
			return prev
		}
		release, err := s.catalog.GetRelease(ctx, track.ReleaseID)
		if err != nil || release == nil {
			prev.Allowed = false
			prev.Message = "Release not found in catalog."
			return prev
		}

		wantPath, err := s.library.Layout().TrackPath(*release, *track, filepath.Ext(f.RelativePath))
		if err != nil {
			prev.Allowed = false
			prev.Message = "Cannot calculate canonical path: " + err.Error()
			return prev
		}
		destRel := s.library.RelPath(wantPath)
		prev.DestinationPath = destRel

		if s.library.Exists(wantPath) {
			prev.Allowed = false
			prev.Message = fmt.Sprintf("A file already exists at canonical target %q.", destRel)
			return prev
		}

		prev.Allowed = true
		prev.FileChanges = []string{
			fmt.Sprintf("Move audio file: %s -> %s", f.RelativePath, destRel),
			fmt.Sprintf("Move sidecars (.lrc, .txt) if present next to audio file"),
		}
		prev.DBChanges = []string{
			fmt.Sprintf("Update files path to %q", destRel),
		}

	case music.ActionRestoreTags:
		if f.TrackID == "" {
			prev.Allowed = false
			prev.Message = "No track ID associated with finding."
			return prev
		}
		if !s.library.Exists(absSource) {
			prev.Allowed = false
			prev.Message = "Physical file does not exist on disk."
			return prev
		}

		prev.Allowed = true
		prev.FileChanges = []string{
			fmt.Sprintf("Rewrite Vorbis comment tags from catalog on file %s (no audio re-encoding)", f.RelativePath),
		}
		prev.DBChanges = []string{
			"Update file size and duration records if changed after retagging",
		}

	case music.ActionAdoptFile:
		// Strict Adoption Rules:
		// EXACT_CATALOG_ID or EXACT_CONTENT: allowed for preview and apply.
		// STRONG_METADATA: allowed for preview with clear warning, but rejected on apply.
		// WEAK_METADATA or UNKNOWN: disallowed for both preview and apply.
		if f.Evidence.Level != music.EvidenceExactCatalogID && f.Evidence.Level != music.EvidenceExactContent && f.Evidence.Level != music.EvidenceStrongMetadata {
			prev.Allowed = false
			prev.Message = fmt.Sprintf("Adoption requires deterministic identity (%s). Level %s cannot be adopted.",
				music.EvidenceExactCatalogID, f.Evidence.Level)
			return prev
		}
		if f.Evidence.Level == music.EvidenceStrongMetadata {
			prev.Allowed = true
			prev.Message = "Preview only. Automatic adoption is disabled for STRONG_METADATA. Deterministic catalog match required."
		} else {
			prev.Allowed = true
		}

		prev.FileChanges = []string{
			fmt.Sprintf("Register untracked file %q in database", f.RelativePath),
		}
		prev.DBChanges = []string{
			fmt.Sprintf("Insert files row for track %s", f.TrackID),
		}
		if !s.library.Exists(absSource) {
			prev.Allowed = false
			prev.Message = "Physical file missing on disk."
			return prev
		}

		// Check if DB already has a file for this track
		existingFiles, _ := s.files.ListByTrack(ctx, f.TrackID)
		if len(existingFiles) > 0 {
			prev.Allowed = false
			prev.Message = fmt.Sprintf("Track already has %d registered file(s) in catalog.", len(existingFiles))
			return prev
		}

		prev.Allowed = true
		prev.FileChanges = []string{
			"File remains in place (no move)",
		}
		prev.DBChanges = []string{
			fmt.Sprintf("Create new files record pointing to %q for track ID %s", f.RelativePath, f.TrackID),
		}

	case music.ActionQuarantineFile:
		if !s.library.Exists(absSource) {
			prev.Allowed = false
			prev.Message = "File does not exist on storage."
			return prev
		}

		destRel := filepath.Join(TrashDirName, f.ID, filepath.Base(f.RelativePath))
		prev.DestinationPath = destRel
		prev.Allowed = true
		prev.FileChanges = []string{
			fmt.Sprintf("Move duplicate/untracked file into quarantine: %s -> %s", f.RelativePath, destRel),
		}
		prev.DBChanges = []string{
			"No catalog metadata altered",
		}
		prev.Warnings = []string{
			".ytmdl-trash is a reserved internal YTMDL directory. YTMDL itself excludes it from integrity scans. Media-server administrators should exclude this directory from media scanning.",
		}

	default:
		prev.Allowed = false
		prev.Message = fmt.Sprintf("Unsupported repair action %q.", action)
	}

	return prev
}

// ApplyRepairs executes the confirmed repair actions.
func (s *Service) ApplyRepairs(ctx context.Context, req RepairApplyRequest) (*RepairApplyResult, error) {
	if !req.Confirm {
		return nil, apperr.New(apperr.CodeInvalidRequest, "Repair execution requires explicit confirmation (confirm: true).")
	}
	if len(req.Actions) == 0 {
		return nil, apperr.New(apperr.CodeInvalidRequest, "No repair actions specified.")
	}
	if s.auditRepo == nil {
		return nil, apperr.New(apperr.CodeInternal, "Audit repository is not configured.")
	}

	// Verify Storage Guard writability
	if guard := s.library.Guard(); guard != nil {
		if err := guard.RequireWritable(); err != nil {
			return nil, apperr.Wrap(apperr.CodeStorageReadOnly, "Storage is not writable. Repair aborted.", err)
		}
	}

	result := &RepairApplyResult{Requested: len(req.Actions)}

	for _, item := range req.Actions {
		finding, err := s.auditRepo.GetFinding(ctx, item.FindingID)
		if err != nil || finding == nil {
			result.Failed++
			result.Warnings = append(result.Warnings, fmt.Sprintf("Finding %s not found or already resolved.", item.FindingID))
			continue
		}

		// Lock source path, target path, and track ID on the shared KeyedMutex instance
		// to guarantee mutual exclusion across download finalization, reorganization, retagging, deletion, and repair.
		unlockSource := s.locks.Lock("path:" + filepath.Clean(finding.RelativePath))
		var unlockTarget func()
		if finding.Evidence.ExpectedPath != "" {
			unlockTarget = s.locks.Lock("path:" + filepath.Clean(finding.Evidence.ExpectedPath))
		}
		var unlockTrack func()
		if finding.TrackID != "" {
			unlockTrack = s.locks.Lock("track:" + finding.TrackID)
		}

		var opErr error
		switch item.Action {
		case music.ActionMoveCanonical:
			opErr = s.applyMoveCanonical(ctx, finding)
		case music.ActionRestoreTags:
			opErr = s.applyRestoreTags(ctx, finding)
		case music.ActionAdoptFile:
			opErr = s.applyAdoptFile(ctx, finding)
		case music.ActionQuarantineFile:
			opErr = s.applyQuarantineFile(ctx, finding)
			if opErr == nil {
				result.Quarantined++
			}
		default:
			opErr = fmt.Errorf("unsupported action %s", item.Action)
		}

		if unlockTrack != nil {
			unlockTrack()
		}
		if unlockTarget != nil {
			unlockTarget()
		}
		unlockSource()

		if opErr != nil {
			result.Failed++
			result.Warnings = append(result.Warnings, fmt.Sprintf("%s: %s", finding.RelativePath, opErr.Error()))
			continue
		}

		// Delete resolved finding from repository
		_ = s.auditRepo.DeleteFinding(ctx, finding.ID)
		result.Applied++
	}

	return result, nil
}

func (s *Service) applyMoveCanonical(ctx context.Context, f *music.AuditFinding) error {
	track, err := s.catalog.GetTrack(ctx, f.TrackID)
	if err != nil || track == nil {
		return apperr.New(apperr.CodeTrackNotFound, "Track not found in catalog.")
	}
	release, err := s.catalog.GetRelease(ctx, track.ReleaseID)
	if err != nil || release == nil {
		return apperr.New(apperr.CodeReleaseNotFound, "Release not found in catalog.")
	}

	wantPath, err := s.library.Layout().TrackPath(*release, *track, filepath.Ext(f.RelativePath))
	if err != nil {
		return err
	}

	sourceAbs, _, err := VerifyPathConfinement(s.library.Root(), f.RelativePath, true)
	if err != nil {
		return err
	}
	destAbs, destRel, err := VerifyPathConfinement(s.library.Root(), s.library.RelPath(wantPath), true)
	if err != nil {
		return err
	}

	// Critical Window Recovery:
	// If source file is already gone from sourceAbs, check if it already committed to destAbs.
	if !s.library.Exists(sourceAbs) && s.library.Exists(destAbs) {
		if f.Evidence.SHA256 != "" {
			destSHA, hashErr := computeFileSHA256(destAbs)
			if hashErr == nil && destSHA == f.Evidence.SHA256 {
				// Move already succeeded before crash. Sync DB and return success.
				stored, err := s.files.FindByPath(ctx, f.RelativePath)
				if err == nil && stored != nil {
					_ = s.files.DeleteByPath(ctx, f.RelativePath)
					stored.Path = destRel
					_, _ = s.files.Upsert(ctx, *stored)
				}
				_ = os.Remove(filepath.Dir(sourceAbs))
				_ = os.Remove(filepath.Dir(filepath.Dir(sourceAbs)))
				return nil
			}
		}
	}

	if !s.library.Exists(sourceAbs) {
		return apperr.Newf(apperr.CodeFileNotFound, "Source file %q not found on storage.", f.RelativePath)
	}

	// TOCTOU SHA-256 Revalidation
	if f.Evidence.SHA256 != "" {
		curSHA, hashErr := computeFileSHA256(sourceAbs)
		if hashErr != nil || curSHA != f.Evidence.SHA256 {
			return apperr.Newf(apperr.CodeConflict, "STALE_REPAIR: Source content changed on disk (expected SHA-256 %s, got %s).", f.Evidence.SHA256, curSHA)
		}
	}

	if s.library.Exists(destAbs) {
		return apperr.Newf(apperr.CodePathConflict, "Target file already exists at %q.", destRel)
	}

	// Move audio track + sidecars (.lrc, .txt) atomically
	if err := s.library.MoveTrack(sourceAbs, destAbs); err != nil {
		return err
	}

	// Update DB files path
	stored, err := s.files.FindByPath(ctx, f.RelativePath)
	if err == nil && stored != nil {
		_ = s.files.DeleteByPath(ctx, f.RelativePath)
		stored.Path = destRel
		_, _ = s.files.Upsert(ctx, *stored)
	}

	// Clean up empty directories left behind
	_ = os.Remove(filepath.Dir(sourceAbs))
	_ = os.Remove(filepath.Dir(filepath.Dir(sourceAbs)))

	return nil
}

func (s *Service) applyRestoreTags(ctx context.Context, f *music.AuditFinding) error {
	absSource, _, err := VerifyPathConfinement(s.library.Root(), f.RelativePath, true)
	if err != nil {
		return err
	}
	if !s.library.Exists(absSource) {
		return apperr.Newf(apperr.CodeFileNotFound, "Source file %q not found on storage.", f.RelativePath)
	}

	// TOCTOU SHA-256 Revalidation
	if f.Evidence.SHA256 != "" {
		curSHA, hashErr := computeFileSHA256(absSource)
		if hashErr != nil || curSHA != f.Evidence.SHA256 {
			return apperr.Newf(apperr.CodeConflict, "STALE_REPAIR: Source content changed on disk (expected SHA-256 %s, got %s).", f.Evidence.SHA256, curSHA)
		}
	}

	return s.retagTrackLocked(ctx, f.TrackID)
}

func (s *Service) applyAdoptFile(ctx context.Context, f *music.AuditFinding) error {
	if f.Evidence.Level != music.EvidenceExactCatalogID && f.Evidence.Level != music.EvidenceExactContent {
		return apperr.Newf(apperr.CodeInvalidRequest, "Adoption rejected: requires deterministic catalog match (got %s).", f.Evidence.Level)
	}

	track, err := s.catalog.GetTrack(ctx, f.TrackID)
	if err != nil || track == nil {
		return apperr.New(apperr.CodeTrackNotFound, "Track not found in catalog.")
	}

	absSource, relSource, err := VerifyPathConfinement(s.library.Root(), f.RelativePath, true)
	if err != nil {
		return err
	}

	// Idempotency / Crash Recovery check: if already registered for this track, return success
	existing, err := s.files.FindByPath(ctx, relSource)
	if err == nil && existing != nil && existing.TrackID == track.ID {
		return nil
	}

	if !s.library.Exists(absSource) {
		return apperr.Newf(apperr.CodeFileNotFound, "Source file %q not found on storage.", f.RelativePath)
	}

	// TOCTOU SHA-256 Revalidation
	if f.Evidence.SHA256 != "" {
		curSHA, hashErr := computeFileSHA256(absSource)
		if hashErr != nil || curSHA != f.Evidence.SHA256 {
			return apperr.Newf(apperr.CodeConflict, "STALE_REPAIR: Source content changed on disk (expected SHA-256 %s, got %s).", f.Evidence.SHA256, curSHA)
		}
	}

	// Check probe info for file record
	var sizeBytes int64
	var durationMS int
	if info, statErr := os.Stat(absSource); statErr == nil {
		sizeBytes = info.Size()
	}
	if s.prober != nil {
		if pInfo, pErr := s.prober.Probe(ctx, absSource); pErr == nil && pInfo != nil {
			sizeBytes = pInfo.SizeBytes
			durationMS = pInfo.DurationMS
		}
	}
	if durationMS <= 0 {
		durationMS = track.DurationMS
	}

	// Upsert file row
	newFile := music.File{
		ID:             music.NewID(),
		TrackID:        track.ID,
		Path:           relSource,
		SizeBytes:      sizeBytes,
		DurationMS:     durationMS,
		SourceProvider: track.SourceProvider,
		SourceID:       track.SourceID,
	}

	if _, err := s.files.Upsert(ctx, newFile); err != nil {
		return apperr.Wrap(apperr.CodeInternal, "Failed to register adopted file in database.", err)
	}

	return nil
}

func (s *Service) applyQuarantineFile(_ context.Context, f *music.AuditFinding) error {
	absSource, _, err := VerifyPathConfinement(s.library.Root(), f.RelativePath, true)
	if err != nil {
		return err
	}

	trashDir := filepath.Join(s.library.Root(), TrashDirName, f.ID)
	destAbs := filepath.Join(trashDir, filepath.Base(f.RelativePath))

	// Critical Window Recovery:
	// If source file is already gone from absSource, check if it already committed to quarantine destAbs.
	if !s.library.Exists(absSource) && s.library.Exists(destAbs) {
		if f.Evidence.SHA256 != "" {
			destSHA, hashErr := computeFileSHA256(destAbs)
			if hashErr == nil && destSHA == f.Evidence.SHA256 {
				// Move already succeeded before crash. Return success.
				_ = os.Remove(filepath.Dir(absSource))
				_ = os.Remove(filepath.Dir(filepath.Dir(absSource)))
				return nil
			}
		} else {
			_ = os.Remove(filepath.Dir(absSource))
			_ = os.Remove(filepath.Dir(filepath.Dir(absSource)))
			return nil
		}
	}

	if !s.library.Exists(absSource) {
		return apperr.Newf(apperr.CodeFileNotFound, "Source file %q not found on storage.", f.RelativePath)
	}

	// TOCTOU SHA-256 Revalidation
	if f.Evidence.SHA256 != "" {
		curSHA, hashErr := computeFileSHA256(absSource)
		if hashErr != nil || curSHA != f.Evidence.SHA256 {
			return apperr.Newf(apperr.CodeConflict, "STALE_REPAIR: Source content changed on disk (expected SHA-256 %s, got %s).", f.Evidence.SHA256, curSHA)
		}
	}

	if err := os.MkdirAll(trashDir, 0o755); err != nil {
		return apperr.Wrap(apperr.CodeInternal, "Failed to create quarantine directory.", err)
	}

	// Move file into quarantine with no-replace protection
	if s.library.Exists(destAbs) {
		return apperr.Newf(apperr.CodePathConflict, "A file already exists in quarantine at %s.", destAbs)
	}

	if err := s.library.MoveTrack(absSource, destAbs); err != nil {
		return apperr.Wrap(apperr.CodeInternal, "Failed to move file to quarantine.", err)
	}

	// Clean up empty directories
	_ = os.Remove(filepath.Dir(absSource))
	_ = os.Remove(filepath.Dir(filepath.Dir(absSource)))

	return nil
}
