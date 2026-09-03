package library

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"ytdm/backend/internal/apperr"
	"ytdm/backend/internal/database/repository"
	"ytdm/backend/internal/downloader"
	"ytdm/backend/internal/jobs"
	"ytdm/backend/internal/metadata"
	"ytdm/backend/internal/music"
	"ytdm/backend/internal/storage"
)

// Mock implementations for testing
type mockCatalog struct {
	tracks   map[string]music.Track
	releases map[string]music.Release
	sources  map[string][]music.Source
}

func newMockCatalog() *mockCatalog {
	return &mockCatalog{
		tracks:   make(map[string]music.Track),
		releases: make(map[string]music.Release),
		sources:  make(map[string][]music.Source),
	}
}

func (m *mockCatalog) GetTrack(_ context.Context, id string) (*music.Track, error) {
	t, ok := m.tracks[id]
	if !ok {
		return nil, apperr.New(apperr.CodeTrackNotFound, "track not found")
	}
	return &t, nil
}

func (m *mockCatalog) ListAllTracks(_ context.Context) ([]repository.StoredTrack, error) {
	var res []repository.StoredTrack
	for _, t := range m.tracks {
		res = append(res, repository.StoredTrack{Track: t, IdentityKey: t.ID})
	}
	return res, nil
}

func (m *mockCatalog) ListReleases(_ context.Context, _ string, _, _ int) ([]music.Release, error) {
	var res []music.Release
	for _, r := range m.releases {
		res = append(res, r)
	}
	return res, nil
}

func (m *mockCatalog) GetRelease(_ context.Context, id string) (*music.Release, error) {
	r, ok := m.releases[id]
	if !ok {
		return nil, apperr.New(apperr.CodeReleaseNotFound, "release not found")
	}
	return &r, nil
}

func (m *mockCatalog) ListTracks(_ context.Context, releaseID string, limit, offset int) ([]music.Track, error) {
	var res []music.Track
	for _, t := range m.tracks {
		if t.ReleaseID == releaseID {
			res = append(res, t)
		}
	}
	return res, nil
}

func (m *mockCatalog) DeleteTrack(_ context.Context, id string) error {
	delete(m.tracks, id)
	delete(m.sources, id)
	return nil
}

func (m *mockCatalog) DeleteRelease(_ context.Context, id string) error {
	delete(m.releases, id)
	return nil
}

func (m *mockCatalog) UpdateReleaseCover(_ context.Context, releaseID string, coverURL string) error {
	r, ok := m.releases[releaseID]
	if !ok {
		return apperr.New(apperr.CodeReleaseNotFound, "release not found")
	}
	r.CoverURL = coverURL
	m.releases[releaseID] = r
	for id, t := range m.tracks {
		if t.ReleaseID == releaseID {
			t.CoverURL = coverURL
			m.tracks[id] = t
		}
	}
	return nil
}

func (m *mockCatalog) ListSources(_ context.Context, trackID string) ([]music.Source, error) {
	return m.sources[trackID], nil
}

func (m *mockCatalog) SetLyricsState(_ context.Context, trackID string, state music.LyricsState, provider string, checkedAt time.Time) error {
	t, ok := m.tracks[trackID]
	if !ok {
		return apperr.New(apperr.CodeTrackNotFound, "track not found")
	}
	t.LyricsState = state
	t.LyricsProvider = provider
	if !checkedAt.IsZero() {
		t.LyricsCheckedAt = &checkedAt
	}
	m.tracks[trackID] = t
	return nil
}

func (m *mockCatalog) ListTracksNeedingLyrics(_ context.Context, before time.Time, limit int) ([]repository.StoredTrack, error) {
	var res []repository.StoredTrack
	for _, t := range m.tracks {
		if t.LyricsState == music.LyricsAvailableSynced {
			continue
		}
		if t.LyricsCheckedAt == nil || t.LyricsCheckedAt.Before(before) {
			res = append(res, repository.StoredTrack{Track: t, IdentityKey: t.ID})
			if limit > 0 && len(res) >= limit {
				break
			}
		}
	}
	return res, nil
}

