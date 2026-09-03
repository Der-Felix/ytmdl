package ytmusic

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"ytdm/backend/internal/apperr"
	"ytdm/backend/internal/music"
)

// parse builds a node from a JSON literal, the way an InnerTube answer arrives.
func parse(t *testing.T, raw string) node {
	t.Helper()
	var decoded any
	if err := json.Unmarshal([]byte(raw), &decoded); err != nil {
		t.Fatalf("fixture is not valid JSON: %v", err)
	}
	return wrap(decoded)
}

const artistSearchFixture = `{
  "contents": {"tabbedSearchResultsRenderer": {"tabs": [{"tabRenderer": {"content": {
    "sectionListRenderer": {"contents": [{"musicShelfRenderer": {"contents": [
      {"musicResponsiveListItemRenderer": {
        "thumbnail": {"musicThumbnailRenderer": {"thumbnail": {"thumbnails": [
          {"url": "https://img.test/small.jpg", "width": 60, "height": 60},
          {"url": "https://img.test/large.jpg", "width": 480, "height": 480}
        ]}}},
        "flexColumns": [
          {"musicResponsiveListItemFlexColumnRenderer": {"text": {"runs": [{"text": "Test Artist"}]}}},
          {"musicResponsiveListItemFlexColumnRenderer": {"text": {"runs": [{"text": "Artist"}, {"text": " • "}, {"text": "1.2M subscribers"}]}}}
        ],
        "navigationEndpoint": {"browseEndpoint": {"browseId": "UCabcdef123456"}}
      }},
      {"musicResponsiveListItemRenderer": {
        "flexColumns": [{"musicResponsiveListItemFlexColumnRenderer": {"text": {"runs": [{"text": "Not An Artist"}]}}}],
        "navigationEndpoint": {"browseEndpoint": {"browseId": "MPREb_notanartist"}}
      }}
    ]}}]}
  }}}]}}
}`

func TestExtractArtists(t *testing.T) {
	artists := extractArtists(parse(t, artistSearchFixture), 20)
	if len(artists) != 1 {
		t.Fatalf("got %d artists, want 1 (only channel ids are artists)", len(artists))
	}
	got := artists[0]
	if got.Name != "Test Artist" {
		t.Errorf("name = %q", got.Name)
	}
	if got.SourceID != "UCabcdef123456" || got.Provider != ProviderName {
		t.Errorf("identity = %+v", got)
	}
	if got.ImageURL != "https://img.test/large.jpg" {
		t.Errorf("image = %q, want the largest thumbnail", got.ImageURL)
	}
	if got.SourceURL != "https://music.youtube.com/channel/UCabcdef123456" {
		t.Errorf("url = %q", got.SourceURL)
	}
}

const artistPageFixture = `{
  "header": {"musicImmersiveHeaderRenderer": {
    "title": {"runs": [{"text": "Test Artist"}]},
    "thumbnail": {"musicThumbnailRenderer": {"thumbnail": {"thumbnails": [{"url": "https://img.test/artist.jpg", "width": 540}]}}}
  }},
  "contents": {"singleColumnBrowseResultsRenderer": {"tabs": [{"tabRenderer": {"content": {
    "sectionListRenderer": {"contents": [
      {"musicCarouselShelfRenderer": {
        "header": {"musicCarouselShelfBasicHeaderRenderer": {
          "title": {"runs": [{"text": "Albums"}]},
          "moreContentButton": {"buttonRenderer": {"navigationEndpoint": {"browseEndpoint": {
            "browseId": "UCabcdef123456", "params": "ALBUMS_PARAMS"
          }}}}
        }},
        "contents": [
          {"musicTwoRowItemRenderer": {
            "title": {"runs": [{"text": "First Album"}]},
            "subtitle": {"runs": [{"text": "Album"}, {"text": " • "}, {"text": "2001"}]},
            "thumbnailRenderer": {"musicThumbnailRenderer": {"thumbnail": {"thumbnails": [{"url": "https://img.test/album1.jpg", "width": 226}]}}},
            "navigationEndpoint": {"browseEndpoint": {"browseId": "MPREb_album1"}}
          }},
          {"musicTwoRowItemRenderer": {
            "title": {"runs": [{"text": "Live In Berlin"}]},
            "subtitle": {"runs": [{"text": "Album"}, {"text": " • "}, {"text": "2005"}]},
            "navigationEndpoint": {"browseEndpoint": {"browseId": "MPREb_live"}}
          }}
        ]
      }},
      {"musicCarouselShelfRenderer": {
        "header": {"musicCarouselShelfBasicHeaderRenderer": {"title": {"runs": [{"text": "Singles"}]}}},
        "contents": [
          {"musicTwoRowItemRenderer": {
            "title": {"runs": [{"text": "A Single"}]},
            "subtitle": {"runs": [{"text": "Single"}, {"text": " • "}, {"text": "2020"}]},
            "navigationEndpoint": {"browseEndpoint": {"browseId": "MPREb_single"}}
          }}
        ]
      }}
    ]}
  }}}]}}
}`

