package settings

import (
	"context"
	"io"
	"log/slog"
	"testing"
	"time"

	"ytdm/backend/internal/config"
	"ytdm/backend/internal/discography"
	"ytdm/backend/internal/downloader"
	"ytdm/backend/internal/jobs"
	"ytdm/backend/internal/matcher"
	"ytdm/backend/internal/metadata"
	"ytdm/backend/internal/music"
	"ytdm/backend/internal/provider"
	"ytdm/backend/internal/storage"
)

type mockSettingsRepo struct {
	values map[string]string
}

func (m *mockSettingsRepo) All(_ context.Context) (map[string]string, error) {
	return m.values, nil
}

func (m *mockSettingsRepo) SetMany(_ context.Context, values map[string]string) error {
	for k, v := range values {
		m.values[k] = v
	}
	return nil
}

type fakeJobStore struct{}

func (f *fakeJobStore) Create(context.Context, *jobs.Job) error { return nil }
func (f *fakeJobStore) Get(context.Context, string) (*jobs.Job, error) {
	return nil, nil
}
func (f *fakeJobStore) List(context.Context, jobs.ListFilter) ([]jobs.Job, int, error) {
	return nil, 0, nil
}
func (f *fakeJobStore) ListUnfinished(context.Context) ([]jobs.Job, error) { return nil, nil }

type testGeniusCtrl struct {
	enabled bool
}

func (c *testGeniusCtrl) IsEnabled() bool   { return c.enabled }
func (c *testGeniusCtrl) SetEnabled(v bool) { c.enabled = v }
func (f *fakeJobStore) SetStatus(context.Context, string, jobs.Status, string, string) error {
	return nil
}
func (f *fakeJobStore) SetLabel(context.Context, string, string) error { return nil }
func (f *fakeJobStore) SetTotal(context.Context, string, int) error    { return nil }
func (f *fakeJobStore) CancelPendingItems(context.Context, string) (int, error) {
	return 0, nil
}
func (f *fakeJobStore) RefreshCounters(context.Context, string) (*jobs.Job, error) {
	return nil, nil
}
func (f *fakeJobStore) SetPriority(context.Context, string, jobs.Priority) error { return nil }
func (f *fakeJobStore) SetPaused(context.Context, string, bool) error            { return nil }
func (f *fakeJobStore) DeleteHistory(context.Context, time.Time, []jobs.Status) (int, int, error) {
	return 0, 0, nil
}
func (f *fakeJobStore) ResetItemForRetry(context.Context, string, string) error { return nil }
func (f *fakeJobStore) ResetFailedItemsInJob(context.Context, string) (int, int, error) {
	return 0, 0, nil
}
func (f *fakeJobStore) AddItems(context.Context, string, []jobs.Item) error { return nil }
func (f *fakeJobStore) ListItems(context.Context, string) ([]jobs.Item, error) {
	return nil, nil
}
func (f *fakeJobStore) ListPendingItems(context.Context, string) ([]jobs.Item, error) {
	return nil, nil
}
func (f *fakeJobStore) GetItem(context.Context, string) (*jobs.Item, error) { return nil, nil }
func (f *fakeJobStore) UpdateItem(context.Context, string, jobs.ItemUpdate) error {
	return nil
}
func (f *fakeJobStore) HasItems(context.Context, string) (bool, error)  { return false, nil }
func (f *fakeJobStore) ResetInFlightItems(context.Context) (int, error) { return 0, nil }
func (f *fakeJobStore) ResetInterruptedJobs(context.Context) (int, error) {
	return 0, nil
}
func (f *fakeJobStore) QueueCounts(context.Context) (jobs.QueueCounts, error) {
	return jobs.QueueCounts{}, nil
}
func (f *fakeJobStore) NextUpJobs(context.Context, int) ([]jobs.NextUpJob, error) { return nil, nil }

type fakeCatalog struct{}

