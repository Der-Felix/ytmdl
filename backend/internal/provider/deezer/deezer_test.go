package deezer

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"ytdm/backend/internal/apperr"
	"ytdm/backend/internal/music"
)

// newTestProvider builds a provider against a stub server.
//
// Pacing is switched off: these tests are about the endpoints, not about the
// request budget, and the production rate of eight per second would otherwise
// add seconds to every paginating case. The pacing itself is covered by the
// limiter tests and by TestPacingIsSharedAcrossConcurrentRequests.
func newTestProvider(t *testing.T, handler http.HandlerFunc) *Provider {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)

	return New(Config{
		APIBaseURL:        server.URL,
		HTTPClient:        server.Client(),
		RequestsPerSecond: -1,
		// Retrying stays on so these cases still traverse the real path, but
		// the waits are negligible: what they assert is the error code, not
		// the timing.
		RetryBackoff:    time.Millisecond,
		MaxRetryBackoff: time.Millisecond,
	})
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

func TestSearchArtists(t *testing.T) {
	provider := newTestProvider(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/search/artist" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		if q := r.URL.Query().Get("q"); q != "Daft Punk" {
			t.Fatalf("unexpected query: %s", q)
		}

		writeJSON(w, map[string]any{
			"data": []map[string]any{
				{
					"id":             27,
					"name":           "Daft Punk",
					"link":           "https://www.deezer.com/artist/27",
					"picture_small":  "https://cdn.dzcdn.net/small.jpg",
					"picture_medium": "https://cdn.dzcdn.net/medium.jpg",
					"picture_big":    "https://cdn.dzcdn.net/big.jpg",
					"picture_xl":     "https://cdn.dzcdn.net/xl.jpg",
					"nb_album":       38,
				},
			},
			"total": 1,
		})
	})

	artists, err := provider.SearchArtists(context.Background(), "Daft Punk")
	if err != nil {
		t.Fatalf("SearchArtists: %v", err)
	}
	if len(artists) != 1 {
		t.Fatalf("got %d artists, want 1", len(artists))
	}
	a := artists[0]
	if a.ID != "27" || a.Name != "Daft Punk" || a.Provider != "deezer" {
		t.Errorf("unexpected artist: %+v", a)
	}
	if a.ImageURL != "https://cdn.dzcdn.net/xl.jpg" {
		t.Errorf("image = %q, want xl", a.ImageURL)
	}
}

func TestSearchArtists_EmptyQuery(t *testing.T) {
	provider := newTestProvider(t, func(w http.ResponseWriter, r *http.Request) {})
	if _, err := provider.SearchArtists(context.Background(), "   "); apperr.CodeOf(err) != apperr.CodeInvalidRequest {
		t.Errorf("code = %v, want INVALID_REQUEST", apperr.CodeOf(err))
	}
}

func TestGetArtist(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		provider := newTestProvider(t, func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/artist/27" {
				t.Fatalf("unexpected path: %s", r.URL.Path)
			}
			writeJSON(w, map[string]any{
				"id":         27,
				"name":       "Daft Punk",
				"link":       "https://www.deezer.com/artist/27",
				"picture_xl": "https://cdn.dzcdn.net/xl.jpg",
				"nb_album":   38,
			})
		})

		artist, err := provider.GetArtist(context.Background(), "27")
		if err != nil {
			t.Fatalf("GetArtist: %v", err)
		}
		if artist.ID != "27" || artist.Name != "Daft Punk" || artist.Provider != "deezer" {
			t.Errorf("unexpected artist: %+v", artist)
		}
	})

	t.Run("not found (Deezer error payload)", func(t *testing.T) {
		provider := newTestProvider(t, func(w http.ResponseWriter, r *http.Request) {
			writeJSON(w, map[string]any{
				"error": map[string]any{
					"type":    "DataException",
					"message": "no data",
					"code":    800,
				},
			})
		})

		_, err := provider.GetArtist(context.Background(), "999999")
		if code := apperr.CodeOf(err); code != apperr.CodeArtistNotFound {
			t.Fatalf("code = %s, want %s (err: %v)", code, apperr.CodeArtistNotFound, err)
		}
	})

	t.Run("malformed ID", func(t *testing.T) {
		provider := newTestProvider(t, func(w http.ResponseWriter, r *http.Request) {})
		for _, id := range []string{"", "abc", "-5", "27a", "12 34"} {
			if _, err := provider.GetArtist(context.Background(), id); apperr.CodeOf(err) != apperr.CodeInvalidRequest {
				t.Errorf("id %q: code = %v, want INVALID_REQUEST", id, apperr.CodeOf(err))
			}
		}
	})
}