func TestExtractArtistHeader(t *testing.T) {
	artist := extractArtistHeader(parse(t, artistPageFixture), "UCabcdef123456")
	if artist == nil {
		t.Fatal("no artist was extracted")
	}
	if artist.Name != "Test Artist" {
		t.Errorf("name = %q", artist.Name)
	}
	if artist.ImageURL != "https://img.test/artist.jpg" {
		t.Errorf("image = %q", artist.ImageURL)
	}
}

func TestExtractReleases(t *testing.T) {
	releases := extractReleases(parse(t, artistPageFixture), "Test Artist")
	if len(releases) != 3 {
		t.Fatalf("got %d releases, want 3: %+v", len(releases), releases)
	}

	byTitle := make(map[string]music.Release, len(releases))
	for _, release := range releases {
		byTitle[release.Title] = release
	}

	first := byTitle["First Album"]
	if first.ReleaseType != music.ReleaseAlbum || first.Year != 2001 {
		t.Errorf("first album = %+v", first)
	}
	if first.SourceID != "MPREb_album1" || first.CoverURL != "https://img.test/album1.jpg" {
		t.Errorf("first album identity = %+v", first)
	}
	if first.AlbumArtist != "Test Artist" {
		t.Errorf("album artist = %q", first.AlbumArtist)
	}
	if got := byTitle["Live In Berlin"].ReleaseType; got != music.ReleaseLive {
		t.Errorf("live album classified as %q", got)
	}
	if got := byTitle["A Single"].ReleaseType; got != music.ReleaseSingle {
		t.Errorf("single classified as %q", got)
	}
}

func TestExtractShelfContinuations(t *testing.T) {
	targets := extractShelfContinuations(parse(t, artistPageFixture))
	if len(targets) != 1 {
		t.Fatalf("got %d continuations, want 1", len(targets))
	}
	if targets[0].browseID != "UCabcdef123456" || targets[0].params != "ALBUMS_PARAMS" {
		t.Errorf("continuation = %+v", targets[0])
	}
}

const albumFixture = `{
  "header": {"musicDetailHeaderRenderer": {
    "title": {"runs": [{"text": "First Album"}]},
    "subtitle": {"runs": [{"text": "Album"}, {"text": " • "}, {"text": "Test Artist"}, {"text": " • "}, {"text": "2001"}]},
    "secondSubtitle": {"runs": [{"text": "3 songs"}, {"text": " • "}, {"text": "12 minutes"}]},
    "thumbnail": {"croppedSquareThumbnailRenderer": {"thumbnail": {"thumbnails": [{"url": "https://img.test/cover.jpg", "width": 544}]}}}
  }},
  "contents": {"singleColumnBrowseResultsRenderer": {"tabs": [{"tabRenderer": {"content": {
    "sectionListRenderer": {"contents": [{"musicShelfRenderer": {"contents": [
      {"musicResponsiveListItemRenderer": {
        "index": {"runs": [{"text": "1"}]},
        "flexColumns": [
          {"musicResponsiveListItemFlexColumnRenderer": {"text": {"runs": [{"text": "Opening Track"}]}}},
          {"musicResponsiveListItemFlexColumnRenderer": {"text": {"runs": [
            {"text": "Test Artist", "navigationEndpoint": {"browseEndpoint": {"browseId": "UCabcdef123456"}}}
          ]}}}
        ],
        "fixedColumns": [{"musicResponsiveListItemFixedColumnRenderer": {"text": {"runs": [{"text": "3:25"}]}}}],
        "playlistItemData": {"videoId": "video000001"}
      }},
      {"musicResponsiveListItemRenderer": {
        "index": {"runs": [{"text": "2"}]},
        "flexColumns": [
          {"musicResponsiveListItemFlexColumnRenderer": {"text": {"runs": [{"text": "Second Track"}]}}},
          {"musicResponsiveListItemFlexColumnRenderer": {"text": {"runs": [
            {"text": "Test Artist", "navigationEndpoint": {"browseEndpoint": {"browseId": "UCabcdef123456"}}},
            {"text": " & "},
            {"text": "Guest", "navigationEndpoint": {"browseEndpoint": {"browseId": "UCguest000000"}}}
          ]}}}
        ],
        "fixedColumns": [{"musicResponsiveListItemFixedColumnRenderer": {"text": {"runs": [{"text": "4:02"}]}}}],
        "playlistItemData": {"videoId": "video000002"}
      }}
    ]}}]}
  }}}]}}
}`

