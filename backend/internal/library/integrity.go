package library

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"ytdm/backend/internal/apperr"
	"ytdm/backend/internal/database/repository"
	"ytdm/backend/internal/jobs"
	"ytdm/backend/internal/logging"
	"ytdm/backend/internal/metadata"
	"ytdm/backend/internal/music"
	"ytdm/backend/internal/storage"
)

// ActiveAuditContext tracks the running audit goroutine and its cancel handle.
type ActiveAuditContext struct {
	RunID     string
	Cancel    context.CancelFunc
	Done      chan struct{}
	StartedAt time.Time
}

// StartAudit initiates an asynchronous read-only library integrity audit (quick or deep).
func (s *Service) StartAudit(ctx context.Context, mode music.AuditMode, userID *string) (*music.AuditRun, error) {
	if mode != music.AuditModeQuick && mode != music.AuditModeDeep {
		mode = music.AuditModeQuick
	}

	// Ensure audit repository is configured
	if s.auditRepo == nil {
		return nil, apperr.New(apperr.CodeInternal, "Audit repository is not configured.")
	}

	// 1. Concurrency guard: Only ONE active audit at a time
	s.activeAuditMu.Lock()
	if s.activeAudit != nil {
		s.activeAuditMu.Unlock()
		return nil, apperr.New(apperr.CodeAlreadyExists, "A library audit is already running.")
	}

	// Double check database for active run
	dbActive, err := s.auditRepo.GetActiveRun(ctx)
	if err != nil {
		s.activeAuditMu.Unlock()
		return nil, err
	}
	if dbActive != nil {
		s.activeAuditMu.Unlock()
		return nil, apperr.New(apperr.CodeAlreadyExists, "A library audit is already running in database.")
	}

	// 2. Storage Guard: Read-only validation of storage identity
	if guard := s.library.Guard(); guard != nil {
		if _, err := guard.ValidateIdentity(); err != nil {
			s.activeAuditMu.Unlock()
			return nil, apperr.Wrap(apperr.CodeInternal, "Storage guard validation failed. Audit cannot proceed.", err)
		}
	}

	runID := music.NewID()
	now := time.Now().UTC()

	run := music.AuditRun{
		ID:        runID,
		Mode:      mode,
		Status:    music.AuditRunRunning,
		StartedAt: now,
		CreatedBy: userID,
		CreatedAt: now,
	}

	if err := s.auditRepo.CreateRun(ctx, run); err != nil {
		s.activeAuditMu.Unlock()
		return nil, err
	}

	auditCtx, cancel := context.WithCancel(context.Background())
	doneChan := make(chan struct{})

	active := &ActiveAuditContext{
		RunID:     runID,
		Cancel:    cancel,
		Done:      doneChan,
		StartedAt: now,
	}
	s.activeAudit = active
	s.activeAuditMu.Unlock()

	// Broadcast SSE event
	if s.broker != nil {
		s.broker.Publish(jobs.Event{
			Type:   "library_audit_started",
			JobID:  runID,
			Status: "running",
			Label:  fmt.Sprintf("Library %s audit started", mode),
		})
	}

	// Launch worker in background
	go func() {
		defer close(doneChan)
		defer func() {
			s.activeAuditMu.Lock()
			if s.activeAudit != nil && s.activeAudit.RunID == runID {
				s.activeAudit = nil
			}
			s.activeAuditMu.Unlock()
		}()

		s.executeAudit(auditCtx, runID, mode)
	}()

	return &run, nil
}

