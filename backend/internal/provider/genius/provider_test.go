package genius

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"ytdm/backend/internal/music"
)

// syntheticLyricsHTML creates a synthetic, copyright-safe Genius song page fixture.
func syntheticLyricsHTML(containers ...string) string {
	var sb strings.Builder
	sb.WriteString(`<!doctype html><html><head><title>Test Artist – Test Song Lyrics | Genius Lyrics</title></head><body>`)
	sb.WriteString(`<div class="header"><h1>Test Song</h1></div>`)
	for _, c := range containers {
		sb.WriteString(fmt.Sprintf(`<div data-lyrics-container="true" class="Lyrics__Container">%s</div>`, c))
	}
	sb.WriteString(`</body></html>`)
	return sb.String()
}

func TestGeniusProvider_Disabled(t *testing.T) {
	ctx := context.Background()
	track := music.Track{Title: "Test Track", Artists: []string{"Test Artist"}}

	// Disabled
	pDisabled := NewLyricsProvider(Config{
		Enabled:     false,
		AccessToken: "test-token",
	})
	res, err := pDisabled.Lyrics(ctx, track, "")
	if err != nil || res != nil {
		t.Fatalf("expected nil, nil for disabled provider, got %v, %v", res, err)
	}
	if pDisabled.IsEnabled() {
		t.Fatal("expected IsEnabled=false")
	}
}

func TestGeniusProvider_PublicWebSearch_Success(t *testing.T) {
	ctx := context.Background()
	track := music.Track{
		ID:          "track-public",
		Title:       "Public Synthetic Song",
		Artists:     []string{"Public Artist"},
		AlbumArtist: "Public Artist",
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/search/song":
			// Must NOT have Authorization header
			if auth := r.Header.Get("Authorization"); auth != "" {
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{
				"meta": {"status": 200},
				"response": {
					"sections": [
						{
							"type": "song",
							"hits": [
								{
									"type": "song",
									"result": {
										"id": 888123,
										"title": "Public Synthetic Song",
										"url": "` + "http://" + r.Host + `/public-song-page",
										"primary_artist": {
											"id": 555,
											"name": "Public Artist"
										}
									}
								}
							]
						}
					]
				}
			}`))

		case "/public-song-page":
			w.Header().Set("Content-Type", "text/html")
			w.WriteHeader(http.StatusOK)
			html := syntheticLyricsHTML(
				`[Verse 1]<br/>Line one from public web search.<br/>Line two from public web search.`,
			)
			_, _ = w.Write([]byte(html))

		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	p := NewLyricsProvider(Config{
		Enabled:     true,
		AccessToken: "",
		BaseURL:     server.URL,
		HTTPClient:  server.Client(),
	})

	res, err := p.Lyrics(ctx, track, "")
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if res == nil {
		t.Fatal("expected lyrics result, got nil")
	}
	if res.SourceID != "888123" {
		t.Errorf("expected SourceID '888123', got %q", res.SourceID)
	}
	if !strings.Contains(res.PlainText, "Line one from public web search.") {
		t.Errorf("expected lyrics content, got:\n%s", res.PlainText)
	}
}

func TestGeniusProvider_SearchAndExtract_Success(t *testing.T) {
	ctx := context.Background()
	track := music.Track{
		ID:          "track-1",
		Title:       "Synthetic Song",
		Artists:     []string{"Synthetic Artist"},
		AlbumArtist: "Synthetic Artist",
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/search":
			// Verify Authorization header
			auth := r.Header.Get("Authorization")
			if auth != "Bearer valid-token" {
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{
				"meta": {"status": 200},
				"response": {
					"hits": [
						{
							"type": "song",
							"result": {
								"id": 999123,
								"title": "Synthetic Song",
								"url": "` + "http://" + r.Host + `/song-page",
								"primary_artist": {
									"id": 444,
									"name": "Synthetic Artist"
								}
							}
						}
					]
				}
			}`))

		case "/song-page":
			w.Header().Set("Content-Type", "text/html")
			w.WriteHeader(http.StatusOK)
			html := syntheticLyricsHTML(
				`[Verse 1]<br/>This is line one of synthetic song.<br/>And this is line two.<br/>`,
				`[Chorus]<br/>Synthetic refrain line.<br/>Another test line for length check.`,
			)
			_, _ = w.Write([]byte(html))

		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	p := NewLyricsProvider(Config{
		Enabled:     true,
		AccessToken: "valid-token",
		BaseURL:     server.URL,
		HTTPClient:  server.Client(),
	})

	res, err := p.Lyrics(ctx, track, "")
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if res == nil {
		t.Fatal("expected lyrics result, got nil")
	}

	if res.Provider != "genius" {
		t.Errorf("expected provider 'genius', got %q", res.Provider)
	}
	if res.Synced {
		t.Errorf("expected Synced=false, got true")
	}
	if res.SourceID != "999123" {
		t.Errorf("expected SourceID '999123', got %q", res.SourceID)
	}
	if !strings.Contains(res.PlainText, "[Verse 1]") || !strings.Contains(res.PlainText, "[Chorus]") {
		t.Errorf("expected plain text to contain section labels, got:\n%s", res.PlainText)
	}
	if !strings.Contains(res.PlainText, "This is line one of synthetic song.") {
		t.Errorf("expected line content, got:\n%s", res.PlainText)
	}
}