func (m *mockCatalog) LyricsStats(_ context.Context, cutoff time.Time) (repository.LyricsStats, error) {
	var stats repository.LyricsStats
	stats.TracksScanned = len(m.tracks)
	for _, t := range m.tracks {
		switch t.LyricsState {
		case music.LyricsAvailableSynced:
			stats.AlreadyLRC++
		case music.LyricsAvailablePlain:
			stats.AlreadyTXT++
		case music.LyricsInstrumental:
			stats.Instrumental++
		default:
			stats.Missing++
			if t.LyricsCheckedAt == nil || t.LyricsCheckedAt.Before(cutoff) {
				stats.Eligible++
			}
		}
	}
	return stats, nil
}

func (m *mockCatalog) GetLibraryAggregates(_ context.Context) (
	artistCount, releaseCount, trackCount, fileCount int,
	totalBytes int64,
	lyricsCoverage map[music.LyricsState]int,
	codecBreakdown map[string]int,
	err error,
) {
	lyricsCoverage = make(map[music.LyricsState]int)
	codecBreakdown = make(map[string]int)
	for _, t := range m.tracks {
		lyricsCoverage[t.LyricsState]++
	}
	return 1, len(m.releases), len(m.tracks), len(m.tracks), 1024, lyricsCoverage, codecBreakdown, nil
}

func (m *mockCatalog) ListAllReleases(_ context.Context) ([]music.Release, error) {
	var res []music.Release
	for _, r := range m.releases {
		res = append(res, r)
	}
	return res, nil
}

func (m *mockCatalog) UpsertArtist(_ context.Context, artist music.Artist) (music.Artist, error) {
	return artist, nil
}

func (m *mockCatalog) UpsertRelease(_ context.Context, release music.Release, _ string) (music.Release, error) {
	m.releases[release.ID] = release
	return release, nil
}

func (m *mockCatalog) UpsertTrack(_ context.Context, track music.Track, _, _ string, _ int) (music.Track, error) {
	m.tracks[track.ID] = track
	return track, nil
}

type mockFiles struct {
	files map[string]music.File // path -> file
}

func newMockFiles() *mockFiles {
	return &mockFiles{files: make(map[string]music.File)}
}

func (m *mockFiles) ListAll(_ context.Context) ([]music.File, error) {
	var res []music.File
	for _, f := range m.files {
		res = append(res, f)
	}
	return res, nil
}

func (m *mockFiles) FindByID(_ context.Context, id string) (*music.File, error) {
	for _, f := range m.files {
		if f.ID == id {
			return &f, nil
		}
	}
	return nil, nil
}

func (m *mockFiles) FindByPath(_ context.Context, path string) (*music.File, error) {
	f, ok := m.files[path]
	if !ok {
		return nil, nil
	}
	return &f, nil
}

func (m *mockFiles) ListByTrack(_ context.Context, trackID string) ([]music.File, error) {
	var res []music.File
	for _, f := range m.files {
		if f.TrackID == trackID {
			res = append(res, f)
		}
	}
	return res, nil
}

func (m *mockFiles) Delete(_ context.Context, id string) error {
	for path, f := range m.files {
		if f.ID == id {
			delete(m.files, path)
			break
		}
	}
	return nil
}

func (m *mockFiles) DeleteByTrack(_ context.Context, trackID string) error {
	for path, f := range m.files {
		if f.TrackID == trackID {
			delete(m.files, path)
		}
	}
	return nil
}

func (m *mockFiles) DeleteByPath(_ context.Context, path string) error {
	delete(m.files, path)
	return nil
}

func (m *mockFiles) Upsert(_ context.Context, file music.File) (music.File, error) {
	if file.ID == "" {
		file.ID = music.NewID()
	}
	m.files[file.Path] = file
	return file, nil
}

type mockJobs struct {
	enqueued      []jobs.Request
	unfinishedMap map[string]bool // targetID -> bool
}