// CancelAudit cancels a currently running library audit.
func (s *Service) CancelAudit(ctx context.Context, runID string) error {
	s.activeAuditMu.Lock()
	defer s.activeAuditMu.Unlock()

	if s.activeAudit == nil || s.activeAudit.RunID != runID {
		// If not in memory, check DB
		if s.auditRepo != nil {
			run, err := s.auditRepo.GetRun(ctx, runID)
			if err != nil {
				return err
			}
			if run == nil {
				return apperr.New(apperr.CodeFileNotFound, "Audit run not found.")
			}
			if run.Status == music.AuditRunRunning {
				_ = s.auditRepo.CompleteRun(ctx, runID, music.AuditRunCancelled, run.Scanned, run.Total, run.FindingsCount, "Audit cancelled by user.")
			}
		}
		return nil
	}

	s.activeAudit.Cancel()
	if s.auditRepo != nil {
		_ = s.auditRepo.CompleteRun(ctx, runID, music.AuditRunCancelled, 0, 0, 0, "Audit cancelled by user.")
	}

	if s.broker != nil {
		s.broker.Publish(jobs.Event{
			Type:   "library_audit_cancelled",
			JobID:  runID,
			Status: "cancelled",
			Label:  "Library audit cancelled",
		})
	}

	return nil
}

// WaitForAudit blocks until an audit run finishes or timeout expires. Useful for tests.
func (s *Service) WaitForAudit(ctx context.Context, runID string, timeout time.Duration) (*music.AuditRun, error) {
	deadline := time.After(timeout)
	ticker := time.NewTicker(50 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-deadline:
			return nil, errors.New("timeout waiting for audit run to finish")
		case <-ticker.C:
			if s.auditRepo == nil {
				return nil, errors.New("audit repo not configured")
			}
			run, err := s.auditRepo.GetRun(ctx, runID)
			if err != nil {
				return nil, err
			}
			if run != nil && run.Status != music.AuditRunRunning {
				return run, nil
			}
		}
	}
}

