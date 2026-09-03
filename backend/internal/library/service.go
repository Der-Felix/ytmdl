package library

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"ytdm/backend/internal/apperr"
	"ytdm/backend/internal/database/repository"
	"ytdm/backend/internal/downloader"
	"ytdm/backend/internal/jobs"
	"ytdm/backend/internal/metadata"
	"ytdm/backend/internal/music"
	"ytdm/backend/internal/provider"
	"ytdm/backend/internal/storage"
)

// CatalogStore defines the database catalogue operations required by the library service.
type CatalogStore interface {
	GetTrack(ctx context.Context, id string) (*music.Track, error)
	SetLyricsState(ctx context.Context, trackID string, state music.LyricsState, provider string, checkedAt time.Time) error
	ListTracksNeedingLyrics(ctx context.Context, before time.Time, limit int) ([]repository.StoredTrack, error)
	LyricsStats(ctx context.Context, cutoff time.Time) (repository.LyricsStats, error)
	ListAllTracks(ctx context.Context) ([]repository.StoredTrack, error)
	ListAllReleases(ctx context.Context) ([]music.Release, error)
	ListReleases(ctx context.Context, artistID string, limit, offset int) ([]music.Release, error)
	GetRelease(ctx context.Context, id string) (*music.Release, error)
	ListTracks(ctx context.Context, releaseID string, limit, offset int) ([]music.Track, error)
	DeleteTrack(ctx context.Context, id string) error
	DeleteRelease(ctx context.Context, id string) error
	ListSources(ctx context.Context, trackID string) ([]music.Source, error)
	UpsertArtist(ctx context.Context, artist music.Artist) (music.Artist, error)
	UpsertRelease(ctx context.Context, release music.Release, artistID string) (music.Release, error)
	UpsertTrack(ctx context.Context, track music.Track, releaseID, artistID string, toleranceMS int) (music.Track, error)
	UpdateReleaseCover(ctx context.Context, releaseID string, coverURL string) error
	GetLibraryAggregates(ctx context.Context) (
		artistCount, releaseCount, trackCount, fileCount int,
		totalBytes int64,
		lyricsCoverage map[music.LyricsState]int,
		codecBreakdown map[string]int,
		err error,
	)
}

// FileStore defines the database file operations required by the library service.
type FileStore interface {
	ListAll(ctx context.Context) ([]music.File, error)
	FindByID(ctx context.Context, id string) (*music.File, error)
	FindByPath(ctx context.Context, path string) (*music.File, error)
	ListByTrack(ctx context.Context, trackID string) ([]music.File, error)
	Delete(ctx context.Context, id string) error
	DeleteByTrack(ctx context.Context, trackID string) error
	DeleteByPath(ctx context.Context, path string) error
	Upsert(ctx context.Context, file music.File) (music.File, error)
}

// JobManager defines download job queue interactions.
type JobManager interface {
	Enqueue(ctx context.Context, req jobs.Request) (*jobs.Job, error)
	HasUnfinishedJob(ctx context.Context, jobType jobs.Type, targetID string) (bool, error)
}

// AudioProber inspects audio streams using ffprobe.
type AudioProber interface {
	Probe(ctx context.Context, path string) (*downloader.AudioInfo, error)
}

// Tagger applies metadata tags onto files.
type Tagger interface {
	Apply(ctx context.Context, path string, tags metadata.Tags, artwork *metadata.Artwork) error
	UpdateArtwork(ctx context.Context, path string, artwork *metadata.Artwork) error
}

// EventBroker publishes live SSE events.
type EventBroker interface {
	Publish(event jobs.Event)
}

// LyricsResolver looks up the lyrics of a track. It is optional: a service
// without one refuses the lyrics endpoints instead of pretending.
type LyricsResolver interface {
	Resolve(ctx context.Context, track music.Track, mediaID string) (*music.Lyrics, error)
}

// AudioInfo aliases the downloader audio probe info.
type AudioInfo = downloader.AudioInfo

