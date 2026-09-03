package metadata

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/binary"
	"image"
	"image/color"
	"image/jpeg"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"ytdm/backend/internal/music"
	"ytdm/backend/internal/provider"
)

// makeOpusFile renders a short Opus file with ffmpeg. Tests that need a real
// container are skipped when ffmpeg is unavailable.
func makeOpusFile(t *testing.T, seconds string) string {
	t.Helper()
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ffmpeg is not installed")
	}
	path := filepath.Join(t.TempDir(), "sample.opus")
	cmd := exec.Command("ffmpeg", "-hide_banner", "-loglevel", "error", "-y",
		"-f", "lavfi", "-i", "sine=frequency=440:duration="+seconds,
		"-c:a", "libopus", "-b:a", "64k", path)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		t.Skipf("ffmpeg could not produce an Opus file: %v: %s", err, stderr.String())
	}
	return path
}

func sampleTags() Tags {
	return TagsFor(music.Track{
		Title:       "Song",
		Artists:     []string{"Artist", "Guest"},
		Album:       "Album",
		AlbumArtist: "Artist",
		TrackNumber: 3,
		TrackTotal:  12,
		DiscNumber:  1,
		DiscTotal:   2,
		Year:        2001,
		ISRC:        "DEA123456789",
	}, provider.MediaSource{
		Provider: "ytmusic",
		ID:       "abc123",
		URL:      "https://music.youtube.com/watch?v=abc123",
	})
}

func TestTagsForBuildsEveryRequiredField(t *testing.T) {
	comments := sampleTags().Comments()
	joined := strings.Join(comments, "\n")

	for _, want := range []string{
		"TITLE=Song", "ARTIST=Artist", "ARTIST=Guest", "ALBUM=Album",
		"ALBUMARTIST=Artist", "TRACKNUMBER=3", "TRACKTOTAL=12",
		"DISCNUMBER=1", "DISCTOTAL=2", "DATE=2001", "ISRC=DEA123456789",
		"SOURCE=ytmusic", "SOURCE_ID=abc123",
		"SOURCE_URL=https://music.youtube.com/watch?v=abc123",
	} {
		if !strings.Contains(joined, want) {
			t.Errorf("comment %q is missing from:\n%s", want, joined)
		}
	}
}

func TestTagsOmitEmptyValues(t *testing.T) {
	tags := Tags{Title: "Song", Artists: []string{"Artist"}}
	for _, comment := range tags.Comments() {
		if strings.HasSuffix(comment, "=") {
			t.Errorf("empty value was written: %q", comment)
		}
	}
}

func TestApplyOpusWritesReadableTags(t *testing.T) {
	path := makeOpusFile(t, "2")
	tagger := NewTagger(nil)

	if err := tagger.Apply(context.Background(), path, sampleTags(), nil); err != nil {
		t.Fatalf("apply: %v", err)
	}

	tags, err := ReadTags(path)
	if err != nil {
		t.Fatalf("read tags: %v", err)
	}
	if got := tags["TITLE"]; len(got) != 1 || got[0] != "Song" {
		t.Errorf("TITLE = %v", got)
	}
	if got := tags["ARTIST"]; len(got) != 2 || got[0] != "Artist" || got[1] != "Guest" {
		t.Errorf("ARTIST = %v, want both credited artists", got)
	}
	if got := tags["ISRC"]; len(got) != 1 || got[0] != "DEA123456789" {
		t.Errorf("ISRC = %v", got)
	}
	if got := tags["SOURCE_URL"]; len(got) != 1 {
		t.Errorf("SOURCE_URL = %v", got)
	}
}

func TestApplyOpusKeepsAudioIntact(t *testing.T) {
	path := makeOpusFile(t, "3")
	if _, err := exec.LookPath("ffprobe"); err != nil {
		t.Skip("ffprobe is not installed")
	}

	before := probeDuration(t, path)
	beforeAudio := extractAudioPages(t, path)

	if err := NewTagger(nil).Apply(context.Background(), path, sampleTags(), nil); err != nil {
		t.Fatalf("apply: %v", err)
	}

	after := probeDuration(t, path)
	if before != after {
		t.Errorf("duration changed from %q to %q", before, after)
	}

	afterAudio := extractAudioPages(t, path)
	if !bytes.Equal(beforeAudio, afterAudio) {
		t.Error("the audio payload changed; tagging must not re-encode")
	}
}