func (m *mockJobs) Enqueue(_ context.Context, req jobs.Request) (*jobs.Job, error) {
	m.enqueued = append(m.enqueued, req)
	return &jobs.Job{ID: "job-123", Type: req.Type, TargetID: req.TargetID}, nil
}

func (m *mockJobs) HasUnfinishedJob(_ context.Context, jobType jobs.Type, targetID string) (bool, error) {
	return m.unfinishedMap[targetID], nil
}

type mockProber struct {
	probeFn func(ctx context.Context, path string) (*downloader.AudioInfo, error)
}

func (m *mockProber) Probe(ctx context.Context, path string) (*downloader.AudioInfo, error) {
	if m.probeFn != nil {
		return m.probeFn(ctx, path)
	}
	return &downloader.AudioInfo{
		Codec:       "opus",
		Container:   "ogg",
		BitrateKbps: 160,
		DurationMS:  180000,
		Channels:    2,
		SampleRate:  48000,
		SizeBytes:   1024,
	}, nil
}

type mockTagger struct {
	applied []string
}

func (m *mockTagger) Apply(_ context.Context, path string, _ metadata.Tags, _ *metadata.Artwork) error {
	m.applied = append(m.applied, path)
	return nil
}

func (m *mockTagger) UpdateArtwork(_ context.Context, path string, _ *metadata.Artwork) error {
	m.applied = append(m.applied, path)
	return nil
}

type mockBroker struct {
	events []jobs.Event
}

func (m *mockBroker) Publish(event jobs.Event) {
	m.events = append(m.events, event)
}

func setupTestServiceWithLifecycle(t *testing.T, lifecycle context.Context) (*Service, string, *mockCatalog, *mockFiles, *mockJobs, *mockProber, *mockTagger) {
	t.Helper()
	root := t.TempDir()
	lib, err := storage.NewLibrary(root)
	if err != nil {
		t.Fatal(err)
	}

	catalog := newMockCatalog()
	files := newMockFiles()
	jobMgr := &mockJobs{unfinishedMap: make(map[string]bool)}
	prober := &mockProber{}
	tagger := &mockTagger{}
	broker := &mockBroker{}

	svc, err := NewService(ServiceOptions{
		Lifecycle:   lifecycle,
		Library:     lib,
		Catalog:     catalog,
		Files:       files,
		Jobs:        jobMgr,
		Prober:      prober,
		Tagger:      tagger,
		Broker:      broker,
		Concurrency: 2,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		svc.Stop()
	})

	return svc, root, catalog, files, jobMgr, prober, tagger
}

func setupTestService(t *testing.T) (*Service, string, *mockCatalog, *mockFiles, *mockJobs, *mockProber, *mockTagger) {
	return setupTestServiceWithLifecycle(t, nil)
}