// executeAudit runs the full inspection pipeline and records findings.
func (s *Service) executeAudit(ctx context.Context, runID string, mode music.AuditMode) {
	startedAt := time.Now().UTC()
	logger := slog.Default().With(logging.KeyJobID, runID, "mode", mode)

	// 1. Filesystem scan (Discovery)
	discovered, err := WalkLibraryFiles(s.library.Root())
	if err != nil {
		logger.Error("failed to walk library files", logging.KeyError, err.Error())
		_ = s.auditRepo.CompleteRun(context.Background(), runID, music.AuditRunFailed, 0, 0, 0, "Failed to scan library filesystem: "+err.Error())
		return
	}

	if ctx.Err() != nil {
		_ = s.auditRepo.CompleteRun(context.Background(), runID, music.AuditRunCancelled, 0, len(discovered), 0, "Audit cancelled.")
		return
	}

	// 2. Fetch Catalog & Files from Database
	dbFiles, err := s.files.ListAll(ctx)
	if err != nil {
		logger.Error("failed to list database files", logging.KeyError, err.Error())
		_ = s.auditRepo.CompleteRun(context.Background(), runID, music.AuditRunFailed, 0, len(discovered), 0, "Failed to query files database: "+err.Error())
		return
	}

	storedTracks, err := s.catalog.ListAllTracks(ctx)
	if err != nil {
		logger.Error("failed to list database tracks", logging.KeyError, err.Error())
		_ = s.auditRepo.CompleteRun(context.Background(), runID, music.AuditRunFailed, 0, len(discovered), 0, "Failed to query catalog tracks: "+err.Error())
		return
	}

	storedReleases, err := s.catalog.ListAllReleases(ctx)
	if err != nil {
		logger.Error("failed to list database releases", logging.KeyError, err.Error())
		_ = s.auditRepo.CompleteRun(context.Background(), runID, music.AuditRunFailed, 0, len(discovered), 0, "Failed to query catalog releases: "+err.Error())
		return
	}

	totalItems := len(discovered) + len(dbFiles)
	_ = s.auditRepo.UpdateRunProgress(ctx, runID, 0, totalItems, 0)

	// Index mappings
	physFilesByPath := make(map[string]DiscoveredFile, len(discovered))
	for _, df := range discovered {
		physFilesByPath[df.RelPath] = df
	}

	dbFilesByPath := make(map[string][]music.File, len(dbFiles))
	dbFilesByTrackID := make(map[string][]music.File, len(dbFiles))
	for _, f := range dbFiles {
		dbFilesByPath[f.Path] = append(dbFilesByPath[f.Path], f)
		dbFilesByTrackID[f.TrackID] = append(dbFilesByTrackID[f.TrackID], f)
	}

	dbTracksByID := make(map[string]music.Track, len(storedTracks))
	dbTracksBySourceID := make(map[string]music.Track, len(storedTracks))
	for _, st := range storedTracks {
		dbTracksByID[st.Track.ID] = st.Track
		if st.Track.SourceID != "" {
			dbTracksBySourceID[st.Track.SourceID] = st.Track
		}
	}

	dbReleasesByID := make(map[string]music.Release, len(storedReleases))
	for _, rel := range storedReleases {
		dbReleasesByID[rel.ID] = rel
	}

	var (
		findings   []music.AuditFinding
		findingsMu sync.Mutex
		now        = time.Now().UTC()
	)

	addFinding := func(f music.AuditFinding) {
		findingsMu.Lock()
		defer findingsMu.Unlock()
		if f.ID == "" {
			f.ID = music.NewID()
		}
		f.RunID = runID
		if f.CreatedAt.IsZero() {
			f.CreatedAt = now
		}
		findings = append(findings, f)
	}

	// 3. DB -> Filesystem Checks (Missing Files, Duplicate References)
	for relPath, fList := range dbFilesByPath {
		if ctx.Err() != nil {
			_ = s.auditRepo.CompleteRun(context.Background(), runID, music.AuditRunCancelled, len(findings), totalItems, len(findings), "Audit cancelled.")
			return
		}

		// Check multiple DB files pointing to same path
		if len(fList) > 1 {
			for _, f := range fList {
				trk := dbTracksByID[f.TrackID]
				addFinding(music.AuditFinding{
					FindingCode:  music.FindingFileDuplicate,
					Severity:     music.SeverityWarning,
					RelativePath: relPath,
					TrackID:      f.TrackID,
					ReleaseID:    trk.ReleaseID,
					ArtistName:   trk.DisplayArtist(),
					TrackTitle:   trk.DisplayTitle(),
					Evidence: music.FindingEvidence{
						Level:        music.EvidenceExactCatalogID,
						ActualPath:   relPath,
						ExpectedPath: relPath,
						Details:      fmt.Sprintf("Multiple DB file records (%d) point to this path.", len(fList)),
					},
				})
			}
		}

		// Existence on disk
		if _, exists := physFilesByPath[relPath]; !exists {
			for _, f := range fList {
				trk := dbTracksByID[f.TrackID]
				addFinding(music.AuditFinding{
					FindingCode:  music.FindingFileMissing,
					Severity:     music.SeverityError,
					RelativePath: relPath,
					TrackID:      f.TrackID,
					ReleaseID:    trk.ReleaseID,
					ArtistName:   trk.DisplayArtist(),
					TrackTitle:   trk.DisplayTitle(),
					Evidence: music.FindingEvidence{
						Level:        music.EvidenceExactCatalogID,
						ExpectedPath: relPath,
						Details:      "Database record exists, but physical file is missing from library storage.",
					},
				})
			}
		}
	}

	// 4. Track layout & sidecar checks
	for _, st := range storedTracks {
		if ctx.Err() != nil {
			_ = s.auditRepo.CompleteRun(context.Background(), runID, music.AuditRunCancelled, len(findings), totalItems, len(findings), "Audit cancelled.")
			return
		}

		track := st.Track
		files := dbFilesByTrackID[track.ID]
		release, ok := dbReleasesByID[track.ReleaseID]
		if !ok {
			release = releaseFromStored(st)
		}

		for _, f := range files {
			// Path Mismatch Check
			wantPath, err := s.library.Layout().TrackPath(release, track, filepath.Ext(f.Path))
			if err == nil {
				wantRel := s.library.RelPath(wantPath)
				if wantRel != f.Path {
					action := music.ActionMoveCanonical
					addFinding(music.AuditFinding{
						FindingCode:     music.FindingPathMismatch,
						Severity:        music.SeverityInfo,
						RelativePath:    f.Path,
						TrackID:         track.ID,
						ReleaseID:       track.ReleaseID,
						ArtistName:      track.DisplayArtist(),
						TrackTitle:      track.DisplayTitle(),
						SuggestedAction: &action,
						Evidence: music.FindingEvidence{
							Level:        music.EvidenceExactCatalogID,
							ExpectedPath: wantRel,
							ActualPath:   f.Path,
							Details:      "File path differs from current canonical library layout.",
						},
					})
				}
			}

			// Lyrics Missing Check (only if state expects a local sidecar)
			if track.LyricsState == music.LyricsAvailableSynced || track.LyricsState == music.LyricsAvailablePlain {
				absPath, _, confErr := VerifyPathConfinement(s.library.Root(), f.Path, false)
				if confErr == nil {
					lrcPath, _, readErr := s.library.ReadLyrics(absPath)
					if readErr != nil || lrcPath == "" {
						addFinding(music.AuditFinding{
							FindingCode:  music.FindingLyricsMissing,
							Severity:     music.SeverityInfo,
							RelativePath: f.Path,
							TrackID:      track.ID,
							ReleaseID:    track.ReleaseID,
							ArtistName:   track.DisplayArtist(),
							TrackTitle:   track.DisplayTitle(),
							Evidence: music.FindingEvidence{
								Level:        music.EvidenceExactCatalogID,
								ExpectedPath: strings.TrimSuffix(f.Path, filepath.Ext(f.Path)) + ".lrc",
								Details:      fmt.Sprintf("Lyrics state is %q but no local sidecar file exists.", track.LyricsState),
							},
						})
					}
				}
			}
		}
	}

	// 5. Release Completeness Check
	for _, rel := range storedReleases {
		if rel.TrackCount > 0 {
			var presentFiles int
			tracks, _ := s.catalog.ListTracks(ctx, rel.ID, 500, 0)
			for _, trk := range tracks {
				if len(dbFilesByTrackID[trk.ID]) > 0 {
					presentFiles++
				}
			}
			if presentFiles < rel.TrackCount {
				addFinding(music.AuditFinding{
					FindingCode:  music.FindingReleaseIncomplete,
					Severity:     music.SeverityInfo,
					RelativePath: rel.Title,
					ReleaseID:    rel.ID,
					ArtistName:   rel.AlbumArtist,
					ReleaseTitle: rel.Title,
					Evidence: music.FindingEvidence{
						Level:   music.EvidenceExactCatalogID,
						Details: fmt.Sprintf("Release expects %d tracks but only %d are present in local library.", rel.TrackCount, presentFiles),
					},
				})
			}
		}
	}

	// 6. Filesystem -> DB Checks (Untracked Files & Legacy Duplicates)
	var untrackedFiles []DiscoveredFile
	for _, df := range discovered {
		if _, exists := dbFilesByPath[df.RelPath]; !exists {
			untrackedFiles = append(untrackedFiles, df)
		}
	}

	if mode == music.AuditModeQuick {
		for _, uf := range untrackedFiles {
			addFinding(music.AuditFinding{
				FindingCode:  music.FindingFileUntracked,
				Severity:     music.SeverityWarning,
				RelativePath: uf.RelPath,
				Evidence: music.FindingEvidence{
					Level:      music.EvidenceUnknown,
					ActualPath: uf.RelPath,
					SizeBytes:  uf.SizeBytes,
					Details:    "Audio file exists on disk with no database registration.",
				},
			})
		}
	} else {
		// Deep Audit: Bounded Prober Pool (concurrency: 2..4)
		concurrency := 2
		workChan := make(chan DiscoveredFile, len(discovered))
		for _, df := range discovered {
			workChan <- df
		}
		close(workChan)

		var wg sync.WaitGroup
		var scannedCount atomic.Int64
		lastProgress := time.Now()

		for i := 0; i < concurrency; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				for df := range workChan {
					if ctx.Err() != nil {
						return
					}

					s.deepProbeFile(ctx, df, dbFilesByPath, dbTracksByID, dbTracksBySourceID, storedTracks, addFinding)
					scanned := scannedCount.Add(1)

					// Throttle progress updates to at most once per second
					if time.Since(lastProgress) >= time.Second {
						lastProgress = time.Now()
						_ = s.auditRepo.UpdateRunProgress(ctx, runID, int(scanned), totalItems, len(findings))
					}
				}
			}()
		}
		wg.Wait()

		// Deep Audit: Cover Validation
		for _, rel := range storedReleases {
			s.validateReleaseCover(rel, addFinding)
		}
	}

	// 7. Batch insert all findings into repository
	if err := s.auditRepo.InsertFindings(context.Background(), findings); err != nil {
		logger.Error("failed to insert audit findings", logging.KeyError, err.Error())
		_ = s.auditRepo.CompleteRun(context.Background(), runID, music.AuditRunFailed, len(discovered), totalItems, len(findings), "Failed to persist findings: "+err.Error())
		return
	}

	// 8. Mark Run Completed
	_ = s.auditRepo.CompleteRun(context.Background(), runID, music.AuditRunCompleted, len(discovered), totalItems, len(findings), "")

	if s.broker != nil {
		s.broker.Publish(jobs.Event{
			Type:   "library_audit_completed",
			JobID:  runID,
			Status: "completed",
			Label:  fmt.Sprintf("Library audit completed with %d findings in %s", len(findings), time.Since(startedAt)),
		})
	}
}

