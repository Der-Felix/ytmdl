package spotify

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"ytdm/backend/internal/apperr"
	"ytdm/backend/internal/music"
)

// newTestProvider starts a fake Spotify API and returns a provider pointed at
// it. Tests never talk to the real service.
func newTestProvider(t *testing.T, handler http.HandlerFunc) *Provider {
	t.Helper()

	mux := http.NewServeMux()
	mux.HandleFunc("/token", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("token request used %s", r.Method)
		}
		if id, secret, ok := r.BasicAuth(); !ok || id != "client" || secret != "secret" {
			t.Errorf("token request carried the wrong credentials: %q/%q", id, secret)
		}
		writeJSON(w, map[string]any{"access_token": "token-123", "token_type": "Bearer", "expires_in": 3600})
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer token-123" {
			t.Errorf("Authorization = %q", got)
		}
		handler(w, r)
	})

	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)

	provider, err := New(Config{
		ClientID:     "client",
		ClientSecret: "secret",
		Market:       "DE",
		APIBaseURL:   server.URL,
		AuthURL:      server.URL + "/token",
		HTTPClient:   server.Client(),
	})
	if err != nil {
		t.Fatalf("new provider: %v", err)
	}
	return provider
}

func writeJSON(w http.ResponseWriter, payload any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(payload)
}

func TestNewRequiresCredentials(t *testing.T) {
	if _, err := New(Config{ClientSecret: "secret"}); err == nil {
		t.Fatal("expected an error without a client id")
	}
	if _, err := New(Config{ClientID: "client"}); err == nil {
		t.Fatal("expected an error without a client secret")
	}
}

func TestSearchArtists(t *testing.T) {
	provider := newTestProvider(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/search" {
			t.Fatalf("unexpected path %q", r.URL.Path)
		}
		if got := r.URL.Query().Get("type"); got != "artist" {
			t.Errorf("type = %q", got)
		}
		if got := r.URL.Query().Get("market"); got != "DE" {
			t.Errorf("market = %q", got)
		}
		writeJSON(w, map[string]any{"artists": map[string]any{"items": []any{
			map[string]any{
				"id": "a1", "name": "Artist", "popularity": 77,
				"genres": []string{"rock"},
				"images": []any{
					map[string]any{"url": "https://cdn.test/small.jpg", "width": 160},
					map[string]any{"url": "https://cdn.test/large.jpg", "width": 640},
				},
				"external_urls": map[string]any{"spotify": "https://open.spotify.com/artist/a1"},
			},
			map[string]any{"id": "", "name": "Broken"},
		}}})
	})

	artists, err := provider.SearchArtists(context.Background(), "artist")
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(artists) != 1 {
		t.Fatalf("got %d artists, want 1 (entries without an id must be dropped)", len(artists))
	}
	got := artists[0]
	if got.Name != "Artist" || got.Provider != ProviderName || got.SourceID != "a1" {
		t.Errorf("artist = %+v", got)
	}
	if got.ImageURL != "https://cdn.test/large.jpg" {
		t.Errorf("image = %q, want the largest one", got.ImageURL)
	}
}

func TestSearchArtistsRejectsEmptyQuery(t *testing.T) {
	provider := newTestProvider(t, func(http.ResponseWriter, *http.Request) {
		t.Error("no request should be sent for an empty query")
	})
	if _, err := provider.SearchArtists(context.Background(), "   "); apperr.CodeOf(err) != apperr.CodeInvalidRequest {
		t.Fatalf("code = %s, want %s", apperr.CodeOf(err), apperr.CodeInvalidRequest)
	}
}

func TestGetArtistNotFound(t *testing.T) {
	provider := newTestProvider(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		writeJSON(w, map[string]any{"error": map[string]any{"message": "not found"}})
	})
	_, err := provider.GetArtist(context.Background(), "a1")
	if code := apperr.CodeOf(err); code != apperr.CodeArtistNotFound {
		t.Fatalf("code = %s, want %s", code, apperr.CodeArtistNotFound)
	}
}

func TestGetArtistRejectsMalformedID(t *testing.T) {
	provider := newTestProvider(t, func(http.ResponseWriter, *http.Request) {
		t.Error("no request should be sent for a malformed id")
	})
	for _, id := range []string{"", "../albums", "a1/tracks", "a 1"} {
		if _, err := provider.GetArtist(context.Background(), id); apperr.CodeOf(err) != apperr.CodeInvalidRequest {
			t.Errorf("id %q: code = %s, want %s", id, apperr.CodeOf(err), apperr.CodeInvalidRequest)
		}
	}
}

