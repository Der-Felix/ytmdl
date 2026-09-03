package jobs

import (
	"context"
	"errors"
	"testing"

	"ytdm/backend/internal/apperr"
	"ytdm/backend/internal/matcher"
	"ytdm/backend/internal/music"
	"ytdm/backend/internal/provider"
)

type mockSearchProvider struct {
	name            string
	directCandidate *provider.MediaCandidate
	directErr       error
	genericList     []provider.MediaCandidate
	genericErr      error

	directSearches  int
	genericSearches int
}

func (p *mockSearchProvider) Name() string { return p.name }

func (p *mockSearchProvider) Search(_ context.Context, track music.Track) ([]provider.MediaCandidate, error) {
	if track.SourceID != "" {
		p.directSearches++
		if p.directErr != nil {
			return nil, p.directErr
		}
		if p.directCandidate != nil {
			return []provider.MediaCandidate{*p.directCandidate}, nil
		}
		return nil, nil
	}

	p.genericSearches++
	if p.genericErr != nil {
		return nil, p.genericErr
	}
	return p.genericList, nil
}

func (p *mockSearchProvider) Resolve(_ context.Context, _ provider.MediaCandidate) (*provider.MediaSource, error) {
	return nil, nil
}

func setupTestMatcherManager(p provider.MediaProvider, minScore float64) *Manager {
	reg := provider.NewRegistry()
	reg.RegisterMedia(p)

	engine := matcher.New(matcher.Options{
		MinScore:            minScore,
		DurationToleranceMS: 4000,
	})

	return &Manager{
		registry: reg,
		matcher:  engine,
	}
}

func TestDirectID_AcceptedWhenExactMatch(t *testing.T) {
	mock := &mockSearchProvider{
		name: "ytmusic",
		directCandidate: &provider.MediaCandidate{
			ID:         "Jhm1qqF79E0",
			Title:      "64B",
			Artists:    []string{"LACAZETTE"},
			DurationMS: 206000,
		},
		genericList: []provider.MediaCandidate{
			{ID: "generic1", Title: "64B", Artists: []string{"LACAZETTE"}, DurationMS: 206000},
		},
	}

	m := setupTestMatcherManager(mock, 73.5)
	track := music.Track{
		Title:          "64B",
		Artists:        []string{"LACAZETTE"},
		DurationMS:     206000,
		SourceProvider: "ytmusic",
		SourceID:       "Jhm1qqF79E0",
	}

	job := Job{MediaProvider: "ytmusic"}
	res, err := m.match(context.Background(), job, track)
	if err != nil {
		t.Fatalf("match failed: %v", err)
	}

	if res.Candidate.ID != "Jhm1qqF79E0" {
		t.Fatalf("candidate ID = %q, want direct ID %q", res.Candidate.ID, "Jhm1qqF79E0")
	}
	if res.Score < 73.5 {
		t.Fatalf("score = %.1f, want >= 73.5", res.Score)
	}
	if mock.directSearches != 1 {
		t.Errorf("directSearches = %d, want 1", mock.directSearches)
	}
	if mock.genericSearches != 0 {
		t.Errorf("genericSearches = %d, want 0 (fast path should not call generic search)", mock.genericSearches)
	}
}

func TestDirectID_FallbackWhenWrongTitleOrArtist(t *testing.T) {
	mock := &mockSearchProvider{
		name: "ytmusic",
		directCandidate: &provider.MediaCandidate{
			ID:         "Jhm1qqF79E0",
			Title:      "Completely Unrelated Track",
			Artists:    []string{"Different Artist"},
			DurationMS: 206000, // same duration, but wrong title/artist
		},
		genericList: []provider.MediaCandidate{
			{ID: "genericGood", Title: "64B", Artists: []string{"LACAZETTE"}, DurationMS: 206000},
		},
	}

	m := setupTestMatcherManager(mock, 73.5)
	track := music.Track{
		Title:          "64B",
		Artists:        []string{"LACAZETTE"},
		DurationMS:     206000,
		SourceProvider: "ytmusic",
		SourceID:       "Jhm1qqF79E0",
	}

	job := Job{MediaProvider: "ytmusic"}
	res, err := m.match(context.Background(), job, track)
	if err != nil {
		t.Fatalf("match failed: %v", err)
	}

	// Direct ID should be rejected by matcher and fallback to genericGood
	if res.Candidate.ID != "genericGood" {
		t.Fatalf("candidate ID = %q, want fallback candidate genericGood", res.Candidate.ID)
	}
	if mock.directSearches != 1 {
		t.Errorf("directSearches = %d, want 1", mock.directSearches)
	}
	if mock.genericSearches != 1 {
		t.Errorf("genericSearches = %d, want 1 (fallback should have been invoked)", mock.genericSearches)
	}
}