// deepProbeFile inspects one physical file with ffprobe and Vorbis tags.
func (s *Service) deepProbeFile(
	ctx context.Context,
	df DiscoveredFile,
	dbFilesByPath map[string][]music.File,
	dbTracksByID map[string]music.Track,
	dbTracksBySourceID map[string]music.Track,
	storedTracks []repository.StoredTrack,
	addFinding func(music.AuditFinding),
) {
	// Probe Audio with Prober
	var (
		info     *AudioInfo
		probeErr error
	)
	if s.prober != nil {
		info, probeErr = s.prober.Probe(ctx, df.AbsPath)
	}

	// Read Tags
	tags, _ := metadata.ReadTags(df.AbsPath)

	dbFileList, isTracked := dbFilesByPath[df.RelPath]

	if isTracked {
		// 1. Tracked file audio validation
		if probeErr != nil || info == nil || info.DurationMS <= 0 {
			errStr := "Audio probe failed or file has 0 duration."
			if probeErr != nil {
				errStr = probeErr.Error()
			}
			addFinding(music.AuditFinding{
				FindingCode:  music.FindingAudioInvalid,
				Severity:     music.SeverityError,
				RelativePath: df.RelPath,
				TrackID:      dbFileList[0].TrackID,
				Evidence: music.FindingEvidence{
					Level:      music.EvidenceExactCatalogID,
					ActualPath: df.RelPath,
					SizeBytes:  df.SizeBytes,
					Details:    errStr,
				},
			})
			return
		}

		// 2. Tracked file tag validation
		if tags != nil && len(dbFileList) > 0 {
			trk, ok := dbTracksByID[dbFileList[0].TrackID]
			if ok {
				mismatches := CompareMetadata(trk, tags)
				if len(mismatches) > 0 {
					action := music.ActionRestoreTags
					addFinding(music.AuditFinding{
						FindingCode:     music.FindingTagMismatch,
						Severity:        music.SeverityWarning,
						RelativePath:    df.RelPath,
						TrackID:         trk.ID,
						ReleaseID:       trk.ReleaseID,
						ArtistName:      trk.DisplayArtist(),
						TrackTitle:      trk.DisplayTitle(),
						SuggestedAction: &action,
						Evidence: music.FindingEvidence{
							Level:          music.EvidenceExactCatalogID,
							ActualPath:     df.RelPath,
							MismatchedTags: mismatches,
							Details:        "Tags on disk diverge from authoritative catalog: " + strings.Join(mismatches, ", "),
						},
					})
				}
			}
		}
		return
	}

	// 3. Untracked file deep analysis (Match against catalog)
	evidenceLevel := music.EvidenceUnknown
	var matchedTrack *music.Track

	// A. Source ID or ISRC match
	if tags != nil {
		sourceID := firstTag(tags, metadata.FieldSourceID, "source_id")
		isrc := firstTag(tags, metadata.FieldISRC, "isrc")
		if sourceID != "" {
			if t, ok := dbTracksBySourceID[sourceID]; ok {
				matchedTrack = &t
				evidenceLevel = music.EvidenceExactCatalogID
			}
		}
		if matchedTrack == nil && isrc != "" {
			for _, st := range storedTracks {
				if st.Track.ISRC == isrc {
					t := st.Track
					matchedTrack = &t
					evidenceLevel = music.EvidenceExactCatalogID
					break
				}
			}
		}
	}

	// B. Strong metadata match (Artist + Title + Album + Duration within 1.5s)
	if matchedTrack == nil && tags != nil && info != nil {
		tagTitle := firstTag(tags, metadata.FieldTitle, "title")
		tagArtist := firstTag(tags, metadata.FieldArtist, "artist")
		for _, st := range storedTracks {
			t := st.Track
			if strings.EqualFold(strings.TrimSpace(t.Title), strings.TrimSpace(tagTitle)) &&
				strings.EqualFold(strings.TrimSpace(t.DisplayArtist()), strings.TrimSpace(tagArtist)) {
				diff := absInt(t.DurationMS - info.DurationMS)
				if diff <= 1500 {
					matchedTrack = &t
					evidenceLevel = music.EvidenceStrongMetadata
					break
				} else if diff <= 3000 {
					matchedTrack = &t
					evidenceLevel = music.EvidenceWeakMetadata
				}
			}
		}
	}

	// Classification: Is it a LEGACY DUPLICATE (active canonical file already exists) or UNTRACKED?
	if matchedTrack != nil {
		canonicalFiles, _ := s.files.ListByTrack(ctx, matchedTrack.ID)
		var canonicalActiveFile *music.File
		for _, cf := range canonicalFiles {
			if s.library.Exists(filepath.Join(s.library.Root(), cf.Path)) {
				canonicalActiveFile = &cf
				break
			}
		}

		if canonicalActiveFile != nil && canonicalActiveFile.Path != df.RelPath {
			// Legacy duplicate finding
			action := music.ActionQuarantineFile
			addFinding(music.AuditFinding{
				FindingCode:     music.FindingLegacyDuplicate,
				Severity:        music.SeverityWarning,
				RelativePath:    df.RelPath,
				TrackID:         matchedTrack.ID,
				ReleaseID:       matchedTrack.ReleaseID,
				ArtistName:      matchedTrack.DisplayArtist(),
				TrackTitle:      matchedTrack.DisplayTitle(),
				SuggestedAction: &action,
				Evidence: music.FindingEvidence{
					Level:           evidenceLevel,
					ActualPath:      df.RelPath,
					CanonicalPath:   canonicalActiveFile.Path,
					CanonicalFileID: canonicalActiveFile.ID,
					SizeBytes:       df.SizeBytes,
					DurationMS:      infoDuration(info),
					Details:         fmt.Sprintf("Legacy file matches catalog track %q which already has a valid canonical file.", matchedTrack.DisplayTitle()),
				},
			})
			return
		}

		// Untracked file with catalog match
		var action *music.RepairAction
		if evidenceLevel == music.EvidenceExactCatalogID {
			act := music.ActionAdoptFile
			action = &act
		}

		addFinding(music.AuditFinding{
			FindingCode:     music.FindingFileUntracked,
			Severity:        music.SeverityWarning,
			RelativePath:    df.RelPath,
			TrackID:         matchedTrack.ID,
			ReleaseID:       matchedTrack.ReleaseID,
			ArtistName:      matchedTrack.DisplayArtist(),
			TrackTitle:      matchedTrack.DisplayTitle(),
			SuggestedAction: action,
			Evidence: music.FindingEvidence{
				Level:      evidenceLevel,
				ActualPath: df.RelPath,
				SizeBytes:  df.SizeBytes,
				DurationMS: infoDuration(info),
				Details:    fmt.Sprintf("Untracked file matches catalog track %q (%s).", matchedTrack.DisplayTitle(), evidenceLevel),
			},
		})
		return
	}

	// Completely unknown untracked file
	addFinding(music.AuditFinding{
		FindingCode:  music.FindingFileUntracked,
		Severity:     music.SeverityWarning,
		RelativePath: df.RelPath,
		Evidence: music.FindingEvidence{
			Level:      music.EvidenceUnknown,
			ActualPath: df.RelPath,
			SizeBytes:  df.SizeBytes,
			DurationMS: infoDuration(info),
			Details:    "Audio file exists on disk with no catalog match.",
		},
	})
}

