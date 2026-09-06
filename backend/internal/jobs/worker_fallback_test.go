package jobs

import (
	"context"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"ytdm/backend/internal/apperr"
	"ytdm/backend/internal/downloader"
	"ytdm/backend/internal/matcher"
	"ytdm/backend/internal/metadata"
	"ytdm/backend/internal/music"
	"ytdm/backend/internal/provider"
	"ytdm/backend/internal/storage"
)

type mockFallbackMediaProvider struct {
	name           string
	candidates     []provider.MediaCandidate
	resolveResults map[string]*provider.MediaSource
	resolveErrors  map[string]error
	resolvedOrder  []string
	resolveCalls   map[string]int
	mu             sync.Mutex
}

func newMockFallbackMediaProvider(name string, candidates []provider.MediaCandidate) *mockFallbackMediaProvider {
	return &mockFallbackMediaProvider{
		name:           name,
		candidates:     candidates,
		resolveResults: make(map[string]*provider.MediaSource),
		resolveErrors:  make(map[string]error),
		resolveCalls:   make(map[string]int),
	}
}

func (p *mockFallbackMediaProvider) Name() string { return p.name }

func (p *mockFallbackMediaProvider) Search(_ context.Context, _ music.Track) ([]provider.MediaCandidate, error) {
	return p.candidates, nil
}

func (p *mockFallbackMediaProvider) Resolve(_ context.Context, c provider.MediaCandidate) (*provider.MediaSource, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.resolvedOrder = append(p.resolvedOrder, c.ID)
	p.resolveCalls[c.ID]++

	if err, exists := p.resolveErrors[c.ID]; exists {
		return nil, err
	}
	if src, exists := p.resolveResults[c.ID]; exists {
		return src, nil
	}
	return &provider.MediaSource{
		Provider:   p.name,
		ID:         c.ID,
		URL:        c.URL,
		DurationMS: c.DurationMS,
		Formats: []provider.AudioFormat{
			{ID: "251", Codec: "opus", BitrateKbps: 160},
		},
	}, nil
}

type mockFallbackDownloader struct{}

func (d *mockFallbackDownloader) Download(_ context.Context, _ provider.MediaSource, destination string, _ downloader.ProgressCallback) (*downloader.Result, error) {
	_ = os.MkdirAll(filepath.Dir(destination), 0755)
	_ = os.WriteFile(destination, []byte("fake-audio-data"), 0644)
	return &downloader.Result{
		Path:        destination,
		SourceCodec: "opus",
		NativeOpus:  true,
	}, nil
}

type mockFallbackTagger struct{}

func (t *mockFallbackTagger) Apply(_ context.Context, _ string, _ metadata.Tags, _ *metadata.Artwork) error {
	return nil
}

type mockFallbackCatalog struct {
	fakeCatalog
}

func (c *mockFallbackCatalog) PersistDownload(_ context.Context, entry music.LibraryEntry, _ int) (music.StoredEntry, error) {
	return music.StoredEntry{
		ArtistID:  "artist-1",
		ReleaseID: "rel-1",
		TrackID:   "tr-1",
	}, nil
}

func setupTestFallbackEnvironment(t *testing.T, mediaProv provider.MediaProvider) (*Manager, *fakeStore) {
	t.Helper()
	root := t.TempDir()
	library, err := storage.NewLibrary(root)
	if err != nil {
		t.Fatalf("NewLibrary: %v", err)
	}

	stagingDir := t.TempDir()
	stagingMgr, err := storage.NewStagingManager(stagingDir, 0, 0)
	if err != nil {
		t.Fatalf("NewStagingManager: %v", err)
	}

	store := &fakeStore{
		items: map[string]Item{
			"item-1": {
				ID:          "item-1",
				JobID:       "job-1",
				Status:      ItemPending,
				Attempts:    0,
				MaxAttempts: 5,
				Track: music.Track{
					Title:      "The Visitors",
					Artists:    []string{"ABBA"},
					Album:      "The Visitors",
					DurationMS: 349000,
				},
				Label: "ABBA - The Visitors",
			},
		},
	}

	reg := provider.NewRegistry()
	reg.RegisterMedia(mediaProv)

	engine := matcher.New(matcher.Options{
		MinScore:            70.0,
		DurationToleranceMS: 4000,
	})

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	broker := NewBroker(logger)

	mgr := &Manager{
		store:        store,
		library:      library,
		staging:      stagingMgr,
		broker:       broker,
		cooldown:     NewMediaCooldownManager(),
		logger:       logger,
		registry:     reg,
		matcher:      engine,
		downloader:   &mockFallbackDownloader{},
		tagger:       &mockFallbackTagger{},
		catalog:      &mockFallbackCatalog{},
		finalizerSem: make(chan struct{}, 1),
	}

	return mgr, store
}

