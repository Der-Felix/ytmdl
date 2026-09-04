package reconcile_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"ytdm/backend/cmd/ytmdlctl/internal/reconcile"
)

type fakeExecutor struct {
	candidates      []reconcile.Candidate
	mergeCalls      int
	mergedWinners   []string
	mergedDups      [][]string
	relMoved        int
	trkMoved        int
	mergeErr        error
	integrityErr    error
	secondQueryData []reconcile.Candidate
	queryCount      int
}

func (f *fakeExecutor) QueryCandidates(ctx context.Context) ([]reconcile.Candidate, error) {
	f.queryCount++
	if f.queryCount > 1 && f.secondQueryData != nil {
		return f.secondQueryData, nil
	}
	return f.candidates, nil
}

func (f *fakeExecutor) GetArtists(ctx context.Context, ids []string) ([]reconcile.Candidate, error) {
	idMap := make(map[string]reconcile.Candidate)
	for _, c := range f.candidates {
		idMap[c.ID] = c
	}
	var out []reconcile.Candidate
	for _, id := range ids {
		if c, ok := idMap[id]; ok {
			out = append(out, c)
		}
	}
	return out, nil
}

func (f *fakeExecutor) MergeGroup(ctx context.Context, winnerID string, dupIDs []string, bestImage string) (int, int, error) {
	f.mergeCalls++
	f.mergedWinners = append(f.mergedWinners, winnerID)
	f.mergedDups = append(f.mergedDups, dupIDs)
	if f.mergeErr != nil {
		return 0, 0, f.mergeErr
	}
	return f.relMoved, f.trkMoved, nil
}

func (f *fakeExecutor) VerifyIntegrity(ctx context.Context) error {
	return f.integrityErr
}

func TestAnalyze_ProvedSameProviderSynthetic(t *testing.T) {
	now := time.Now()
	candidates := []reconcile.Candidate{
		{
			ID:           "art_real_1",
			Name:         "Alan Walker",
			Provider:     "deezer",
			SourceID:     "12345", // real ID
			ImageURL:     "https://artwork/walker.jpg",
			CreatedAt:    now.Add(-2 * time.Hour),
			ReleaseCount: 2,
			TrackCount:   5,
			HasSub:       true,
		},
		{
			ID:           "art_synth_2",
			Name:         "Alan Walker",
			Provider:     "deezer",
			SourceID:     "artist:alan-walker", // synthetic ID
			ImageURL:     "",
			CreatedAt:    now.Add(-1 * time.Hour),
			ReleaseCount: 1,
			TrackCount:   2,
			HasSub:       false,
		},
	}

	proved, ambiguous := reconcile.Analyze(candidates)

	if len(ambiguous) != 0 {
		t.Fatalf("expected 0 ambiguous groups, got %d", len(ambiguous))
	}
	if len(proved) != 1 {
		t.Fatalf("expected 1 proved group, got %d", len(proved))
	}

	pg := proved[0]
	if pg.Winner.ID != "art_real_1" {
		t.Errorf("expected winner art_real_1, got %s", pg.Winner.ID)
	}
	if len(pg.Duplicates) != 1 || pg.Duplicates[0].ID != "art_synth_2" {
		t.Errorf("expected duplicate art_synth_2, got %+v", pg.Duplicates)
	}
	if pg.ReleasesToReassign != 1 {
		t.Errorf("expected 1 release to reassign, got %d", pg.ReleasesToReassign)
	}
	if pg.TracksToReassign != 2 {
		t.Errorf("expected 2 tracks to reassign, got %d", pg.TracksToReassign)
	}
	if pg.BestImage != "https://artwork/walker.jpg" {
		t.Errorf("expected image preserved, got %s", pg.BestImage)
	}
}

func TestAnalyze_CrossProviderSameName_Ambiguous(t *testing.T) {
	now := time.Now()
	candidates := []reconcile.Candidate{
		{
			ID:        "art_deezer",
			Name:      "Alan Walker",
			Provider:  "deezer",
			SourceID:  "12345",
			CreatedAt: now,
		},
		{
			ID:        "art_spotify",
			Name:      "Alan Walker",
			Provider:  "spotify",
			SourceID:  "spotify:artist:699023",
			CreatedAt: now,
		},
	}

	proved, ambiguous := reconcile.Analyze(candidates)

	if len(proved) != 0 {
		t.Fatalf("expected 0 proved groups for cross-provider match, got %d", len(proved))
	}
	if len(ambiguous) != 1 {
		t.Fatalf("expected 1 ambiguous group, got %d", len(ambiguous))
	}
	if len(ambiguous[0].Candidates) != 2 {
		t.Errorf("expected 2 candidates in ambiguous group, got %d", len(ambiguous[0].Candidates))
	}
}

func TestAnalyze_DistinctRealIDs_Ambiguous(t *testing.T) {
	now := time.Now()
	candidates := []reconcile.Candidate{
		{
			ID:        "art_jw_composer",
			Name:      "John Williams",
			Provider:  "deezer",
			SourceID:  "1158", // Film composer
			CreatedAt: now.Add(-5 * time.Hour),
		},
		{
			ID:        "art_jw_guitarist",
			Name:      "John Williams",
			Provider:  "deezer",
			SourceID:  "8740", // Classical guitarist
			CreatedAt: now.Add(-1 * time.Hour),
		},
	}

	proved, ambiguous := reconcile.Analyze(candidates)

	if len(proved) != 0 {
		t.Fatalf("expected 0 proved groups for distinct real IDs, got %d", len(proved))
	}
	if len(ambiguous) != 1 {
		t.Fatalf("expected 1 ambiguous group, got %d", len(ambiguous))
	}
}