func TestExtractReleaseHeader(t *testing.T) {
	release := extractReleaseHeader(parse(t, albumFixture), "MPREb_album1", "")
	if release == nil {
		t.Fatal("no release was extracted")
	}
	if release.Title != "First Album" {
		t.Errorf("title = %q", release.Title)
	}
	if release.Year != 2001 {
		t.Errorf("year = %d", release.Year)
	}
	if release.ReleaseType != music.ReleaseAlbum {
		t.Errorf("type = %q", release.ReleaseType)
	}
	if release.AlbumArtist != "Test Artist" {
		t.Errorf("album artist = %q, want the artist without the type and year", release.AlbumArtist)
	}
	if release.TrackCount != 3 {
		t.Errorf("track count = %d, want 3", release.TrackCount)
	}
	if release.CoverURL != "https://img.test/cover.jpg" {
		t.Errorf("cover = %q", release.CoverURL)
	}
}

const responsiveHeaderWithAvatarFixture = `{
  "header": {"musicResponsiveHeaderRenderer": {
    "title": {"runs": [{"text": "64B"}]},
    "subtitle": {"runs": [{"text": "Single"}, {"text": " • "}, {"text": "2026"}]},
    "secondSubtitle": {"runs": [{"text": "1 song"}, {"text": " • "}, {"text": "3 minutes"}]},
    "straplineTextOne": {"runs": [{"text": "LACAZETTE", "navigationEndpoint": {"browseEndpoint": {"browseId": "UCartist123"}}}]},
    "straplineThumbnail": {"musicThumbnailRenderer": {"thumbnail": {"thumbnails": [
      {"url": "https://img.test/artist_avatar_small.jpg", "width": 60, "height": 60},
      {"url": "https://img.test/artist_avatar_544.jpg", "width": 544, "height": 544}
    ]}}},
    "thumbnail": {"musicThumbnailRenderer": {"thumbnail": {"thumbnails": [
      {"url": "https://img.test/release_cover_small.jpg", "width": 60, "height": 60},
      {"url": "https://img.test/release_cover_226.jpg", "width": 226, "height": 226},
      {"url": "https://img.test/release_cover_544.jpg", "width": 544, "height": 544}
    ]}}}
  }}
}`

func TestExtractReleaseHeader_StraplineAvatarNotUsedAsCover(t *testing.T) {
	release := extractReleaseHeader(parse(t, responsiveHeaderWithAvatarFixture), "MPREb_64b", "")
	if release == nil {
		t.Fatal("no release was extracted")
	}
	if release.Title != "64B" {
		t.Errorf("title = %q, want 64B", release.Title)
	}
	if release.ReleaseType != music.ReleaseSingle {
		t.Errorf("type = %q, want Single", release.ReleaseType)
	}
	if release.AlbumArtist != "LACAZETTE" {
		t.Errorf("album artist = %q, want LACAZETTE", release.AlbumArtist)
	}
	// Verify that the actual release cover was chosen, NOT the artist avatar
	if release.CoverURL != "https://img.test/release_cover_544.jpg" {
		t.Errorf("cover = %q, want https://img.test/release_cover_544.jpg (release cover, not artist avatar)", release.CoverURL)
	}
}

const responsiveHeaderOnlyAvatarFixture = `{
  "header": {"musicResponsiveHeaderRenderer": {
    "title": {"runs": [{"text": "Mystery Release"}]},
    "subtitle": {"runs": [{"text": "EP"}, {"text": " • "}, {"text": "2025"}]},
    "straplineTextOne": {"runs": [{"text": "Mystery Artist", "navigationEndpoint": {"browseEndpoint": {"browseId": "UCmystery123"}}}]},
    "straplineThumbnail": {"musicThumbnailRenderer": {"thumbnail": {"thumbnails": [
      {"url": "https://img.test/artist_avatar_544.jpg", "width": 544, "height": 544}
    ]}}}
  }}
}`

