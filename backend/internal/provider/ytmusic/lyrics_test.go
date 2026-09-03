package ytmusic

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"ytdm/backend/internal/music"
)

// nextLyricsTabFixture is the shape music.youtube.com returns for a watch
// context: the lyrics tab carries an MPLY browse id behind a page type marker.
const nextLyricsTabFixture = `{
  "contents": {"singleColumnMusicWatchNextResultsRenderer": {"tabbedRenderer": {
    "watchNextTabbedResultsRenderer": {"tabs": [
      {"tabRenderer": {"title": "Up next"}},
      {"tabRenderer": {
        "title": "Lyrics",
        "endpoint": {"browseEndpoint": {
          "browseId": "MPLYt_7ltM34kr0mH-4",
          "browseEndpointContextSupportedConfigs": {"browseEndpointContextMusicConfig": {
            "pageType": "MUSIC_PAGE_TYPE_TRACK_LYRICS"
          }}
        }}
      }},
      {"tabRenderer": {
        "title": "Related",
        "endpoint": {"browseEndpoint": {
          "browseId": "MPTRt_7ltM34kr0mH-4",
          "browseEndpointContextSupportedConfigs": {"browseEndpointContextMusicConfig": {
            "pageType": "MUSIC_PAGE_TYPE_TRACK_RELATED"
          }}
        }}
      }}
    ]}
  }}}
}`

const browseLyricsFixture = `{
  "contents": {"sectionListRenderer": {"contents": [
    {"musicDescriptionShelfRenderer": {
      "description": {"runs": [{"text": "Work it, make it\nDo it, makes us"}]},
      "footer": {"runs": [{"text": "Source: LyricFind"}]}
    }}
  ]}}
}`

// newLyricsServer serves the InnerTube endpoints from fixtures.
func newLyricsServer(t *testing.T, next, browse string) (*LyricsProvider, *string) {
	t.Helper()
	var sawBrowseID string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload struct {
			VideoID  string `json:"videoId"`
			BrowseID string `json:"browseId"`
		}
		_ = json.NewDecoder(r.Body).Decode(&payload)
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.HasSuffix(r.URL.Path, "/next"):
			if payload.VideoID == "" {
				t.Error("the watch request must carry a video id")
			}
			_, _ = w.Write([]byte(next))
		case strings.HasSuffix(r.URL.Path, "/browse"):
			sawBrowseID = payload.BrowseID
			_, _ = w.Write([]byte(browse))
		default:
			t.Errorf("unexpected path %q", r.URL.Path)
		}
	}))
	t.Cleanup(server.Close)

	return NewLyricsProvider(Config{
		BaseURL: server.URL, Language: "en", Region: "US", HTTPClient: server.Client(),
	}), &sawBrowseID
}

func TestLyricsProviderReturnsPlainLyrics(t *testing.T) {
	provider, browseID := newLyricsServer(t, nextLyricsTabFixture, browseLyricsFixture)

	got, err := provider.Lyrics(context.Background(), music.Track{Title: "A"}, "JhulBGMA7G4")
	if err != nil {
		t.Fatalf("Lyrics: %v", err)
	}
	if got == nil {
		t.Fatal("no lyrics were returned")
	}
	if got.State() != music.LyricsAvailablePlain {
		t.Fatalf("state = %q, want available_plain", got.State())
	}
	if got.Synced {
		t.Error("YouTube Music cannot deliver timed lyrics anonymously; Synced must stay false")
	}
	if !strings.Contains(got.PlainText, "Work it") {
		t.Errorf("body = %q", got.PlainText)
	}
	if !strings.Contains(got.PlainText, "Source: LyricFind") {
		t.Errorf("the attribution was dropped: %q", got.PlainText)
	}
	if got.SourceID != "JhulBGMA7G4" {
		t.Errorf("source id = %q", got.SourceID)
	}
	if *browseID != "MPLYt_7ltM34kr0mH-4" {
		t.Errorf("browse id = %q, want the lyrics tab", *browseID)
	}
}

// TestLyricsProviderWithoutAMediaIDDoesNotSearch is correction twelve: a track
// that was not matched on YouTube must not be looked up by title, because that
// could return the lyrics of a different recording.
func TestLyricsProviderWithoutAMediaIDDoesNotSearch(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {
		t.Error("no request may be sent without a media id")
	}))
	defer server.Close()

	provider := NewLyricsProvider(Config{BaseURL: server.URL, HTTPClient: server.Client()})
	got, err := provider.Lyrics(context.Background(),
		music.Track{Title: "A", Artists: []string{"X"}}, "  ")
	if err != nil || got != nil {
		t.Fatalf("Lyrics = %v, %v; want a quiet miss", got, err)
	}
}

func TestLyricsProviderWithoutALyricsTabIsAMiss(t *testing.T) {
	provider, _ := newLyricsServer(t, `{"contents":{}}`, browseLyricsFixture)
	got, err := provider.Lyrics(context.Background(), music.Track{Title: "A"}, "abc")
	if err != nil || got != nil {
		t.Fatalf("Lyrics = %v, %v; a track without a lyrics tab must miss quietly", got, err)
	}
}

func TestLyricsProviderWithAnEmptyShelfIsAMiss(t *testing.T) {
	provider, _ := newLyricsServer(t, nextLyricsTabFixture,
		`{"contents":{"sectionListRenderer":{"contents":[{"musicDescriptionShelfRenderer":{"description":{"runs":[]}}}]}}}`)
	got, err := provider.Lyrics(context.Background(), music.Track{Title: "A"}, "abc")
	if err != nil || got != nil {
		t.Fatalf("Lyrics = %v, %v", got, err)
	}
}

func TestLyricsProviderIgnoresANonLyricsTab(t *testing.T) {
	const onlyRelated = `{"contents": {"tabs": [{"tabRenderer": {
	  "title": "Related",
	  "endpoint": {"browseEndpoint": {"browseId": "MPTRabc",
	    "browseEndpointContextSupportedConfigs": {"browseEndpointContextMusicConfig": {
	      "pageType": "MUSIC_PAGE_TYPE_TRACK_RELATED"}}}}
	}}]}}`
	provider, _ := newLyricsServer(t, onlyRelated, browseLyricsFixture)
	got, err := provider.Lyrics(context.Background(), music.Track{Title: "A"}, "abc")
	if err != nil || got != nil {
		t.Fatalf("Lyrics = %v, %v", got, err)
	}
}

func TestLyricsProviderUpstreamErrorIsTransient(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	provider := NewLyricsProvider(Config{BaseURL: server.URL, HTTPClient: server.Client()})
	got, err := provider.Lyrics(context.Background(), music.Track{Title: "A"}, "abc")
	if err == nil {
		t.Fatal("an upstream failure must surface as an error, not as a miss")
	}
	if got != nil {
		t.Fatalf("result = %+v", got)
	}
}
