package subscriptions

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"ytdm/backend/internal/apperr"
	"ytdm/backend/internal/discography"
	"ytdm/backend/internal/jobs"
	"ytdm/backend/internal/music"
	"ytdm/backend/internal/provider"
)

/* ------------------------------------------------------------------- doubles */

// fakeProvider is a metadata provider whose catalogue the test writes out.
//
// Every getter hands out a copy. A real provider decodes its answer afresh on
// every call, so the caller may mutate what it gets back — the discography
// service does exactly that when it stamps the release onto a track — and a
// double that returned its own slice would turn two parallel syncs into a
// data race that no production code has.
type fakeProvider struct {
	name     string
	artist   *music.Artist
	releases []music.Release
	tracks   map[string][]music.Track

	artistErr error
	discoErr  error
	trackErrs map[string]error

	mu         sync.Mutex
	trackCalls int
}

func (f *fakeProvider) Name() string {
	if f.name != "" {
		return f.name
	}
	return "fake"
}

func (f *fakeProvider) SearchArtists(context.Context, string) ([]music.Artist, error) {
	return nil, nil
}

func (f *fakeProvider) GetArtist(_ context.Context, id string) (*music.Artist, error) {
	if f.artistErr != nil {
		return nil, f.artistErr
	}
	if f.artist != nil {
		return f.artist, nil
	}
	return &music.Artist{Name: "Artist " + id, Provider: f.Name(), SourceID: id}, nil
}

func (f *fakeProvider) GetDiscography(context.Context, string) ([]music.Release, error) {
	if f.discoErr != nil {
		return nil, f.discoErr
	}
	return append([]music.Release(nil), f.releases...), nil
}

func (f *fakeProvider) GetRelease(_ context.Context, id string) (*music.Release, error) {
	for _, r := range f.releases {
		if r.SourceID == id {
			return &r, nil
		}
	}
	return nil, apperr.Newf(apperr.CodeReleaseNotFound, "no release %q", id)
}

func (f *fakeProvider) GetReleaseTracks(_ context.Context, id string) ([]music.Track, error) {
	f.mu.Lock()
	f.trackCalls++
	f.mu.Unlock()

	if err, ok := f.trackErrs[id]; ok {
		return nil, err
	}
	return append([]music.Track(nil), f.tracks[id]...), nil
}

// fakeStore is an in-memory subscription store.
type fakeStore struct {
	mu    sync.Mutex
	items map[string]*Subscription
	seq   int

	createErr error
	getErr    error
	listErr   error
	recordErr error

	recorded []SyncOutcome
}

func newFakeStore() *fakeStore {
	return &fakeStore{items: make(map[string]*Subscription)}
}

func (f *fakeStore) Create(_ context.Context, req NewSubscription) (*Subscription, error) {
	if f.createErr != nil {
		return nil, f.createErr
	}
	if err := req.Validate(); err != nil {
		return nil, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()

	for _, sub := range f.items {
		if sub.Provider == req.Provider && sub.ArtistSourceID == req.ArtistSourceID {
			copied := *sub
			return &copied, nil
		}
	}
	f.seq++
	now := time.Now().UTC()
	filter := music.DefaultReleaseFilter()
	if req.ReleaseFilter != nil && req.ReleaseFilter.Any() {
		filter = *req.ReleaseFilter
	}
	priority := jobs.PriorityLow
	if req.DownloadPriority != nil && req.DownloadPriority.Valid() {
		priority = *req.DownloadPriority
	}
	enabled := true
	if req.Enabled != nil {
		enabled = *req.Enabled
	}
	sub := &Subscription{
		ID:               "sub-" + string(rune('a'+f.seq-1)),
		Provider:         req.Provider,
		ArtistSourceID:   req.ArtistSourceID,
		ArtistName:       req.ArtistName,
		ArtistImageURL:   req.ArtistImageURL,
		Enabled:          enabled,
		AutoDownload:     req.AutoDownload,
		ReleaseFilter:    filter,
		DownloadPriority: priority,
		NextSyncAt:       now,
		LastSyncStatus:   StatusPending,
		CreatedAt:        now,
		UpdatedAt:        now,
	}
	f.items[sub.ID] = sub
	copied := *sub
	return &copied, nil
}

func (f *fakeStore) Get(_ context.Context, id string) (*Subscription, error) {
	if f.getErr != nil {
		return nil, f.getErr
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	sub, ok := f.items[id]
	if !ok {
		return nil, apperr.Newf(apperr.CodeSubscriptionNotFound, "Subscription %q does not exist.", id)
	}
	copied := *sub
	return &copied, nil
}

func (f *fakeStore) FindBySource(_ context.Context, provider, sourceID string) (*Subscription, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, sub := range f.items {
		if sub.Provider == provider && sub.ArtistSourceID == sourceID {
			copied := *sub
			return &copied, nil
		}
	}
	return nil, nil
}

func (f *fakeStore) List(context.Context, ListFilter) ([]Subscription, error) {
	if f.listErr != nil {
		return nil, f.listErr
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]Subscription, 0, len(f.items))
	for _, sub := range f.items {
		out = append(out, *sub)
	}
	return out, nil
}

func (f *fakeStore) ListAll(context.Context) ([]Subscription, error) {
	if f.listErr != nil {
		return nil, f.listErr
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]Subscription, 0, len(f.items))
	for _, sub := range f.items {
		out = append(out, *sub)
	}
	return out, nil
}

func (f *fakeStore) ApplyImport(_ context.Context, newSubs []NewSubscription, updates []ImportUpdate) (*ImportResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	result := &ImportResult{}
	now := time.Now().UTC()

	for _, req := range newSubs {
		f.seq++
		enabled := true
		if req.Enabled != nil {
			enabled = *req.Enabled
		}
		filter := music.DefaultReleaseFilter()
		if req.ReleaseFilter != nil && req.ReleaseFilter.Any() {
			filter = *req.ReleaseFilter
		}
		priority := jobs.PriorityLow
		if req.DownloadPriority != nil && req.DownloadPriority.Valid() {
			priority = *req.DownloadPriority
		}
		sub := &Subscription{
			ID:               "sub-" + string(rune('a'+f.seq-1)),
			Provider:         req.Provider,
			ArtistSourceID:   req.ArtistSourceID,
			ArtistName:       req.ArtistName,
			ArtistImageURL:   req.ArtistImageURL,
			Enabled:          enabled,
			AutoDownload:     req.AutoDownload,
			ReleaseFilter:    filter,
			DownloadPriority: priority,
			NextSyncAt:       now,
			LastSyncStatus:   StatusPending,
			CreatedAt:        now,
			UpdatedAt:        now,
		}
		f.items[sub.ID] = sub
		result.Created++
	}

	for _, up := range updates {
		sub, ok := f.items[up.ID]
		if ok {
			sub.Enabled = up.Enabled
			sub.AutoDownload = up.AutoDownload
			sub.ReleaseFilter = up.ReleaseFilter
			sub.DownloadPriority = up.DownloadPriority
			if up.ArtistName != "" {
				sub.ArtistName = up.ArtistName
			}
			if up.ArtistImageURL != "" {
				sub.ArtistImageURL = up.ArtistImageURL
			}
			sub.UpdatedAt = now
			result.Updated++
		}
	}

	return result, nil
}