func TestRun_DryRun_NoWrites(t *testing.T) {
	now := time.Now()
	fake := &fakeExecutor{
		candidates: []reconcile.Candidate{
			{
				ID:           "art_real",
				Name:         "Apache 207",
				Provider:     "deezer",
				SourceID:     "14878271",
				CreatedAt:    now.Add(-2 * time.Hour),
				ReleaseCount: 2,
				TrackCount:   4,
			},
			{
				ID:           "art_synth",
				Name:         "Apache 207",
				Provider:     "deezer",
				SourceID:     "artist:apache-207",
				CreatedAt:    now,
				ReleaseCount: 1,
				TrackCount:   2,
			},
		},
	}

	opts := reconcile.Options{
		DryRun: true,
		Apply:  false,
	}

	report, err := reconcile.Run(context.Background(), fake, opts)
	if err != nil {
		t.Fatalf("Run dry-run failed: %v", err)
	}

	if !report.DryRun {
		t.Errorf("expected DryRun = true")
	}
	if fake.mergeCalls != 0 {
		t.Errorf("expected 0 merge calls in dry-run, got %d", fake.mergeCalls)
	}
	if report.ProvedClusters != 1 {
		t.Errorf("expected 1 proved cluster, got %d", report.ProvedClusters)
	}
	if report.ProvedDups != 1 {
		t.Errorf("expected 1 proved dup, got %d", report.ProvedDups)
	}
	if report.ReassignedReleases != 1 {
		t.Errorf("expected 1 planned release reassignment, got %d", report.ReassignedReleases)
	}
	if report.ReassignedTracks != 2 {
		t.Errorf("expected 2 planned track reassignments, got %d", report.ReassignedTracks)
	}
	if report.MergedRows != 0 {
		t.Errorf("expected 0 merged rows in dry-run, got %d", report.MergedRows)
	}
}

func TestRun_Mutating_MergesSuccessfully(t *testing.T) {
	now := time.Now()
	fake := &fakeExecutor{
		candidates: []reconcile.Candidate{
			{
				ID:           "art_real",
				Name:         "Apache 207",
				Provider:     "deezer",
				SourceID:     "14878271",
				CreatedAt:    now.Add(-2 * time.Hour),
				ReleaseCount: 2,
				TrackCount:   4,
			},
			{
				ID:           "art_synth",
				Name:         "Apache 207",
				Provider:     "deezer",
				SourceID:     "artist:apache-207",
				CreatedAt:    now,
				ReleaseCount: 1,
				TrackCount:   2,
			},
		},
		relMoved:        1,
		trkMoved:        2,
		secondQueryData: []reconcile.Candidate{}, // 0 remaining duplicates
	}

	opts := reconcile.Options{
		DryRun: false,
		Apply:  true,
	}

	report, err := reconcile.Run(context.Background(), fake, opts)
	if err != nil {
		t.Fatalf("Run mutating failed: %v", err)
	}

	if report.DryRun {
		t.Errorf("expected DryRun = false")
	}
	if fake.mergeCalls != 1 {
		t.Errorf("expected 1 merge call, got %d", fake.mergeCalls)
	}
	if report.MergedRows != 1 {
		t.Errorf("expected 1 merged row, got %d", report.MergedRows)
	}
	if report.ReassignedReleases != 1 {
		t.Errorf("expected 1 reassigned release, got %d", report.ReassignedReleases)
	}
	if report.ReassignedTracks != 2 {
		t.Errorf("expected 2 reassigned tracks, got %d", report.ReassignedTracks)
	}
}

func TestRun_Idempotent_SecondRunZeroMerges(t *testing.T) {
	fake := &fakeExecutor{
		candidates: []reconcile.Candidate{}, // already clean
	}

	opts := reconcile.Options{
		DryRun: false,
		Apply:  true,
	}

	report, err := reconcile.Run(context.Background(), fake, opts)
	if err != nil {
		t.Fatalf("Run second pass failed: %v", err)
	}

	if fake.mergeCalls != 0 {
		t.Errorf("expected 0 merge calls on clean database, got %d", fake.mergeCalls)
	}
	if report.MergedRows != 0 {
		t.Errorf("expected 0 merged rows, got %d", report.MergedRows)
	}
	if report.ProvedClusters != 0 {
		t.Errorf("expected 0 proved clusters, got %d", report.ProvedClusters)
	}
}

func TestRun_IntegrityFailure_Aborts(t *testing.T) {
	now := time.Now()
	fake := &fakeExecutor{
		candidates: []reconcile.Candidate{
			{
				ID:        "art_real",
				Name:      "Apache 207",
				Provider:  "deezer",
				SourceID:  "14878271",
				CreatedAt: now.Add(-2 * time.Hour),
			},
			{
				ID:        "art_synth",
				Name:      "Apache 207",
				Provider:  "deezer",
				SourceID:  "artist:apache-207",
				CreatedAt: now,
			},
		},
		integrityErr: errors.New("dangling reference found"),
	}

	opts := reconcile.Options{
		DryRun: false,
		Apply:  true,
	}

	_, err := reconcile.Run(context.Background(), fake, opts)
	if err == nil {
		t.Fatalf("expected integrity check failure, got nil")
	}
}