func TestGetDiscographyPaginates(t *testing.T) {
	pages := 0
	provider := newTestProvider(t, func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/albums") {
			t.Fatalf("unexpected path %q", r.URL.Path)
		}
		if got := r.URL.Query().Get("include_groups"); got != "album,single,compilation" {
			t.Errorf("include_groups = %q", got)
		}

		offset := r.URL.Query().Get("offset")
		pages++
		if offset == "0" {
			items := make([]any, 0, 50)
			for i := range 50 {
				items = append(items, map[string]any{
					"id":   "album" + string(rune('a'+i%26)) + string(rune('a'+i/26)),
					"name": "Album", "album_type": "album", "total_tracks": 10,
					"release_date": "2001-05-04",
				})
			}
			writeJSON(w, map[string]any{"items": items, "next": "more", "total": 51})
			return
		}
		writeJSON(w, map[string]any{"items": []any{
			map[string]any{"id": "single1", "name": "Song", "album_type": "single", "total_tracks": 1, "release_date": "2020"},
		}, "next": nil})
	})

	releases, err := provider.GetDiscography(context.Background(), "a1")
	if err != nil {
		t.Fatalf("discography: %v", err)
	}
	if pages != 2 {
		t.Errorf("requested %d pages, want 2", pages)
	}
	if len(releases) != 51 {
		t.Fatalf("got %d releases, want 51", len(releases))
	}
	last := releases[len(releases)-1]
	if last.ReleaseType != music.ReleaseSingle || last.Year != 2020 {
		t.Errorf("last release = %+v", last)
	}
}

func TestGetReleaseTracksFillsISRC(t *testing.T) {
	var trackBatch string
	provider := newTestProvider(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/albums/al1":
			writeJSON(w, map[string]any{
				"id": "al1", "name": "Album", "album_type": "album", "total_tracks": 2,
				"release_date": "1999-01-02",
				"artists":      []any{map[string]any{"id": "a1", "name": "Artist"}},
				"images":       []any{map[string]any{"url": "https://cdn.test/cover.jpg", "width": 640}},
			})
		case r.URL.Path == "/albums/al1/tracks":
			writeJSON(w, map[string]any{"items": []any{
				map[string]any{
					"id": "t1", "name": "One", "track_number": 1, "disc_number": 1, "duration_ms": 205000,
					"artists": []any{map[string]any{"name": "Artist"}},
				},
				map[string]any{
					"id": "t2", "name": "Two", "track_number": 2, "disc_number": 1, "duration_ms": 190000,
					"artists": []any{map[string]any{"name": "Artist"}},
				},
				map[string]any{"id": "local", "name": "Local", "is_local": true},
			}, "next": nil})
		case r.URL.Path == "/tracks":
			trackBatch = r.URL.Query().Get("ids")
			writeJSON(w, map[string]any{"tracks": []any{
				map[string]any{"id": "t1", "external_ids": map[string]any{"isrc": "DEA123456789"}},
				map[string]any{"id": "t2", "external_ids": map[string]any{"isrc": "DEA987654321"}},
			}})
		default:
			t.Fatalf("unexpected path %q", r.URL.Path)
		}
	})

	tracks, err := provider.GetReleaseTracks(context.Background(), "al1")
	if err != nil {
		t.Fatalf("tracks: %v", err)
	}
	if len(tracks) != 2 {
		t.Fatalf("got %d tracks, want 2 (local files must be dropped)", len(tracks))
	}
	if trackBatch != "t1,t2" {
		t.Errorf("ids = %q, want the two track ids in one batch", trackBatch)
	}
	if tracks[0].ISRC != "DEA123456789" || tracks[1].ISRC != "DEA987654321" {
		t.Errorf("ISRCs were not filled in: %q / %q", tracks[0].ISRC, tracks[1].ISRC)
	}
	if tracks[0].Album != "Album" || tracks[0].Year != 1999 || tracks[0].AlbumArtist != "Artist" {
		t.Errorf("album context is missing: %+v", tracks[0])
	}
	if tracks[0].CoverURL != "https://cdn.test/cover.jpg" {
		t.Errorf("cover = %q", tracks[0].CoverURL)
	}
	if tracks[0].TrackTotal != 2 || tracks[0].DiscTotal != 1 {
		t.Errorf("totals = %d/%d, want 2/1", tracks[0].TrackTotal, tracks[0].DiscTotal)
	}
}