// AuditStore defines interactions with persisted library audit runs and findings.
type AuditStore interface {
	CreateRun(ctx context.Context, run music.AuditRun) error
	UpdateRunProgress(ctx context.Context, id string, scanned, total, findingsCount int) error
	CompleteRun(ctx context.Context, id string, status music.AuditRunStatus, scanned, total, findingsCount int, errorSummary string) error
	GetRun(ctx context.Context, id string) (*music.AuditRun, error)
	GetActiveRun(ctx context.Context) (*music.AuditRun, error)
	GetLatestRun(ctx context.Context) (*music.AuditRun, error)
	RecoverRunningRuns(ctx context.Context) (int64, error)
	ListRuns(ctx context.Context, limit, offset int) ([]music.AuditRun, int, error)
	DeleteRun(ctx context.Context, id string) error
	InsertFindings(ctx context.Context, findings []music.AuditFinding) error
	ListFindings(ctx context.Context, runID string, opts repository.ListFindingsOptions) ([]music.AuditFinding, int, error)
	GetFinding(ctx context.Context, id string) (*music.AuditFinding, error)
	UpdateFindingEvidence(ctx context.Context, id string, evidence music.FindingEvidence) error
	DeleteFinding(ctx context.Context, id string) error
}

// ServiceOptions configures the library service.
type ServiceOptions struct {
	Lifecycle      context.Context
	Library        *storage.Library
	Catalog        CatalogStore
	Files          FileStore
	Jobs           JobManager
	Prober         AudioProber
	Tagger         Tagger
	Broker         EventBroker
	Lyrics         LyricsResolver
	Audit          AuditStore
	Providers      *provider.Registry
	ArtworkFetcher *metadata.ArtworkFetcher
	Logger         *slog.Logger
	Concurrency    int
}

// Service coordinates library reconciliation, maintenance actions, and safe file mutations.
type Service struct {
	ctx      context.Context
	stop     context.CancelFunc
	stopOnce sync.Once
	wg       sync.WaitGroup

	library        *storage.Library
	catalog        CatalogStore
	files          FileStore
	jobs           JobManager
	prober         AudioProber
	tagger         Tagger
	broker         EventBroker
	lyrics         LyricsResolver
	auditRepo      AuditStore
	providers      *provider.Registry
	artworkFetcher *metadata.ArtworkFetcher
	logger         *slog.Logger
	concurrency    int

	locks *KeyedMutex

	activeAuditMu sync.Mutex
	activeAudit   *ActiveAuditContext

	scanMu       sync.Mutex
	activeScan   atomic.Pointer[ScanResult]
	latestResult atomic.Pointer[ScanResult]

	issuesMu     sync.RWMutex
	orphanIssues map[string]string // issueID -> relative path

	backfill     backfillState
	latestCompat atomic.Pointer[CompatReport]
}

// NewService constructs a library Service.
func NewService(opts ServiceOptions) (*Service, error) {
	if opts.Library == nil {
		return nil, apperr.New(apperr.CodeInternal, "The library service needs a library storage.")
	}
	if opts.Catalog == nil {
		return nil, apperr.New(apperr.CodeInternal, "The library service needs a catalog store.")
	}
	if opts.Files == nil {
		return nil, apperr.New(apperr.CodeInternal, "The library service needs a file store.")
	}
	logger := opts.Logger
	if logger == nil {
		logger = slog.Default()
	}
	concurrency := opts.Concurrency
	if concurrency <= 0 {
		concurrency = 4
	}

	baseCtx := opts.Lifecycle
	if baseCtx == nil {
		baseCtx = context.Background()
	}
	ctx, cancel := context.WithCancel(baseCtx)

	fetcher := opts.ArtworkFetcher
	if fetcher == nil {
		fetcher = metadata.NewArtworkFetcher(nil)
	}

	return &Service{
		ctx:            ctx,
		stop:           cancel,
		library:        opts.Library,
		catalog:        opts.Catalog,
		files:          opts.Files,
		jobs:           opts.Jobs,
		prober:         opts.Prober,
		tagger:         opts.Tagger,
		broker:         opts.Broker,
		lyrics:         opts.Lyrics,
		auditRepo:      opts.Audit,
		providers:      opts.Providers,
		artworkFetcher: fetcher,
		logger:         logger,
		concurrency:    concurrency,
		locks:          NewKeyedMutex(),
		orphanIssues:   make(map[string]string),
	}, nil
}

