package lyrics_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"ytdm/backend/internal/lyrics"
	"ytdm/backend/internal/music"
)

// loopback rewrites a httptest address to the "localhost" name. The SSRF guard
// in the production client refuses a non-public IP literal, which is exactly
// the protection we want to keep.
func loopback(raw string) string {
	return strings.Replace(raw, "127.0.0.1", "localhost", 1)
}

func newLRCLib(t *testing.T, handler http.HandlerFunc) (*lyrics.LRCLib, *httptest.Server) {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	return lyrics.NewLRCLib(lyrics.LRCLibConfig{
		BaseURL:   loopback(server.URL),
		UserAgent: "YTMDL/test (https://example.test)",
		Client:    server.Client(),
	}), server
}

func TestLRCLibPrefersSyncedLyrics(t *testing.T) {
	var gotQuery, gotAgent string
	client, _ := newLRCLib(t, func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.RawQuery
		gotAgent = r.Header.Get("User-Agent")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":1,"trackName":"A","artistName":"X","albumName":"B",
			"duration":180,"instrumental":false,"plainLyrics":"line","syncedLyrics":"[00:01.00]line"}`))
	})

	track := music.Track{Title: "A", Artists: []string{"X", "Y"}, Album: "B", DurationMS: 180_000}
	got, err := client.Lyrics(context.Background(), track, "")
	if err != nil {
		t.Fatalf("Lyrics: %v", err)
	}
	if got.State() != music.LyricsAvailableSynced {
		t.Fatalf("state = %q, want available_synced", got.State())
	}
	if got.LRC != "[00:01.00]line" || got.PlainText != "line" {
		t.Fatalf("result = %+v", got)
	}
	if got.Provider != "lrclib" || got.SourceID != "1" {
		t.Errorf("provenance = %q/%q", got.Provider, got.SourceID)
	}
	if gotAgent == "" {
		t.Error("LRCLIB requires a User-Agent that identifies the client")
	}
	if !strings.Contains(gotQuery, "artist_name=X") || strings.Contains(gotQuery, "artist_name=X%3B") {
		t.Errorf("query = %q, want the primary artist only", gotQuery)
	}
	if !strings.Contains(gotQuery, "duration=180") || !strings.Contains(gotQuery, "album_name=B") {
		t.Errorf("query = %q, want duration and album", gotQuery)
	}
}

func TestLRCLibFallsBackToPlainLyrics(t *testing.T) {
	client, _ := newLRCLib(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"id":2,"plainLyrics":"just words","syncedLyrics":null}`))
	})
	got, err := client.Lyrics(context.Background(),
		music.Track{Title: "A", Artists: []string{"X"}}, "")
	if err != nil {
		t.Fatalf("Lyrics: %v", err)
	}
	if got.State() != music.LyricsAvailablePlain || got.PlainText != "just words" {
		t.Fatalf("result = %+v", got)
	}
}

func TestLRCLibDerivesPlainTextFromTheLRC(t *testing.T) {
	client, _ := newLRCLib(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"id":3,"plainLyrics":null,
			"syncedLyrics":"[ar: X]\n[00:01.00]first\n[00:02.00]second"}`))
	})
	got, err := client.Lyrics(context.Background(),
		music.Track{Title: "A", Artists: []string{"X"}}, "")
	if err != nil {
		t.Fatalf("Lyrics: %v", err)
	}
	if got.PlainText != "first\nsecond" {
		t.Fatalf("plain text = %q", got.PlainText)
	}
}

func TestLRCLibInstrumentalIsAnAnswer(t *testing.T) {
	client, _ := newLRCLib(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"id":4,"instrumental":true,"plainLyrics":null,"syncedLyrics":null}`))
	})
	got, err := client.Lyrics(context.Background(),
		music.Track{Title: "A", Artists: []string{"X"}}, "")
	if err != nil {
		t.Fatalf("Lyrics: %v", err)
	}
	if got.State() != music.LyricsInstrumental {
		t.Fatalf("state = %q, want instrumental", got.State())
	}
}