func (f *fakeCatalog) FindTrack(context.Context, music.Track, int) (*music.Track, error) {
	return nil, nil
}
func (f *fakeCatalog) PersistDownload(context.Context, music.LibraryEntry, int) (music.StoredEntry, error) {
	return music.StoredEntry{}, nil
}
func (f *fakeCatalog) FindArtistBySource(context.Context, string, string) (*music.Artist, error) {
	return nil, nil
}

type fakeFiles struct{}

func (f *fakeFiles) ListByTrack(context.Context, string) ([]music.File, error) { return nil, nil }
func (f *fakeFiles) FindByPath(context.Context, string) (*music.File, error)   { return nil, nil }

type fakeDownloader struct{}

func (f *fakeDownloader) Download(context.Context, provider.MediaSource, string, downloader.ProgressCallback) (*downloader.Result, error) {
	return &downloader.Result{}, nil
}

type fakeTagger struct{}

func (f *fakeTagger) Apply(context.Context, string, metadata.Tags, *metadata.Artwork) error {
	return nil
}

type fakeArtworkFetcher struct{}

func (f *fakeArtworkFetcher) Fetch(context.Context, string) (*metadata.Artwork, error) {
	return nil, nil
}

func setupTestSettings(t *testing.T) (*Service, *jobs.Manager, *matcher.Matcher, *mockSettingsRepo) {
	t.Helper()
	root := t.TempDir()
	lib, err := storage.NewLibrary(root)
	if err != nil {
		t.Fatal(err)
	}

	repo := &mockSettingsRepo{values: make(map[string]string)}
	registry := provider.NewRegistry()
	disco, _ := discography.NewService(discography.Options{Registry: registry})
	engine := matcher.New(matcher.Options{MinScore: 80, DurationToleranceMS: 2000})
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	broker := jobs.NewBroker(logger)

	mgr, err := jobs.NewManager(jobs.ManagerOptions{
		Store:          &fakeJobStore{},
		Catalog:        &fakeCatalog{},
		Files:          &fakeFiles{},
		Library:        lib,
		Registry:       registry,
		Discography:    disco,
		Matcher:        engine,
		Downloader:     &fakeDownloader{},
		Tagger:         &fakeTagger{},
		Artwork:        &fakeArtworkFetcher{},
		Broker:         broker,
		Logger:         logger,
		EmbedCover:     true,
		WriteCoverFile: true,
		SkipExisting:   true,
		LyricsEnabled:  true,
	})
	if err != nil {
		t.Fatal(err)
	}

	cfg := config.Config{
		Library:   config.LibraryConfig{Path: root},
		Matching:  config.MatchingConfig{MinScore: 80, DurationToleranceMS: 2000},
		Downloads: config.DownloadsConfig{Concurrent: 2},
	}

	svc, err := New(repo, mgr, engine, cfg)
	if err != nil {
		t.Fatal(err)
	}

	return svc, mgr, engine, repo
}

func TestSettingsLoadAndApplyLyricsToggles(t *testing.T) {
	svc, mgr, _, repo := setupTestSettings(t)
	ctx := context.Background()

	// Initial load with defaults
	repo.values[KeyLyricsEnabled] = "true"
	repo.values[KeyLyricsWriteSidecar] = "true"

	if err := svc.Load(ctx); err != nil {
		t.Fatalf("Load: %v", err)
	}

	cur := svc.Current()
	if !cur.LyricsEnabled || !cur.LyricsWriteSidecar {
		t.Fatalf("unexpected settings: %+v", cur)
	}

	// Apply updates
	lyricsEnabled := false
	lyricsWriteSidecar := false

	updated, err := svc.Apply(ctx, Update{
		LyricsEnabled:      &lyricsEnabled,
		LyricsWriteSidecar: &lyricsWriteSidecar,
	})
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}

	if updated.LyricsEnabled || updated.LyricsWriteSidecar {
		t.Fatalf("updated settings mismatch: %+v", updated)
	}

	if mgr.LyricsEnabled() != false || mgr.LyricsWriteSidecar() != false {
		t.Fatalf("manager state mismatch")
	}

	if repo.values[KeyLyricsEnabled] != "false" || repo.values[KeyLyricsWriteSidecar] != "false" {
		t.Fatalf("repo values mismatch: %+v", repo.values)
	}

	ctrl := &testGeniusCtrl{enabled: false}
	svc.SetGeniusController(ctrl)

	geniusEnabled := true
	updatedGenius, err := svc.Apply(ctx, Update{
		LyricsGeniusEnabled: &geniusEnabled,
	})
	if err != nil {
		t.Fatalf("Apply genius toggle: %v", err)
	}
	if !updatedGenius.LyricsGeniusEnabled {
		t.Fatal("expected LyricsGeniusEnabled=true")
	}
	if !ctrl.enabled {
		t.Fatal("expected controller to be enabled")
	}
	if repo.values[KeyLyricsGeniusEnabled] != "true" {
		t.Fatalf("expected repo value 'true', got %q", repo.values[KeyLyricsGeniusEnabled])
	}
}