func (f *fakeStore) Update(_ context.Context, id string, update Update) (*Subscription, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	sub, ok := f.items[id]
	if !ok {
		return nil, apperr.Newf(apperr.CodeSubscriptionNotFound, "Subscription %q does not exist.", id)
	}
	if update.Enabled != nil {
		sub.Enabled = *update.Enabled
	}
	if update.AutoDownload != nil {
		sub.AutoDownload = *update.AutoDownload
	}
	copied := *sub
	return &copied, nil
}

func (f *fakeStore) Delete(_ context.Context, id string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, ok := f.items[id]; !ok {
		return apperr.Newf(apperr.CodeSubscriptionNotFound, "Subscription %q does not exist.", id)
	}
	delete(f.items, id)
	return nil
}

func (f *fakeStore) ListDueForSync(_ context.Context, now time.Time, limit int) ([]Subscription, error) {
	if f.listErr != nil {
		return nil, f.listErr
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]Subscription, 0, len(f.items))
	for _, sub := range f.items {
		if sub.Enabled && !sub.NextSyncAt.After(now) {
			out = append(out, *sub)
		}
		if limit > 0 && len(out) >= limit {
			break
		}
	}
	return out, nil
}

func (f *fakeStore) RecordSync(_ context.Context, id string, outcome SyncOutcome) error {
	if f.recordErr != nil {
		return f.recordErr
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	sub, ok := f.items[id]
	if !ok {
		return apperr.Newf(apperr.CodeSubscriptionNotFound, "Subscription %q does not exist.", id)
	}
	at := outcome.At
	sub.LastSyncAt = &at
	sub.NextSyncAt = outcome.NextAt
	sub.LastSyncStatus = outcome.Status
	sub.LastError = outcome.Error
	f.recorded = append(f.recorded, outcome)
	return nil
}

func (f *fakeStore) outcomes() []SyncOutcome {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]SyncOutcome(nil), f.recorded...)
}

// fakeCatalog answers what the library already holds.
type fakeCatalog struct {
	mu       sync.Mutex
	releases map[string]*music.Release // provider|source_id
	tracks   map[string]*music.Track   // identity key

	releaseErr error
	trackErr   error
}

func newFakeCatalog() *fakeCatalog {
	return &fakeCatalog{
		releases: make(map[string]*music.Release),
		tracks:   make(map[string]*music.Track),
	}
}

func (f *fakeCatalog) addRelease(provider, sourceID string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.releases[provider+"|"+sourceID] = &music.Release{Provider: provider, SourceID: sourceID}
}

func (f *fakeCatalog) addTrack(id string, track music.Track) *music.Track {
	f.mu.Lock()
	defer f.mu.Unlock()
	stored := track
	stored.ID = id
	f.tracks[discography.IdentityKey(track)] = &stored
	return &stored
}

func (f *fakeCatalog) FindReleaseBySource(_ context.Context, provider, sourceID string) (*music.Release, error) {
	if f.releaseErr != nil {
		return nil, f.releaseErr
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.releases[provider+"|"+sourceID], nil
}

func (f *fakeCatalog) FindTrack(_ context.Context, track music.Track, _ int) (*music.Track, error) {
	if f.trackErr != nil {
		return nil, f.trackErr
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.tracks[discography.IdentityKey(track)], nil
}

// fakeFiles answers whether a recording was downloaded.
type fakeFiles struct {
	mu    sync.Mutex
	byID  map[string][]music.File
	fails error
}

func newFakeFiles() *fakeFiles { return &fakeFiles{byID: make(map[string][]music.File)} }

func (f *fakeFiles) markDownloaded(trackID string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.byID[trackID] = []music.File{{ID: "file-" + trackID, TrackID: trackID, Path: "/x/" + trackID}}
}

func (f *fakeFiles) ListByTrack(_ context.Context, trackID string) ([]music.File, error) {
	if f.fails != nil {
		return nil, f.fails
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.byID[trackID], nil
}

// fakeDownloader records what was handed to the queue and, like the real one,
// refuses to take a release an unfinished job already covers.
type fakeDownloader struct {
	mu       sync.Mutex
	releases []string
	active   map[string]struct{}
	err      error
}

func (f *fakeDownloader) EnqueueRelease(ctx context.Context, provider, releaseID, label string) (bool, error) {
	return f.EnqueueReleaseWithPriority(ctx, provider, releaseID, label, jobs.PriorityNormal)
}

func (f *fakeDownloader) EnqueueReleaseWithPriority(_ context.Context, _, releaseID, _ string, _ jobs.Priority) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.err != nil {
		return false, f.err
	}
	if _, running := f.active[releaseID]; running {
		return false, nil
	}
	if f.active == nil {
		f.active = make(map[string]struct{})
	}
	f.active[releaseID] = struct{}{}
	f.releases = append(f.releases, releaseID)
	return true, nil
}

// finish marks the job for a release as no longer active.
func (f *fakeDownloader) finish(releaseID string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.active, releaseID)
}

func (f *fakeDownloader) queued() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.releases...)
}

/* -------------------------------------------------------------------- setup */

type harness struct {
	service    *Service
	store      *fakeStore
	catalog    *fakeCatalog
	files      *fakeFiles
	downloader *fakeDownloader
	provider   *fakeProvider
	broker     *jobs.Broker
}

func track(title string, durationMS int, isrc string) music.Track {
	return music.Track{
		Title: title, Artists: []string{"Daft Punk"},
		DurationMS: durationMS, ISRC: isrc,
		SourceProvider: "fake", SourceID: strings.ToLower(title),
	}
}

func newHarness(t *testing.T, p *fakeProvider) *harness {
	t.Helper()

	registry := provider.NewRegistry()
	registry.RegisterMetadata(p)

	quiet := slog.New(slog.NewTextHandler(io.Discard, nil))
	disco, err := discography.NewService(discography.Options{Registry: registry, Logger: quiet})
	if err != nil {
		t.Fatalf("discography service: %v", err)
	}

	h := &harness{
		store:      newFakeStore(),
		catalog:    newFakeCatalog(),
		files:      newFakeFiles(),
		downloader: &fakeDownloader{},
		provider:   p,
		broker:     jobs.NewBroker(nil),
	}

	service, err := New(Options{
		Store:         h.store,
		Catalog:       h.catalog,
		Files:         h.files,
		Discography:   disco,
		Registry:      registry,
		Downloader:    h.downloader,
		Broker:        h.broker,
		Logger:        quiet,
		SyncInterval:  24 * time.Hour,
		RetryInterval: time.Hour,
	})
	if err != nil {
		t.Fatalf("subscription service: %v", err)
	}
	h.service = service
	return h
}