// Stop cancels the service context and waits for all background workers to finish.
func (s *Service) Stop() {
	s.stopOnce.Do(func() {
		if s.stop != nil {
			s.stop()
		}
		s.wg.Wait()
	})
}

// Reconcile performs a complete read-only scan of the library filesystem and database.
// At most one scan runs concurrently. If a scan is already running, it returns the running scan.
func (s *Service) Reconcile(ctx context.Context) (*ScanResult, error) {
	if current := s.activeScan.Load(); current != nil && current.Status == ScanRunning {
		return current, nil
	}

	s.scanMu.Lock()
	if current := s.activeScan.Load(); current != nil && current.Status == ScanRunning {
		s.scanMu.Unlock()
		return current, nil
	}

	startedAt := time.Now().UTC()
	scan := &ScanResult{
		ID:        music.NewID(),
		Status:    ScanRunning,
		StartedAt: startedAt,
		Issues:    make([]ScanIssue, 0, 32),
		Warnings:  make([]string, 0),
	}
	s.activeScan.Store(scan)
	s.scanMu.Unlock()

	if s.broker != nil {
		s.broker.Publish(jobs.Event{
			Type:   "library_scan_started",
			JobID:  scan.ID,
			Status: "running",
			Label:  "Library reconciliation scan started",
		})
	}

	// 1. Walk filesystem
	discovered, err := WalkLibraryFiles(s.library.Root())
	if err != nil {
		scan.Status = ScanFailed
		scan.Warnings = append(scan.Warnings, err.Error())
		now := time.Now().UTC()
		scan.FinishedAt = &now
		scan.DurationMS = time.Since(startedAt).Milliseconds()
		s.activeScan.Store(nil)
		s.latestResult.Store(scan)
		return scan, err
	}

	// 2. Load DB records
	dbFiles, err := s.files.ListAll(ctx)
	if err != nil {
		scan.Status = ScanFailed
		scan.Warnings = append(scan.Warnings, err.Error())
		now := time.Now().UTC()
		scan.FinishedAt = &now
		scan.DurationMS = time.Since(startedAt).Milliseconds()
		s.activeScan.Store(nil)
		s.latestResult.Store(scan)
		return scan, err
	}

	dbTracks, err := s.catalog.ListAllTracks(ctx)
	if err != nil {
		scan.Status = ScanFailed
		scan.Warnings = append(scan.Warnings, err.Error())
		now := time.Now().UTC()
		scan.FinishedAt = &now
		scan.DurationMS = time.Since(startedAt).Milliseconds()
		s.activeScan.Store(nil)
		s.latestResult.Store(scan)
		return scan, err
	}

	// Build index maps
	dbFilesByPath := make(map[string][]music.File)
	dbFilesByTrack := make(map[string][]music.File)
	for _, f := range dbFiles {
		cleanRel := filepath.Clean(f.Path)
		dbFilesByPath[cleanRel] = append(dbFilesByPath[cleanRel], f)
		if f.TrackID != "" {
			dbFilesByTrack[f.TrackID] = append(dbFilesByTrack[f.TrackID], f)
		}
	}

	dbTracksByID := make(map[string]music.Track, len(dbTracks))
	for _, st := range dbTracks {
		dbTracksByID[st.Track.ID] = st.Track
	}

	physFilesByPath := make(map[string]DiscoveredFile, len(discovered))
	for _, d := range discovered {
		cleanRel := filepath.Clean(d.RelPath)
		physFilesByPath[cleanRel] = d
	}

	scan.FilesScanned = len(discovered)

	type probeResult struct {
		file     DiscoveredFile
		dbFile   *music.File
		track    *music.Track
		info     *downloader.AudioInfo
		probeErr error
		tags     map[string][]string
		tagErr   error
	}

	resultsChan := make(chan probeResult, len(discovered))
	jobsChan := make(chan DiscoveredFile, len(discovered))

	// Worker pool for probing audio
	var wg sync.WaitGroup
	for range s.concurrency {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for d := range jobsChan {
				cleanRel := filepath.Clean(d.RelPath)
				var res probeResult
				res.file = d
				if dbFList, ok := dbFilesByPath[cleanRel]; ok && len(dbFList) > 0 {
					dbF := dbFList[0]
					res.dbFile = &dbF
					if t, tOk := dbTracksByID[dbF.TrackID]; tOk {
						res.track = &t
					}
				}

				if s.prober != nil {
					res.info, res.probeErr = s.prober.Probe(ctx, d.AbsPath)
				}
				if strings.HasSuffix(strings.ToLower(d.AbsPath), ".opus") || strings.HasSuffix(strings.ToLower(d.AbsPath), ".ogg") {
					res.tags, res.tagErr = metadata.ReadTags(d.AbsPath)
				}
				resultsChan <- res
			}
		}()
	}

	for _, d := range discovered {
		jobsChan <- d
	}
	close(jobsChan)
	wg.Wait()
	close(resultsChan)

	s.issuesMu.Lock()
	s.orphanIssues = make(map[string]string)
	s.issuesMu.Unlock()

	var issues []ScanIssue
	var healthyCount, invalidCount, mismatchCount, dupCount, orphanCount, missingCount int

	for res := range resultsChan {
		cleanRel := filepath.Clean(res.file.RelPath)

		if res.dbFile == nil {
			// Orphan file
			orphanCount++
			issueID := music.NewID()
			s.issuesMu.Lock()
			s.orphanIssues[issueID] = cleanRel
			s.issuesMu.Unlock()

			details := "File exists on disk but is not registered in the database catalog."
			if res.tags != nil {
				if isrc := res.tags[metadata.FieldISRC]; len(isrc) > 0 {
					details += " (embedded ISRC: " + isrc[0] + ")"
				}
			}

			issues = append(issues, ScanIssue{
				ID:      issueID,
				Status:  StatusOrphanFile,
				Path:    cleanRel,
				Details: details,
			})
			continue
		}

		// DB file matched
		if res.probeErr != nil || (res.info != nil && (res.info.DurationMS <= 0 || res.info.SizeBytes <= 0)) {
			invalidCount++
			var trackTitle, trackID, artist string
			if res.track != nil {
				trackTitle = res.track.Title
				trackID = res.track.ID
				artist = res.track.DisplayArtist()
			}
			issues = append(issues, ScanIssue{
				ID:         music.NewID(),
				Status:     StatusInvalidFile,
				TrackID:    trackID,
				TrackTitle: trackTitle,
				ArtistName: artist,
				Path:       cleanRel,
				Details:    "Audio file cannot be parsed or carries no playable audio stream.",
			})
			continue
		}

		// Check for duplicates
		if (res.track != nil && len(dbFilesByTrack[res.track.ID]) > 1) || len(dbFilesByPath[cleanRel]) > 1 {
			dupCount++
			trackTitle := ""
			trackID := ""
			artist := ""
			if res.track != nil {
				trackTitle = res.track.Title
				trackID = res.track.ID
				artist = res.track.DisplayArtist()
			}
			issues = append(issues, ScanIssue{
				ID:         music.NewID(),
				Status:     StatusDuplicateFile,
				TrackID:    trackID,
				TrackTitle: trackTitle,
				ArtistName: artist,
				Path:       cleanRel,
				Details:    "Multiple database records reference this physical file or track.",
			})
			continue
		}

		// Check metadata tags
		if res.track != nil && res.tags != nil {
			mismatches := CompareMetadata(*res.track, res.tags)
			if len(mismatches) > 0 {
				mismatchCount++
				issues = append(issues, ScanIssue{
					ID:         music.NewID(),
					Status:     StatusMetadataMismatch,
					TrackID:    res.track.ID,
					TrackTitle: res.track.Title,
					ArtistName: res.track.DisplayArtist(),
					Path:       cleanRel,
					Expected:   res.track.Label(),
					Details:    "Metadata mismatch in fields: " + strings.Join(mismatches, ", "),
				})
				continue
			}
		}

		healthyCount++
	}

	// 3. Detect Missing Files (in DB, not on disk)
	for relPath, dbFList := range dbFilesByPath {
		if _, exists := physFilesByPath[relPath]; !exists {
			for _, dbF := range dbFList {
				missingCount++
				var trackTitle, artist, releaseID string
				if t, ok := dbTracksByID[dbF.TrackID]; ok {
					trackTitle = t.Title
					artist = t.DisplayArtist()
					releaseID = t.ReleaseID
				}
				issues = append(issues, ScanIssue{
					ID:         music.NewID(),
					Status:     StatusMissingFile,
					TrackID:    dbF.TrackID,
					TrackTitle: trackTitle,
					ArtistName: artist,
					ReleaseID:  releaseID,
					Path:       relPath,
					Details:    "Database record exists, but file is missing from library storage.",
				})
			}
		}
	}

	finishedAt := time.Now().UTC()
	scan.Status = ScanCompleted
	scan.FinishedAt = &finishedAt
	scan.DurationMS = time.Since(startedAt).Milliseconds()
	scan.Issues = issues
	scan.Summary = ScanSummary{
		TotalFilesScanned:  len(discovered),
		Healthy:            healthyCount,
		MissingFiles:       missingCount,
		OrphanFiles:        orphanCount,
		InvalidFiles:       invalidCount,
		MetadataMismatches: mismatchCount,
		DuplicateFiles:     dupCount,
	}

	s.activeScan.Store(nil)
	s.latestResult.Store(scan)

	if s.broker != nil {
		s.broker.Publish(jobs.Event{
			Type:   "library_scan_completed",
			JobID:  scan.ID,
			Status: "completed",
			Label:  "Library reconciliation scan completed",
		})
	}

	return scan, nil
}