func TestExtractReleaseHeader_ThumbnailMissing_StraplineAvatarNotFallback(t *testing.T) {
	release := extractReleaseHeader(parse(t, responsiveHeaderOnlyAvatarFixture), "MPREb_mystery", "")
	if release == nil {
		t.Fatal("no release was extracted")
	}
	// When release artwork is missing, it must NOT fall back to the artist's avatar
	if release.CoverURL != "" {
		t.Errorf("cover = %q, want empty string when release thumbnail is missing", release.CoverURL)
	}
}

const responsiveAlbumFixture = `{
  "header": {"musicResponsiveHeaderRenderer": {
    "title": {"runs": [{"text": "Hybrid Theory"}]},
    "subtitle": {"runs": [{"text": "Album"}, {"text": " • "}, {"text": "2000"}]},
    "straplineTextOne": {"runs": [{"text": "Linkin Park", "navigationEndpoint": {"browseEndpoint": {"browseId": "UCxgN32UVVztKAQd2HkXzBtw"}}}]},
    "straplineThumbnail": {"musicThumbnailRenderer": {"thumbnail": {"thumbnails": [
      {"url": "https://img.test/lp_artist_avatar_544.jpg", "width": 544, "height": 544}
    ]}}},
    "thumbnail": {"musicThumbnailRenderer": {"thumbnail": {"thumbnails": [
      {"url": "https://img.test/hybrid_theory_cover_100.jpg", "width": 100, "height": 100},
      {"url": "https://img.test/hybrid_theory_cover_544.jpg", "width": 544, "height": 544},
      {"url": "https://img.test/hybrid_theory_cover_1200.jpg", "width": 1200, "height": 1200}
    ]}}}
  }}
}`

func TestExtractReleaseHeader_AlbumReleaseWithAvatarAndMultiResCover(t *testing.T) {
	release := extractReleaseHeader(parse(t, responsiveAlbumFixture), "MPREb_hybrid_theory", "")
	if release == nil {
		t.Fatal("no release was extracted")
	}
	if release.Title != "Hybrid Theory" {
		t.Errorf("title = %q, want Hybrid Theory", release.Title)
	}
	if release.ReleaseType != music.ReleaseAlbum {
		t.Errorf("type = %q, want Album", release.ReleaseType)
	}
	if release.AlbumArtist != "Linkin Park" {
		t.Errorf("album artist = %q, want Linkin Park", release.AlbumArtist)
	}
	// Verify that the highest resolution release cover was chosen (1200), NOT the artist avatar (544)
	if release.CoverURL != "https://img.test/hybrid_theory_cover_1200.jpg" {
		t.Errorf("cover = %q, want https://img.test/hybrid_theory_cover_1200.jpg", release.CoverURL)
	}
}

const responsiveEPFixture = `{
  "header": {"musicResponsiveHeaderRenderer": {
    "title": {"runs": [{"text": "All Your Fault: Pt. 1"}]},
    "subtitle": {"runs": [{"text": "EP"}, {"text": " • "}, {"text": "2017"}]},
    "straplineTextOne": {"runs": [{"text": "Bebe Rexha", "navigationEndpoint": {"browseEndpoint": {"browseId": "UCjqdH_2_kLgq5sE3_rN_Kkw"}}}]},
    "straplineThumbnail": {"musicThumbnailRenderer": {"thumbnail": {"thumbnails": [
      {"url": "https://img.test/bebe_avatar_544.jpg", "width": 544, "height": 544}
    ]}}},
    "thumbnail": {"musicThumbnailRenderer": {"thumbnail": {"thumbnails": [
      {"url": "https://img.test/all_your_fault_cover_544.jpg", "width": 544, "height": 544}
    ]}}}
  }}
}`

func TestExtractReleaseHeader_EPReleaseWithAvatar(t *testing.T) {
	release := extractReleaseHeader(parse(t, responsiveEPFixture), "MPREb_ayf1", "")
	if release == nil {
		t.Fatal("no release was extracted")
	}
	if release.Title != "All Your Fault: Pt. 1" {
		t.Errorf("title = %q, want All Your Fault: Pt. 1", release.Title)
	}
	if release.ReleaseType != music.ReleaseEP {
		t.Errorf("type = %q, want EP", release.ReleaseType)
	}
	if release.CoverURL != "https://img.test/all_your_fault_cover_544.jpg" {
		t.Errorf("cover = %q, want https://img.test/all_your_fault_cover_544.jpg (release cover, not avatar)", release.CoverURL)
	}
}