func TestApplyOpusEmbedsCover(t *testing.T) {
	path := makeOpusFile(t, "2")
	artwork := makeArtwork(t, 300, 300)

	if err := NewTagger(nil).Apply(context.Background(), path, sampleTags(), artwork); err != nil {
		t.Fatalf("apply: %v", err)
	}

	tags, err := ReadTags(path)
	if err != nil {
		t.Fatalf("read tags: %v", err)
	}
	pictures := tags[FieldPicture]
	if len(pictures) != 1 {
		t.Fatalf("got %d picture blocks, want 1", len(pictures))
	}

	raw, err := base64.StdEncoding.DecodeString(pictures[0])
	if err != nil {
		t.Fatalf("the picture block is not valid base64: %v", err)
	}
	if got := binary.BigEndian.Uint32(raw[0:4]); got != pictureTypeFrontCover {
		t.Errorf("picture type = %d, want %d", got, pictureTypeFrontCover)
	}
	mimeLen := binary.BigEndian.Uint32(raw[4:8])
	if got := string(raw[8 : 8+mimeLen]); got != "image/jpeg" {
		t.Errorf("mime = %q", got)
	}
	if !bytes.HasSuffix(raw, artwork.Data) {
		t.Error("the image data is not part of the picture block")
	}
}

func TestApplyOpusIsRepeatable(t *testing.T) {
	path := makeOpusFile(t, "2")
	tagger := NewTagger(nil)

	for i := range 3 {
		if err := tagger.Apply(context.Background(), path, sampleTags(), makeArtwork(t, 200, 200)); err != nil {
			t.Fatalf("apply run %d: %v", i, err)
		}
	}

	tags, err := ReadTags(path)
	if err != nil {
		t.Fatalf("read tags: %v", err)
	}
	if got := tags["TITLE"]; len(got) != 1 {
		t.Fatalf("TITLE = %v, want exactly one entry after repeated tagging", got)
	}
	if got := tags[FieldPicture]; len(got) != 1 {
		t.Fatalf("got %d picture blocks after repeated tagging, want 1", len(got))
	}
}

func TestApplyRejectsMissingFile(t *testing.T) {
	err := NewTagger(nil).Apply(context.Background(), filepath.Join(t.TempDir(), "missing.opus"), sampleTags(), nil)
	if err == nil {
		t.Fatal("expected an error for a missing file")
	}
}

func TestNewArtworkRejectsUnsupportedData(t *testing.T) {
	if _, err := NewArtwork([]byte("not an image")); err == nil {
		t.Fatal("expected an error for non image data")
	}
	if _, err := NewArtwork(nil); err == nil {
		t.Fatal("expected an error for empty data")
	}
}

func makeArtwork(t *testing.T, width, height int) *Artwork {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, width, height))
	for y := range height {
		for x := range width {
			img.Set(x, y, color.RGBA{R: uint8(x % 256), G: uint8(y % 256), B: 128, A: 255})
		}
	}
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, nil); err != nil {
		t.Fatalf("encode cover: %v", err)
	}
	artwork, err := NewArtwork(buf.Bytes())
	if err != nil {
		t.Fatalf("new artwork: %v", err)
	}
	return artwork
}

func probeDuration(t *testing.T, path string) string {
	t.Helper()
	out, err := exec.Command("ffprobe", "-v", "error", "-show_entries", "format=duration",
		"-of", "default=noprint_wrappers=1:nokey=1", path).Output()
	if err != nil {
		t.Fatalf("ffprobe: %v", err)
	}
	return strings.TrimSpace(string(out))
}

// extractAudioPages returns the payload of every page after the comment
// header, i.e. the encoded audio itself.
func extractAudioPages(t *testing.T, path string) []byte {
	t.Helper()
	file, err := os.Open(path)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer file.Close()

	reader := newTestReader(file)
	if _, err := readPage(reader); err != nil {
		t.Fatalf("read identification header: %v", err)
	}
	for {
		page, err := readPage(reader)
		if err != nil {
			t.Fatalf("read comment header: %v", err)
		}
		if page.endsPacket() {
			break
		}
	}

	var audio []byte
	for {
		page, err := readPage(reader)
		if err != nil {
			break
		}
		audio = append(audio, page.payload...)
	}
	return audio
}