func TestDirectID_FallbackWhenRemixOfferedForOriginal(t *testing.T) {
	mock := &mockSearchProvider{
		name: "ytmusic",
		directCandidate: &provider.MediaCandidate{
			ID:         "Jhm1qqF79E0",
			Title:      "64B (Tiesto Remix)",
			Artists:    []string{"LACAZETTE"},
			DurationMS: 206000,
		},
		genericList: []provider.MediaCandidate{
			{ID: "genericOriginal", Title: "64B", Artists: []string{"LACAZETTE"}, DurationMS: 206000},
		},
	}

	m := setupTestMatcherManager(mock, 73.5)
	track := music.Track{
		Title:          "64B",
		Artists:        []string{"LACAZETTE"},
		DurationMS:     206000,
		SourceProvider: "ytmusic",
		SourceID:       "Jhm1qqF79E0",
	}

	job := Job{MediaProvider: "ytmusic"}
	res, err := m.match(context.Background(), job, track)
	if err != nil {
		t.Fatalf("match failed: %v", err)
	}

	// Remix penalty should drop direct candidate below 73.5, triggering fallback
	if res.Candidate.ID != "genericOriginal" {
		t.Fatalf("candidate ID = %q, want genericOriginal", res.Candidate.ID)
	}
	if mock.directSearches != 1 || mock.genericSearches != 1 {
		t.Errorf("searches: direct=%d, generic=%d", mock.directSearches, mock.genericSearches)
	}
}

func TestDirectID_FallbackWhenLiveOfferedForStudio(t *testing.T) {
	mock := &mockSearchProvider{
		name: "ytmusic",
		directCandidate: &provider.MediaCandidate{
			ID:         "Jhm1qqF79E0",
			Title:      "64B (Live in Paris)",
			Artists:    []string{"LACAZETTE"},
			DurationMS: 206000,
		},
		genericList: []provider.MediaCandidate{
			{ID: "genericStudio", Title: "64B", Artists: []string{"LACAZETTE"}, DurationMS: 206000},
		},
	}

	m := setupTestMatcherManager(mock, 73.5)
	track := music.Track{
		Title:          "64B",
		Artists:        []string{"LACAZETTE"},
		DurationMS:     206000,
		SourceProvider: "ytmusic",
		SourceID:       "Jhm1qqF79E0",
	}

	job := Job{MediaProvider: "ytmusic"}
	res, err := m.match(context.Background(), job, track)
	if err != nil {
		t.Fatalf("match failed: %v", err)
	}

	// Live penalty drops direct candidate below 73.5, triggering fallback
	if res.Candidate.ID != "genericStudio" {
		t.Fatalf("candidate ID = %q, want genericStudio", res.Candidate.ID)
	}
	if mock.directSearches != 1 || mock.genericSearches != 1 {
		t.Errorf("searches: direct=%d, generic=%d", mock.directSearches, mock.genericSearches)
	}
}

func TestDirectID_FallbackWhenUnavailable(t *testing.T) {
	mock := &mockSearchProvider{
		name:      "ytmusic",
		directErr: errors.New("video unavailable (404)"),
		genericList: []provider.MediaCandidate{
			{ID: "genericGood", Title: "64B", Artists: []string{"LACAZETTE"}, DurationMS: 206000},
		},
	}

	m := setupTestMatcherManager(mock, 73.5)
	track := music.Track{
		Title:          "64B",
		Artists:        []string{"LACAZETTE"},
		DurationMS:     206000,
		SourceProvider: "ytmusic",
		SourceID:       "Jhm1qqF79E0",
	}

	job := Job{MediaProvider: "ytmusic"}
	res, err := m.match(context.Background(), job, track)
	if err != nil {
		t.Fatalf("match failed: %v", err)
	}

	if res.Candidate.ID != "genericGood" {
		t.Fatalf("candidate ID = %q, want genericGood", res.Candidate.ID)
	}
}

func TestDirectID_MinScoreEnforcedWhenNoGoodGenericMatch(t *testing.T) {
	mock := &mockSearchProvider{
		name: "ytmusic",
		directCandidate: &provider.MediaCandidate{
			ID:         "Jhm1qqF79E0",
			Title:      "Unrelated Direct",
			Artists:    []string{"Unknown"},
			DurationMS: 206000,
		},
		genericList: []provider.MediaCandidate{
			{ID: "genericBad", Title: "Unrelated Generic", Artists: []string{"Unknown"}, DurationMS: 206000},
		},
	}

	m := setupTestMatcherManager(mock, 73.5)
	track := music.Track{
		Title:          "64B",
		Artists:        []string{"LACAZETTE"},
		DurationMS:     206000,
		SourceProvider: "ytmusic",
		SourceID:       "Jhm1qqF79E0",
	}

	job := Job{MediaProvider: "ytmusic"}
	_, err := m.match(context.Background(), job, track)
	if err == nil {
		t.Fatal("expected match to fail when both direct and generic candidates are below min_score")
	}
	if apperr.CodeOf(err) != apperr.CodeMatchFailed {
		t.Fatalf("error code = %q, want MATCH_FAILED", apperr.CodeOf(err))
	}
}