// discoveryProvider is the standard catalogue used by most tests: one album
// with two tracks.
func discoveryProvider() *fakeProvider {
	return &fakeProvider{
		artist: &music.Artist{Name: "Daft Punk", Provider: "fake", SourceID: "27"},
		releases: []music.Release{{
			Title: "Discovery", ReleaseType: music.ReleaseAlbum, Year: 2001,
			Provider: "fake", SourceID: "302127",
		}},
		tracks: map[string][]music.Track{
			"302127": {
				track("One More Time", 320_000, "GBDUW0000059"),
				track("Aerodynamic", 212_000, "GBDUW0000060"),
			},
		},
	}
}

func subscribe(t *testing.T, h *harness, autoDownload bool) *Subscription {
	t.Helper()
	sub, err := h.service.Create(context.Background(), NewSubscription{
		Provider: "fake", ArtistSourceID: "27", ArtistName: "Daft Punk",
		AutoDownload: autoDownload,
	})
	if err != nil {
		t.Fatalf("subscribe: %v", err)
	}
	return sub
}

/* -------------------------------------------------------------------- tests */

func TestSyncReportsEverythingAsNewOnAnEmptyLibrary(t *testing.T) {
	h := newHarness(t, discoveryProvider())
	sub := subscribe(t, h, false)

	result, err := h.service.Sync(context.Background(), sub.ID)
	if err != nil {
		t.Fatalf("sync: %v", err)
	}

	if result.Status != StatusSuccess {
		t.Fatalf("expected success, got %q", result.Status)
	}
	if result.ReleasesSeen != 1 || result.NewReleases != 1 {
		t.Fatalf("releases: seen %d, new %d", result.ReleasesSeen, result.NewReleases)
	}
	if result.TracksSeen != 2 || result.NewTracks != 2 {
		t.Fatalf("tracks: seen %d, new %d", result.TracksSeen, result.NewTracks)
	}
	if result.SkippedTracks != 0 {
		t.Fatalf("nothing is downloaded yet, so nothing may be skipped: %d", result.SkippedTracks)
	}
	if result.Artist != "Daft Punk" {
		t.Fatalf("the artist name is missing from the result: %q", result.Artist)
	}
}

func TestSyncReportsNoChangesWhenTheLibraryIsUpToDate(t *testing.T) {
	p := discoveryProvider()
	h := newHarness(t, p)
	sub := subscribe(t, h, false)

	h.catalog.addRelease("fake", "302127")
	for i, tr := range p.tracks["302127"] {
		id := "track-" + string(rune('a'+i))
		h.catalog.addTrack(id, tr)
		h.files.markDownloaded(id)
	}

	result, err := h.service.Sync(context.Background(), sub.ID)
	if err != nil {
		t.Fatalf("sync: %v", err)
	}

	if result.NewReleases != 0 {
		t.Fatalf("expected no new releases, got %d", result.NewReleases)
	}
	if result.NewTracks != 0 {
		t.Fatalf("expected no new tracks, got %d", result.NewTracks)
	}
	if result.SkippedTracks != 2 {
		t.Fatalf("both tracks are downloaded and should be skipped, got %d", result.SkippedTracks)
	}
	if result.Status != StatusSuccess {
		t.Fatalf("expected success, got %q", result.Status)
	}
}

func TestSyncDetectsANewRelease(t *testing.T) {
	p := discoveryProvider()
	p.releases = append(p.releases, music.Release{
		Title: "Random Access Memories", ReleaseType: music.ReleaseAlbum, Year: 2013,
		Provider: "fake", SourceID: "6982633",
	})
	p.tracks["6982633"] = []music.Track{track("Get Lucky", 369_000, "USQX91300108")}
	h := newHarness(t, p)
	sub := subscribe(t, h, false)

	// Discovery is already known in full; only the second album is new.
	h.catalog.addRelease("fake", "302127")
	for i, tr := range p.tracks["302127"] {
		id := "track-" + string(rune('a'+i))
		h.catalog.addTrack(id, tr)
		h.files.markDownloaded(id)
	}

	result, err := h.service.Sync(context.Background(), sub.ID)
	if err != nil {
		t.Fatalf("sync: %v", err)
	}

	if result.ReleasesSeen != 2 {
		t.Fatalf("expected two releases, got %d", result.ReleasesSeen)
	}
	if result.NewReleases != 1 {
		t.Fatalf("expected exactly one new release, got %d", result.NewReleases)
	}
	if result.NewTracks != 1 {
		t.Fatalf("expected exactly one new track, got %d", result.NewTracks)
	}
	if result.SkippedTracks != 2 {
		t.Fatalf("the known album should be skipped, got %d", result.SkippedTracks)
	}
}

// A track that the catalogue knows but that never produced a file is not new,
// and it is not "already there" either — it still has to be fetched.
func TestSyncSeparatesKnownTracksWithoutAFileFromDownloadedOnes(t *testing.T) {
	p := discoveryProvider()
	h := newHarness(t, p)
	sub := subscribe(t, h, false)

	h.catalog.addRelease("fake", "302127")
	h.catalog.addTrack("track-a", p.tracks["302127"][0])
	h.files.markDownloaded("track-a")
	// The second track is known but was never downloaded.
	h.catalog.addTrack("track-b", p.tracks["302127"][1])

	result, err := h.service.Sync(context.Background(), sub.ID)
	if err != nil {
		t.Fatalf("sync: %v", err)
	}

	if result.NewTracks != 0 {
		t.Fatalf("a known track must not count as new, got %d", result.NewTracks)
	}
	if result.SkippedTracks != 1 {
		t.Fatalf("only the downloaded track may be skipped, got %d", result.SkippedTracks)
	}
}

func TestSyncQueuesNothingWhenAutoDownloadIsOff(t *testing.T) {
	h := newHarness(t, discoveryProvider())
	sub := subscribe(t, h, false)

	result, err := h.service.Sync(context.Background(), sub.ID)
	if err != nil {
		t.Fatalf("sync: %v", err)
	}
	if result.QueuedTracks != 0 {
		t.Fatalf("auto download is off, nothing may be queued: %d", result.QueuedTracks)
	}
	if queued := h.downloader.queued(); len(queued) != 0 {
		t.Fatalf("the queue was used anyway: %v", queued)
	}
}