// TEST 1: Candidate #1 resolve fail, Candidate #2 resolve success -> Candidate #2 selected, item completes.
func TestWorker_Fallback_Candidate1Fails_Candidate2Succeeds(t *testing.T) {
	candidates := []provider.MediaCandidate{
		{ID: "cand-1", Title: "The Visitors", Artists: []string{"ABBA"}, DurationMS: 349000, Provider: "youtube"},
		{ID: "cand-2", Title: "The Visitors", Artists: []string{"ABBA"}, DurationMS: 348000, Provider: "youtube"},
	}
	prov := newMockFallbackMediaProvider("youtube", candidates)
	prov.resolveErrors["cand-1"] = apperr.New(apperr.CodeTrackNotFound, "Video unavailable")

	mgr, store := setupTestFallbackEnvironment(t, prov)
	w := &worker{manager: mgr}
	job := Job{ID: "job-1", MediaProvider: "youtube"}
	item := store.items["item-1"]

	w.process(context.Background(), job, item)

	updated := store.items["item-1"]
	if updated.Status != ItemCompleted {
		t.Fatalf("expected item status Completed, got %v (error: %v)", updated.Status, updated.ErrorMessage)
	}
	if updated.MediaID != "cand-2" {
		t.Fatalf("expected MediaID = cand-2, got %q", updated.MediaID)
	}

	prov.mu.Lock()
	defer prov.mu.Unlock()
	if len(prov.resolvedOrder) != 2 || prov.resolvedOrder[0] != "cand-1" || prov.resolvedOrder[1] != "cand-2" {
		t.Fatalf("expected resolution order [cand-1, cand-2], got %v", prov.resolvedOrder)
	}
}

// TEST 2: Candidate #1 & #2 fail, Candidate #3 succeeds -> Rank 3 selected.
func TestWorker_Fallback_Candidate1And2Fail_Candidate3Succeeds(t *testing.T) {
	candidates := []provider.MediaCandidate{
		{ID: "cand-1", Title: "The Visitors", Artists: []string{"ABBA"}, DurationMS: 349000, Provider: "youtube"},
		{ID: "cand-2", Title: "The Visitors", Artists: []string{"ABBA"}, DurationMS: 348000, Provider: "youtube"},
		{ID: "cand-3", Title: "The Visitors", Artists: []string{"ABBA"}, DurationMS: 347000, Provider: "youtube"},
	}
	prov := newMockFallbackMediaProvider("youtube", candidates)
	prov.resolveErrors["cand-1"] = apperr.New(apperr.CodeTrackNotFound, "Video unavailable")
	prov.resolveErrors["cand-2"] = apperr.New(apperr.CodeDownloadFailed, "The media item offers no audio only stream.")

	mgr, store := setupTestFallbackEnvironment(t, prov)
	w := &worker{manager: mgr}
	job := Job{ID: "job-1", MediaProvider: "youtube"}
	item := store.items["item-1"]

	w.process(context.Background(), job, item)

	updated := store.items["item-1"]
	if updated.Status != ItemCompleted {
		t.Fatalf("expected item status Completed, got %v (error: %v)", updated.Status, updated.ErrorMessage)
	}
	if updated.MediaID != "cand-3" {
		t.Fatalf("expected MediaID = cand-3, got %q", updated.MediaID)
	}

	prov.mu.Lock()
	defer prov.mu.Unlock()
	if len(prov.resolvedOrder) != 3 || prov.resolvedOrder[2] != "cand-3" {
		t.Fatalf("expected resolution order to end in cand-3, got %v", prov.resolvedOrder)
	}
}