func TestTagsForNormalisesDiscNumbers(t *testing.T) {
	track := music.Track{
		Title: "A", Artists: []string{"X"}, Album: "B", AlbumArtist: "X",
		TrackNumber: 1, TrackTotal: 12,
	}
	tags := TagsFor(track, provider.MediaSource{Provider: "ytmusic", ID: "v"})

	if tags.DiscNumber != 1 || tags.DiscTotal != 1 {
		t.Fatalf("disc = %d/%d, want 1/1", tags.DiscNumber, tags.DiscTotal)
	}
	comments := tags.Comments()
	for _, want := range []string{"DISCNUMBER=1", "DISCTOTAL=1", "TRACKNUMBER=1", "TRACKTOTAL=12"} {
		if !hasComment(comments, want) {
			t.Errorf("comments missing %q: %v", want, comments)
		}
	}
}

// TestTagsForDoesNotInventATrackTotal keeps the tagger honest: a total the
// provider never delivered must not be conjured from the track number.
func TestTagsForDoesNotInventATrackTotal(t *testing.T) {
	track := music.Track{Title: "A", Artists: []string{"X"}, Album: "B", AlbumArtist: "X", TrackNumber: 7}
	tags := TagsFor(track, provider.MediaSource{})

	if tags.TrackTotal != 0 {
		t.Fatalf("TrackTotal = %d, want 0", tags.TrackTotal)
	}
	for _, comment := range tags.Comments() {
		if strings.HasPrefix(comment, "TRACKTOTAL=") {
			t.Errorf("an unknown track total must not be written: %q", comment)
		}
	}
}

func TestTagsForRaisesDiscTotalToTheDiscNumber(t *testing.T) {
	track := music.Track{Title: "A", Artists: []string{"X"}, DiscNumber: 3, DiscTotal: 0}
	tags := TagsFor(track, provider.MediaSource{})
	if tags.DiscNumber != 3 || tags.DiscTotal != 3 {
		t.Fatalf("disc = %d/%d, want 3/3", tags.DiscNumber, tags.DiscTotal)
	}
}

func TestTagsForMarksACompilation(t *testing.T) {
	track := music.Track{
		Title: "Hooked On A Feeling", Artists: []string{"Blue Swede"}, Album: "Awesome Mix",
		AlbumArtist: music.VariousArtists, ReleaseType: music.ReleaseCompilation,
		Compilation: true, TrackNumber: 1, TrackTotal: 12,
	}
	tags := TagsFor(track, provider.MediaSource{})
	if !tags.Compilation {
		t.Fatal("the compilation flag was not set")
	}
	comments := tags.Comments()
	for _, want := range []string{"COMPILATION=1", "ALBUMARTIST=Various Artists", "ARTIST=Blue Swede"} {
		if !hasComment(comments, want) {
			t.Errorf("comments missing %q: %v", want, comments)
		}
	}
}

func TestTagsForOrdinaryReleaseIsNotACompilation(t *testing.T) {
	track := music.Track{Title: "A", Artists: []string{"X", "Y"}, AlbumArtist: "X", TrackNumber: 1}
	tags := TagsFor(track, provider.MediaSource{})
	if tags.Compilation {
		t.Fatal("an ordinary release must not be flagged as a compilation")
	}
	for _, comment := range tags.Comments() {
		if strings.HasPrefix(comment, "COMPILATION=") {
			t.Errorf("unexpected %q", comment)
		}
	}
}

func TestTagsForWritesOneArtistCommentPerCredit(t *testing.T) {
	track := music.Track{Title: "CCN", Artists: []string{"LACAZETTE", "Bushido"}, AlbumArtist: "LACAZETTE"}
	comments := TagsFor(track, provider.MediaSource{}).Comments()

	if !hasComment(comments, "ARTIST=LACAZETTE") || !hasComment(comments, "ARTIST=Bushido") {
		t.Errorf("both credits must be written: %v", comments)
	}
	if !hasComment(comments, "ALBUMARTIST=LACAZETTE") {
		t.Errorf("album artist missing: %v", comments)
	}
}

func hasComment(comments []string, want string) bool {
	for _, comment := range comments {
		if comment == want {
			return true
		}
	}
	return false
}