func TestGeniusMatcher_VariantRejection(t *testing.T) {
	liveTrack := music.Track{Title: "Song Title (Live)", Artists: []string{"Band A"}}
	studioTrack := music.Track{Title: "Song Title", Artists: []string{"Band A"}}
	remixTrack := music.Track{Title: "Song Title (Remix)", Artists: []string{"Band A"}}

	candStudio := SongResult{
		Title: "Song Title",
		PrimaryArtist: struct {
			ID   int64  `json:"id"`
			Name string `json:"name"`
		}{Name: "Band A"},
	}

	candLive := SongResult{
		Title: "Song Title (Live at Wembley)",
		PrimaryArtist: struct {
			ID   int64  `json:"id"`
			Name string `json:"name"`
		}{Name: "Band A"},
	}

	// 1. Live track vs Studio candidate -> REJECT
	score1 := MatchCandidate(liveTrack, candStudio, MinMatchConfidence)
	if score1.Accepted {
		t.Errorf("expected live track vs studio candidate to be rejected")
	}

	// 2. Studio track vs Live candidate -> REJECT
	score2 := MatchCandidate(studioTrack, candLive, MinMatchConfidence)
	if score2.Accepted {
		t.Errorf("expected studio track vs live candidate to be rejected")
	}

	// 3. Remix track vs Studio candidate -> REJECT
	score3 := MatchCandidate(remixTrack, candStudio, MinMatchConfidence)
	if score3.Accepted {
		t.Errorf("expected remix track vs studio candidate to be rejected")
	}

	// 4. Live track vs Live candidate -> ACCEPT
	score4 := MatchCandidate(liveTrack, candLive, MinMatchConfidence)
	if !score4.Accepted {
		t.Errorf("expected live track vs live candidate to be accepted, got reason: %s", score4.Reason)
	}
}

func TestGeniusParser_ChallengeDetection(t *testing.T) {
	challengeHTML := `<!doctype html><html><head><title>Attention Required! | Cloudflare</title></head><body><h1>Please verify you are human</h1></body></html>`
	_, err := ParseLyrics(strings.NewReader(challengeHTML))
	if err == nil || !strings.Contains(err.Error(), "challenge") {
		t.Fatalf("expected challenge error, got %v", err)
	}
}

func TestGeniusParser_CleanTextExtraction(t *testing.T) {
	html := syntheticLyricsHTML(
		`[Verse 1]<br>Line with &amp; entity.<br><a href="/annotation">Annotated line text</a><script>evil()</script>`,
		`<div data-exclude-from-selection="true">Ad text</div>[Outro]<br>Final test line of song.`,
	)

	text, err := ParseLyrics(strings.NewReader(html))
	if err != nil {
		t.Fatalf("unexpected parse error: %v", err)
	}

	if strings.Contains(text, "evil()") {
		t.Error("script content was not excluded")
	}
	if strings.Contains(text, "Ad text") {
		t.Error("excluded-from-selection content was not stripped")
	}
	if !strings.Contains(text, "Annotated line text") {
		t.Error("link text was not preserved")
	}
	if !strings.Contains(text, "Line with & entity.") {
		t.Error("html entity was not unescaped")
	}
	if !strings.Contains(text, "[Outro]") {
		t.Error("outro section label missing")
	}
}

func TestGeniusProvider_CircuitBreaker(t *testing.T) {
	ctx := context.Background()
	track := music.Track{Title: "Test Track", Artists: []string{"Artist"}}

	callCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		// Return 429 Too Many Requests
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer server.Close()

	p := NewLyricsProvider(Config{
		Enabled:     true,
		AccessToken: "test-token",
		BaseURL:     server.URL,
		HTTPClient:  server.Client(),
	})

	// Make 3 failing calls to trip circuit breaker
	for i := 0; i < circuitBreakerThreshold; i++ {
		_, err := p.Lyrics(ctx, track, "")
		if err == nil {
			t.Fatalf("expected error on call %d", i+1)
		}
	}

	if !p.isTripped() {
		t.Fatal("expected circuit breaker to be tripped")
	}

	initialCalls := callCount
	// Next call must be blocked immediately by circuit breaker without reaching server
	res, err := p.Lyrics(ctx, track, "")
	if err != nil || res != nil {
		t.Fatalf("expected nil, nil when circuit breaker tripped, got %v, %v", res, err)
	}
	if callCount != initialCalls {
		t.Errorf("server was contacted even though circuit breaker was tripped")
	}
}