const unknownHeaderFixture = `{
  "header": {"unknownHeaderRenderer": {
    "title": {"runs": [{"text": "Something"}]}
  }}
}`

func TestExtractReleaseHeader_UnknownRendererReturnsNil(t *testing.T) {
	release := extractReleaseHeader(parse(t, unknownHeaderFixture), "MPREb_unknown", "")
	if release != nil {
		t.Errorf("expected nil release for unknown renderer, got: %+v", release)
	}
}

func TestExtractTracks(t *testing.T) {
	response := parse(t, albumFixture)
	release := extractReleaseHeader(response, "MPREb_album1", "")
	if release == nil {
		t.Fatal("no release was extracted")
	}

	tracks := extractTracks(response, *release, 300)
	if len(tracks) != 2 {
		t.Fatalf("got %d tracks, want 2", len(tracks))
	}

	first := tracks[0]
	if first.Title != "Opening Track" || first.TrackNumber != 1 {
		t.Errorf("first track = %+v", first)
	}
	if first.DurationMS != 205000 {
		t.Errorf("duration = %d ms, want 205000", first.DurationMS)
	}
	if first.SourceID != "video000001" || first.SourceProvider != ProviderName {
		t.Errorf("identity = %+v", first)
	}
	if first.Album != "First Album" || first.Year != 2001 {
		t.Errorf("album context missing: %+v", first)
	}

	second := tracks[1]
	if len(second.Artists) != 2 || second.Artists[0] != "Test Artist" || second.Artists[1] != "Guest" {
		t.Errorf("artists = %v, want both credited artists without the separator", second.Artists)
	}
	if second.DurationMS != 242000 {
		t.Errorf("duration = %d ms, want 242000", second.DurationMS)
	}
}

func TestParseDuration(t *testing.T) {
	tests := []struct {
		in   string
		want int
	}{
		{"3:25", 205000}, {"0:59", 59000}, {"1:02:03", 3723000},
		{"", 0}, {"abc", 0}, {"12", 0}, {"1:2:3:4", 0}, {"-1:00", 0},
	}
	for _, tc := range tests {
		if got := parseDuration(tc.in); got != tc.want {
			t.Errorf("parseDuration(%q) = %d, want %d", tc.in, got, tc.want)
		}
	}
}

func TestParseYear(t *testing.T) {
	tests := []struct {
		in   string
		want int
	}{
		{"Album • 2001", 2001}, {"2001", 2001}, {"Single • Artist • 1975", 1975},
		{"", 0}, {"12345", 0}, {"Album", 0}, {"1899", 0},
	}
	for _, tc := range tests {
		if got := parseYear(tc.in); got != tc.want {
			t.Errorf("parseYear(%q) = %d, want %d", tc.in, got, tc.want)
		}
	}
}

func TestValidateBrowseID(t *testing.T) {
	if _, err := validateBrowseID("UCabc", "UC"); err != nil {
		t.Errorf("valid channel id was rejected: %v", err)
	}
	for _, id := range []string{"", "   ", "MPREb_x", "UC abc", "UC/../x", "UC\"x"} {
		if _, err := validateBrowseID(id, "UC"); err == nil {
			t.Errorf("id %q was accepted", id)
		}
	}
}

func TestNodeIsTolerantOfMissingData(t *testing.T) {
	empty := parse(t, `{}`)
	if empty.get("a", "b", "c").exists() {
		t.Error("missing path reported as existing")
	}
	if got := empty.get("a").text(); got != "" {
		t.Errorf("text of a missing node = %q", got)
	}
	if got := empty.findFirst("nothing").int(); got != 0 {
		t.Errorf("int of a missing node = %d", got)
	}
	if got := extractArtists(empty, 10); len(got) != 0 {
		t.Errorf("got %d artists from an empty document", len(got))
	}
	if got := extractReleaseHeader(empty, "MPREb_x", ""); got != nil {
		t.Errorf("got a release from an empty document: %+v", got)
	}
}

