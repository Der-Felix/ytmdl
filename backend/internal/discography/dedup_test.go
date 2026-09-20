package discography

import (
	"testing"

	"ytdm/backend/internal/music"
)

func track(title string, opts ...func(*music.Track)) music.Track {
	t := music.Track{
		Title:       title,
		Artists:     []string{"Artist"},
		DurationMS:  205000,
		ReleaseType: music.ReleaseAlbum,
	}
	for _, opt := range opts {
		opt(&t)
	}
	return t
}

func withDuration(ms int) func(*music.Track) {
	return func(t *music.Track) { t.DurationMS = ms }
}
func withISRC(isrc string) func(*music.Track) {
	return func(t *music.Track) { t.ISRC = isrc }
}
func withRelease(rt music.ReleaseType, album string, year int) func(*music.Track) {
	return func(t *music.Track) {
		t.ReleaseType = rt
		t.Album = album
		t.Year = year
	}
}
func withArtists(names ...string) func(*music.Track) {
	return func(t *music.Track) { t.Artists = names }
}
func withID(id string) func(*music.Track) {
	return func(t *music.Track) { t.ID = id }
}
func withSource(provider, sourceID string) func(*music.Track) {
	return func(t *music.Track) {
		t.SourceProvider = provider
		t.SourceID = sourceID
	}
}

func TestDeduplicateMergesIdenticalTrackIDAcrossDifferentTitles(t *testing.T) {
	groups := Deduplicate([]music.Track{
		track("Heart Still Beating", withID("nt88JCYM6Yg"), withRelease(music.ReleaseSingle, "Heart Still Beating", 2023)),
		track("Heart Still Beating (Extended)", withID("nt88JCYM6Yg"), withRelease(music.ReleaseSingle, "Heart Still Beating (Extended)", 2023)),
	}, DedupOptions{})

	if len(groups) != 1 {
		t.Fatalf("got %d groups, want 1 — a shared track ID identifies the recording", len(groups))
	}
	if groups[0].Reason != ReasonTrackID {
		t.Errorf("reason = %q, want %q", groups[0].Reason, ReasonTrackID)
	}
	if len(groups[0].Duplicates) != 1 {
		t.Errorf("got %d duplicates, want 1", len(groups[0].Duplicates))
	}
}

func TestDeduplicateMergesIdenticalSourceIDAcrossDifferentTitles(t *testing.T) {
	groups := Deduplicate([]music.Track{
		track("Song", withSource("ytmusic", "vid_123"), withRelease(music.ReleaseSingle, "Single A", 2024)),
		track("Song (Alt Version)", withSource("ytmusic", "vid_123"), withRelease(music.ReleaseSingle, "Single B", 2024)),
	}, DedupOptions{})

	if len(groups) != 1 {
		t.Fatalf("got %d groups, want 1 — shared source ID identifies the recording", len(groups))
	}
	if groups[0].Reason != ReasonTrackID {
		t.Errorf("reason = %q, want %q", groups[0].Reason, ReasonTrackID)
	}
}

func TestDeduplicateRepresentativeSelectionDeterministicAcrossOrders(t *testing.T) {
	tSingle := track("Title", withID("shared_id"), withRelease(music.ReleaseSingle, "Single Release", 2023))
	tEP := track("Title", withID("shared_id"), withRelease(music.ReleaseEP, "EP Release", 2023))
	tAlbum := track("Title", withID("shared_id"), withRelease(music.ReleaseAlbum, "Album Release", 2023))

	// Single -> Album vs Album -> Single
	if g := Deduplicate([]music.Track{tSingle, tAlbum}, DedupOptions{}); len(g) != 1 || g[0].Track.ReleaseType != music.ReleaseAlbum {
		t.Fatalf("Single -> Album: expected Album representative, got %+v", g)
	}
	if g := Deduplicate([]music.Track{tAlbum, tSingle}, DedupOptions{}); len(g) != 1 || g[0].Track.ReleaseType != music.ReleaseAlbum {
		t.Fatalf("Album -> Single: expected Album representative, got %+v", g)
	}

	// EP -> Album vs Album -> EP
	if g := Deduplicate([]music.Track{tEP, tAlbum}, DedupOptions{}); len(g) != 1 || g[0].Track.ReleaseType != music.ReleaseAlbum {
		t.Fatalf("EP -> Album: expected Album representative, got %+v", g)
	}
	if g := Deduplicate([]music.Track{tAlbum, tEP}, DedupOptions{}); len(g) != 1 || g[0].Track.ReleaseType != music.ReleaseAlbum {
		t.Fatalf("Album -> EP: expected Album representative, got %+v", g)
	}

	// Single -> EP vs EP -> Single
	if g := Deduplicate([]music.Track{tSingle, tEP}, DedupOptions{}); len(g) != 1 || g[0].Track.ReleaseType != music.ReleaseEP {
		t.Fatalf("Single -> EP: expected EP representative, got %+v", g)
	}
	if g := Deduplicate([]music.Track{tEP, tSingle}, DedupOptions{}); len(g) != 1 || g[0].Track.ReleaseType != music.ReleaseEP {
		t.Fatalf("EP -> Single: expected EP representative, got %+v", g)
	}

	// Single -> EP -> Album
	g1 := Deduplicate([]music.Track{tSingle, tEP, tAlbum}, DedupOptions{})
	if len(g1) != 1 || g1[0].Track.ReleaseType != music.ReleaseAlbum {
		t.Fatalf("order 1: expected 1 group with Album representative, got %+v", g1)
	}

	// Album -> EP -> Single
	g2 := Deduplicate([]music.Track{tAlbum, tEP, tSingle}, DedupOptions{})
	if len(g2) != 1 || g2[0].Track.ReleaseType != music.ReleaseAlbum {
		t.Fatalf("order 2: expected 1 group with Album representative, got %+v", g2)
	}

	// EP -> Single -> Album
	g3 := Deduplicate([]music.Track{tEP, tSingle, tAlbum}, DedupOptions{})
	if len(g3) != 1 || g3[0].Track.ReleaseType != music.ReleaseAlbum {
		t.Fatalf("order 3: expected 1 group with Album representative, got %+v", g3)
	}
}