func TestGetDiscography_Pagination(t *testing.T) {
	t.Run("multi-page with deduplication", func(t *testing.T) {
		var serverURL string
		provider := newTestProvider(t, func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/artist/27/albums" {
				index := r.URL.Query().Get("index")
				if index == "" || index == "0" {
					writeJSON(w, map[string]any{
						"data": []map[string]any{
							{
								"id":           101,
								"title":        "Album One",
								"record_type":  "album",
								"release_date": "2020-01-01",
								"nb_tracks":    10,
							},
							{
								"id":           102,
								"title":        "Single One",
								"record_type":  "single",
								"release_date": "2020-05-01",
								"nb_tracks":    1,
							},
						},
						"total": 3,
						"next":  serverURL + "/artist/27/albums?limit=50&index=2",
					})
					return
				}
				if index == "2" {
					writeJSON(w, map[string]any{
						"data": []map[string]any{
							{
								"id":           101, // duplicate of page 1
								"title":        "Album One",
								"record_type":  "album",
								"release_date": "2020-01-01",
								"nb_tracks":    10,
							},
							{
								"id":           103,
								"title":        "EP One",
								"record_type":  "ep",
								"release_date": "2021-01-01",
								"nb_tracks":    4,
							},
						},
						"total": 3,
						"next":  "",
					})
					return
				}
			}
			t.Fatalf("unexpected request: %s", r.URL.String())
		})
		serverURL = provider.client.baseURL

		releases, err := provider.GetDiscography(context.Background(), "27")
		if err != nil {
			t.Fatalf("GetDiscography: %v", err)
		}
		if len(releases) != 3 {
			t.Fatalf("got %d releases, want 3 (deduplicated)", len(releases))
		}
		if releases[0].ID != "101" || releases[1].ID != "102" || releases[2].ID != "103" {
			t.Errorf("unexpected release IDs: %+v", releases)
		}
		if releases[1].ReleaseType != music.ReleaseSingle || releases[2].ReleaseType != music.ReleaseEP {
			t.Errorf("unexpected types: r1=%v, r2=%v", releases[1].ReleaseType, releases[2].ReleaseType)
		}
	})

	t.Run("empty discography", func(t *testing.T) {
		provider := newTestProvider(t, func(w http.ResponseWriter, r *http.Request) {
			writeJSON(w, map[string]any{"data": []any{}, "total": 0})
		})

		releases, err := provider.GetDiscography(context.Background(), "27")
		if err != nil {
			t.Fatalf("GetDiscography: %v", err)
		}
		if len(releases) != 0 {
			t.Fatalf("got %d releases, want 0", len(releases))
		}
	})
}

func TestGetRelease(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		provider := newTestProvider(t, func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/album/6575789" {
				t.Fatalf("unexpected path: %s", r.URL.Path)
			}
			writeJSON(w, map[string]any{
				"id":           6575789,
				"title":        "Random Access Memories",
				"release_date": "2013-05-17",
				"record_type":  "album",
				"nb_tracks":    13,
				"cover_xl":     "https://cdn.dzcdn.net/ram_xl.jpg",
				"artist": map[string]any{
					"id":   27,
					"name": "Daft Punk",
				},
				"contributors": []map[string]any{
					{"id": 27, "name": "Daft Punk"},
				},
			})
		})

		rel, err := provider.GetRelease(context.Background(), "6575789")
		if err != nil {
			t.Fatalf("GetRelease: %v", err)
		}
		if rel.ID != "6575789" || rel.Title != "Random Access Memories" || rel.Year != 2013 {
			t.Errorf("unexpected release: %+v", rel)
		}
		if rel.ReleaseType != music.ReleaseAlbum || rel.TrackCount != 13 {
			t.Errorf("type=%v, count=%d", rel.ReleaseType, rel.TrackCount)
		}
		if rel.AlbumArtist != "Daft Punk" || len(rel.Artists) != 1 || rel.Artists[0] != "Daft Punk" {
			t.Errorf("artists=%+v, albumArtist=%q", rel.Artists, rel.AlbumArtist)
		}
	})

	t.Run("not found", func(t *testing.T) {
		provider := newTestProvider(t, func(w http.ResponseWriter, r *http.Request) {
			writeJSON(w, map[string]any{
				"error": map[string]any{
					"type":    "DataException",
					"message": "no data",
					"code":    800,
				},
			})
		})

		_, err := provider.GetRelease(context.Background(), "999999")
		if code := apperr.CodeOf(err); code != apperr.CodeReleaseNotFound {
			t.Fatalf("code = %s, want %s", code, apperr.CodeReleaseNotFound)
		}
	})
}