// TEST 3: All candidates permanent fail -> ItemFailed only after all eligible exhausted, German summary.
func TestWorker_Fallback_AllCandidatesPermanentFail(t *testing.T) {
	candidates := []provider.MediaCandidate{
		{ID: "c1", Title: "The Visitors", Artists: []string{"ABBA"}, DurationMS: 349000, Provider: "youtube"},
		{ID: "c2", Title: "The Visitors", Artists: []string{"ABBA"}, DurationMS: 349000, Provider: "youtube"},
		{ID: "c3", Title: "The Visitors", Artists: []string{"ABBA"}, DurationMS: 349000, Provider: "youtube"},
		{ID: "c4", Title: "The Visitors", Artists: []string{"ABBA"}, DurationMS: 349000, Provider: "youtube"},
		{ID: "c5", Title: "The Visitors", Artists: []string{"ABBA"}, DurationMS: 349000, Provider: "youtube"},
	}
	prov := newMockFallbackMediaProvider("youtube", candidates)
	for _, c := range candidates {
		prov.resolveErrors[c.ID] = apperr.New(apperr.CodeTrackNotFound, "Video unavailable")
	}

	mgr, store := setupTestFallbackEnvironment(t, prov)
	w := &worker{manager: mgr}
	job := Job{ID: "job-1", MediaProvider: "youtube"}
	item := store.items["item-1"]

	w.process(context.Background(), job, item)

	updated := store.items["item-1"]
	if updated.Status != ItemFailed {
		t.Fatalf("expected item status Failed, got %v", updated.Status)
	}
	expectedMsg := "Keine der 5 passenden Quellen konnte aufgelöst werden."
	if !strings.Contains(updated.ErrorMessage, expectedMsg) {
		t.Fatalf("expected error message to contain %q, got %q", expectedMsg, updated.ErrorMessage)
	}

	prov.mu.Lock()
	defer prov.mu.Unlock()
	if len(prov.resolvedOrder) != 5 {
		t.Fatalf("expected all 5 candidates to be attempted, got %d", len(prov.resolvedOrder))
	}
}

// TEST 4: Duplicate media IDs in candidate list -> each unique source resolved at most once.
func TestWorker_Fallback_DuplicateCandidateIDsEliminated(t *testing.T) {
	candidates := []provider.MediaCandidate{
		{ID: "dup-1", Title: "The Visitors", Artists: []string{"ABBA"}, DurationMS: 349000, Provider: "youtube"},
		{ID: "dup-1", Title: "The Visitors", Artists: []string{"ABBA"}, DurationMS: 348000, Provider: "youtube"}, // duplicate
		{ID: "dup-2", Title: "The Visitors", Artists: []string{"ABBA"}, DurationMS: 347000, Provider: "youtube"},
	}
	prov := newMockFallbackMediaProvider("youtube", candidates)
	prov.resolveErrors["dup-1"] = apperr.New(apperr.CodeTrackNotFound, "Video unavailable")

	mgr, store := setupTestFallbackEnvironment(t, prov)
	w := &worker{manager: mgr}
	job := Job{ID: "job-1", MediaProvider: "youtube"}
	item := store.items["item-1"]

	w.process(context.Background(), job, item)

	prov.mu.Lock()
	defer prov.mu.Unlock()
	if prov.resolveCalls["dup-1"] != 1 {
		t.Fatalf("expected dup-1 to be resolved exactly 1 time, got %d", prov.resolveCalls["dup-1"])
	}
	if prov.resolveCalls["dup-2"] != 1 {
		t.Fatalf("expected dup-2 to be resolved exactly 1 time, got %d", prov.resolveCalls["dup-2"])
	}
}

// TEST 5: Fallback depth limit reached -> exactly bounded to 5 candidates, no unbounded loop.
func TestWorker_Fallback_DepthBounded(t *testing.T) {
	var candidates []provider.MediaCandidate
	for i := 1; i <= 10; i++ {
		id := strings.Repeat("x", i)
		candidates = append(candidates, provider.MediaCandidate{
			ID:         id,
			Title:      "The Visitors",
			Artists:    []string{"ABBA"},
			DurationMS: 349000,
			Provider:   "youtube",
		})
	}
	prov := newMockFallbackMediaProvider("youtube", candidates)
	for _, c := range candidates {
		prov.resolveErrors[c.ID] = apperr.New(apperr.CodeTrackNotFound, "Video unavailable")
	}

	mgr, store := setupTestFallbackEnvironment(t, prov)
	w := &worker{manager: mgr}
	job := Job{ID: "job-1", MediaProvider: "youtube"}
	item := store.items["item-1"]

	w.process(context.Background(), job, item)

	prov.mu.Lock()
	defer prov.mu.Unlock()
	if len(prov.resolvedOrder) != 5 {
		t.Fatalf("expected exactly 5 candidates resolved (bounded), got %d", len(prov.resolvedOrder))
	}
}