func TestSettingsQueuePausePersistence(t *testing.T) {
	svc, mgr, _, repo := setupTestSettings(t)
	ctx := context.Background()

	// Initial load: queue paused = true in repo
	repo.values[KeyQueuePaused] = "true"
	if err := svc.Load(ctx); err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !mgr.QueuePaused() {
		t.Fatal("expected queue paused after Load")
	}

	// SetQueuePaused(false)
	if err := svc.SetQueuePaused(ctx, false); err != nil {
		t.Fatalf("SetQueuePaused(false): %v", err)
	}
	if mgr.QueuePaused() {
		t.Fatal("expected queue unpaused")
	}
	if repo.values[KeyQueuePaused] != "false" {
		t.Fatalf("expected repo value 'false', got %q", repo.values[KeyQueuePaused])
	}
}

func TestSettingsDownloadManagement(t *testing.T) {
	svc, mgr, _, repo := setupTestSettings(t)
	ctx := context.Background()

	// Initial load with custom download management settings
	repo.values[KeyMaxWorkers] = "3"
	repo.values[KeyRateLimit] = "5M"
	repo.values[KeyScheduleEnabled] = "true"
	repo.values[KeyScheduleStart] = "23:00"
	repo.values[KeyScheduleEnd] = "05:00"
	repo.values[KeyScheduleTimezone] = "Europe/Berlin"
	repo.values[KeySubscriptionAutoDownload] = "false"
	repo.values[KeySubscriptionPriority] = "high"
	repo.values[KeySubscriptionReleaseFilter] = `{"albums": true, "singles": false, "eps": true, "live": false, "compilations": false, "remixes": false}`

	if err := svc.Load(ctx); err != nil {
		t.Fatalf("Load: %v", err)
	}

	cur := svc.Current()
	if cur.MaxWorkers != 3 || cur.RateLimit != "5M" || !cur.ScheduleEnabled || cur.ScheduleStart != "23:00" || cur.ScheduleEnd != "05:00" {
		t.Fatalf("unexpected current settings: %+v", cur)
	}
	if cur.SubscriptionAutoDownload != false || cur.SubscriptionPriority != "high" || cur.SubscriptionReleaseFilter.Singles != false {
		t.Fatalf("unexpected subscription defaults: %+v", cur)
	}
	if mgr.MaxWorkers() != 3 || mgr.RateLimit() != "5M" || !mgr.ScheduleEnabled() {
		t.Fatalf("manager state not updated")
	}

	// Apply updates
	newWorkers := 4
	newRate := "10M"
	newSched := false
	newPri := "normal"
	newAuto := true
	filter := music.DefaultReleaseFilter()

	updated, err := svc.Apply(ctx, Update{
		MaxWorkers:                &newWorkers,
		RateLimit:                 &newRate,
		ScheduleEnabled:           &newSched,
		SubscriptionPriority:      &newPri,
		SubscriptionAutoDownload:  &newAuto,
		SubscriptionReleaseFilter: &filter,
	})
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}

	if updated.MaxWorkers != 4 || updated.RateLimit != "10M" || updated.ScheduleEnabled != false || updated.SubscriptionPriority != "normal" || !updated.SubscriptionAutoDownload {
		t.Fatalf("unexpected updated settings: %+v", updated)
	}
	if mgr.MaxWorkers() != 4 || mgr.RateLimit() != "10M" || mgr.ScheduleEnabled() != false {
		t.Fatalf("manager state not updated after Apply")
	}
	if repo.values[KeyMaxWorkers] != "4" || repo.values[KeyRateLimit] != "10M" || repo.values[KeyScheduleEnabled] != "false" {
		t.Fatalf("repo values not persisted: %+v", repo.values)
	}
}