func TestRateLimitIsReported(t *testing.T) {
	provider := newTestProvider(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Retry-After", "17")
		w.WriteHeader(http.StatusTooManyRequests)
	})
	_, err := provider.SearchArtists(context.Background(), "artist")
	if code := apperr.CodeOf(err); code != apperr.CodeProviderRateLimited {
		t.Fatalf("code = %s, want %s", code, apperr.CodeProviderRateLimited)
	}
	if !strings.Contains(apperr.MessageOf(err), "17") {
		t.Errorf("message %q should mention the retry delay", apperr.MessageOf(err))
	}
}

func TestExpiredTokenIsRefreshed(t *testing.T) {
	calls := 0
	provider := newTestProvider(t, func(w http.ResponseWriter, r *http.Request) {
		calls++
		if calls == 1 {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		writeJSON(w, map[string]any{"artists": map[string]any{"items": []any{
			map[string]any{"id": "a1", "name": "Artist"},
		}}})
	})

	artists, err := provider.SearchArtists(context.Background(), "artist")
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(artists) != 1 {
		t.Fatalf("got %d artists, want 1", len(artists))
	}
	if calls != 2 {
		t.Errorf("made %d calls, want 2 (one rejected, one after the refresh)", calls)
	}
}

func TestClassifyRelease(t *testing.T) {
	tests := []struct {
		albumType   string
		totalTracks int
		title       string
		want        music.ReleaseType
	}{
		{"album", 12, "Album", music.ReleaseAlbum},
		{"single", 1, "Song", music.ReleaseSingle},
		{"single", 3, "Song", music.ReleaseSingle},
		{"single", 5, "Short Collection", music.ReleaseEP},
		{"compilation", 40, "Greatest Hits", music.ReleaseCompilation},
		{"album", 12, "Live at Wembley", music.ReleaseLive},
		{"album", 10, "The Remixes", music.ReleaseRemix},
		{"album", 5, "Debut EP", music.ReleaseEP},
		{"album", 12, "Unplugged", music.ReleaseLive},
	}
	for _, tc := range tests {
		if got := classifyRelease(tc.albumType, tc.totalTracks, tc.title); got != tc.want {
			t.Errorf("classifyRelease(%q, %d, %q) = %q, want %q",
				tc.albumType, tc.totalTracks, tc.title, got, tc.want)
		}
	}
}

func TestYearFromReleaseDate(t *testing.T) {
	tests := []struct {
		in   string
		want int
	}{
		{"2001-05-04", 2001}, {"1999-01", 1999}, {"1975", 1975},
		{"", 0}, {"abc", 0}, {"0001", 0},
	}
	for _, tc := range tests {
		if got := yearFromReleaseDate(tc.in); got != tc.want {
			t.Errorf("yearFromReleaseDate(%q) = %d, want %d", tc.in, got, tc.want)
		}
	}
}

func TestApplyDiscTotals(t *testing.T) {
	tracks := []music.Track{
		{DiscNumber: 1, TrackNumber: 1}, {DiscNumber: 1, TrackNumber: 2},
		{DiscNumber: 2, TrackNumber: 1}, {DiscNumber: 2, TrackNumber: 2}, {DiscNumber: 2, TrackNumber: 3},
	}
	applyDiscTotals(tracks)

	for _, track := range tracks {
		if track.DiscTotal != 2 {
			t.Fatalf("disc total = %d, want 2", track.DiscTotal)
		}
	}
	if tracks[0].TrackTotal != 2 {
		t.Errorf("disc one track total = %d, want 2", tracks[0].TrackTotal)
	}
	if tracks[4].TrackTotal != 3 {
		t.Errorf("disc two track total = %d, want 3", tracks[4].TrackTotal)
	}
}

func TestParallelTokenRefresh(t *testing.T) {
	var tokenCalls atomic.Int32
	mux := http.NewServeMux()
	mux.HandleFunc("/token", func(w http.ResponseWriter, r *http.Request) {
		tokenCalls.Add(1)
		time.Sleep(20 * time.Millisecond)
		writeJSON(w, map[string]any{"access_token": "concurrent-token", "token_type": "Bearer", "expires_in": 3600})
	})
	mux.HandleFunc("/artists/a1", func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer concurrent-token" {
			t.Errorf("Authorization = %q", got)
		}
		writeJSON(w, map[string]any{"id": "a1", "name": "Artist 1"})
	})

	server := httptest.NewServer(mux)
	defer server.Close()

	provider, err := New(Config{
		ClientID:     "client",
		ClientSecret: "secret",
		APIBaseURL:   server.URL,
		AuthURL:      server.URL + "/token",
		HTTPClient:   server.Client(),
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	const goroutines = 20
	var wg sync.WaitGroup
	errs := make(chan error, goroutines)

	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			artist, err := provider.GetArtist(context.Background(), "a1")
			if err != nil {
				errs <- err
				return
			}
			if artist.Name != "Artist 1" {
				errs <- fmt.Errorf("unexpected name: %s", artist.Name)
			}
		}()
	}

	wg.Wait()
	close(errs)

	for err := range errs {
		t.Fatalf("concurrent GetArtist failed: %v", err)
	}

	if calls := tokenCalls.Load(); calls != 1 {
		t.Fatalf("token endpoint called %d times, want 1", calls)
	}
}