func TestReconcileSixStates(t *testing.T) {
	svc, root, catalog, files, _, prober, _ := setupTestService(t)
	ctx := context.Background()

	artistDir := filepath.Join(root, "Artist", "2020 - Album")
	if err := os.MkdirAll(artistDir, 0o755); err != nil {
		t.Fatal(err)
	}

	// 1. Healthy Track: DB + File + Valid Probe
	healthyRel := filepath.Join("Artist", "2020 - Album", "01 - Healthy.opus")
	healthyAbs := filepath.Join(root, healthyRel)
	_ = os.WriteFile(healthyAbs, []byte("healthy audio"), 0o644)
	healthyTrack := music.Track{ID: "t-healthy", Title: "Healthy", Artists: []string{"Artist"}, Album: "Album", TrackNumber: 1, Year: 2020}
	catalog.tracks[healthyTrack.ID] = healthyTrack
	files.files[healthyRel] = music.File{ID: "f-healthy", TrackID: healthyTrack.ID, Path: healthyRel, SizeBytes: 100, Codec: "opus"}

	// 2. Missing File: DB exists, file missing on disk
	missingRel := filepath.Join("Artist", "2020 - Album", "02 - Missing.opus")
	missingTrack := music.Track{ID: "t-missing", Title: "Missing", Artists: []string{"Artist"}}
	catalog.tracks[missingTrack.ID] = missingTrack
	files.files[missingRel] = music.File{ID: "f-missing", TrackID: missingTrack.ID, Path: missingRel, SizeBytes: 100, Codec: "opus"}

	// 3. Orphan File: File exists on disk, no DB record
	orphanRel := filepath.Join("Artist", "2020 - Album", "03 - Orphan.opus")
	orphanAbs := filepath.Join(root, orphanRel)
	_ = os.WriteFile(orphanAbs, []byte("orphan audio"), 0o644)

	// 4. Invalid File: File exists, but prober reports error or 0 duration
	invalidRel := filepath.Join("Artist", "2020 - Album", "04 - Invalid.opus")
	invalidAbs := filepath.Join(root, invalidRel)
	_ = os.WriteFile(invalidAbs, []byte("corrupt"), 0o644)
	invalidTrack := music.Track{ID: "t-invalid", Title: "Invalid", Artists: []string{"Artist"}}
	catalog.tracks[invalidTrack.ID] = invalidTrack
	files.files[invalidRel] = music.File{ID: "f-invalid", TrackID: invalidTrack.ID, Path: invalidRel, SizeBytes: 100, Codec: "opus"}

	prober.probeFn = func(_ context.Context, path string) (*downloader.AudioInfo, error) {
		if strings.Contains(path, "Invalid") {
			return nil, errors.New("ffprobe decode error")
		}
		return &downloader.AudioInfo{Codec: "opus", DurationMS: 180000, Channels: 2, SampleRate: 48000, SizeBytes: 1024}, nil
	}

	// 5. Duplicate File: Two files for the same track
	dup1Rel := filepath.Join("Artist", "2020 - Album", "05 - Dup1.opus")
	dup2Rel := filepath.Join("Artist", "2020 - Album", "05 - Dup2.opus")
	_ = os.WriteFile(filepath.Join(root, dup1Rel), []byte("dup1"), 0o644)
	_ = os.WriteFile(filepath.Join(root, dup2Rel), []byte("dup2"), 0o644)
	dupTrack := music.Track{ID: "t-dup", Title: "Duplicate", Artists: []string{"Artist"}}
	catalog.tracks[dupTrack.ID] = dupTrack
	files.files[dup1Rel] = music.File{ID: "f-dup1", TrackID: dupTrack.ID, Path: dup1Rel, SizeBytes: 100, Codec: "opus"}
	files.files[dup2Rel] = music.File{ID: "f-dup2", TrackID: dupTrack.ID, Path: dup2Rel, SizeBytes: 100, Codec: "opus"}

	// Run full reconciliation scan
	result, err := svc.Reconcile(ctx)
	if err != nil {
		t.Fatalf("Reconcile: %v", err)
	}

	if result.Status != ScanCompleted {
		t.Fatalf("scan status = %s, want %s", result.Status, ScanCompleted)
	}

	if result.Summary.Healthy != 1 {
		t.Errorf("Healthy = %d, want 1", result.Summary.Healthy)
	}
	if result.Summary.MissingFiles != 1 {
		t.Errorf("MissingFiles = %d, want 1", result.Summary.MissingFiles)
	}
	if result.Summary.OrphanFiles != 1 {
		t.Errorf("OrphanFiles = %d, want 1", result.Summary.OrphanFiles)
	}
	if result.Summary.InvalidFiles != 1 {
		t.Errorf("InvalidFiles = %d, want 1", result.Summary.InvalidFiles)
	}
	if result.Summary.DuplicateFiles < 1 {
		t.Errorf("DuplicateFiles = %d, want >= 1", result.Summary.DuplicateFiles)
	}

	// Verify stats
	stats, err := svc.Stats(ctx)
	if err != nil {
		t.Fatalf("Stats: %v", err)
	}
	if stats.HealthyCount != 1 {
		t.Errorf("stats.HealthyCount = %d, want 1", stats.HealthyCount)
	}
	if stats.IssueCount < 3 {
		t.Errorf("stats.IssueCount = %d, want >= 3", stats.IssueCount)
	}
}