// GetScan returns the current running scan or the latest finished scan result with pagination and status filter.
func (s *Service) GetScan(_ context.Context, statusFilter string, limit, offset int) (*ScanResult, error) {
	var scan *ScanResult
	if active := s.activeScan.Load(); active != nil && active.Status == ScanRunning {
		scan = active
	} else {
		scan = s.latestResult.Load()
	}

	if scan == nil {
		return &ScanResult{
			ID:     "empty",
			Status: ScanCompleted,
			Issues: []ScanIssue{},
		}, nil
	}

	if statusFilter == "" && limit <= 0 && offset <= 0 {
		return scan, nil
	}

	filtered := make([]ScanIssue, 0, len(scan.Issues))
	for _, iss := range scan.Issues {
		if statusFilter == "" || string(iss.Status) == statusFilter {
			filtered = append(filtered, iss)
		}
	}

	if limit <= 0 {
		limit = 100
	}
	if limit > 500 {
		limit = 500
	}
	if offset < 0 {
		offset = 0
	}

	start := offset
	if start > len(filtered) {
		start = len(filtered)
	}
	end := start + limit
	if end > len(filtered) {
		end = len(filtered)
	}

	copyResult := *scan
	copyResult.Issues = filtered[start:end]
	return &copyResult, nil
}

