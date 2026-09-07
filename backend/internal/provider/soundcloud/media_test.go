package soundcloud

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"ytdm/backend/internal/apperr"
	"ytdm/backend/internal/music"
	"ytdm/backend/internal/provider"
	"ytdm/backend/internal/ytdlp"
)

func TestSoundCloudProvider_Basics(t *testing.T) {
	_, err := New(Config{Client: nil})
	if err == nil {
		t.Fatal("expected error for nil client")
	}

	client := ytdlp.New(ytdlp.Options{})
	p, err := New(Config{Client: client, Limit: 0})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if p.Name() != ProviderName {
		t.Errorf("Name() = %q, want %q", p.Name(), ProviderName)
	}
	if p.Family() != provider.FamilySoundCloud {
		t.Errorf("Family() = %q, want %q", p.Family(), provider.FamilySoundCloud)
	}
	if p.limit != 10 {
		t.Errorf("expected default limit 10, got %d", p.limit)
	}
}

func TestSoundCloud_Titles(t *testing.T) {
	artists := splitArtists("Artist feat. Guest, SecondGuest")
	if len(artists) != 3 {
		t.Fatalf("expected 3 artists, got %d (%v)", len(artists), artists)
	}
	if artists[0] != "Artist" || artists[1] != "Guest" || artists[2] != "SecondGuest" {
		t.Errorf("unexpected artists: %v", artists)
	}

	artist, title, ok := splitArtistTitle("Major Artist - Hit Track")
	if !ok || artist != "Major Artist" || title != "Hit Track" {
		t.Errorf("splitArtistTitle failed: got (%q, %q, %v)", artist, title, ok)
	}

	_, _, ok = splitArtistTitle("JustATitle")
	if ok {
		t.Errorf("expected splitArtistTitle to return false for single title")
	}
}

func TestSoundCloud_ToCandidate(t *testing.T) {
	client := ytdlp.New(ytdlp.Options{})
	p, _ := New(Config{Client: client})

	// 1. Valid track
	info := ytdlp.Info{
		ID:         "1001",
		Title:      "Some Song",
		Artist:     "The Artist",
		Uploader:   "ArtistOfficial",
		Duration:   215.5,
		WebpageURL: "https://soundcloud.com/artistofficial/some-song",
		Album:      "Sample Album",
	}
	cand, ok := p.toCandidate(info)
	if !ok {
		t.Fatal("expected candidate to be accepted")
	}
	if cand.Provider != ProviderName {
		t.Errorf("expected provider %q, got %q", ProviderName, cand.Provider)
	}
	if cand.ID != "1001" {
		t.Errorf("expected ID 1001, got %q", cand.ID)
	}
	if cand.Title != "Some Song" {
		t.Errorf("expected Title 'Some Song', got %q", cand.Title)
	}
	if len(cand.Artists) != 1 || cand.Artists[0] != "The Artist" {
		t.Errorf("expected Artists ['The Artist'], got %v", cand.Artists)
	}
	if cand.DurationMS != 215500 {
		t.Errorf("expected DurationMS 215500, got %d", cand.DurationMS)
	}
	if cand.Album != "Sample Album" {
		t.Errorf("expected Album 'Sample Album', got %q", cand.Album)
	}
	if cand.IsMusicService {
		t.Errorf("IsMusicService should be false for SoundCloud")
	}

	// 2. Title with Artist - Title format and missing structured artist
	info2 := ytdlp.Info{
		ID:         "1002",
		Title:      "Producer - Beat Track",
		Uploader:   "SoundCloudReposter",
		Duration:   180,
		WebpageURL: "https://soundcloud.com/soundcloudreposter/beat-track",
	}
	cand2, ok := p.toCandidate(info2)
	if !ok {
		t.Fatal("expected candidate to be accepted")
	}
	if len(cand2.Artists) != 1 || cand2.Artists[0] != "Producer" {
		t.Errorf("expected Artists ['Producer'], got %v", cand2.Artists)
	}
	if cand2.Title != "Beat Track" {
		t.Errorf("expected Title 'Beat Track', got %q", cand2.Title)
	}
	if cand2.Uploader != "SoundCloudReposter" {
		t.Errorf("expected Uploader 'SoundCloudReposter', got %q", cand2.Uploader)
	}

	// 3. Fallback to uploader when no artist and no separator in title
	info3 := ytdlp.Info{
		ID:         "1003",
		Title:      "UniqueTrackName",
		Uploader:   "SoloMusician",
		Duration:   120,
		WebpageURL: "https://soundcloud.com/solomusician/uniquetrackname",
	}
	cand3, ok := p.toCandidate(info3)
	if !ok {
		t.Fatal("expected candidate to be accepted")
	}
	if len(cand3.Artists) != 1 || cand3.Artists[0] != "SoloMusician" {
		t.Errorf("expected Artists ['SoloMusician'], got %v", cand3.Artists)
	}

	// 4. Filter live status
	infoLive := info
	infoLive.IsLive = true
	if _, ok := p.toCandidate(infoLive); ok {
		t.Error("expected live stream to be filtered")
	}

	// 5. Filter empty ID
	infoNoID := info
	infoNoID.ID = ""
	if _, ok := p.toCandidate(infoNoID); ok {
		t.Error("expected empty ID to be filtered")
	}

	// 6. Filter playlist / sets URL
	infoSet := info
	infoSet.WebpageURL = "https://soundcloud.com/artist/sets/full-album"
	if _, ok := p.toCandidate(infoSet); ok {
		t.Error("expected sets URL to be filtered")
	}

	// 7. Filter non-SoundCloud URL
	infoNonSC := info
	infoNonSC.WebpageURL = "https://youtube.com/watch?v=12345"
	if _, ok := p.toCandidate(infoNonSC); ok {
		t.Error("expected non-SoundCloud URL to be filtered")
	}
}