func TestLRCLibRetriesOnceWithoutTheAlbum(t *testing.T) {
	var calls []string
	client, _ := newLRCLib(t, func(w http.ResponseWriter, r *http.Request) {
		calls = append(calls, r.URL.RawQuery)
		if strings.Contains(r.URL.RawQuery, "album_name=") {
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"statusCode":404,"name":"TrackNotFound","message":"Failed to find specified track"}`))
			return
		}
		_, _ = w.Write([]byte(`{"id":5,"plainLyrics":"line"}`))
	})

	got, err := client.Lyrics(context.Background(),
		music.Track{Title: "A", Artists: []string{"X"}, Album: "B", DurationMS: 10_000}, "")
	if err != nil {
		t.Fatalf("Lyrics: %v", err)
	}
	if got == nil || got.State() != music.LyricsAvailablePlain {
		t.Fatalf("result = %+v", got)
	}
	if len(calls) != 2 {
		t.Fatalf("calls = %d, want exactly 2 (with album, then without)", len(calls))
	}
	if strings.Contains(calls[1], "album_name=") {
		t.Errorf("the retry still sent an album: %q", calls[1])
	}
}

func TestLRCLibDoesNotRetryEndlessly(t *testing.T) {
	var calls int
	client, _ := newLRCLib(t, func(w http.ResponseWriter, _ *http.Request) {
		calls++
		w.WriteHeader(http.StatusNotFound)
	})
	got, err := client.Lyrics(context.Background(),
		music.Track{Title: "A", Artists: []string{"X"}, Album: "B"}, "")
	if err != nil {
		t.Fatalf("a miss must not be an error: %v", err)
	}
	if got != nil {
		t.Fatalf("result = %+v, want nil", got)
	}
	if calls != 2 {
		t.Fatalf("calls = %d, want 2", calls)
	}
}

func TestLRCLibMissWithoutAnAlbumIsASingleCall(t *testing.T) {
	var calls int
	client, _ := newLRCLib(t, func(w http.ResponseWriter, _ *http.Request) {
		calls++
		w.WriteHeader(http.StatusNotFound)
	})
	if _, err := client.Lyrics(context.Background(),
		music.Track{Title: "A", Artists: []string{"X"}}, ""); err != nil {
		t.Fatalf("Lyrics: %v", err)
	}
	if calls != 1 {
		t.Fatalf("calls = %d, want 1", calls)
	}
}

func TestLRCLibHonoursRetryAfter(t *testing.T) {
	client, _ := newLRCLib(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Retry-After", "7")
		w.WriteHeader(http.StatusTooManyRequests)
	})
	_, err := client.Lyrics(context.Background(), music.Track{Title: "A", Artists: []string{"X"}}, "")
	if err == nil {
		t.Fatal("429 must surface as an error")
	}
	wait, limited := lyrics.RetryAfter(err)
	if !limited || wait != 7*time.Second {
		t.Fatalf("RetryAfter = %v, %v, want 7s", wait, limited)
	}
}

func TestLRCLibRateLimitWithoutAHeaderFallsBack(t *testing.T) {
	client, _ := newLRCLib(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
	})
	_, err := client.Lyrics(context.Background(), music.Track{Title: "A", Artists: []string{"X"}}, "")
	wait, limited := lyrics.RetryAfter(err)
	if !limited || wait <= 0 {
		t.Fatalf("RetryAfter = %v, %v, want a positive fallback", wait, limited)
	}
}

func TestLRCLibServerErrorIsTransient(t *testing.T) {
	client, _ := newLRCLib(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
	})
	_, err := client.Lyrics(context.Background(), music.Track{Title: "A", Artists: []string{"X"}}, "")
	if err == nil {
		t.Fatal("a 5xx must surface as an error, not as a miss")
	}
	if _, limited := lyrics.RetryAfter(err); limited {
		t.Error("a 5xx is not a rate limit")
	}
}

func TestLRCLibUnparsableBodyIsAnError(t *testing.T) {
	client, _ := newLRCLib(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("<html>not json</html>"))
	})
	if _, err := client.Lyrics(context.Background(),
		music.Track{Title: "A", Artists: []string{"X"}}, ""); err == nil {
		t.Fatal("an unparsable response must not be reported as a miss")
	}
}

func TestLRCLibWithoutTitleOrArtistIsAMiss(t *testing.T) {
	client, _ := newLRCLib(t, func(w http.ResponseWriter, _ *http.Request) {
		t.Error("the provider must not be asked without a title and an artist")
	})
	for _, track := range []music.Track{
		{Artists: []string{"X"}},
		{Title: "A"},
	} {
		got, err := client.Lyrics(context.Background(), track, "")
		if err != nil || got != nil {
			t.Fatalf("Lyrics(%+v) = %v, %v", track, got, err)
		}
	}
}

func TestStripTimestamps(t *testing.T) {
	got := lyrics.StripTimestamps("[ar: X]\n[ti: A]\n[00:10.89]first\n\n[00:14.58]second\n")
	if got != "first\nsecond" {
		t.Fatalf("StripTimestamps = %q", got)
	}
}