// Stats calculates summary storage metrics and health statistics.
func (s *Service) Stats(ctx context.Context) (*StorageStats, error) {
	artistCount, releaseCount, trackCount, fileCount, totalBytes, lyricsCoverage, codecBreakdown, err := s.catalog.GetLibraryAggregates(ctx)
	if err != nil {
		return nil, err
	}

	var healthyCount, issueCount int
	if latest := s.latestResult.Load(); latest != nil {
		healthyCount = latest.Summary.Healthy
		issueCount = len(latest.Issues)
	} else {
		healthyCount = fileCount
	}

	return &StorageStats{
		TotalArtists:   artistCount,
		TotalReleases:  releaseCount,
		TotalTracks:    trackCount,
		TotalFiles:     fileCount,
		TotalBytes:     totalBytes,
		HealthyCount:   healthyCount,
		IssueCount:     issueCount,
		CodecBreakdown: codecBreakdown,
		LyricsCoverage: lyricsCoverage,
	}, nil
}

// RedownloadTrack enqueues a redownload job for a known track using the existing jobs manager.
func (s *Service) RedownloadTrack(ctx context.Context, trackID string) (*jobs.Job, error) {
	trackID = strings.TrimSpace(trackID)
	if trackID == "" {
		return nil, apperr.New(apperr.CodeInvalidRequest, "track_id is required.")
	}

	unlock := s.locks.Lock("track:" + trackID)
	defer unlock()

	track, err := s.catalog.GetTrack(ctx, trackID)
	if err != nil {
		return nil, err
	}

	sources, err := s.catalog.ListSources(ctx, trackID)
	if err != nil {
		return nil, err
	}

	var metadataProvider, targetID string
	for _, src := range sources {
		if src.Kind == music.SourceMetadata {
			metadataProvider = src.Provider
			targetID = src.SourceID
			break
		}
	}
	if metadataProvider == "" {
		metadataProvider = track.SourceProvider
		targetID = track.SourceID
	}
	if metadataProvider == "" || targetID == "" {
		return nil, apperr.New(apperr.CodeInvalidRequest, "The track has no recorded provider origin to redownload from.")
	}

	if s.jobs != nil {
		busy, err := s.jobs.HasUnfinishedJob(ctx, jobs.TypeTrack, targetID)
		if err != nil {
			return nil, err
		}
		if busy {
			return nil, apperr.Newf(apperr.CodeAlreadyExists, "A download job for track %q is already in progress.", track.DisplayTitle())
		}

		return s.jobs.Enqueue(ctx, jobs.Request{
			Type:             jobs.TypeTrack,
			MetadataProvider: metadataProvider,
			TargetID:         targetID,
			ReleaseID:        track.ReleaseID,
			Label:            track.Label(),
		})
	}

	return nil, apperr.New(apperr.CodeInternal, "Job manager is not configured.")
}