// Transient error: Rate limit -> retry_wait, systemic stop prevents hammering remaining candidates.
func TestWorker_SystemicStop_RateLimit_EntersRetryWaitWithoutFanOut(t *testing.T) {
	candidates := []provider.MediaCandidate{
		{ID: "cand-1", Title: "The Visitors", Artists: []string{"ABBA"}, DurationMS: 349000, Provider: "youtube"},
		{ID: "cand-2", Title: "The Visitors", Artists: []string{"ABBA"}, DurationMS: 348000, Provider: "youtube"},
		{ID: "cand-3", Title: "The Visitors", Artists: []string{"ABBA"}, DurationMS: 347000, Provider: "youtube"},
	}
	prov := newMockFallbackMediaProvider("youtube", candidates)
	prov.resolveErrors["cand-1"] = apperr.New(apperr.CodeProviderRateLimited, "The media provider rate limited the request: try again later")

	mgr, store := setupTestFallbackEnvironment(t, prov)
	w := &worker{manager: mgr}
	job := Job{ID: "job-1", MediaProvider: "youtube"}
	item := store.items["item-1"]

	w.process(context.Background(), job, item)

	updated := store.items["item-1"]
	if updated.Status != ItemRetryWait {
		t.Fatalf("expected item status RetryWait, got %v", updated.Status)
	}
	if updated.ErrorCode != string(apperr.CodeProviderRateLimited) {
		t.Fatalf("expected error code %v, got %v", apperr.CodeProviderRateLimited, updated.ErrorCode)
	}

	prov.mu.Lock()
	defer prov.mu.Unlock()
	// Systemic stop MUST prevent cand-2 and cand-3 from being resolved
	if len(prov.resolvedOrder) != 1 || prov.resolvedOrder[0] != "cand-1" {
		t.Fatalf("expected only cand-1 to be called before systemic stop, got %v", prov.resolvedOrder)
	}
}

// Bot/Auth challenge -> enters retry_wait (not permanent TrackNotFound), systemic stop prevents fan-out.
func TestWorker_SystemicStop_BotAuthChallenge_EntersRetryWait(t *testing.T) {
	candidates := []provider.MediaCandidate{
		{ID: "cand-1", Title: "The Visitors", Artists: []string{"ABBA"}, DurationMS: 349000, Provider: "youtube"},
		{ID: "cand-2", Title: "The Visitors", Artists: []string{"ABBA"}, DurationMS: 348000, Provider: "youtube"},
	}
	prov := newMockFallbackMediaProvider("youtube", candidates)
	prov.resolveErrors["cand-1"] = apperr.New(apperr.CodeProviderUnavailable, "The media provider requires authentication or bot verification: not a bot")

	mgr, store := setupTestFallbackEnvironment(t, prov)
	w := &worker{manager: mgr}
	job := Job{ID: "job-1", MediaProvider: "youtube"}
	item := store.items["item-1"]

	w.process(context.Background(), job, item)

	updated := store.items["item-1"]
	if updated.Status != ItemRetryWait {
		t.Fatalf("expected item status RetryWait, got %v", updated.Status)
	}
	if updated.ErrorCode == string(apperr.CodeTrackNotFound) {
		t.Fatalf("auth/bot challenge MUST NOT be classified as TrackNotFound")
	}

	prov.mu.Lock()
	defer prov.mu.Unlock()
	if len(prov.resolvedOrder) != 1 {
		t.Fatalf("expected only 1 call before systemic stop, got %d", len(prov.resolvedOrder))
	}
}