func TestAccessTokenErrors(t *testing.T) {
	tests := []struct {
		name       string
		statusCode int
		body       any
		wantCode   apperr.Code
	}{
		{
			name:       "401 unauthorized (bad credentials)",
			statusCode: http.StatusUnauthorized,
			body:       map[string]any{"error": "invalid_client"},
			wantCode:   apperr.CodeProviderUnavailable,
		},
		{
			name:       "403 forbidden",
			statusCode: http.StatusForbidden,
			body:       map[string]any{"error": "forbidden"},
			wantCode:   apperr.CodeProviderUnavailable,
		},
		{
			name:       "429 rate limit",
			statusCode: http.StatusTooManyRequests,
			body:       map[string]any{"error": "too_many_requests"},
			wantCode:   apperr.CodeProviderRateLimited,
		},
		{
			name:       "500 server error",
			statusCode: http.StatusInternalServerError,
			body:       map[string]any{"error": "server_error"},
			wantCode:   apperr.CodeProviderUnavailable,
		},
		{
			name:       "empty access token",
			statusCode: http.StatusOK,
			body:       map[string]any{"access_token": "", "expires_in": 3600},
			wantCode:   apperr.CodeProviderUnavailable,
		},
		{
			name:       "malformed JSON",
			statusCode: http.StatusOK,
			body:       "not-valid-json",
			wantCode:   apperr.CodeProviderUnavailable,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tc.statusCode)
				if str, ok := tc.body.(string); ok {
					_, _ = w.Write([]byte(str))
				} else {
					writeJSON(w, tc.body)
				}
			}))
			defer server.Close()

			provider, err := New(Config{
				ClientID:     "client",
				ClientSecret: "secret",
				APIBaseURL:   server.URL,
				AuthURL:      server.URL + "/token",
				HTTPClient:   server.Client(),
			})
			if err != nil {
				t.Fatalf("New: %v", err)
			}

			_, err = provider.GetArtist(context.Background(), "a1")
			if code := apperr.CodeOf(err); code != tc.wantCode {
				t.Fatalf("code = %s, want %s (err: %v)", code, tc.wantCode, err)
			}
		})
	}
}

func TestProviderAvailable(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		provider := newTestProvider(t, func(w http.ResponseWriter, r *http.Request) {})
		if err := provider.Available(context.Background()); err != nil {
			t.Fatalf("Available() returned error: %v", err)
		}
	})

	t.Run("failure on auth error", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusUnauthorized)
		}))
		defer server.Close()

		provider, err := New(Config{
			ClientID:     "bad_client",
			ClientSecret: "bad_secret",
			APIBaseURL:   server.URL,
			AuthURL:      server.URL + "/token",
			HTTPClient:   server.Client(),
		})
		if err != nil {
			t.Fatalf("New: %v", err)
		}

		if err := provider.Available(context.Background()); err == nil {
			t.Fatal("Available() want error, got nil")
		}
	})
}