// RetagTrack rewrites the metadata tags of an existing audio file from catalog records without re-encoding audio samples.
func (s *Service) RetagTrack(ctx context.Context, trackID string) error {
	trackID = strings.TrimSpace(trackID)
	if trackID == "" {
		return apperr.New(apperr.CodeInvalidRequest, "track_id is required.")
	}

	unlock := s.locks.Lock("track:" + trackID)
	defer unlock()

	return s.retagTrackLocked(ctx, trackID)
}

func (s *Service) retagTrackLocked(ctx context.Context, trackID string) error {

	track, err := s.catalog.GetTrack(ctx, trackID)
	if err != nil {
		return err
	}

	if s.jobs != nil {
		busy, err := s.jobs.HasUnfinishedJob(ctx, jobs.TypeTrack, trackID)
		if err != nil {
			return err
		}
		if !busy && track.SourceID != "" {
			busy, err = s.jobs.HasUnfinishedJob(ctx, jobs.TypeTrack, track.SourceID)
			if err != nil {
				return err
			}
		}
		if busy {
			return apperr.New(apperr.CodeAlreadyExists, "Cannot retag track while a download job is active.")
		}
	}

	files, err := s.files.ListByTrack(ctx, trackID)
	if err != nil {
		return err
	}
	if len(files) == 0 {
		return apperr.New(apperr.CodeFileNotFound, "No library files found for this track.")
	}

	if s.tagger == nil {
		return apperr.New(apperr.CodeInternal, "Tagger component is not configured.")
	}

	for _, f := range files {
		absPath, _, confErr := VerifyPathConfinement(s.library.Root(), f.Path, false)
		if confErr != nil {
			return confErr
		}

		mediaSource := provider.MediaSource{
			Provider: f.SourceProvider,
			ID:       f.SourceID,
			URL:      f.SourceURL,
		}
		tags := metadata.TagsFor(*track, mediaSource)

		var artwork *metadata.Artwork
		coverPath := filepath.Join(filepath.Dir(absPath), storage.CoverFileName)
		if coverData, err := os.ReadFile(coverPath); err == nil && len(coverData) > 0 {
			artwork = &metadata.Artwork{
				Data: coverData,
				MIME: "image/jpeg",
			}
		}

		if err := s.tagger.Apply(ctx, absPath, tags, artwork); err != nil {
			return err
		}

		if s.prober != nil {
			info, probeErr := s.prober.Probe(ctx, absPath)
			if probeErr == nil && info != nil {
				f.SizeBytes = info.SizeBytes
				f.DurationMS = info.DurationMS
				_, _ = s.files.Upsert(ctx, f)
			}
		}
	}

	return nil
}