// Case D: Candidate 1 unavailable, Candidate 2 rate-limited -> enters retry_wait, not permanent failed.
func TestWorker_Fallback_Candidate1Unavailable_Candidate2RateLimited(t *testing.T) {
	candidates := []provider.MediaCandidate{
		{ID: "cand-1", Title: "The Visitors", Artists: []string{"ABBA"}, DurationMS: 349000, Provider: "youtube"},
		{ID: "cand-2", Title: "The Visitors", Artists: []string{"ABBA"}, DurationMS: 348000, Provider: "youtube"},
		{ID: "cand-3", Title: "The Visitors", Artists: []string{"ABBA"}, DurationMS: 347000, Provider: "youtube"},
	}
	prov := newMockFallbackMediaProvider("youtube", candidates)
	prov.resolveErrors["cand-1"] = apperr.New(apperr.CodeTrackNotFound, "Video unavailable")
	prov.resolveErrors["cand-2"] = apperr.New(apperr.CodeProviderRateLimited, "rate limited: try again later")

	mgr, store := setupTestFallbackEnvironment(t, prov)
	w := &worker{manager: mgr}
	job := Job{ID: "job-1", MediaProvider: "youtube"}
	item := store.items["item-1"]

	w.process(context.Background(), job, item)

	updated := store.items["item-1"]
	if updated.Status != ItemRetryWait {
		t.Fatalf("expected item status RetryWait due to cand-2 rate limiting, got %v", updated.Status)
	}
	if updated.ErrorCode != string(apperr.CodeProviderRateLimited) {
		t.Fatalf("expected error code %v, got %v", apperr.CodeProviderRateLimited, updated.ErrorCode)
	}

	prov.mu.Lock()
	defer prov.mu.Unlock()
	// Cand 1 failed with unavailable -> fallback called cand-2 -> cand-2 was rate-limited -> systemic stop halted cand-3
	if len(prov.resolvedOrder) != 2 || prov.resolvedOrder[0] != "cand-1" || prov.resolvedOrder[1] != "cand-2" {
		t.Fatalf("expected [cand-1, cand-2], got %v", prov.resolvedOrder)
	}
}

// Matching rejects low-quality candidate during fallback (Section 20).
func TestWorker_Fallback_RejectsLowQualityCandidates(t *testing.T) {
	candidates := []provider.MediaCandidate{
		{ID: "cand-1", Title: "The Visitors", Artists: []string{"ABBA"}, DurationMS: 349000, Provider: "youtube"},
		{ID: "cand-bad", Title: "Completely Different Song", Artists: []string{"Other"}, DurationMS: 100000, Provider: "youtube"},
	}
	prov := newMockFallbackMediaProvider("youtube", candidates)
	prov.resolveErrors["cand-1"] = apperr.New(apperr.CodeTrackNotFound, "Video unavailable")

	mgr, store := setupTestFallbackEnvironment(t, prov)
	w := &worker{manager: mgr}
	job := Job{ID: "job-1", MediaProvider: "youtube"}
	item := store.items["item-1"]

	w.process(context.Background(), job, item)

	prov.mu.Lock()
	defer prov.mu.Unlock()
	// cand-bad should NOT even be attempted for resolution because matcher rejected it!
	if prov.resolveCalls["cand-bad"] != 0 {
		t.Fatalf("expected cand-bad not to be resolved (rejected by matcher), but was called %d times", prov.resolveCalls["cand-bad"])
	}

	updated := store.items["item-1"]
	if updated.Status != ItemFailed {
		t.Fatalf("expected item status Failed, got %v", updated.Status)
	}
}

// Section 18: Network timeout -> retry policy (RetryWait), systemic stop prevents fan-out.
func TestWorker_Fallback_NetworkTimeout_EntersRetryWait(t *testing.T) {
	candidates := []provider.MediaCandidate{
		{ID: "cand-1", Title: "The Visitors", Artists: []string{"ABBA"}, DurationMS: 349000, Provider: "youtube"},
		{ID: "cand-2", Title: "The Visitors", Artists: []string{"ABBA"}, DurationMS: 348000, Provider: "youtube"},
	}
	prov := newMockFallbackMediaProvider("youtube", candidates)
	prov.resolveErrors["cand-1"] = apperr.New(apperr.CodeProviderUnavailable, "connection timed out")

	mgr, store := setupTestFallbackEnvironment(t, prov)
	w := &worker{manager: mgr}
	job := Job{ID: "job-1", MediaProvider: "youtube"}
	item := store.items["item-1"]

	w.process(context.Background(), job, item)

	updated := store.items["item-1"]
	if updated.Status != ItemRetryWait {
		t.Fatalf("expected item status RetryWait for network timeout, got %v", updated.Status)
	}
	if updated.ErrorCode != string(apperr.CodeProviderUnavailable) {
		t.Fatalf("expected error code %v, got %v", apperr.CodeProviderUnavailable, updated.ErrorCode)
	}

	prov.mu.Lock()
	defer prov.mu.Unlock()
	if len(prov.resolvedOrder) != 1 {
		t.Fatalf("expected only 1 call before systemic stop on network timeout, got %d", len(prov.resolvedOrder))
	}
}