func TestSyncQueuesNewMaterialWhenAutoDownloadIsOn(t *testing.T) {
	h := newHarness(t, discoveryProvider())
	sub := subscribe(t, h, true)

	result, err := h.service.Sync(context.Background(), sub.ID)
	if err != nil {
		t.Fatalf("sync: %v", err)
	}
	if result.QueuedTracks != 2 {
		t.Fatalf("expected both tracks to be queued, got %d", result.QueuedTracks)
	}
	queued := h.downloader.queued()
	if len(queued) != 1 || queued[0] != "302127" {
		t.Fatalf("expected one release job for the album, got %v", queued)
	}
}

// Everything the library already has as a file must stay out of the queue.
func TestSyncQueuesNothingWhenEverythingIsDownloaded(t *testing.T) {
	p := discoveryProvider()
	h := newHarness(t, p)
	sub := subscribe(t, h, true)

	h.catalog.addRelease("fake", "302127")
	for i, tr := range p.tracks["302127"] {
		id := "track-" + string(rune('a'+i))
		h.catalog.addTrack(id, tr)
		h.files.markDownloaded(id)
	}

	result, err := h.service.Sync(context.Background(), sub.ID)
	if err != nil {
		t.Fatalf("sync: %v", err)
	}
	if result.QueuedTracks != 0 {
		t.Fatalf("nothing is missing, nothing may be queued: %d", result.QueuedTracks)
	}
	if queued := h.downloader.queued(); len(queued) != 0 {
		t.Fatalf("the queue was used anyway: %v", queued)
	}
}

// A queue that refuses the work must not lose the sync: the comparison
// succeeded, so the run is partial with a warning rather than failed.
func TestSyncSurvivesAQueueFailure(t *testing.T) {
	h := newHarness(t, discoveryProvider())
	h.downloader.err = apperr.New(apperr.CodeShuttingDown, "The service is shutting down.")
	sub := subscribe(t, h, true)

	result, err := h.service.Sync(context.Background(), sub.ID)
	if err != nil {
		t.Fatalf("a queue failure must not fail the sync: %v", err)
	}
	if result.Status != StatusPartial {
		t.Fatalf("expected a partial run, got %q", result.Status)
	}
	if len(result.Warnings) == 0 {
		t.Fatal("a queue failure must leave a warning")
	}
	if result.QueuedTracks != 0 {
		t.Fatalf("nothing reached the queue, so nothing may be counted: %d", result.QueuedTracks)
	}

	outcomes := h.store.outcomes()
	if len(outcomes) != 1 {
		t.Fatalf("expected one recorded outcome, got %d", len(outcomes))
	}
	if outcomes[0].Error != "" {
		t.Fatalf("a partial run is not a sync failure and must not set last_error: %q", outcomes[0].Error)
	}
}

// One unreadable release must not cost the whole artist — the existing
// discography policy, which the sync inherits.
func TestSyncKeepsGoingWhenOneReleaseCannotBeRead(t *testing.T) {
	p := discoveryProvider()
	p.releases = append(p.releases, music.Release{
		Title: "Broken", ReleaseType: music.ReleaseAlbum,
		Provider: "fake", SourceID: "broken",
	})
	p.trackErrs = map[string]error{
		"broken": apperr.New(apperr.CodeProviderUnavailable, "Deezer did not answer."),
	}
	h := newHarness(t, p)
	sub := subscribe(t, h, false)

	result, err := h.service.Sync(context.Background(), sub.ID)
	if err != nil {
		t.Fatalf("one broken release must not fail the sync: %v", err)
	}
	if result.Status != StatusPartial {
		t.Fatalf("expected a partial run, got %q", result.Status)
	}
	if len(result.Warnings) != 1 {
		t.Fatalf("expected one warning, got %v", result.Warnings)
	}
	if !strings.Contains(result.Warnings[0], "Broken") {
		t.Fatalf("the warning does not name the release: %q", result.Warnings[0])
	}
	if result.NewTracks != 2 {
		t.Fatalf("the readable album's tracks were lost: %d", result.NewTracks)
	}
}

func TestSyncFailsWhenTheProviderIsUnavailable(t *testing.T) {
	p := discoveryProvider()
	p.discoErr = apperr.New(apperr.CodeProviderUnavailable, "Deezer did not answer.")
	h := newHarness(t, p)
	sub := subscribe(t, h, false)

	_, err := h.service.Sync(context.Background(), sub.ID)
	if apperr.CodeOf(err) != apperr.CodeProviderUnavailable {
		t.Fatalf("expected PROVIDER_UNAVAILABLE, got %v", err)
	}

	outcomes := h.store.outcomes()
	if len(outcomes) != 1 {
		t.Fatalf("the failure was not recorded: %d outcomes", len(outcomes))
	}
	if outcomes[0].Status != StatusFailed {
		t.Fatalf("expected a failed status, got %q", outcomes[0].Status)
	}
	if outcomes[0].Error == "" {
		t.Fatal("a failed sync must record why")
	}
	// A transient failure retries later, not immediately.
	if !outcomes[0].NextAt.After(outcomes[0].At.Add(30 * time.Minute)) {
		t.Fatalf("the retry is scheduled too soon: %v", outcomes[0].NextAt.Sub(outcomes[0].At))
	}
}

func TestSyncFailsWhenTheProviderRateLimits(t *testing.T) {
	p := discoveryProvider()
	p.discoErr = apperr.New(apperr.CodeProviderRateLimited, "Too many requests.")
	h := newHarness(t, p)
	sub := subscribe(t, h, false)

	_, err := h.service.Sync(context.Background(), sub.ID)
	if apperr.CodeOf(err) != apperr.CodeProviderRateLimited {
		t.Fatalf("expected PROVIDER_RATE_LIMITED, got %v", err)
	}
	outcomes := h.store.outcomes()
	if len(outcomes) != 1 || outcomes[0].Status != StatusFailed {
		t.Fatalf("the rate limit was not recorded as a failure: %+v", outcomes)
	}
}

func TestSyncFailsWhenTheArtistIsUnknownToTheProvider(t *testing.T) {
	p := discoveryProvider()
	p.artistErr = apperr.New(apperr.CodeArtistNotFound, "No such artist.")
	h := newHarness(t, p)
	sub := subscribe(t, h, false)

	_, err := h.service.Sync(context.Background(), sub.ID)
	if apperr.CodeOf(err) != apperr.CodeArtistNotFound {
		t.Fatalf("expected ARTIST_NOT_FOUND, got %v", err)
	}
}

