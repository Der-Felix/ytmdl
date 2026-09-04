package artistidentity_test

import (
	"testing"
	"time"

	"ytdm/backend/internal/artistidentity"
	"ytdm/backend/internal/music"
)

func TestClassifyCandidatePair_JohnWilliamsNegativeTest(t *testing.T) {
	// Rule 6 & Section 1.2: Two distinct artists with the same name and provider,
	// but different real provider IDs, MUST NOT be merged (AMBIGUOUS).
	filmComposer := artistidentity.Candidate{
		ID:         "jw_film",
		Name:       "John Williams",
		Provider:   "deezer",
		SourceID:   "1158",
		SourceKind: music.SourceKindExternal,
	}
	guitarist := artistidentity.Candidate{
		ID:         "jw_guitar",
		Name:       "John Williams",
		Provider:   "deezer",
		SourceID:   "8740",
		SourceKind: music.SourceKindExternal,
	}

	level := artistidentity.ClassifyCandidatePair(filmComposer, guitarist)
	if level != artistidentity.LevelAmbiguous {
		t.Fatalf("expected John Williams pair to be AMBIGUOUS, got: %s", level)
	}
}

func TestClassifyCandidatePair_ExactSource(t *testing.T) {
	cand1 := artistidentity.Candidate{
		ID:         "alan_1",
		Name:       "Alan Walker",
		Provider:   "deezer",
		SourceID:   "288164",
		SourceKind: music.SourceKindExternal,
	}
	cand2 := artistidentity.Candidate{
		ID:         "alan_2",
		Name:       "Alan Walker",
		Provider:   "deezer",
		SourceID:   "288164",
		SourceKind: music.SourceKindExternal,
	}

	level := artistidentity.ClassifyCandidatePair(cand1, cand2)
	if level != artistidentity.LevelExactSource {
		t.Fatalf("expected EXACT_SOURCE for identical provider+source_id, got: %s", level)
	}
}

func TestClassifyCandidatePair_SyntheticAndSubscription(t *testing.T) {
	realSub := artistidentity.Candidate{
		ID:         "sub_1",
		Name:       "Alan Walker",
		Provider:   "deezer",
		SourceID:   "288164",
		SourceKind: music.SourceKindExternal,
		HasSub:     true,
	}
	synth := artistidentity.Candidate{
		ID:         "synth_1",
		Name:       "Alan Walker",
		Provider:   "deezer",
		SourceID:   "artist:alan-walker",
		SourceKind: music.SourceKindLegacySynthetic,
	}

	level := artistidentity.ClassifyCandidatePair(realSub, synth)
	if level != artistidentity.LevelSubscriptionProven {
		t.Fatalf("expected SUBSCRIPTION_PROVEN for synthetic matching subscription, got: %s", level)
	}
}

func TestChooseWinner_DeterministicPriority(t *testing.T) {
	now := time.Now()

	candSyntheticWithMoreTracks := artistidentity.Candidate{
		ID:           "id_synth",
		Name:         "Alan Walker",
		Provider:     "deezer",
		SourceID:     "artist:alan-walker",
		SourceKind:   music.SourceKindLegacySynthetic,
		ReleaseCount: 5,
		TrackCount:   20,
		CreatedAt:    now.Add(-2 * time.Hour),
	}

	candRealSub := artistidentity.Candidate{
		ID:           "id_real_sub",
		Name:         "Alan Walker",
		Provider:     "deezer",
		SourceID:     "288164",
		SourceKind:   music.SourceKindExternal,
		HasSub:       true,
		ImageURL:     "https://example.com/avatar.jpg",
		ReleaseCount: 1,
		TrackCount:   2,
		CreatedAt:    now.Add(-1 * time.Hour),
	}

	candRealNoSub := artistidentity.Candidate{
		ID:           "id_real_nosub",
		Name:         "Alan Walker",
		Provider:     "deezer",
		SourceID:     "288164_alt",
		SourceKind:   music.SourceKindExternal,
		HasSub:       false,
		ImageURL:     "https://example.com/avatar.jpg",
		ReleaseCount: 2,
		TrackCount:   5,
		CreatedAt:    now.Add(-3 * time.Hour),
	}

	candidates := []artistidentity.Candidate{candSyntheticWithMoreTracks, candRealNoSub, candRealSub}
	winner, duplicates, ok := artistidentity.ChooseWinner(candidates)
	if !ok {
		t.Fatalf("expected ChooseWinner to succeed")
	}

	// Active subscription has Priority 1
	if winner.ID != "id_real_sub" {
		t.Fatalf("expected winner to be id_real_sub due to active subscription, got %s", winner.ID)
	}
	if len(duplicates) != 2 {
		t.Fatalf("expected 2 duplicates, got %d", len(duplicates))
	}
}