func TestGetRelease(t *testing.T) {
	provider := newTestProvider(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/albums/al1":
			if r.URL.Query().Get("market") != "DE" {
				t.Errorf("market = %q", r.URL.Query().Get("market"))
			}
			writeJSON(w, map[string]any{
				"id": "al1", "name": "Album Title", "album_type": "album", "total_tracks": 12,
				"release_date": "2022-03-15",
				"artists":      []any{map[string]any{"id": "a1", "name": "Main Artist"}, map[string]any{"id": "a2", "name": "Featured"}},
				"images": []any{
					map[string]any{"url": "https://cdn.test/cover_small.jpg", "width": 300},
					map[string]any{"url": "https://cdn.test/cover_large.jpg", "width": 640},
				},
				"external_urls": map[string]any{"spotify": "https://open.spotify.com/album/al1"},
			})
		case "/albums/al404":
			w.WriteHeader(http.StatusNotFound)
			writeJSON(w, map[string]any{"error": map[string]any{"message": "album not found"}})
		default:
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
	})

	t.Run("success", func(t *testing.T) {
		rel, err := provider.GetRelease(context.Background(), "al1")
		if err != nil {
			t.Fatalf("GetRelease: %v", err)
		}
		if rel.ID != "al1" || rel.Title != "Album Title" || rel.ReleaseType != music.ReleaseAlbum {
			t.Errorf("release = %+v", rel)
		}
		if rel.Year != 2022 || rel.ReleaseDate != "2022-03-15" || rel.TrackCount != 12 {
			t.Errorf("release metadata = %+v", rel)
		}
		if rel.AlbumArtist != "Main Artist" || len(rel.Artists) != 2 {
			t.Errorf("artists = %+v", rel.Artists)
		}
		if rel.CoverURL != "https://cdn.test/cover_large.jpg" {
			t.Errorf("cover = %q", rel.CoverURL)
		}
		if rel.SourceURL != "https://open.spotify.com/album/al1" {
			t.Errorf("source_url = %q", rel.SourceURL)
		}
	})

	t.Run("not found", func(t *testing.T) {
		_, err := provider.GetRelease(context.Background(), "al404")
		if code := apperr.CodeOf(err); code != apperr.CodeReleaseNotFound {
			t.Fatalf("code = %s, want %s", code, apperr.CodeReleaseNotFound)
		}
	})

	t.Run("malformed id", func(t *testing.T) {
		for _, id := range []string{"", "bad id", "../bad", "al/1"} {
			if _, err := provider.GetRelease(context.Background(), id); apperr.CodeOf(err) != apperr.CodeInvalidRequest {
				t.Errorf("id %q: code = %s, want %s", id, apperr.CodeOf(err), apperr.CodeInvalidRequest)
			}
		}
	})
}

func TestGetTrack(t *testing.T) {
	provider := newTestProvider(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/tracks/t1":
			if r.URL.Query().Get("market") != "DE" {
				t.Errorf("market = %q", r.URL.Query().Get("market"))
			}
			writeJSON(w, map[string]any{
				"id": "t1", "name": "Track Name", "track_number": 3, "disc_number": 1,
				"duration_ms": 215000,
				"artists":     []any{map[string]any{"id": "a1", "name": "Artist Name"}},
				"album": map[string]any{
					"id": "al1", "name": "Album Name", "album_type": "album", "total_tracks": 10,
					"release_date":  "2020-05-10",
					"images":        []any{map[string]any{"url": "https://cdn.test/cover.jpg", "width": 640}},
					"artists":       []any{map[string]any{"id": "a1", "name": "Artist Name"}},
					"external_urls": map[string]any{"spotify": "https://open.spotify.com/album/al1"},
				},
				"external_ids":  map[string]any{"isrc": "USRC12345678"},
				"external_urls": map[string]any{"spotify": "https://open.spotify.com/track/t1"},
			})
		case "/tracks/t404":
			w.WriteHeader(http.StatusNotFound)
			writeJSON(w, map[string]any{"error": map[string]any{"message": "track not found"}})
		case "/tracks/tEmpty":
			writeJSON(w, map[string]any{"id": ""})
		default:
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
	})

	t.Run("success", func(t *testing.T) {
		tr, err := provider.GetTrack(context.Background(), "t1")
		if err != nil {
			t.Fatalf("GetTrack: %v", err)
		}
		if tr.ID != "t1" || tr.Title != "Track Name" || tr.TrackNumber != 3 || tr.DiscNumber != 1 {
			t.Errorf("track = %+v", tr)
		}
		if tr.ISRC != "USRC12345678" || tr.DurationMS != 215000 {
			t.Errorf("ISRC / duration = %q / %d", tr.ISRC, tr.DurationMS)
		}
		if tr.Album != "Album Name" || tr.AlbumArtist != "Artist Name" || tr.Year != 2020 {
			t.Errorf("album context = %+v", tr)
		}
		if tr.CoverURL != "https://cdn.test/cover.jpg" {
			t.Errorf("cover = %q", tr.CoverURL)
		}
		if tr.SourceURL != "https://open.spotify.com/track/t1" {
			t.Errorf("source_url = %q", tr.SourceURL)
		}
		if tr.DiscTotal != 1 {
			t.Errorf("disc total = %d, want 1", tr.DiscTotal)
		}
	})

	t.Run("not found", func(t *testing.T) {
		_, err := provider.GetTrack(context.Background(), "t404")
		if code := apperr.CodeOf(err); code != apperr.CodeTrackNotFound {
			t.Fatalf("code = %s, want %s", code, apperr.CodeTrackNotFound)
		}
	})

	t.Run("empty track id payload", func(t *testing.T) {
		_, err := provider.GetTrack(context.Background(), "tEmpty")
		if code := apperr.CodeOf(err); code != apperr.CodeTrackNotFound {
			t.Fatalf("code = %s, want %s", code, apperr.CodeTrackNotFound)
		}
	})

	t.Run("malformed id", func(t *testing.T) {
		for _, id := range []string{"", "bad track", "../bad"} {
			if _, err := provider.GetTrack(context.Background(), id); apperr.CodeOf(err) != apperr.CodeInvalidRequest {
				t.Errorf("id %q: code = %s, want %s", id, apperr.CodeOf(err), apperr.CodeInvalidRequest)
			}
		}
	})
}