// DeleteTrack deletes a track from the library: its physical files, its files records, and its catalog track row.
func (s *Service) DeleteTrack(ctx context.Context, trackID string) error {
	trackID = strings.TrimSpace(trackID)
	if trackID == "" {
		return apperr.New(apperr.CodeInvalidRequest, "track_id is required.")
	}

	unlock := s.locks.Lock("track:" + trackID)
	defer unlock()

	if s.jobs != nil {
		busy, err := s.jobs.HasUnfinishedJob(ctx, jobs.TypeTrack, trackID)
		if err != nil {
			return err
		}
		if !busy {
			if trk, trkErr := s.catalog.GetTrack(ctx, trackID); trkErr == nil && trk != nil && trk.SourceID != "" {
				busy, err = s.jobs.HasUnfinishedJob(ctx, jobs.TypeTrack, trk.SourceID)
				if err != nil {
					return err
				}
			}
		}
		if busy {
			return apperr.New(apperr.CodeAlreadyExists, "Cannot delete track while a download job is active.")
		}
	}

	files, err := s.files.ListByTrack(ctx, trackID)
	if err != nil {
		return err
	}

	for _, f := range files {
		absPath, _, confErr := VerifyPathConfinement(s.library.Root(), f.Path, true)
		if confErr == nil {
			_ = os.Remove(absPath)
		}
		_ = s.files.Delete(ctx, f.ID)
	}

	_ = s.catalog.DeleteTrack(ctx, trackID)
	return nil
}

// DeleteRelease removes all known files of a release and its cover.jpg, preserving any unknown/orphan files in the folder.
func (s *Service) DeleteRelease(ctx context.Context, releaseID string) error {
	releaseID = strings.TrimSpace(releaseID)
	if releaseID == "" {
		return apperr.New(apperr.CodeInvalidRequest, "release_id is required.")
	}

	unlock := s.locks.Lock("release:" + releaseID)
	defer unlock()

	if s.jobs != nil {
		busy, err := s.jobs.HasUnfinishedJob(ctx, jobs.TypeRelease, releaseID)
		if err != nil {
			return err
		}
		if !busy {
			if rel, relErr := s.catalog.GetRelease(ctx, releaseID); relErr == nil && rel != nil && rel.SourceID != "" {
				busy, err = s.jobs.HasUnfinishedJob(ctx, jobs.TypeRelease, rel.SourceID)
				if err != nil {
					return err
				}
			}
		}
		if busy {
			return apperr.New(apperr.CodeAlreadyExists, "Cannot delete release while a download job is active.")
		}
	}

	tracks, err := s.catalog.ListTracks(ctx, releaseID, 500, 0)

	if err != nil {
		return err
	}

	var candidateDirs []string

	for _, trk := range tracks {
		files, err := s.files.ListByTrack(ctx, trk.ID)
		if err != nil {
			continue
		}
		for _, f := range files {
			absPath, _, confErr := VerifyPathConfinement(s.library.Root(), f.Path, true)
			if confErr == nil {
				_ = os.Remove(absPath)
				dir := filepath.Dir(absPath)
				candidateDirs = append(candidateDirs, dir)
			}
			_ = s.files.Delete(ctx, f.ID)
		}
		_ = s.catalog.DeleteTrack(ctx, trk.ID)
	}

	// Delete known cover.jpg in release directory
	for _, dir := range candidateDirs {
		coverPath := filepath.Join(dir, storage.CoverFileName)
		if _, err := os.Stat(coverPath); err == nil {
			_ = os.Remove(coverPath)
		}
		// Attempt to remove directory if empty (os.Remove will fail if other files exist)
		_ = os.Remove(dir)
	}

	_ = s.catalog.DeleteRelease(ctx, releaseID)
	return nil
}

// DeleteOrphanIssue deletes an unindexed orphan file identified by its Scan Issue ID.
func (s *Service) DeleteOrphanIssue(ctx context.Context, issueID string) error {
	issueID = strings.TrimSpace(issueID)
	if issueID == "" {
		return apperr.New(apperr.CodeInvalidRequest, "issue_id is required.")
	}

	unlock := s.locks.Lock("issue:" + issueID)
	defer unlock()

	s.issuesMu.RLock()
	relPath, ok := s.orphanIssues[issueID]
	s.issuesMu.RUnlock()

	if !ok || relPath == "" {
		// Also check latest scan issues
		latest := s.latestResult.Load()
		if latest != nil {
			for _, iss := range latest.Issues {
				if iss.ID == issueID && iss.Status == StatusOrphanFile {
					relPath = iss.Path
					break
				}
			}
		}
	}

	if relPath == "" {
		return apperr.New(apperr.CodeFileNotFound, "Orphan file issue not found or already resolved.")
	}

	// Verify no DB record exists for this path
	existing, err := s.files.FindByPath(ctx, relPath)
	if err != nil {
		return err
	}
	if existing != nil {
		return apperr.New(apperr.CodeInvalidRequest, "Cannot delete file as an orphan because it is registered in the database.")
	}

	// Confinement check
	absPath, _, confErr := VerifyPathConfinement(s.library.Root(), relPath, false)
	if confErr != nil {
		return confErr
	}

	if err := os.Remove(absPath); err != nil && !errors.Is(err, os.ErrNotExist) {
		return apperr.Wrap(apperr.CodeInternal, "Failed to remove orphan file.", err)
	}

	s.issuesMu.Lock()
	delete(s.orphanIssues, issueID)
	s.issuesMu.Unlock()

	return nil
}

