package artistgenres

import (
	"context"
	"reflect"
	"sync"
	"testing"
	"time"

	"ytdm/backend/internal/apperr"
	"ytdm/backend/internal/database/repository"
)

// fakeStore keeps candidates until a lookup is recorded, like the
// artist_genre_lookups table does.
type fakeStore struct {
	mu      sync.Mutex
	pending []repository.GenreCandidate
	stored  map[string][]string
	queries int
}

func (s *fakeStore) ArtistsMissingGenres(_ context.Context, provider string, _ time.Duration, limit int) ([]repository.GenreCandidate, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.queries++
	out := []repository.GenreCandidate{}
	for _, c := range s.pending {
		if _, done := s.stored[c.ArtistID]; !done && len(out) < limit {
			out = append(out, c)
		}
	}
	return out, nil
}

func (s *fakeStore) StoreArtistGenreLookup(_ context.Context, artistID string, genres []string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.stored[artistID] = genres
	return nil
}

func (s *fakeStore) ArtistMissingGenresBySource(_ context.Context, _, sourceID, _ string) (string, error) {
	if sourceID == "27" {
		return "lib-daft", nil
	}
	return "", nil
}

type fakeSource struct {
	mu       sync.Mutex
	requests []string
	fail     map[string]error
}

func (f *fakeSource) Name() string { return "deezer" }

func (f *fakeSource) ArtistGenres(_ context.Context, id string) ([]string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.requests = append(f.requests, "artist:"+id)
	if err := f.fail[id]; err != nil {
		return nil, err
	}
	if id == "0" {
		return nil, nil
	}
	return []string{"Genre " + id}, nil
}

func (f *fakeSource) AlbumArtist(_ context.Context, id string) (string, string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.requests = append(f.requests, "album:"+id)
	switch id {
	case "302127":
		return "27", "Daft Punk", nil
	case "1":
		return "5080", "Various Artists", nil
	}
	return "", "", apperr.New(apperr.CodeReleaseNotFound, "gone")
}

func candidates(n int) []repository.GenreCandidate {
	out := []repository.GenreCandidate{}
	for i := range n {
		out = append(out, repository.GenreCandidate{ArtistID: string(rune('a' + i)), Name: "x", SourceArtistID: string(rune('1' + i))})
	}
	return out
}

func newTest(store *fakeStore, source *fakeSource, max int) *Backfill {
	return New(Options{Store: store, Source: source, Pace: -1, MaxPerPass: max})
}

func TestPassIsBoundedAndRecordsEveryArtist(t *testing.T) {
	store := &fakeStore{pending: candidates(5), stored: map[string][]string{}}
	source := &fakeSource{}
	n, err := newTest(store, source, 3).RunPass(t.Context())
	if err != nil || n != 3 || len(source.requests) != 3 || len(store.stored) != 3 {
		t.Fatalf("first pass: %d %v %v", n, err, source.requests)
	}
	n, err = newTest(store, source, 3).RunPass(t.Context())
	if err != nil || n != 2 || len(store.stored) != 5 {
		t.Fatalf("second pass resumes: %d %v", n, err)
	}
	if n, _ = newTest(store, source, 3).RunPass(t.Context()); n != 0 {
		t.Fatal("a finished library is asked again")
	}
}

func TestProviderOutageStopsThePassWithoutRecording(t *testing.T) {
	store := &fakeStore{pending: candidates(3), stored: map[string][]string{}}
	source := &fakeSource{fail: map[string]error{"2": apperr.New(apperr.CodeProviderRateLimited, "slow down")}}
	n, err := newTest(store, source, 10).RunPass(t.Context())
	if apperr.CodeOf(err) != apperr.CodeProviderRateLimited || n != 1 {
		t.Fatalf("pass went on after a rate limit: %d %v", n, err)
	}
	if _, recorded := store.stored["b"]; recorded {
		t.Fatal("an artist the provider could not answer was recorded")
	}
	// An artist the provider does not know is recorded without genres.
	source.fail = map[string]error{"2": apperr.New(apperr.CodeArtistNotFound, "gone")}
	if _, err = newTest(store, source, 10).RunPass(t.Context()); err != nil {
		t.Fatal(err)
	}
	if genres, recorded := store.stored["b"]; !recorded || len(genres) != 0 {
		t.Fatalf("unknown artist not recorded: %v", store.stored)
	}
}

func TestAlbumFallbackOnlyTrustsTheSameArtist(t *testing.T) {
	store := &fakeStore{pending: []repository.GenreCandidate{
		{ArtistID: "daft", Name: "Daft Punk", SourceAlbumID: "302127"},
		{ArtistID: "comp", Name: "Someone", SourceAlbumID: "1"},
		{ArtistID: "gone", Name: "Gone", SourceAlbumID: "404"},
	}, stored: map[string][]string{}}
	source := &fakeSource{}
	if _, err := newTest(store, source, 10).RunPass(t.Context()); err != nil {
		t.Fatal(err)
	}
	want := map[string][]string{"daft": {"Genre 27"}, "comp": nil, "gone": nil}
	if !reflect.DeepEqual(store.stored, want) {
		t.Fatalf("stored %v", store.stored)
	}
	if !reflect.DeepEqual(source.requests, []string{"album:302127", "artist:27", "album:1", "album:404"}) {
		t.Fatalf("requests %v", source.requests)
	}
}

func TestEnrichSubscribed(t *testing.T) {
	store := &fakeStore{stored: map[string][]string{}}
	source := &fakeSource{}
	b := newTest(store, source, 10)
	b.EnrichSubscribed(t.Context(), "spotify", "27", "Daft Punk")
	b.EnrichSubscribed(t.Context(), "deezer", "99", "Not in library")
	if len(source.requests) != 0 {
		t.Fatalf("asked the provider without a library artist: %v", source.requests)
	}
	b.EnrichSubscribed(t.Context(), "deezer", "27", "Daft Punk")
	if !reflect.DeepEqual(store.stored["lib-daft"], []string{"Genre 27"}) {
		t.Fatalf("stored %v", store.stored)
	}
}

func TestStartWaitsAndStopsCleanly(t *testing.T) {
	store := &fakeStore{pending: candidates(3), stored: map[string][]string{}}
	b := New(Options{Store: store, Source: &fakeSource{}, StartDelay: time.Hour})
	b.Start(context.Background())
	done := make(chan struct{})
	go func() { b.Stop(); close(done) }()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Stop did not return")
	}
	if store.queries != 0 {
		t.Fatal("a pass ran before the start delay")
	}
	// Paced passes stop between two artists.
	ctx, cancel := context.WithCancel(context.Background())
	paced := New(Options{Store: store, Source: &fakeSource{}, Pace: time.Hour})
	go func() { time.Sleep(50 * time.Millisecond); cancel() }()
	if n, _ := paced.RunPass(ctx); n != 1 {
		t.Fatalf("paced pass handled %d artists before cancellation, want 1", n)
	}
}