func TestGetReleaseTracks(t *testing.T) {
	provider := newTestProvider(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/album/6575789":
			writeJSON(w, map[string]any{
				"id":           6575789,
				"title":        "Random Access Memories",
				"release_date": "2013-05-17",
				"record_type":  "album",
				"cover_xl":     "https://cdn.dzcdn.net/cover.jpg",
				"artist":       map[string]any{"id": 27, "name": "Daft Punk"},
			})
		case "/album/6575789/tracks":
			writeJSON(w, map[string]any{
				"data": []map[string]any{
					{
						"id":             67238728,
						"title":          "Give Life Back to Music",
						"isrc":           "USQX91300101",
						"duration":       274,
						"track_position": 1,
						"disk_number":    1,
						"artist":         map[string]any{"id": 27, "name": "Daft Punk"},
					},
					{
						"id":             67238732,
						"title":          "Instant Crush (feat. Julian Casablancas)",
						"isrc":           "US-QX9-13-00105",
						"duration":       337,
						"track_position": 5,
						"disk_number":    1,
						"artist":         map[string]any{"id": 27, "name": "Daft Punk"},
						"contributors": []map[string]any{
							{"id": 27, "name": "Daft Punk"},
							{"id": 295821, "name": "Julian Casablancas"},
						},
					},
				},
				"total": 2,
			})
		default:
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
	})

	tracks, err := provider.GetReleaseTracks(context.Background(), "6575789")
	if err != nil {
		t.Fatalf("GetReleaseTracks: %v", err)
	}
	if len(tracks) != 2 {
		t.Fatalf("got %d tracks, want 2", len(tracks))
	}

	t1 := tracks[0]
	if t1.ID != "67238728" || t1.Title != "Give Life Back to Music" || t1.ISRC != "USQX91300101" {
		t.Errorf("t1 unexpected: %+v", t1)
	}
	if t1.DurationMS != 274000 || t1.TrackNumber != 1 || t1.DiscNumber != 1 {
		t.Errorf("t1 timing/numbering: %+v", t1)
	}

	t2 := tracks[1]
	if t2.ID != "67238732" || t2.ISRC != "USQX91300105" { // cleaned ISRC
		t.Errorf("t2 ISRC = %q, want USQX91300105", t2.ISRC)
	}
	if len(t2.Artists) != 2 || t2.Artists[0] != "Daft Punk" || t2.Artists[1] != "Julian Casablancas" {
		t.Errorf("t2 artists = %+v", t2.Artists)
	}
	if t2.CoverURL != "https://cdn.dzcdn.net/cover.jpg" || t2.ReleaseID != "6575789" {
		t.Errorf("t2 album context: cover=%q, releaseID=%q", t2.CoverURL, t2.ReleaseID)
	}
}