// unrelatedFixture is a well formed JSON document that contains none of the
// renderers the parser knows. A tolerant search through the tree must not turn
// any of it into an artist, a release or a track.
const unrelatedFixture = `{
  "responseContext": {"visitorData": "abc"},
  "contents": {"sectionListRenderer": {"contents": [
    {"messageRenderer": {"text": {"runs": [{"text": "Something went wrong"}]}}},
    {"itemSectionRenderer": {"contents": [
      {"videoId": "dQw4w9WgXcQ", "title": {"simpleText": "Not a music item"}},
      {"browseId": "UCnotarenderer", "subtitle": {"simpleText": "Album • 1999"}}
    ]}}
  ]}}
}`

func TestExtractorsIgnoreUnknownDocuments(t *testing.T) {
	response := parse(t, unrelatedFixture)

	if artists := extractArtists(response, 20); len(artists) != 0 {
		t.Errorf("extractArtists invented %d artists: %+v", len(artists), artists)
	}
	if releases := extractReleases(response, ""); len(releases) != 0 {
		t.Errorf("extractReleases invented %d releases: %+v", len(releases), releases)
	}
	if tracks := extractTracks(response, music.Release{Title: "X"}, 50); len(tracks) != 0 {
		t.Errorf("extractTracks invented %d tracks: %+v", len(tracks), tracks)
	}
	if header := extractArtistHeader(response, "UCabc"); header != nil {
		t.Errorf("extractArtistHeader invented an artist: %+v", header)
	}
	if header := extractReleaseHeader(response, "MPREabc", ""); header != nil {
		t.Errorf("extractReleaseHeader invented a release: %+v", header)
	}
	if targets := extractShelfContinuations(response); len(targets) != 0 {
		t.Errorf("extractShelfContinuations invented %d targets: %+v", len(targets), targets)
	}
}

// TestGetReleaseTracksReportsUnreadablePages pins that a release page the
// parser cannot read produces a provider error instead of an empty album.
func TestGetReleaseTracksReportsUnreadablePages(t *testing.T) {
	body := `{
	  "header": {"musicDetailHeaderRenderer": {
	    "title": {"runs": [{"text": "Some Album"}]},
	    "subtitle": {"runs": [{"text": "Album"}, {"text": " • "}, {"text": "2020"}]}
	  }},
	  "contents": {"sectionListRenderer": {"contents": [
	    {"messageRenderer": {"text": {"runs": [{"text": "unavailable"}]}}}
	  ]}}
	}`

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, body)
	}))
	defer server.Close()

	provider := NewMetadataProvider(Config{BaseURL: server.URL, HTTPClient: server.Client()})
	_, err := provider.GetReleaseTracks(context.Background(), "MPREb_unreadable")
	if code := apperr.CodeOf(err); code != apperr.CodeProviderUnavailable {
		t.Fatalf("code = %s, want %s (err = %v)", code, apperr.CodeProviderUnavailable, err)
	}
}

// TestProviderErrorsInsteadOfPanicOnBrokenJSON covers a response that is not
// JSON at all.
func TestProviderErrorsInsteadOfPanicOnBrokenJSON(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, "<html>blocked</html>")
	}))
	defer server.Close()

	provider := NewMetadataProvider(Config{BaseURL: server.URL, HTTPClient: server.Client()})
	if _, err := provider.SearchArtists(context.Background(), "anything"); apperr.CodeOf(err) != apperr.CodeProviderUnavailable {
		t.Fatalf("search: err = %v, want a provider error", err)
	}
	if _, err := provider.GetArtist(context.Background(), "UCabcdef"); apperr.CodeOf(err) != apperr.CodeProviderUnavailable {
		t.Fatalf("artist: err = %v, want a provider error", err)
	}
}