func TestDirectID_GreenDayRegression_CasesAtoE(t *testing.T) {
	testCases := []struct {
		name        string
		trackTitle  string
		candTitle   string
		durationMS  int
		candDurMS   int
		sourceID    string
		expectScore float64
	}{
		{
			name:        "Case A: 409 in Your Coffeemaker (WFMU-FM Radio)",
			trackTitle:  "409 in Your Coffeemaker (WFMU-FM Radio)",
			candTitle:   "409 in Your Coffeemaker",
			durationMS:  199000,
			candDurMS:   199000,
			sourceID:    "iQg-jocgFW8",
			expectScore: 88.0,
		},
		{
			name:        "Case B: Welcome to Paradise (WFMU-FM Radio)",
			trackTitle:  "Welcome to Paradise (WFMU-FM Radio)",
			candTitle:   "Welcome to Paradise",
			durationMS:  211000,
			candDurMS:   211000,
			sourceID:    "nazLMO9KCn4",
			expectScore: 87.0,
		},
		{
			name:        "Case C: 2000 Light Years Away (WFMU-FM Radio)",
			trackTitle:  "2000 Light Years Away (WFMU-FM Radio)",
			candTitle:   "2000 Light Years Away",
			durationMS:  141000,
			candDurMS:   141000,
			sourceID:    "v0owiup4BcE",
			expectScore: 87.0,
		},
		{
			name:        "Case D: At the Library (WFMU-FM Radio)",
			trackTitle:  "At the Library (WFMU-FM Radio)",
			candTitle:   "At the Library",
			durationMS:  192000,
			candDurMS:   192000,
			sourceID:    "pF-gehjrFio",
			expectScore: 84.0,
		},
		{
			name:        "Case E: Dominated Love Slave (WFMU-FM Radio)",
			trackTitle:  "Dominated Love Slave (WFMU-FM Radio)",
			candTitle:   "Dominated Love Slave",
			durationMS:  103000,
			candDurMS:   102000,
			sourceID:    "IfERk-jCzXk",
			expectScore: 86.0,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			mock := &mockSearchProvider{
				name: "ytmusic",
				directCandidate: &provider.MediaCandidate{
					ID:         tc.sourceID,
					Title:      tc.candTitle,
					Artists:    []string{"Green Day"},
					DurationMS: tc.candDurMS,
				},
				genericList: []provider.MediaCandidate{
					{ID: "genericBootleg", Title: "Green Day Live bootleg clip", Artists: []string{"Green Day"}, DurationMS: tc.durationMS},
				},
			}

			m := setupTestMatcherManager(mock, 73.5)
			track := music.Track{
				Title:          tc.trackTitle,
				Artists:        []string{"Green Day"},
				Album:          "Radio Waves 1991-1994: The Very Best Of Green Day",
				DurationMS:     tc.durationMS,
				SourceProvider: "ytmusic",
				SourceID:       tc.sourceID,
			}

			res, err := m.match(context.Background(), Job{MediaProvider: "ytmusic"}, track)
			if err != nil {
				t.Fatalf("match failed: %v", err)
			}
			if res.Candidate.ID != tc.sourceID {
				t.Fatalf("got candidate ID %q, want direct ID %q", res.Candidate.ID, tc.sourceID)
			}
			if res.Score < 73.5 {
				t.Fatalf("got score %.2f, want >= 73.5", res.Score)
			}
			if mock.directSearches != 1 || mock.genericSearches != 0 {
				t.Errorf("expected 1 direct search and 0 generic search, got direct=%d, generic=%d", mock.directSearches, mock.genericSearches)
			}
		})
	}
}

func TestDirectID_GreenDayRegression_CaseF_Unavailable(t *testing.T) {
	mock := &mockSearchProvider{
		name:      "ytmusic",
		directErr: errors.New("video unavailable (geoblocked/private)"),
		genericList: []provider.MediaCandidate{
			{ID: "genericBad", Title: "Green Day - Fuck Time (Poor live cover)", Artists: []string{"Green Day"}, DurationMS: 200000},
		},
	}

	m := setupTestMatcherManager(mock, 73.5)
	track := music.Track{
		Title:          "Fuck Time",
		Artists:        []string{"Green Day"},
		Album:          "¡DOS!",
		DurationMS:     166000,
		SourceProvider: "ytmusic",
		SourceID:       "00Gqgwgp3YI",
	}

	_, err := m.match(context.Background(), Job{MediaProvider: "ytmusic"}, track)
	if err == nil {
		t.Fatal("expected match to fail when direct source is unavailable and generic candidates fail min_score")
	}
	if apperr.CodeOf(err) != apperr.CodeMatchFailed {
		t.Fatalf("error code = %q, want MATCH_FAILED", apperr.CodeOf(err))
	}
	if mock.directSearches != 1 || mock.genericSearches != 1 {
		t.Errorf("expected 1 direct search and 1 generic fallback search, got direct=%d generic=%d", mock.directSearches, mock.genericSearches)
	}
}