func TestGetTrack(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		provider := newTestProvider(t, func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/track/67238732" {
				t.Fatalf("unexpected path: %s", r.URL.Path)
			}
			writeJSON(w, map[string]any{
				"id":             67238732,
				"title":          "Instant Crush",
				"isrc":           "USQX91300105",
				"duration":       337,
				"track_position": 5,
				"disk_number":    1,
				"release_date":   "2013-05-20",
				"artist":         map[string]any{"id": 27, "name": "Daft Punk"},
				"contributors": []map[string]any{
					{"id": 27, "name": "Daft Punk"},
					{"id": 295821, "name": "Julian Casablancas"},
				},
				"album": map[string]any{
					"id":           6575789,
					"title":        "Random Access Memories",
					"cover_xl":     "https://cdn.dzcdn.net/cover_xl.jpg",
					"release_date": "2013-05-17",
				},
			})
		})

		track, err := provider.GetTrack(context.Background(), "67238732")
		if err != nil {
			t.Fatalf("GetTrack: %v", err)
		}
		if track.ID != "67238732" || track.Title != "Instant Crush" || track.ISRC != "USQX91300105" {
			t.Errorf("unexpected track: %+v", track)
		}
		if track.Album != "Random Access Memories" || track.ReleaseID != "6575789" {
			t.Errorf("unexpected album context: %+v", track)
		}
		if track.CoverURL != "https://cdn.dzcdn.net/cover_xl.jpg" {
			t.Errorf("cover = %q", track.CoverURL)
		}
	})

	t.Run("not found", func(t *testing.T) {
		provider := newTestProvider(t, func(w http.ResponseWriter, r *http.Request) {
			writeJSON(w, map[string]any{
				"error": map[string]any{
					"type":    "DataException",
					"message": "no data",
					"code":    800,
				},
			})
		})

		_, err := provider.GetTrack(context.Background(), "999999")
		if code := apperr.CodeOf(err); code != apperr.CodeTrackNotFound {
			t.Fatalf("code = %s, want %s", code, apperr.CodeTrackNotFound)
		}
	})
}

func TestClassifyRelease(t *testing.T) {
	tests := []struct {
		recordType string
		title      string
		trackCount int
		want       music.ReleaseType
	}{
		{"album", "Random Access Memories", 13, music.ReleaseAlbum},
		{"album", "Live 2007", 12, music.ReleaseLive},
		{"album", "Human After All: The Remixes", 10, music.ReleaseRemix},
		{"single", "Get Lucky", 1, music.ReleaseSingle},
		{"single", "Get Lucky Remix EP", 4, music.ReleaseEP},
		{"ep", "Musique Vol. 1 EP", 3, music.ReleaseEP},
		{"compile", "Greatest Hits", 20, music.ReleaseCompilation},
		{"compilation", "The Best of 2000s", 15, music.ReleaseCompilation},
	}

	for _, tc := range tests {
		got := classifyRelease(tc.recordType, tc.title, tc.trackCount)
		if got != tc.want {
			t.Errorf("classifyRelease(%q, %q, %d) = %v, want %v",
				tc.recordType, tc.title, tc.trackCount, got, tc.want)
		}
	}
}

func TestErrorHandling(t *testing.T) {
	tests := []struct {
		name       string
		statusCode int
		body       any
		wantCode   apperr.Code
	}{
		{
			name:       "400 Bad Request",
			statusCode: http.StatusBadRequest,
			body:       map[string]any{"error": map[string]any{"message": "bad params"}},
			wantCode:   apperr.CodeInvalidRequest,
		},
		{
			name:       "429 Rate Limit",
			statusCode: http.StatusTooManyRequests,
			body:       "Rate limit exceeded",
			wantCode:   apperr.CodeProviderRateLimited,
		},
		{
			name:       "500 Internal Error",
			statusCode: http.StatusInternalServerError,
			body:       "Internal Server Error",
			wantCode:   apperr.CodeProviderUnavailable,
		},
		{
			name:       "Deezer quota exception (code 4)",
			statusCode: http.StatusOK,
			body: map[string]any{
				"error": map[string]any{
					"type":    "QuotaException",
					"message": "Quota limit exceeded",
					"code":    4,
				},
			},
			wantCode: apperr.CodeProviderRateLimited,
		},
		{
			name:       "corrupt JSON payload",
			statusCode: http.StatusOK,
			body:       "this is not json at all",
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

			_, err := provider.GetArtist(context.Background(), "27")
			if code := apperr.CodeOf(err); code != tc.wantCode {
				t.Fatalf("code = %s, want %s (err: %v)", code, tc.wantCode, err)
			}
		})
	}
}

func TestProviderAvailable(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		provider := newTestProvider(t, func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/genre" {
				t.Fatalf("unexpected path: %s", r.URL.Path)
			}
			writeJSON(w, map[string]any{"data": []any{map[string]any{"id": 0, "name": "All"}}})
		})

		if err := provider.Available(context.Background()); err != nil {
			t.Fatalf("Available: %v", err)
		}
	})

	t.Run("failure on server error", func(t *testing.T) {
		provider := newTestProvider(t, func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusServiceUnavailable)
		})

		if err := provider.Available(context.Background()); err == nil {
			t.Fatal("expected error, got nil")
		}
	})
}