func TestDeduplicateMergesIdenticalRecordings(t *testing.T) {
	groups := Deduplicate([]music.Track{
		track("Song", withRelease(music.ReleaseAlbum, "Album", 2001)),
		track("Song", withRelease(music.ReleaseCompilation, "Greatest Hits", 2010)),
	}, DedupOptions{})

	if len(groups) != 1 {
		t.Fatalf("got %d groups, want 1", len(groups))
	}
	if groups[0].Track.Album != "Album" {
		t.Errorf("representative album = %q, want the original album", groups[0].Track.Album)
	}
	if len(groups[0].Duplicates) != 1 {
		t.Errorf("got %d duplicates, want 1", len(groups[0].Duplicates))
	}
	if groups[0].Reason != ReasonMetadata {
		t.Errorf("reason = %q, want %q", groups[0].Reason, ReasonMetadata)
	}
}

func TestDeduplicateKeepsGenuineVariantsApart(t *testing.T) {
	tracks := []music.Track{
		track("Song"),
		track("Song (Live)", withDuration(209000)),
		track("Song (Instrumental)"),
		track("Song (Remix)", withDuration(240000)),
		track("Song (Acoustic)", withDuration(198000)),
	}
	groups := Deduplicate(tracks, DedupOptions{})
	if len(groups) != len(tracks) {
		t.Fatalf("got %d groups, want %d — variants must not be merged", len(groups), len(tracks))
	}
}

func TestDeduplicateUsesISRCAcrossDifferentTitles(t *testing.T) {
	groups := Deduplicate([]music.Track{
		track("Song", withISRC("DEA123456789")),
		track("Song (Radio Edit)", withISRC("DE-A12-34-56789"), withDuration(150000)),
	}, DedupOptions{})

	if len(groups) != 1 {
		t.Fatalf("got %d groups, want 1 — a shared ISRC identifies the recording", len(groups))
	}
	if groups[0].Reason != ReasonISRC {
		t.Errorf("reason = %q, want %q", groups[0].Reason, ReasonISRC)
	}
}

func TestDeduplicateSeparatesDifferentISRCs(t *testing.T) {
	groups := Deduplicate([]music.Track{
		track("Song", withISRC("DEA123456789")),
		track("Other Song", withISRC("USB987654321")),
	}, DedupOptions{})
	if len(groups) != 2 {
		t.Fatalf("got %d groups, want 2", len(groups))
	}
}

func TestDeduplicateRespectsDurationTolerance(t *testing.T) {
	within := Deduplicate([]music.Track{
		track("Song", withDuration(205000)),
		track("Song", withDuration(207000)),
	}, DedupOptions{DurationToleranceMS: 4000})
	if len(within) != 1 {
		t.Fatalf("got %d groups, want 1 for a 2s difference", len(within))
	}

	outside := Deduplicate([]music.Track{
		track("Song", withDuration(205000)),
		track("Song", withDuration(260000)),
	}, DedupOptions{DurationToleranceMS: 4000})
	if len(outside) != 2 {
		t.Fatalf("got %d groups, want 2 for a 55s difference", len(outside))
	}
}

func TestDeduplicateNormalisesFeaturingAndDecoration(t *testing.T) {
	groups := Deduplicate([]music.Track{
		track("Song (feat. Guest)"),
		track("Song", withArtists("Artist", "Guest")),
		track("Song (Remastered 2011)"),
		track("SONG"),
	}, DedupOptions{})
	if len(groups) != 1 {
		t.Fatalf("got %d groups, want 1: %+v", len(groups), groupTitles(groups))
	}
}