func TestSoundCloud_SearchAndResolve_Offline(t *testing.T) {
	tempDir := t.TempDir()
	fakeYTDLP := filepath.Join(tempDir, "fake_ytdlp.sh")

	// Script outputs JSON lines when called with scsearch, and single item when called with URL
	scriptContent := `#!/bin/sh
for arg in "$@"; do
    if [ "$arg" = "--version" ]; then
        echo "2026.01.01"
        exit 0
    fi
done

# Check if search
case "$*" in
    *scsearch*)
        echo '{"id":"sc101","title":"Search Artist - First Track","duration":190.0,"webpage_url":"https://soundcloud.com/searchartist/first-track","uploader":"SearchArtist"}'
        echo '{"id":"sc102","title":"Search Artist - Second Track","duration":210.0,"webpage_url":"https://soundcloud.com/searchartist/second-track","uploader":"SearchArtist"}'
        exit 0
        ;;
    *first-track*)
        echo '{"id":"sc101","title":"Search Artist - First Track","duration":190.0,"webpage_url":"https://soundcloud.com/searchartist/first-track","uploader":"SearchArtist","formats":[{"format_id":"hls_mp3_128k","acodec":"mp3","vcodec":"none","abr":128,"asr":44100,"audio_channels":2,"filesize":3040000},{"format_id":"hls_aac_160k","acodec":"aac","vcodec":"none","abr":160,"asr":44100,"audio_channels":2,"filesize":3800000}]}'
        exit 0
        ;;
    *)
        echo "Unknown target" >&2
        exit 1
        ;;
esac
`
	if err := os.WriteFile(fakeYTDLP, []byte(scriptContent), 0755); err != nil {
		t.Fatalf("failed to write fake ytdlp script: %v", err)
	}

	client := ytdlp.New(ytdlp.Options{
		Binary: fakeYTDLP,
	})

	p, err := New(Config{
		Client: client,
		Limit:  5,
	})
	if err != nil {
		t.Fatalf("New failed: %v", err)
	}

	// 1. Available check
	if err := p.Available(context.Background()); err != nil {
		t.Fatalf("Available check failed: %v", err)
	}

	// 2. Search
	track := music.Track{
		Title:   "First Track",
		Artists: []string{"Search Artist"},
	}
	candidates, err := p.Search(context.Background(), track)
	if err != nil {
		t.Fatalf("Search failed: %v", err)
	}
	if len(candidates) != 2 {
		t.Fatalf("expected 2 candidates, got %d", len(candidates))
	}
	if candidates[0].ID != "sc101" || candidates[0].Provider != "soundcloud" {
		t.Errorf("unexpected candidate 0: %+v", candidates[0])
	}
	if candidates[0].DurationMS != 190000 {
		t.Errorf("expected DurationMS 190000, got %d", candidates[0].DurationMS)
	}

	// 3. Resolve
	src, err := p.Resolve(context.Background(), candidates[0])
	if err != nil {
		t.Fatalf("Resolve failed: %v", err)
	}
	if src.Provider != "soundcloud" {
		t.Errorf("expected provider soundcloud, got %q", src.Provider)
	}
	if src.ID != "sc101" {
		t.Errorf("expected ID sc101, got %q", src.ID)
	}
	if src.SessionID != "" {
		t.Errorf("expected empty SessionID, got %q", src.SessionID)
	}
	if len(src.Formats) != 2 {
		t.Fatalf("expected 2 audio formats, got %d", len(src.Formats))
	}
	if src.Formats[0].Codec != "mp3" || src.Formats[1].Codec != "aac" {
		t.Errorf("unexpected codecs: %+v", src.Formats)
	}
}

func TestSoundCloud_Search_EmptyQuery(t *testing.T) {
	client := ytdlp.New(ytdlp.Options{})
	p, _ := New(Config{Client: client})

	_, err := p.Search(context.Background(), music.Track{})
	if err == nil {
		t.Fatal("expected error for empty track")
	}
	if apperr.CodeOf(err) != apperr.CodeInvalidRequest {
		t.Errorf("expected CodeInvalidRequest, got %v", apperr.CodeOf(err))
	}
}

func TestSoundCloud_LimiterPacing(t *testing.T) {
	l := newLimiter(10.0, 1) // 10 rps, burst 1
	ctx := context.Background()

	start := time.Now()
	if err := l.Wait(ctx); err != nil {
		t.Fatalf("Wait 1 failed: %v", err)
	}
	if err := l.Wait(ctx); err != nil {
		t.Fatalf("Wait 2 failed: %v", err)
	}
	elapsed := time.Since(start)

	// Second request should wait approx 100ms
	if elapsed < 50*time.Millisecond {
		t.Errorf("expected pacing delay, got %v", elapsed)
	}
}