// A database that cannot answer must surface as a failure, not as a sync that
// silently reports every track as new.
func TestSyncFailsOnADatabaseError(t *testing.T) {
	h := newHarness(t, discoveryProvider())
	sub := subscribe(t, h, false)
	h.catalog.trackErr = apperr.New(apperr.CodeInternal, "The database operation failed.")

	_, err := h.service.Sync(context.Background(), sub.ID)
	if apperr.CodeOf(err) != apperr.CodeInternal {
		t.Fatalf("expected INTERNAL_ERROR, got %v", err)
	}
	outcomes := h.store.outcomes()
	if len(outcomes) != 1 || outcomes[0].Status != StatusFailed {
		t.Fatalf("the database error was not recorded as a failure: %+v", outcomes)
	}
}

func TestSyncOfAnUnknownSubscription(t *testing.T) {
	h := newHarness(t, discoveryProvider())
	_, err := h.service.Sync(context.Background(), "does-not-exist")
	if apperr.CodeOf(err) != apperr.CodeSubscriptionNotFound {
		t.Fatalf("expected SUBSCRIPTION_NOT_FOUND, got %v", err)
	}
}

func TestSuccessfulSyncSchedulesTheNextRunAfterTheInterval(t *testing.T) {
	h := newHarness(t, discoveryProvider())
	sub := subscribe(t, h, false)

	if _, err := h.service.Sync(context.Background(), sub.ID); err != nil {
		t.Fatalf("sync: %v", err)
	}
	outcomes := h.store.outcomes()
	if len(outcomes) != 1 {
		t.Fatalf("expected one outcome, got %d", len(outcomes))
	}
	gap := outcomes[0].NextAt.Sub(outcomes[0].At)
	if gap != 24*time.Hour {
		t.Fatalf("the next run should be one interval away, got %v", gap)
	}
}

/* ------------------------------------------------------------- concurrency */

// The same subscription must never be synced twice at once.
func TestConcurrentSyncOfTheSameSubscriptionIsRefused(t *testing.T) {
	p := discoveryProvider()
	release := make(chan struct{})
	blocked := make(chan struct{}, 1)
	p.trackErrs = nil

	// A provider that blocks inside the first sync, so the second one is
	// guaranteed to arrive while the first still holds the subscription.
	blocking := &blockingProvider{fakeProvider: p, enter: blocked, release: release}
	h := newHarness(t, blocking.fakeProvider)
	h.service.discography = mustDiscography(t, blocking)
	sub := subscribe(t, h, false)

	type outcome struct {
		result *SyncResult
		err    error
	}
	first := make(chan outcome, 1)
	go func() {
		result, err := h.service.Sync(context.Background(), sub.ID)
		first <- outcome{result, err}
	}()

	select {
	case <-blocked:
	case <-time.After(5 * time.Second):
		t.Fatal("the first sync never started")
	}

	if _, err := h.service.Sync(context.Background(), sub.ID); apperr.CodeOf(err) != apperr.CodeAlreadyExists {
		t.Fatalf("the second sync should have been refused, got %v", err)
	}

	close(release)
	got := <-first
	if got.err != nil {
		t.Fatalf("the first sync failed: %v", got.err)
	}

	// Once the first run is done the subscription can be synced again.
	if _, err := h.service.Sync(context.Background(), sub.ID); err != nil {
		t.Fatalf("the guard was not released: %v", err)
	}
}

// Two different artists must not block each other.
func TestConcurrentSyncOfDifferentSubscriptionsRunsInParallel(t *testing.T) {
	p := discoveryProvider()
	h := newHarness(t, p)

	ctx := context.Background()
	first, err := h.service.Create(ctx, NewSubscription{
		Provider: "fake", ArtistSourceID: "27", ArtistName: "Daft Punk",
	})
	if err != nil {
		t.Fatalf("subscribe: %v", err)
	}
	second, err := h.service.Create(ctx, NewSubscription{
		Provider: "fake", ArtistSourceID: "28", ArtistName: "Kevin MacLeod",
	})
	if err != nil {
		t.Fatalf("subscribe: %v", err)
	}

	var wg sync.WaitGroup
	errs := make([]error, 2)
	for i, id := range []string{first.ID, second.ID} {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, errs[i] = h.service.Sync(ctx, id)
		}()
	}
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Fatalf("sync %d failed: %v", i, err)
		}
	}
}

// blockingProvider stops inside GetDiscography until the test lets it go.
type blockingProvider struct {
	*fakeProvider
	enter   chan struct{}
	release chan struct{}
	once    sync.Once
}

func (b *blockingProvider) GetDiscography(ctx context.Context, id string) ([]music.Release, error) {
	b.once.Do(func() {
		b.enter <- struct{}{}
		<-b.release
	})
	return b.fakeProvider.GetDiscography(ctx, id)
}