func TestDeduplicateKeepsDifferentArtistsApart(t *testing.T) {
	groups := Deduplicate([]music.Track{
		track("Song", withArtists("Artist")),
		track("Song", withArtists("Another Band")),
	}, DedupOptions{})
	if len(groups) != 2 {
		t.Fatalf("got %d groups, want 2", len(groups))
	}
}

func TestDeduplicateEmptyInput(t *testing.T) {
	if got := Deduplicate(nil, DedupOptions{}); got != nil {
		t.Fatalf("got %v, want nil", got)
	}
}

func TestDeduplicateIsDeterministic(t *testing.T) {
	tracks := []music.Track{
		track("B Song"),
		track("A Song"),
		track("B Song"),
	}
	first := DeduplicateTracks(tracks, DedupOptions{})
	for i := 0; i < 5; i++ {
		again := DeduplicateTracks(tracks, DedupOptions{})
		if len(again) != len(first) {
			t.Fatalf("group count changed between runs")
		}
		for j := range first {
			if again[j].Title != first[j].Title {
				t.Fatalf("order changed between runs: %q vs %q", again[j].Title, first[j].Title)
			}
		}
	}
}

func groupTitles(groups []Group) []string {
	out := make([]string, 0, len(groups))
	for _, g := range groups {
		out = append(out, g.Track.Title)
	}
	return out
}

func TestEquivalentReleases_ConfirmedProductionPattern_TestA(t *testing.T) {
	// TEST A – confirmed duplicate catalog pattern
	// Two distinct provider release IDs with metadata/track structure equivalent enough
	// that they resolve to the same effective canonical release.
	// Expected: EquivalentReleases reports true.
	relA := music.Release{
		ID: "rel_1", SourceID: "MPREb_b2lpllKyWwY", Provider: "ytmusic",
		Title:       "Vivaldi: L'Estro armonico No. 2 - Allegro (Excerpt)",
		AlbumArtist: "Antonio Vivaldi", Artists: []string{"Antonio Vivaldi"},
		Year: 2025, ReleaseType: music.ReleaseSingle,
	}
	tracksA := []music.Track{
		{
			ID: "hbvxKF3RO20", SourceID: "hbvxKF3RO20", Title: "Vivaldi: L'Estro armonico No. 2 - Allegro (Excerpt)",
			Artists: []string{"Antonio Vivaldi"}, AlbumArtist: "Antonio Vivaldi",
			TrackNumber: 1, DiscNumber: 1, DurationMS: 135000,
		},
	}

	relB := music.Release{
		ID: "rel_2", SourceID: "MPREb_abOSjhra9KT", Provider: "ytmusic",
		Title:       "Vivaldi: L'Estro armonico No. 2 - Allegro (Excerpt)",
		AlbumArtist: "Antonio Vivaldi", Artists: []string{"Antonio Vivaldi"},
		Year: 2025, ReleaseType: music.ReleaseSingle,
	}
	tracksB := []music.Track{
		{
			ID: "9Is4gK2iBxY", SourceID: "9Is4gK2iBxY", Title: "Vivaldi: L'Estro armonico No. 2 - Allegro (Excerpt)",
			Artists: []string{"Antonio Vivaldi"}, AlbumArtist: "Antonio Vivaldi",
			TrackNumber: 1, DiscNumber: 1, DurationMS: 75000, // materially different runtime on provider
		},
	}

	if !EquivalentReleases(relA, tracksA, relB, tracksB) {
		t.Fatalf("expected confirmed duplicate catalog entries to be equivalent")
	}

	dedupedReleases, dedupedTracks := DeduplicateReleases(
		[]music.Release{relA, relB},
		[][]music.Track{tracksA, tracksB},
	)
	if len(dedupedReleases) != 1 {
		t.Fatalf("expected exactly 1 release after dedup, got %d", len(dedupedReleases))
	}
	if dedupedReleases[0].SourceID != "MPREb_b2lpllKyWwY" {
		t.Fatalf("expected first representative kept, got %q", dedupedReleases[0].SourceID)
	}
	if len(dedupedTracks) != 1 || len(dedupedTracks[0]) != 1 {
		t.Fatalf("expected 1 track list kept, got %+v", dedupedTracks)
	}
}