func (s *Service) validateReleaseCover(rel music.Release, addFinding func(music.AuditFinding)) {
	if rel.CoverURL == "" {
		return
	}
	relDir, err := s.library.Layout().ReleaseDir(rel)
	if err != nil {
		return
	}
	dirInfo, err := os.Stat(relDir)
	if err != nil || !dirInfo.IsDir() {
		return
	}

	coverPath := filepath.Join(relDir, storage.CoverFileName)
	info, err := os.Stat(coverPath)

	if err != nil {
		if os.IsNotExist(err) {
			relPath := s.library.RelPath(coverPath)
			addFinding(music.AuditFinding{
				FindingCode:  music.FindingCoverMissing,
				Severity:     music.SeverityWarning,
				RelativePath: relPath,
				ReleaseID:    rel.ID,
				ArtistName:   rel.AlbumArtist,
				ReleaseTitle: rel.Title,
				Evidence: music.FindingEvidence{
					ExpectedPath: relPath,
					Details:      "Release directory does not contain cover.jpg.",
				},
			})
		}
		return
	}

	if info.Size() == 0 {
		relPath := s.library.RelPath(coverPath)
		addFinding(music.AuditFinding{
			FindingCode:  music.FindingCoverInvalid,
			Severity:     music.SeverityError,
			RelativePath: relPath,
			ReleaseID:    rel.ID,
			ArtistName:   rel.AlbumArtist,
			ReleaseTitle: rel.Title,
			Evidence: music.FindingEvidence{
				ActualPath: relPath,
				SizeBytes:  0,
				Details:    "Cover file exists but is 0 bytes.",
			},
		})
		return
	}

	// Validate Image Header (bounded read of first 4KB)
	data, err := os.ReadFile(coverPath)
	if err == nil && len(data) > 0 {
		_, _, decErr := image.DecodeConfig(bytes.NewReader(data))
		if decErr != nil {
			relPath := s.library.RelPath(coverPath)
			addFinding(music.AuditFinding{
				FindingCode:  music.FindingCoverInvalid,
				Severity:     music.SeverityError,
				RelativePath: relPath,
				ReleaseID:    rel.ID,
				ArtistName:   rel.AlbumArtist,
				ReleaseTitle: rel.Title,
				Evidence: music.FindingEvidence{
					ActualPath: relPath,
					SizeBytes:  info.Size(),
					Details:    "Cover file is corrupted or not a valid image: " + decErr.Error(),
				},
			})
		}
	}
}

func infoDuration(info *AudioInfo) int64 {
	if info == nil {
		return 0
	}
	return int64(info.DurationMS)
}

func absInt(n int) int {
	if n < 0 {
		return -n
	}
	return n
}