func mustDiscography(t *testing.T, p provider.MetadataProvider) *discography.Service {
	t.Helper()
	registry := provider.NewRegistry()
	registry.RegisterMetadata(p)
	service, err := discography.NewService(discography.Options{
		Registry: registry,
		Logger:   slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	if err != nil {
		t.Fatalf("discography service: %v", err)
	}
	return service
}

/* ------------------------------------------------------------------- events */

func TestSyncPublishesLifecycleEvents(t *testing.T) {
	h := newHarness(t, discoveryProvider())
	sub := subscribe(t, h, false)

	events, unsubscribe := h.broker.Subscribe()
	defer unsubscribe()

	if _, err := h.service.Sync(context.Background(), sub.ID); err != nil {
		t.Fatalf("sync: %v", err)
	}

	seen := drain(events)
	if !seen[jobs.EventSubscriptionSyncStarted] {
		t.Fatal("no started event")
	}
	if !seen[jobs.EventSubscriptionSyncCompleted] {
		t.Fatal("no completed event")
	}
	if seen[jobs.EventSubscriptionSyncFailed] {
		t.Fatal("a successful sync published a failure")
	}
}

func TestFailedSyncPublishesAFailureEvent(t *testing.T) {
	p := discoveryProvider()
	p.discoErr = apperr.New(apperr.CodeProviderUnavailable, "Deezer did not answer.")
	h := newHarness(t, p)
	sub := subscribe(t, h, false)

	events, unsubscribe := h.broker.Subscribe()
	defer unsubscribe()

	if _, err := h.service.Sync(context.Background(), sub.ID); err == nil {
		t.Fatal("expected the sync to fail")
	}

	seen := drain(events)
	if !seen[jobs.EventSubscriptionSyncFailed] {
		t.Fatal("no failure event")
	}
}

func drain(events <-chan jobs.Event) map[string]bool {
	seen := make(map[string]bool)
	deadline := time.After(2 * time.Second)
	for {
		select {
		case event, open := <-events:
			if !open {
				return seen
			}
			seen[event.Type] = true
			if event.Type == jobs.EventSubscriptionSyncCompleted ||
				event.Type == jobs.EventSubscriptionSyncFailed {
				return seen
			}
		case <-deadline:
			return seen
		}
	}
}

/* ---------------------------------------------------------------- lifecycle */

func TestCreateRejectsAnIncompleteRequest(t *testing.T) {
	h := newHarness(t, discoveryProvider())
	_, err := h.service.Create(context.Background(), NewSubscription{Provider: "fake"})
	if apperr.CodeOf(err) != apperr.CodeInvalidRequest {
		t.Fatalf("expected INVALID_REQUEST, got %v", err)
	}
}

func TestCreateRejectsAnUnknownProvider(t *testing.T) {
	h := newHarness(t, discoveryProvider())
	_, err := h.service.Create(context.Background(), NewSubscription{
		Provider: "nope", ArtistSourceID: "27", ArtistName: "Daft Punk",
	})
	if apperr.CodeOf(err) != apperr.CodeProviderNotFound {
		t.Fatalf("expected PROVIDER_NOT_FOUND, got %v", err)
	}
}

func TestUpdateRejectsAnEmptyChange(t *testing.T) {
	h := newHarness(t, discoveryProvider())
	sub := subscribe(t, h, false)

	_, err := h.service.Update(context.Background(), sub.ID, Update{})
	if apperr.CodeOf(err) != apperr.CodeInvalidRequest {
		t.Fatalf("expected INVALID_REQUEST, got %v", err)
	}
}

func TestListReportsWhetherASyncIsRunning(t *testing.T) {
	h := newHarness(t, discoveryProvider())
	sub := subscribe(t, h, false)

	list, err := h.service.List(context.Background(), ListFilter{})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(list) != 1 || list[0].Syncing {
		t.Fatalf("nothing is running, so nothing may report as syncing: %+v", list)
	}

	h.service.active.Store(sub.ID, struct{}{})
	defer h.service.active.Delete(sub.ID)

	list, err = h.service.List(context.Background(), ListFilter{})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if !list[0].Syncing {
		t.Fatal("a running sync was not reported")
	}
}

func TestDeleteRemovesTheSubscription(t *testing.T) {
	h := newHarness(t, discoveryProvider())
	sub := subscribe(t, h, false)

	if err := h.service.Delete(context.Background(), sub.ID); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, err := h.service.Get(context.Background(), sub.ID); apperr.CodeOf(err) != apperr.CodeSubscriptionNotFound {
		t.Fatalf("the subscription is still there: %v", err)
	}
}

func TestErrorsAreApplicationErrors(t *testing.T) {
	h := newHarness(t, discoveryProvider())
	_, err := h.service.Get(context.Background(), "missing")

	var appErr *apperr.Error
	if !errors.As(err, &appErr) {
		t.Fatalf("the error is not an application error: %v", err)
	}
}

/* -------------------------------------------------------- background syncs */

func TestStartSyncRunsInTheBackground(t *testing.T) {
	h := newHarness(t, discoveryProvider())
	if err := h.service.Start(context.Background()); err != nil {
		t.Fatalf("start: %v", err)
	}
	defer h.service.Stop()
	sub := subscribe(t, h, false)

	events, unsubscribe := h.broker.Subscribe()
	defer unsubscribe()

	started, err := h.service.StartSync(context.Background(), sub.ID)
	if err != nil {
		t.Fatalf("start sync: %v", err)
	}
	if !started.Syncing {
		t.Fatal("the answer does not report the run as started")
	}

	if seen := drain(events); !seen[jobs.EventSubscriptionSyncCompleted] {
		t.Fatal("the background run never completed")
	}

	result := h.service.LastResult(sub.ID)
	if result == nil {
		t.Fatal("the background run left no report")
	}
	if result.NewTracks != 2 {
		t.Fatalf("the report is wrong: %+v", result)
	}
}

// The guard is taken before StartSync returns, so a second request is refused
// straight away rather than racing the goroutine.
func TestStartSyncRefusesASecondRunAtOnce(t *testing.T) {
	p := discoveryProvider()
	release := make(chan struct{})
	blocked := make(chan struct{}, 1)
	blocking := &blockingProvider{fakeProvider: p, enter: blocked, release: release}

	h := newHarness(t, p)
	h.service.discography = mustDiscography(t, blocking)
	if err := h.service.Start(context.Background()); err != nil {
		t.Fatalf("start: %v", err)
	}
	sub := subscribe(t, h, false)

	if _, err := h.service.StartSync(context.Background(), sub.ID); err != nil {
		t.Fatalf("start sync: %v", err)
	}
	select {
	case <-blocked:
	case <-time.After(5 * time.Second):
		t.Fatal("the background run never started")
	}

	_, err := h.service.StartSync(context.Background(), sub.ID)
	if apperr.CodeOf(err) != apperr.CodeAlreadyExists {
		t.Fatalf("the second run should have been refused, got %v", err)
	}

	close(release)
	h.service.Stop()
}

// After the shutdown began no run may be started, by request or by schedule.
func TestNoSyncIsStartedDuringShutdown(t *testing.T) {
	h := newHarness(t, discoveryProvider())
	if err := h.service.Start(context.Background()); err != nil {
		t.Fatalf("start: %v", err)
	}
	sub := subscribe(t, h, false)

	h.service.BeginShutdown()

	if _, err := h.service.StartSync(context.Background(), sub.ID); apperr.CodeOf(err) != apperr.CodeShuttingDown {
		t.Fatalf("expected SHUTTING_DOWN, got %v", err)
	}
	if _, err := h.service.Sync(context.Background(), sub.ID); apperr.CodeOf(err) != apperr.CodeShuttingDown {
		t.Fatalf("expected SHUTTING_DOWN, got %v", err)
	}

	h.service.Stop()
}

// Stop waits for the run in flight instead of leaving a goroutine behind, and
// the interrupted run still records why it ended.
func TestStopCancelsTheRunInFlightAndRecordsIt(t *testing.T) {
	p := discoveryProvider()
	release := make(chan struct{})
	blocked := make(chan struct{}, 1)
	blocking := &blockingProvider{fakeProvider: p, enter: blocked, release: release}

	h := newHarness(t, p)
	h.service.discography = mustDiscography(t, blocking)
	if err := h.service.Start(context.Background()); err != nil {
		t.Fatalf("start: %v", err)
	}
	sub := subscribe(t, h, false)

	if _, err := h.service.StartSync(context.Background(), sub.ID); err != nil {
		t.Fatalf("start sync: %v", err)
	}
	select {
	case <-blocked:
	case <-time.After(5 * time.Second):
		t.Fatal("the background run never started")
	}

	stopped := make(chan struct{})
	go func() {
		h.service.Stop()
		close(stopped)
	}()

	// The run is blocked in the provider; letting it go lets Stop finish.
	close(release)
	select {
	case <-stopped:
	case <-time.After(5 * time.Second):
		t.Fatal("Stop did not wait for the run to finish")
	}

	if len(h.store.outcomes()) == 0 {
		t.Fatal("the interrupted run recorded nothing")
	}
}

func TestStopIsIdempotent(t *testing.T) {
	h := newHarness(t, discoveryProvider())
	if err := h.service.Start(context.Background()); err != nil {
		t.Fatalf("start: %v", err)
	}
	h.service.Stop()
	h.service.Stop()
}

func TestDeleteDropsTheCachedReport(t *testing.T) {
	h := newHarness(t, discoveryProvider())
	sub := subscribe(t, h, false)

	if _, err := h.service.Sync(context.Background(), sub.ID); err != nil {
		t.Fatalf("sync: %v", err)
	}
	if h.service.LastResult(sub.ID) == nil {
		t.Fatal("the run left no report")
	}
	if err := h.service.Delete(context.Background(), sub.ID); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if h.service.LastResult(sub.ID) != nil {
		t.Fatal("the report outlived the subscription")
	}
}

/* --------------------------------------------------- partial scheduling */

// A partial run caused by a rate limit or an outage lost part of the catalogue,
// so it must come back early rather than wait out the full interval.
func TestPartialRunFromATransientFailureRetriesEarly(t *testing.T) {
	p := discoveryProvider()
	p.releases = append(p.releases, music.Release{
		Title: "Rate limited", ReleaseType: music.ReleaseAlbum,
		Provider: "fake", SourceID: "limited",
	})
	p.trackErrs = map[string]error{
		"limited": apperr.New(apperr.CodeProviderRateLimited, "Deezer rate limit exceeded"),
	}
	h := newHarness(t, p)
	sub := subscribe(t, h, false)

	result, err := h.service.Sync(context.Background(), sub.ID)
	if err != nil {
		t.Fatalf("sync: %v", err)
	}
	if result.Status != StatusPartial {
		t.Fatalf("expected a partial run, got %q", result.Status)
	}

	outcomes := h.store.outcomes()
	if len(outcomes) != 1 {
		t.Fatalf("expected one outcome, got %d", len(outcomes))
	}
	gap := outcomes[0].NextAt.Sub(outcomes[0].At)
	if gap != time.Hour {
		t.Fatalf("a transient partial should come back after the retry interval, got %v", gap)
	}
}

// A release that is permanently gone will still be gone in an hour, so that
// partial keeps the normal interval.
func TestPartialRunFromAPermanentFailureKeepsTheNormalInterval(t *testing.T) {
	p := discoveryProvider()
	p.releases = append(p.releases, music.Release{
		Title: "Gone", ReleaseType: music.ReleaseAlbum,
		Provider: "fake", SourceID: "gone",
	})
	p.trackErrs = map[string]error{
		"gone": apperr.New(apperr.CodeReleaseNotFound, "Deezer does not know this item."),
	}
	h := newHarness(t, p)
	sub := subscribe(t, h, false)

	result, err := h.service.Sync(context.Background(), sub.ID)
	if err != nil {
		t.Fatalf("sync: %v", err)
	}
	if result.Status != StatusPartial {
		t.Fatalf("expected a partial run, got %q", result.Status)
	}

	outcomes := h.store.outcomes()
	gap := outcomes[0].NextAt.Sub(outcomes[0].At)
	if gap != 24*time.Hour {
		t.Fatalf("a permanent partial should keep the normal interval, got %v", gap)
	}
}

// A queue failure is transient too: the catalogue was read in full, but the
// work it produced did not reach the queue.
func TestPartialRunFromAQueueFailureRetriesEarly(t *testing.T) {
	h := newHarness(t, discoveryProvider())
	h.downloader.err = apperr.New(apperr.CodeShuttingDown, "The service is shutting down.")
	sub := subscribe(t, h, true)

	if _, err := h.service.Sync(context.Background(), sub.ID); err != nil {
		t.Fatalf("sync: %v", err)
	}

	outcomes := h.store.outcomes()
	gap := outcomes[0].NextAt.Sub(outcomes[0].At)
	if gap != time.Hour {
		t.Fatalf("a failed hand-off to the queue should retry early, got %v", gap)
	}
}

// The scheduler has to act on that: a transient partial becomes due again
// after the retry interval, not after a day.
func TestSchedulerPicksUpATransientPartialAfterTheRetryInterval(t *testing.T) {
	p := discoveryProvider()
	p.releases = append(p.releases, music.Release{
		Title: "Rate limited", ReleaseType: music.ReleaseAlbum,
		Provider: "fake", SourceID: "limited",
	})
	p.trackErrs = map[string]error{
		"limited": apperr.New(apperr.CodeProviderRateLimited, "Deezer rate limit exceeded"),
	}
	h := newHarness(t, p)
	sub := subscribe(t, h, false)

	if _, err := h.service.Sync(context.Background(), sub.ID); err != nil {
		t.Fatalf("sync: %v", err)
	}

	loaded, err := h.service.Get(context.Background(), sub.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if loaded.LastSyncStatus != StatusPartial {
		t.Fatalf("expected partial, got %q", loaded.LastSyncStatus)
	}

	// Not due yet...
	due, err := h.service.DueForSync(context.Background(), time.Now().UTC().Add(30*time.Minute), 10)
	if err != nil {
		t.Fatalf("due: %v", err)
	}
	if len(due) != 0 {
		t.Fatalf("the partial became due too early: %+v", due)
	}

	// ...but well before a day has passed.
	due, err = h.service.DueForSync(context.Background(), time.Now().UTC().Add(90*time.Minute), 10)
	if err != nil {
		t.Fatalf("due: %v", err)
	}
	if len(due) != 1 {
		t.Fatalf("the partial did not become due after the retry interval: %+v", due)
	}
}

/* ------------------------------------------------ partial retry dedup */

// A retry after a partial run must not queue work that the first run already
// handed over. The queue reports the release as already covered, and the sync
// neither creates a second job nor loses track of the recordings.
func TestRetryAfterAPartialRunDoesNotQueueTheSameReleaseTwice(t *testing.T) {
	h := newHarness(t, discoveryProvider())
	sub := subscribe(t, h, true)

	first, err := h.service.Sync(context.Background(), sub.ID)
	if err != nil {
		t.Fatalf("first sync: %v", err)
	}
	if first.QueuedTracks != 2 {
		t.Fatalf("the first run should have queued both tracks, got %d", first.QueuedTracks)
	}
	if queued := h.downloader.queued(); len(queued) != 1 {
		t.Fatalf("expected one release job, got %v", queued)
	}

	// The jobs from the first run have not finished, so nothing was
	// downloaded and the catalogue still reports the tracks as missing.
	second, err := h.service.Sync(context.Background(), sub.ID)
	if err != nil {
		t.Fatalf("second sync: %v", err)
	}

	if queued := h.downloader.queued(); len(queued) != 1 {
		t.Fatalf("the retry created a second job for the same release: %v", queued)
	}
	// The recordings are still on their way, so the report still accounts for
	// them rather than pretending nothing is happening.
	if second.QueuedTracks != 2 {
		t.Fatalf("the retry lost track of the queued recordings: %d", second.QueuedTracks)
	}
	if second.Status != StatusSuccess {
		t.Fatalf("an already covered release is not a warning, got %q", second.Status)
	}
}

// Once the first job has finished and the files exist, the recordings are
// skipped outright rather than queued again.
func TestRetryAfterADownloadedRunSkipsInsteadOfQueueing(t *testing.T) {
	p := discoveryProvider()
	h := newHarness(t, p)
	sub := subscribe(t, h, true)

	if _, err := h.service.Sync(context.Background(), sub.ID); err != nil {
		t.Fatalf("first sync: %v", err)
	}

	// The job ran: the recordings are in the catalogue and have files.
	h.catalog.addRelease("fake", "302127")
	for i, tr := range p.tracks["302127"] {
		id := "track-" + string(rune('a'+i))
		h.catalog.addTrack(id, tr)
		h.files.markDownloaded(id)
	}
	h.downloader.finish("302127")

	second, err := h.service.Sync(context.Background(), sub.ID)
	if err != nil {
		t.Fatalf("second sync: %v", err)
	}
	if second.QueuedTracks != 0 {
		t.Fatalf("downloaded recordings were queued again: %d", second.QueuedTracks)
	}
	if second.SkippedTracks != 2 {
		t.Fatalf("expected both recordings to be skipped, got %d", second.SkippedTracks)
	}
	if queued := h.downloader.queued(); len(queued) != 1 {
		t.Fatalf("a second job was created: %v", queued)
	}
}

// A retry must never be scheduled further out than a clean run. Nothing stops
// a deployment from shortening the sync interval below the retry interval, and
// scheduling a degraded run later than a complete one inverts the whole point
// of coming back early.
func TestTransientPartialNeverWaitsLongerThanASuccess(t *testing.T) {
	p := discoveryProvider()
	p.releases = append(p.releases, music.Release{
		Title: "Rate limited", ReleaseType: music.ReleaseAlbum,
		Provider: "fake", SourceID: "limited",
	})
	p.trackErrs = map[string]error{
		"limited": apperr.New(apperr.CodeProviderRateLimited, "Deezer rate limit exceeded"),
	}

	h := newHarness(t, p)
	// A deployment that checks every half hour, with the default hourly retry.
	h.service.syncInterval = 30 * time.Minute
	h.service.retryInterval = time.Hour

	sub := subscribe(t, h, false)
	if _, err := h.service.Sync(context.Background(), sub.ID); err != nil {
		t.Fatalf("sync: %v", err)
	}

	outcomes := h.store.outcomes()
	gap := outcomes[0].NextAt.Sub(outcomes[0].At)
	if gap > 30*time.Minute {
		t.Fatalf("a degraded run was scheduled later than a clean one: %v > 30m", gap)
	}
}

func TestExportAndImportService(t *testing.T) {
	h := newHarness(t, discoveryProvider())
	sub := subscribe(t, h, true)

	// 1. Export
	export, err := h.service.Export(context.Background())
	if err != nil {
		t.Fatalf("export: %v", err)
	}
	if export.Format != ExportFormatName || export.Version != ExportFormatVersion {
		t.Fatalf("unexpected format/version: %s v%d", export.Format, export.Version)
	}
	if len(export.Subscriptions) != 1 {
		t.Fatalf("expected 1 exported sub, got %d", len(export.Subscriptions))
	}
	if export.Subscriptions[0].ArtistName != "Daft Punk" || export.Subscriptions[0].Provider != "fake" {
		t.Fatalf("exported item content wrong: %+v", export.Subscriptions[0])
	}

	// 2. Preview identical import (should be unchanged)
	preview, err := h.service.PreviewImport(context.Background(), *export)
	if err != nil {
		t.Fatalf("preview: %v", err)
	}
	if preview.Total != 1 || preview.Unchanged != 1 || preview.New != 0 || preview.WouldUpdate != 0 {
		t.Fatalf("expected 1 unchanged, got %+v", preview)
	}

	// 3. Add new item and modified existing item
	export.Subscriptions = append(export.Subscriptions, ExportSubscription{
		ArtistName:       "Justice",
		Provider:         "fake",
		ArtistSourceID:   "justice-1",
		Enabled:          true,
		AutoDownload:     true,
		ReleaseFilter:    music.DefaultReleaseFilter(),
		DownloadPriority: jobs.PriorityHigh,
	})
	export.Subscriptions[0].AutoDownload = false // modify existing Daft Punk

	preview2, err := h.service.PreviewImport(context.Background(), *export)
	if err != nil {
		t.Fatalf("preview2: %v", err)
	}
	if preview2.Total != 2 || preview2.New != 1 || preview2.WouldUpdate != 1 || preview2.Unchanged != 0 {
		t.Fatalf("preview2 stats wrong: %+v", preview2)
	}

	// 4. Apply Import - MUST NOT CREATE JOBS
	result, err := h.service.ApplyImport(context.Background(), *export)
	if err != nil {
		t.Fatalf("apply: %v", err)
	}
	if result.Created != 1 || result.Updated != 1 || result.Failed != 0 {
		t.Fatalf("apply result wrong: %+v", result)
	}

	// Crucial check: 0 jobs queued during import
	if queued := h.downloader.queued(); len(queued) != 0 {
		t.Fatalf("import must NOT create any download jobs, but found: %v", queued)
	}

	// 5. Verify updated state in store
	updatedSub, err := h.service.Get(context.Background(), sub.ID)
	if err != nil {
		t.Fatalf("get updated sub: %v", err)
	}
	if updatedSub.AutoDownload != false {
		t.Fatal("existing sub auto_download was not updated to false")
	}
}

func TestImportOversizedLimit(t *testing.T) {
	h := newHarness(t, discoveryProvider())
	subs := make([]ExportSubscription, MaxImportItems+1)
	for i := range subs {
		subs[i] = ExportSubscription{
			ArtistName:     "Artist",
			Provider:       "fake",
			ArtistSourceID: "id",
		}
	}
	payload := ExportPayload{
		Format:        ExportFormatName,
		Version:       ExportFormatVersion,
		Subscriptions: subs,
	}

	_, err := h.service.PreviewImport(context.Background(), payload)
	if apperr.CodeOf(err) != apperr.CodeInvalidRequest {
		t.Fatalf("expected INVALID_REQUEST on oversized import, got %v", err)
	}
}