func TestConcurrentRequests(t *testing.T) {
	provider := newTestProvider(t, func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]any{
			"id":   27,
			"name": "Daft Punk",
		})
	})

	var wg sync.WaitGroup
	errCh := make(chan error, 20)

	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			_, err := provider.GetArtist(context.Background(), fmt.Sprintf("%d", id+1))
			if err != nil {
				errCh <- err
			}
		}(i)
	}

	wg.Wait()
	close(errCh)

	for err := range errCh {
		t.Errorf("concurrent request error: %v", err)
	}
}

func TestApplyDiscTotalsSetsTotalsOnASingleDiscRelease(t *testing.T) {
	tracks := []music.Track{
		{Title: "A", TrackNumber: 1, DiscNumber: 1},
		{Title: "B", TrackNumber: 2, DiscNumber: 1},
		{Title: "C", TrackNumber: 3, DiscNumber: 1},
	}
	applyDiscTotals(tracks)
	for i, track := range tracks {
		if track.TrackTotal != 3 {
			t.Errorf("track %d: TrackTotal = %d, want 3", i, track.TrackTotal)
		}
		if track.DiscTotal != 1 {
			t.Errorf("track %d: DiscTotal = %d, want 1", i, track.DiscTotal)
		}
	}
}

func TestApplyDiscTotalsIsPerDiscForMultiDisc(t *testing.T) {
	tracks := []music.Track{
		{Title: "A", TrackNumber: 1, DiscNumber: 1},
		{Title: "B", TrackNumber: 2, DiscNumber: 1},
		{Title: "C", TrackNumber: 1, DiscNumber: 2},
	}
	applyDiscTotals(tracks)
	if tracks[0].TrackTotal != 2 || tracks[2].TrackTotal != 1 {
		t.Errorf("per disc totals = %d/%d, want 2 and 1", tracks[0].TrackTotal, tracks[2].TrackTotal)
	}
	if tracks[0].DiscTotal != 2 {
		t.Errorf("DiscTotal = %d, want 2", tracks[0].DiscTotal)
	}
}

// TestToReleaseUsesContributorsWhenTheArtistNameIsJoined covers the shape
// Deezer returns for a collaboration single: album.artist is a display string,
// contributors are the structured truth.
func TestToReleaseUsesContributorsWhenTheArtistNameIsJoined(t *testing.T) {
	album := &apiAlbum{
		ID: 1, Title: "CCN", RecordType: "single", NbTracks: 1,
		Artist: &apiArtistRef{ID: 2, Name: "LACAZETTE & Bushido"},
		Contributors: []apiContributor{
			{ID: 2, Name: "LACAZETTE", Role: "Main"},
			{ID: 3, Name: "Bushido", Role: "Main"},
		},
	}
	release := toRelease(album)
	if release.AlbumArtist != "LACAZETTE" {
		t.Errorf("AlbumArtist = %q, want LACAZETTE", release.AlbumArtist)
	}
	if len(release.Artists) != 2 || release.Artists[1] != "Bushido" {
		t.Errorf("Artists = %v, want both credits", release.Artists)
	}
}

// TestToReleaseKeepsABandNameWithAnAmpersand is the counterpart: without a
// contributor list that contradicts it, the name stays whole.
func TestToReleaseKeepsABandNameWithAnAmpersand(t *testing.T) {
	album := &apiAlbum{
		ID: 2, Title: "Bridge Over Troubled Water", RecordType: "album", NbTracks: 11,
		Artist:       &apiArtistRef{ID: 5, Name: "Simon & Garfunkel"},
		Contributors: []apiContributor{{ID: 5, Name: "Simon & Garfunkel", Role: "Main"}},
	}
	release := toRelease(album)
	if release.AlbumArtist != "Simon & Garfunkel" {
		t.Errorf("AlbumArtist = %q, want Simon & Garfunkel", release.AlbumArtist)
	}
}

func TestToReleaseCleanArtistIsUsedAsIs(t *testing.T) {
	album := &apiAlbum{
		ID: 3, Title: "Discovery", RecordType: "album", NbTracks: 14,
		Artist:       &apiArtistRef{ID: 27, Name: "Daft Punk"},
		Contributors: []apiContributor{{ID: 27, Name: "Daft Punk", Role: "Main"}},
	}
	if got := toRelease(album).AlbumArtist; got != "Daft Punk" {
		t.Errorf("AlbumArtist = %q, want Daft Punk", got)
	}
}