func TestGetDiscography_PaginationVariants(t *testing.T) {
	t.Run("empty discography", func(t *testing.T) {
		provider := newTestProvider(t, func(w http.ResponseWriter, r *http.Request) {
			writeJSON(w, map[string]any{"items": []any{}, "next": nil, "total": 0})
		})
		releases, err := provider.GetDiscography(context.Background(), "a1")
		if err != nil {
			t.Fatalf("GetDiscography: %v", err)
		}
		if len(releases) != 0 {
			t.Fatalf("got %d releases, want 0", len(releases))
		}
	})

	t.Run("deduplicates across pages", func(t *testing.T) {
		provider := newTestProvider(t, func(w http.ResponseWriter, r *http.Request) {
			offset := r.URL.Query().Get("offset")
			if offset == "0" {
				items := make([]any, 50)
				for i := range 50 {
					items[i] = map[string]any{"id": "album1", "name": "Album", "album_type": "album", "total_tracks": 10, "release_date": "2020"}
				}
				writeJSON(w, map[string]any{"items": items, "next": "page2", "total": 51})
				return
			}
			writeJSON(w, map[string]any{"items": []any{
				map[string]any{"id": "album1", "name": "Album", "album_type": "album", "total_tracks": 10, "release_date": "2020"},
			}, "next": nil})
		})

		releases, err := provider.GetDiscography(context.Background(), "a1")
		if err != nil {
			t.Fatalf("GetDiscography: %v", err)
		}
		if len(releases) != 1 {
			t.Fatalf("got %d releases, want 1 (dedup by ID)", len(releases))
		}
	})
}