// SetLyricsResolver replaces the lyrics resolver. It exists so that tests can
// drive the lyrics paths without a network.
func (s *Service) SetLyricsResolver(resolver LyricsResolver) { s.lyrics = resolver }

// AuditRepo returns the configured audit repository.
func (s *Service) AuditRepo() AuditStore { return s.auditRepo }

// SetAuditRepository replaces the audit repository.
func (s *Service) SetAuditRepository(repo AuditStore) { s.auditRepo = repo }

// Catalog returns the catalog store.
func (s *Service) Catalog() CatalogStore { return s.catalog }

// Files returns the file store.
func (s *Service) Files() FileStore { return s.files }

// Library returns the underlying library storage.
func (s *Service) Library() *storage.Library { return s.library }

// Locks returns the shared path and track lock coordinator.
func (s *Service) Locks() *KeyedMutex { return s.locks }

// StreamFile verifies and locates an audio file by its database file ID for streaming.
func (s *Service) StreamFile(ctx context.Context, fileID string) (string, *music.File, error) {
	fileID = strings.TrimSpace(fileID)
	if fileID == "" {
		return "", nil, apperr.New(apperr.CodeInvalidRequest, "File ID is required.")
	}
	if s.files == nil {
		return "", nil, apperr.New(apperr.CodeInternal, "File repository is unavailable.")
	}
	file, err := s.files.FindByID(ctx, fileID)
	if err != nil {
		return "", nil, err
	}
	if file == nil {
		return "", nil, apperr.New(apperr.CodeFileNotFound, "File not found in library catalogue.")
	}
	if !IsSupportedAudio(filepath.Ext(file.Path)) {
		return "", nil, apperr.New(apperr.CodeUnsupportedMediaType, "Unsupported audio media format.")
	}
	absPath, _, confErr := VerifyPathConfinement(s.library.Root(), file.Path, false)
	if confErr != nil {
		return "", nil, confErr
	}
	return absPath, file, nil
}

// StreamTrack verifies and locates the primary audio file of a track by its database track ID for streaming.
func (s *Service) StreamTrack(ctx context.Context, trackID string) (string, *music.File, error) {
	trackID = strings.TrimSpace(trackID)
	if trackID == "" {
		return "", nil, apperr.New(apperr.CodeInvalidRequest, "Track ID is required.")
	}
	if s.files == nil {
		return "", nil, apperr.New(apperr.CodeInternal, "File repository is unavailable.")
	}
	files, err := s.files.ListByTrack(ctx, trackID)
	if err != nil {
		return "", nil, err
	}
	for _, file := range files {
		if !IsSupportedAudio(filepath.Ext(file.Path)) {
			continue
		}
		absPath, _, confErr := VerifyPathConfinement(s.library.Root(), file.Path, true)
		if confErr == nil {
			f := file
			return absPath, &f, nil
		}
	}
	// Fallback: check if the provided identifier is directly a file ID
	if file, findErr := s.files.FindByID(ctx, trackID); findErr == nil && file != nil {
		if IsSupportedAudio(filepath.Ext(file.Path)) {
			if absPath, _, confErr := VerifyPathConfinement(s.library.Root(), file.Path, true); confErr == nil {
				return absPath, file, nil
			}
		}
	}
	return "", nil, apperr.New(apperr.CodeFileNotFound, "Audio file is inaccessible or missing from storage.")
}

// SetProber replaces the audio prober component.
func (s *Service) SetProber(prober AudioProber) { s.prober = prober }