// showAllGridFixture mirrors a "show all" page: the releases sit in a grid and
// every entry carries an overflow menu whose "go to artist" entry points at the
// artist. The menu key sorts before navigationEndpoint, so a plain subtree
// search finds the artist first — which used to file every release on these
// pages under the artist's own browse id and made them fail the MPRE/OLAK
// check, capping a discography at the preview shelf.
const showAllGridFixture = `{
  "contents": {"singleColumnBrowseResultsRenderer": {"tabs": [{"tabRenderer": {"content": {
    "sectionListRenderer": {"contents": [{"gridRenderer": {"items": [
      {"musicTwoRowItemRenderer": {
        "title": {"runs": [{"text": "Eleventh Album"}]},
        "subtitle": {"runs": [{"text": "Album"}, {"text": " • "}, {"text": "2011"}]},
        "thumbnailRenderer": {"musicThumbnailRenderer": {"thumbnail": {"thumbnails": [
          {"url": "https://img.test/eleven.jpg", "width": 544, "height": 544}
        ]}}},
        "menu": {"menuRenderer": {"items": [
          {"menuNavigationItemRenderer": {
            "text": {"runs": [{"text": "Go to artist"}]},
            "navigationEndpoint": {"browseEndpoint": {"browseId": "UCabcdef123456"}}
          }}
        ]}},
        "navigationEndpoint": {"browseEndpoint": {"browseId": "MPREb_album11"}}
      }},
      {"musicTwoRowItemRenderer": {
        "title": {"runs": [{"text": "Twelfth Single"}]},
        "subtitle": {"runs": [{"text": "Single"}, {"text": " • "}, {"text": "2012"}]},
        "menu": {"menuRenderer": {"items": [
          {"menuNavigationItemRenderer": {
            "text": {"runs": [{"text": "Go to artist"}]},
            "navigationEndpoint": {"browseEndpoint": {"browseId": "UCabcdef123456"}}
          }}
        ]}},
        "navigationEndpoint": {"browseEndpoint": {"browseId": "MPREb_single12"}}
      }}
    ]}}]}
  }}}]}}
}`

func TestExtractReleasesFromShowAllGrid(t *testing.T) {
	releases := extractReleases(parse(t, showAllGridFixture), "Test Artist")
	if len(releases) != 2 {
		t.Fatalf("got %d releases, want 2: %+v", len(releases), releases)
	}

	if releases[0].SourceID != "MPREb_album11" {
		t.Errorf("album source id = %q, want the release rather than the artist", releases[0].SourceID)
	}
	if releases[0].ReleaseType != music.ReleaseAlbum || releases[0].Year != 2011 {
		t.Errorf("album = %+v", releases[0])
	}
	if releases[1].SourceID != "MPREb_single12" {
		t.Errorf("single source id = %q, want the release rather than the artist", releases[1].SourceID)
	}
	if releases[1].ReleaseType != music.ReleaseSingle {
		t.Errorf("single classified as %q", releases[1].ReleaseType)
	}
}

// TestBrowseIDPrefersOwnNavigation pins the rule directly: an item's own
// navigation endpoint wins over any browse endpoint nested in its menu.
func TestBrowseIDPrefersOwnNavigation(t *testing.T) {
	item := parse(t, `{
      "menu": {"menuRenderer": {"items": [
        {"menuNavigationItemRenderer": {"navigationEndpoint": {"browseEndpoint": {"browseId": "UCartist"}}}}
      ]}},
      "navigationEndpoint": {"browseEndpoint": {"browseId": "MPREb_release"}}
    }`)

	if got := item.browseID(); got != "MPREb_release" {
		t.Errorf("browseID() = %q, want %q", got, "MPREb_release")
	}
}

// collaborationAlbumFixture mirrors what music.youtube.com actually returns for
// a release by two artists: the strapline names them as separate runs, each
// with its own channel id, joined by a separator run.
const collaborationAlbumFixture = `{
  "header": {"musicResponsiveHeaderRenderer": {
    "title": {"runs": [{"text": "CCN"}]},
    "subtitle": {"runs": [{"text": "Single"}, {"text": " • "}, {"text": "2025"}]},
    "straplineTextOne": {"runs": [
      {"text": "LACAZETTE", "navigationEndpoint": {"browseEndpoint": {"browseId": "UC9IBdRWHeNkg7QRsi2B_dFA"}}},
      {"text": " & "},
      {"text": "Bushido", "navigationEndpoint": {"browseEndpoint": {"browseId": "UCbushido00000"}}}
    ]},
    "secondSubtitle": {"runs": [{"text": "1 song"}, {"text": " • "}, {"text": "3 minutes"}]}
  }},
  "contents": {"singleColumnBrowseResultsRenderer": {"tabs": [{"tabRenderer": {"content": {
    "sectionListRenderer": {"contents": [{"musicShelfRenderer": {"contents": [
      {"musicResponsiveListItemRenderer": {
        "index": {"runs": [{"text": "1"}]},
        "flexColumns": [
          {"musicResponsiveListItemFlexColumnRenderer": {"text": {"runs": [{"text": "CCN"}]}}},
          {"musicResponsiveListItemFlexColumnRenderer": {"text": {"runs": []}}}
        ],
        "fixedColumns": [{"musicResponsiveListItemFixedColumnRenderer": {"text": {"runs": [{"text": "2:30"}]}}}],
        "playlistItemData": {"videoId": "video000009"}
      }}
    ]}}]}
  }}}]}}
}`