func TestGetReleaseTracks_MultiBatchISRC(t *testing.T) {
	batchCalls := 0
	var receivedIDs []string

	provider := newTestProvider(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/albums/big1":
			writeJSON(w, map[string]any{
				"id": "big1", "name": "Big Album", "album_type": "album", "total_tracks": 60,
				"release_date": "2021-01-01",
				"artists":      []any{map[string]any{"id": "a1", "name": "Big Artist"}},
			})
		case r.URL.Path == "/albums/big1/tracks":
			offset := r.URL.Query().Get("offset")
			if offset == "0" {
				items := make([]any, 50)
				for i := range 50 {
					items[i] = map[string]any{
						"id": fmt.Sprintf("track%02d", i+1), "name": fmt.Sprintf("Track %d", i+1),
						"track_number": i + 1, "disc_number": 1, "duration_ms": 180000,
						"artists": []any{map[string]any{"name": "Big Artist"}},
					}
				}
				writeJSON(w, map[string]any{"items": items, "next": "more", "total": 60})
				return
			}
			items := make([]any, 10)
			for i := range 10 {
				items[i] = map[string]any{
					"id": fmt.Sprintf("track%02d", i+51), "name": fmt.Sprintf("Track %d", i+51),
					"track_number": i + 51, "disc_number": 1, "duration_ms": 180000,
					"artists": []any{map[string]any{"name": "Big Artist"}},
				}
			}
			writeJSON(w, map[string]any{"items": items, "next": nil, "total": 60})
		case r.URL.Path == "/tracks":
			batchCalls++
			idsStr := r.URL.Query().Get("ids")
			ids := strings.Split(idsStr, ",")
			receivedIDs = append(receivedIDs, ids...)
			items := make([]any, len(ids))
			for i, id := range ids {
				items[i] = map[string]any{
					"id":           id,
					"external_ids": map[string]any{"isrc": fmt.Sprintf("ISRC%s", id)},
				}
			}
			writeJSON(w, map[string]any{"tracks": items})
		default:
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
	})

	tracks, err := provider.GetReleaseTracks(context.Background(), "big1")
	if err != nil {
		t.Fatalf("GetReleaseTracks: %v", err)
	}
	if len(tracks) != 60 {
		t.Fatalf("got %d tracks, want 60", len(tracks))
	}
	if batchCalls != 2 {
		t.Fatalf("batchCalls = %d, want 2", batchCalls)
	}
	if len(receivedIDs) != 60 {
		t.Fatalf("received %d IDs across batches, want 60", len(receivedIDs))
	}
	for i, tr := range tracks {
		wantISRC := fmt.Sprintf("ISRCtrack%02d", i+1)
		if tr.ISRC != wantISRC {
			t.Errorf("track %d ISRC = %q, want %q", i+1, tr.ISRC, wantISRC)
		}
	}
}

func TestAPIErrorHandling(t *testing.T) {
	tests := []struct {
		name       string
		statusCode int
		body       any
		wantCode   apperr.Code
	}{
		{
			name:       "403 Forbidden",
			statusCode: http.StatusForbidden,
			body:       map[string]any{"error": map[string]any{"message": "Forbidden access"}},
			wantCode:   apperr.CodeProviderUnavailable,
		},
		{
			name:       "500 Internal Server Error",
			statusCode: http.StatusInternalServerError,
			body:       map[string]any{"error": map[string]any{"message": "Server error"}},
			wantCode:   apperr.CodeProviderUnavailable,
		},
		{
			name:       "502 Bad Gateway",
			statusCode: http.StatusBadGateway,
			body:       "Bad Gateway html response",
			wantCode:   apperr.CodeProviderUnavailable,
		},
		{
			name:       "malformed json payload",
			statusCode: http.StatusOK,
			body:       "not a json response",
			wantCode:   apperr.CodeProviderUnavailable,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			provider := newTestProvider(t, func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tc.statusCode)
				if s, ok := tc.body.(string); ok {
					_, _ = w.Write([]byte(s))
				} else {
					writeJSON(w, tc.body)
				}
			})

			_, err := provider.GetArtist(context.Background(), "a1")
			if code := apperr.CodeOf(err); code != tc.wantCode {
				t.Fatalf("code = %s, want %s (err: %v)", code, tc.wantCode, err)
			}
		})
	}
}

func TestApplyDiscTotalsSetsTotalsOnASingleDiscRelease(t *testing.T) {
	tracks := []music.Track{
		{Title: "A", TrackNumber: 1, DiscNumber: 1},
		{Title: "B", TrackNumber: 2, DiscNumber: 1},
	}
	applyDiscTotals(tracks)
	for i, track := range tracks {
		if track.TrackTotal != 2 || track.DiscTotal != 1 {
			t.Errorf("track %d: totals = %d/%d, want 2 and 1", i, track.TrackTotal, track.DiscTotal)
		}
	}
}

func TestToReleaseKeepsAStructuredArtistName(t *testing.T) {
	album := apiAlbum{
		ID: "a1", Name: "Bridge Over Troubled Water", AlbumType: "album", TotalTracks: 11,
		Artists: []apiArtist{{ID: "s1", Name: "Simon & Garfunkel"}},
	}
	if got := toRelease(album).AlbumArtist; got != "Simon & Garfunkel" {
		t.Errorf("AlbumArtist = %q, want Simon & Garfunkel", got)
	}
}