func TestEquivalentReleases_DifferentYear_TestB(t *testing.T) {
	// TEST B – different provider IDs, same title, different year
	// Expected: NOT deduplicated.
	relA := music.Release{
		ID: "rel_1", SourceID: "src_1", Title: "Greatest Hits",
		AlbumArtist: "Queen", Artists: []string{"Queen"},
		Year: 1981, ReleaseType: music.ReleaseAlbum,
	}
	tracksA := []music.Track{
		{Title: "Bohemian Rhapsody", TrackNumber: 1, DiscNumber: 1},
	}

	relB := music.Release{
		ID: "rel_2", SourceID: "src_2", Title: "Greatest Hits",
		AlbumArtist: "Queen", Artists: []string{"Queen"},
		Year: 2011, ReleaseType: music.ReleaseAlbum, // reissue year
	}
	tracksB := []music.Track{
		{Title: "Bohemian Rhapsody", TrackNumber: 1, DiscNumber: 1},
	}

	if EquivalentReleases(relA, tracksA, relB, tracksB) {
		t.Fatalf("releases with different years must not be deduplicated")
	}
}

func TestEquivalentReleases_MateriallyDifferentTrackList_TestC(t *testing.T) {
	// TEST C – same artist/title/year but materially different track list
	// Example: standard vs deluxe / additional tracks
	// Expected: NOT incorrectly collapsed.
	relA := music.Release{
		ID: "rel_std", SourceID: "src_std", Title: "Meteora",
		AlbumArtist: "Linkin Park", Artists: []string{"Linkin Park"},
		Year: 2003, ReleaseType: music.ReleaseAlbum,
	}
	tracksA := []music.Track{
		{Title: "Foreword", TrackNumber: 1, DiscNumber: 1},
		{Title: "Don't Stay", TrackNumber: 2, DiscNumber: 1},
	}

	relB := music.Release{
		ID: "rel_dlx", SourceID: "src_dlx", Title: "Meteora",
		AlbumArtist: "Linkin Park", Artists: []string{"Linkin Park"},
		Year: 2003, ReleaseType: music.ReleaseAlbum,
	}
	tracksB := []music.Track{
		{Title: "Foreword", TrackNumber: 1, DiscNumber: 1},
		{Title: "Don't Stay", TrackNumber: 2, DiscNumber: 1},
		{Title: "Bonus Track", TrackNumber: 3, DiscNumber: 1}, // extra track
	}

	if EquivalentReleases(relA, tracksA, relB, tracksB) {
		t.Fatalf("standard vs deluxe with different track count must not be deduplicated")
	}

	// Also verify same track count but different track title
	tracksBVariant := []music.Track{
		{Title: "Foreword", TrackNumber: 1, DiscNumber: 1},
		{Title: "Don't Stay (Live)", TrackNumber: 2, DiscNumber: 1}, // variant title
	}
	if EquivalentReleases(relA, tracksA, relB, tracksBVariant) {
		t.Fatalf("releases with differing track titles must not be deduplicated")
	}
}

func TestEquivalentReleases_ReleaseTypeDiffers_TestD(t *testing.T) {
	// TEST D – release type differs
	// Album vs EP or Single with otherwise similar metadata.
	// Expected: NOT deduplicated.
	relAlbum := music.Release{
		ID: "rel_alb", SourceID: "src_alb", Title: "Numb",
		AlbumArtist: "Linkin Park", Artists: []string{"Linkin Park"},
		Year: 2003, ReleaseType: music.ReleaseAlbum,
	}
	relSingle := music.Release{
		ID: "rel_sng", SourceID: "src_sng", Title: "Numb",
		AlbumArtist: "Linkin Park", Artists: []string{"Linkin Park"},
		Year: 2003, ReleaseType: music.ReleaseSingle,
	}
	tracks := []music.Track{
		{Title: "Numb", TrackNumber: 1, DiscNumber: 1},
	}

	if EquivalentReleases(relAlbum, tracks, relSingle, tracks) {
		t.Fatalf("Album vs Single must not be deduplicated")
	}
}

func TestEquivalentReleases_ExactDuplicateRepeated_TestE(t *testing.T) {
	// TEST E – exact duplicate repeated in same scan
	// Expected: deterministic one-result scheduling.
	rel := music.Release{
		ID: "rel_1", SourceID: "src_1", Title: "In The End",
		AlbumArtist: "Linkin Park", Artists: []string{"Linkin Park"},
		Year: 2000, ReleaseType: music.ReleaseSingle,
	}
	tracks := []music.Track{
		{Title: "In The End", TrackNumber: 1, DiscNumber: 1},
	}

	if !EquivalentReleases(rel, tracks, rel, tracks) {
		t.Fatalf("identical release must be equivalent to itself")
	}

	dedupedReleases, dedupedTracks := DeduplicateReleases(
		[]music.Release{rel, rel},
		[][]music.Track{tracks, tracks},
	)
	if len(dedupedReleases) != 1 {
		t.Fatalf("expected 1 release after dedup, got %d", len(dedupedReleases))
	}
	if len(dedupedTracks) != 1 {
		t.Fatalf("expected 1 track list after dedup, got %d", len(dedupedTracks))
	}
}