func TestReconcileSingleConcurrentScan(t *testing.T) {
	svc, _, _, _, _, _, _ := setupTestService(t)
	ctx := context.Background()

	// Simulate slow scan
	svc.activeScan.Store(&ScanResult{ID: "running-123", Status: ScanRunning, StartedAt: time.Now()})

	res, err := svc.Reconcile(ctx)
	if err != nil {
		t.Fatalf("expected running scan, got err: %v", err)
	}
	if res.ID != "running-123" || res.Status != ScanRunning {
		t.Fatalf("expected existing running scan to be returned, got: %+v", res)
	}
}

func TestRedownloadTrackConflictAndEnqueue(t *testing.T) {
	svc, _, catalog, _, jobsMock, _, _ := setupTestService(t)
	ctx := context.Background()

	track := music.Track{ID: "t-1", Title: "Song", Artists: []string{"Artist"}, SourceProvider: "deezer", SourceID: "12345"}
	catalog.tracks[track.ID] = track
	catalog.sources[track.ID] = []music.Source{
		{TrackID: track.ID, Provider: "deezer", SourceID: "12345", Kind: music.SourceMetadata},
	}

	// 1. Conflict test: active job in progress
	jobsMock.unfinishedMap["12345"] = true
	_, err := svc.RedownloadTrack(ctx, track.ID)
	if code := apperr.CodeOf(err); code != apperr.CodeAlreadyExists {
		t.Fatalf("expected conflict CodeAlreadyExists, got: %v", err)
	}

	// 2. Normal redownload
	jobsMock.unfinishedMap["12345"] = false
	job, err := svc.RedownloadTrack(ctx, track.ID)
	if err != nil {
		t.Fatalf("RedownloadTrack failed: %v", err)
	}
	if job == nil || len(jobsMock.enqueued) != 1 {
		t.Fatalf("job not enqueued properly: %+v", job)
	}
}

func TestDeleteTrackRemovesFileAndDB(t *testing.T) {
	svc, root, catalog, files, jobsMock, _, _ := setupTestService(t)
	ctx := context.Background()

	track := music.Track{ID: "t-del", Title: "To Delete", Artists: []string{"Artist"}}
	catalog.tracks[track.ID] = track

	filePath := filepath.Join(root, "Artist", "Album", "01 - Delete.opus")
	_ = os.MkdirAll(filepath.Dir(filePath), 0o755)
	_ = os.WriteFile(filePath, []byte("audio"), 0o644)
	relPath := filepath.Join("Artist", "Album", "01 - Delete.opus")

	files.files[relPath] = music.File{ID: "f-del", TrackID: track.ID, Path: relPath}

	// Active job conflict test
	jobsMock.unfinishedMap[track.ID] = true
	if err := svc.DeleteTrack(ctx, track.ID); apperr.CodeOf(err) != apperr.CodeAlreadyExists {
		t.Fatalf("expected conflict on active job, got %v", err)
	}

	jobsMock.unfinishedMap[track.ID] = false
	if err := svc.DeleteTrack(ctx, track.ID); err != nil {
		t.Fatalf("DeleteTrack failed: %v", err)
	}

	// File must be gone from disk
	if _, err := os.Stat(filePath); !os.IsNotExist(err) {
		t.Fatalf("expected file to be deleted from disk, err=%v", err)
	}

	// Record must be gone from DB
	if _, err := catalog.GetTrack(ctx, track.ID); err == nil {
		t.Fatal("expected track to be deleted from catalog")
	}
	if f, _ := files.FindByPath(ctx, relPath); f != nil {
		t.Fatal("expected file to be deleted from files store")
	}
}