func TestSettingsValidation(t *testing.T) {
	svc, _, _, _ := setupTestSettings(t)
	ctx := context.Background()

	// Invalid worker counts
	zeroWorkers := 0
	if _, err := svc.Apply(ctx, Update{MaxWorkers: &zeroWorkers}); err == nil {
		t.Fatal("expected error for max_workers=0")
	}
	fiveWorkers := 5
	if _, err := svc.Apply(ctx, Update{MaxWorkers: &fiveWorkers}); err == nil {
		t.Fatal("expected error for max_workers=5")
	}

	// Invalid schedule times
	badStart := "25:00"
	if _, err := svc.Apply(ctx, Update{ScheduleStart: &badStart}); err == nil {
		t.Fatal("expected error for schedule_start=25:00")
	}
	badEnd := "12:60"
	if _, err := svc.Apply(ctx, Update{ScheduleEnd: &badEnd}); err == nil {
		t.Fatal("expected error for schedule_end=12:60")
	}

	// Invalid priority
	badPri := "urgent"
	if _, err := svc.Apply(ctx, Update{SubscriptionPriority: &badPri}); err == nil {
		t.Fatal("expected error for priority=urgent")
	}

	// Invalid rate limit strings
	for _, badRL := range []string{"invalid", "-2M", "; rm -rf", "5X", "10MB"} {
		rl := badRL
		if _, err := svc.Apply(ctx, Update{RateLimit: &rl}); err == nil {
			t.Fatalf("expected error for rate_limit=%q", badRL)
		}
	}

	// Valid rate limits
	for _, goodRL := range []string{"", "0", "500K", "2M", "10M", "1000", "5g", "50k"} {
		rl := goodRL
		if _, err := svc.Apply(ctx, Update{RateLimit: &rl}); err != nil {
			t.Fatalf("unexpected error for valid rate_limit=%q: %v", goodRL, err)
		}
	}

	// Schedule start == end rejection when schedule is enabled
	schedEnabled := true
	sameStart := "06:00"
	sameEnd := "06:00"
	if _, err := svc.Apply(ctx, Update{
		ScheduleEnabled: &schedEnabled,
		ScheduleStart:   &sameStart,
		ScheduleEnd:     &sameEnd,
	}); err == nil {
		t.Fatal("expected error when schedule_start == schedule_end and schedule enabled")
	}

	// Schedule start == end permitted when schedule is DISABLED
	schedDisabled := false
	if _, err := svc.Apply(ctx, Update{
		ScheduleEnabled: &schedDisabled,
		ScheduleStart:   &sameStart,
		ScheduleEnd:     &sameEnd,
	}); err != nil {
		t.Fatalf("unexpected error when schedule is disabled even if start == end: %v", err)
	}

	// Timezone validation
	badTZ := "Mars/Olympus_Mons"
	if _, err := svc.Apply(ctx, Update{ScheduleTimezone: &badTZ}); err == nil {
		t.Fatal("expected error for invalid timezone")
	}
	goodTZ := "Europe/Berlin"
	if _, err := svc.Apply(ctx, Update{ScheduleTimezone: &goodTZ}); err != nil {
		t.Fatalf("unexpected error for Europe/Berlin: %v", err)
	}

	// Empty release filter
	emptyFilter := music.ReleaseFilter{}
	if _, err := svc.Apply(ctx, Update{SubscriptionReleaseFilter: &emptyFilter}); err == nil {
		t.Fatal("expected error for all-false release filter")
	}
}