// bandNameAlbumFixture is the counterpart: one artist whose name contains an
// ampersand, delivered as a single run with a single channel id.
const bandNameAlbumFixture = `{
  "header": {"musicResponsiveHeaderRenderer": {
    "title": {"runs": [{"text": "Bridge Over Troubled Water"}]},
    "subtitle": {"runs": [{"text": "Album"}, {"text": " • "}, {"text": "1970"}]},
    "straplineTextOne": {"runs": [
      {"text": "Simon & Garfunkel", "navigationEndpoint": {"browseEndpoint": {"browseId": "UCsimongarfunkel"}}}
    ]}
  }}
}`

func TestExtractReleaseHeaderUsesStructuredArtistRuns(t *testing.T) {
	release := extractReleaseHeader(parse(t, collaborationAlbumFixture), "MPREb_collab", "")
	if release == nil {
		t.Fatal("no release was extracted")
	}
	want := []string{"LACAZETTE", "Bushido"}
	if len(release.Artists) != 2 || release.Artists[0] != want[0] || release.Artists[1] != want[1] {
		t.Fatalf("artists = %v, want %v", release.Artists, want)
	}
	if release.AlbumArtist != "LACAZETTE" {
		t.Errorf("album artist = %q, want LACAZETTE", release.AlbumArtist)
	}
}

// TestExtractReleaseHeaderKeepsABandName is the regression guard: a name that
// contains "&" but arrives as one run must never be taken apart.
func TestExtractReleaseHeaderKeepsABandName(t *testing.T) {
	release := extractReleaseHeader(parse(t, bandNameAlbumFixture), "MPREb_band", "")
	if release == nil {
		t.Fatal("no release was extracted")
	}
	if len(release.Artists) != 1 || release.Artists[0] != "Simon & Garfunkel" {
		t.Fatalf("artists = %v, want [Simon & Garfunkel]", release.Artists)
	}
	if release.AlbumArtist != "Simon & Garfunkel" {
		t.Errorf("album artist = %q, want Simon & Garfunkel", release.AlbumArtist)
	}
}

func TestExtractTracksInheritsTheStructuredReleaseArtists(t *testing.T) {
	response := parse(t, collaborationAlbumFixture)
	release := extractReleaseHeader(response, "MPREb_collab", "")
	tracks := extractTracks(response, *release, 10)

	if len(tracks) != 1 {
		t.Fatalf("got %d tracks, want 1", len(tracks))
	}
	if len(tracks[0].Artists) != 2 || tracks[0].Artists[0] != "LACAZETTE" {
		t.Errorf("track artists = %v", tracks[0].Artists)
	}
	if tracks[0].AlbumArtist != "LACAZETTE" {
		t.Errorf("track album artist = %q, want LACAZETTE", tracks[0].AlbumArtist)
	}
}

func TestExtractReleaseHeaderFallsBackToTheContextArtist(t *testing.T) {
	const noArtistFixture = `{
	  "header": {"musicResponsiveHeaderRenderer": {
	    "title": {"runs": [{"text": "Some Album"}]},
	    "subtitle": {"runs": [{"text": "Album"}, {"text": " • "}, {"text": "2001"}]}
	  }}
	}`
	release := extractReleaseHeader(parse(t, noArtistFixture), "MPREb_x", "Context Artist")
	if release == nil {
		t.Fatal("no release was extracted")
	}
	if release.AlbumArtist != "Context Artist" {
		t.Fatalf("album artist = %q, want the context artist", release.AlbumArtist)
	}
}

func TestRunArtistsIgnoresRunsWithoutAChannel(t *testing.T) {
	const fixture = `{"text": {"runs": [
	  {"text": "Album"},
	  {"text": " • "},
	  {"text": "Real Artist", "navigationEndpoint": {"browseEndpoint": {"browseId": "UCreal00000000"}}},
	  {"text": " • "},
	  {"text": "2001"}
	]}}`
	got := runArtists(parse(t, fixture).get("text"))
	if len(got) != 1 || got[0] != "Real Artist" {
		t.Fatalf("runArtists = %v, want [Real Artist]", got)
	}
}