func TestDeleteReleaseSurgical(t *testing.T) {
	svc, root, catalog, files, _, _, _ := setupTestService(t)
	ctx := context.Background()

	releaseDir := filepath.Join(root, "Artist", "2021 - Release")
	_ = os.MkdirAll(releaseDir, 0o755)

	release := music.Release{ID: "rel-1", Title: "Release", AlbumArtist: "Artist", Year: 2021}
	catalog.releases[release.ID] = release

	track1 := music.Track{ID: "t-1", ReleaseID: release.ID, Title: "T1"}
	track2 := music.Track{ID: "t-2", ReleaseID: release.ID, Title: "T2"}
	catalog.tracks[track1.ID] = track1
	catalog.tracks[track2.ID] = track2

	p1 := filepath.Join(releaseDir, "01 - T1.opus")
	p2 := filepath.Join(releaseDir, "02 - T2.opus")
	pCover := filepath.Join(releaseDir, "cover.jpg")
	pOrphan := filepath.Join(releaseDir, "random.opus")
	pNotes := filepath.Join(releaseDir, "notes.txt")

	_ = os.WriteFile(p1, []byte("1"), 0o644)
	_ = os.WriteFile(p2, []byte("2"), 0o644)
	_ = os.WriteFile(pCover, []byte("cover"), 0o644)
	_ = os.WriteFile(pOrphan, []byte("orphan"), 0o644)
	_ = os.WriteFile(pNotes, []byte("notes"), 0o644)

	rel1 := filepath.Join("Artist", "2021 - Release", "01 - T1.opus")
	rel2 := filepath.Join("Artist", "2021 - Release", "02 - T2.opus")
	files.files[rel1] = music.File{ID: "f1", TrackID: track1.ID, Path: rel1}
	files.files[rel2] = music.File{ID: "f2", TrackID: track2.ID, Path: rel2}

	if err := svc.DeleteRelease(ctx, release.ID); err != nil {
		t.Fatalf("DeleteRelease failed: %v", err)
	}

	// p1, p2, cover.jpg MUST be deleted
	if _, err := os.Stat(p1); !os.IsNotExist(err) {
		t.Errorf("p1 should be deleted, got: %v", err)
	}
	if _, err := os.Stat(p2); !os.IsNotExist(err) {
		t.Errorf("p2 should be deleted, got: %v", err)
	}
	if _, err := os.Stat(pCover); !os.IsNotExist(err) {
		t.Errorf("cover.jpg should be deleted, got: %v", err)
	}

	// pOrphan and pNotes MUST BE PRESERVED!
	if _, err := os.Stat(pOrphan); err != nil {
		t.Errorf("orphan file must be preserved: %v", err)
	}
	if _, err := os.Stat(pNotes); err != nil {
		t.Errorf("notes file must be preserved: %v", err)
	}
}

func TestDeleteOrphanIssueByID(t *testing.T) {
	svc, root, _, _, _, _, _ := setupTestService(t)
	ctx := context.Background()

	orphanDir := filepath.Join(root, "Artist", "Single")
	_ = os.MkdirAll(orphanDir, 0o755)
	orphanFile := filepath.Join(orphanDir, "01 - Orphan.opus")
	_ = os.WriteFile(orphanFile, []byte("orphan"), 0o644)
	relPath := filepath.Join("Artist", "Single", "01 - Orphan.opus")

	// Run scan to register issue
	result, err := svc.Reconcile(ctx)
	if err != nil {
		t.Fatalf("Reconcile: %v", err)
	}

	var orphanIssueID string
	for _, issue := range result.Issues {
		if issue.Status == StatusOrphanFile && issue.Path == relPath {
			orphanIssueID = issue.ID
			break
		}
	}
	if orphanIssueID == "" {
		t.Fatalf("orphan issue not found in scan results: %+v", result.Issues)
	}

	// Delete orphan by issue ID
	if err := svc.DeleteOrphanIssue(ctx, orphanIssueID); err != nil {
		t.Fatalf("DeleteOrphanIssue failed: %v", err)
	}

	// Verify file is gone
	if _, err := os.Stat(orphanFile); !os.IsNotExist(err) {
		t.Fatalf("expected orphan file to be deleted, err=%v", err)
	}
}
