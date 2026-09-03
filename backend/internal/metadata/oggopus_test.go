package metadata

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/binary"
	"io"
	"os"
	"os/exec"
	"strings"
	"testing"

	"ytdm/backend/internal/music"
	"ytdm/backend/internal/provider"
)

// pageView is one physical Ogg page as it appears in a file on disk, kept
// together with the raw bytes so that the checksum can be recomputed.
type pageView struct {
	headerType byte
	granule    int64
	serial     uint32
	sequence   uint32
	segments   []byte
	payload    []byte
	raw        []byte
	crc        uint32
}

// readPages walks the whole file page by page, straight from the bytes rather
// than through the writer's own encoder, so that a bug in the encoder cannot
// hide behind a matching decoder.
func readPages(t *testing.T, path string) []pageView {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}

	var pages []pageView
	for offset := 0; offset < len(raw); {
		if len(raw)-offset < oggHeaderSize {
			t.Fatalf("page %d at offset %d is truncated", len(pages), offset)
		}
		header := raw[offset : offset+oggHeaderSize]
		if string(header[0:4]) != oggCapturePattern {
			t.Fatalf("page %d does not start with %q", len(pages), oggCapturePattern)
		}
		if header[4] != 0 {
			t.Fatalf("page %d has stream structure version %d, want 0", len(pages), header[4])
		}

		count := int(header[26])
		segStart := offset + oggHeaderSize
		if len(raw) < segStart+count {
			t.Fatalf("page %d has a truncated segment table", len(pages))
		}
		segments := raw[segStart : segStart+count]

		size := 0
		for _, s := range segments {
			size += int(s)
		}
		payloadStart := segStart + count
		if len(raw) < payloadStart+size {
			t.Fatalf("page %d has a truncated payload", len(pages))
		}

		page := pageView{
			headerType: header[5],
			granule:    int64(binary.LittleEndian.Uint64(header[6:14])),
			serial:     binary.LittleEndian.Uint32(header[14:18]),
			sequence:   binary.LittleEndian.Uint32(header[18:22]),
			crc:        binary.LittleEndian.Uint32(header[22:26]),
			segments:   segments,
			payload:    raw[payloadStart : payloadStart+size],
			raw:        raw[offset : payloadStart+size],
		}
		pages = append(pages, page)
		offset = payloadStart + size
	}
	if len(pages) == 0 {
		t.Fatal("the file contains no Ogg page")
	}
	return pages
}

// assertStreamIsWellFormed checks the invariants of RFC 3533 and RFC 7845 that
// the comment rewriter has to preserve.
func assertStreamIsWellFormed(t *testing.T, path string) []pageView {
	t.Helper()
	pages := readPages(t, path)

	serial := pages[0].serial
	for i, page := range pages {
		// The checksum covers the whole page with the checksum field zeroed.
		check := bytes.Clone(page.raw)
		binary.LittleEndian.PutUint32(check[22:26], 0)
		if got := oggCRC(check); got != page.crc {
			t.Fatalf("page %d has checksum %#08x, want %#08x", i, page.crc, got)
		}
		if page.serial != serial {
			t.Fatalf("page %d has serial %d, want %d", i, page.serial, serial)
		}
		if page.sequence != uint32(i) {
			t.Fatalf("page %d carries sequence number %d", i, page.sequence)
		}
		if len(page.segments) == 0 && i != len(pages)-1 {
			t.Fatalf("page %d has an empty segment table", i)
		}
		size := 0
		for _, seg := range page.segments {
			size += int(seg)
		}
		if size != len(page.payload) {
			t.Fatalf("page %d declares %d payload bytes but carries %d", i, size, len(page.payload))
		}
	}

	if pages[0].headerType&headerTypeBOS == 0 {
		t.Fatal("the first page is not marked begin-of-stream")
	}
	if !strings.HasPrefix(string(pages[0].payload), opusHeadMagic) {
		t.Fatal("the first page does not carry the OpusHead packet")
	}
	if pages[0].granule != 0 {
		t.Fatalf("the identification header has granule %d, want 0", pages[0].granule)
	}
	return pages
}

// commentPacket reassembles the OpusTags packet from the pages that follow the
// identification header.
func commentPacket(t *testing.T, pages []pageView) []byte {
	t.Helper()
	var packet []byte
	for i := 1; i < len(pages); i++ {
		packet = append(packet, pages[i].payload...)
		if pages[i].granule != 0 {
			t.Fatalf("comment page %d has granule %d, want 0", i, pages[i].granule)
		}
		if i > 1 && pages[i].headerType&headerTypeContinued == 0 {
			t.Fatalf("comment page %d is not marked as a continuation", i)
		}
		last := pages[i].segments[len(pages[i].segments)-1]
		if last < maxSegmentSize {
			return packet
		}
	}
	t.Fatal("the comment packet is never terminated")
	return nil
}

// TestApplyOpusKeepsTheStreamValid is the structural guarantee of the native
// tag writer: rewriting the comment header must leave a stream that still
// satisfies the Ogg framing rules, with the identification header untouched.
func TestApplyOpusKeepsTheStreamValid(t *testing.T) {
	path := makeOpusFile(t, "3")

	before := readPages(t, path)
	headBefore := bytes.Clone(before[0].payload)
	audioBefore := extractAudioPages(t, path)

	if err := NewTagger(nil).Apply(context.Background(), path, sampleTags(), nil); err != nil {
		t.Fatalf("apply: %v", err)
	}

	after := assertStreamIsWellFormed(t, path)
	if !bytes.Equal(after[0].payload, headBefore) {
		t.Fatal("the OpusHead packet was modified")
	}
	if !bytes.Equal(extractAudioPages(t, path), audioBefore) {
		t.Fatal("the audio payload changed; tagging must not touch the samples")
	}
	commentPacket(t, after)
}

// TestApplyOpusHandlesLargeCovers pushes the comment packet far past the 255
// byte segment limit and past the 255 segments a single page can hold, which
// is where the lacing and continuation logic actually has to work.
func TestApplyOpusHandlesLargeCovers(t *testing.T) {
	path := makeOpusFile(t, "2")
	artwork := makeArtwork(t, 1400, 1400)
	if len(artwork.Data) < 64*1024 {
		t.Fatalf("the test cover is only %d bytes; it has to span several pages", len(artwork.Data))
	}

	audioBefore := extractAudioPages(t, path)
	if err := NewTagger(nil).Apply(context.Background(), path, sampleTags(), artwork); err != nil {
		t.Fatalf("apply: %v", err)
	}

	pages := assertStreamIsWellFormed(t, path)
	packet := commentPacket(t, pages)

	// The comment header has to have needed more than one page.
	commentPages := 0
	for i := 1; i < len(pages) && pages[i].granule == 0; i++ {
		commentPages++
	}
	if commentPages < 2 {
		t.Fatalf("the comment header occupies %d page(s); the large cover should need several", commentPages)
	}
	if len(packet) < len(artwork.Data) {
		t.Fatalf("the comment packet is %d bytes, smaller than the %d byte cover", len(packet), len(artwork.Data))
	}
	if !bytes.Equal(extractAudioPages(t, path), audioBefore) {
		t.Fatal("the audio payload changed while a large cover was embedded")
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
	if !bytes.HasSuffix(raw, artwork.Data) {
		t.Fatal("the embedded image does not match the cover that was written")
	}

	assertPlayable(t, path)
}

// TestApplyOpusWritesUnicodeMetadata covers the fields the providers actually
// deliver: names with diacritics, non-Latin scripts and emoji have to survive
// the round trip byte for byte.
func TestApplyOpusWritesUnicodeMetadata(t *testing.T) {
	path := makeOpusFile(t, "2")

	tags := TagsFor(music.Track{
		Title:       "Ágætis byrjun — 日本語 🎧",
		Artists:     []string{"Sigur Rós", "Ólafur Arnalds", "坂本龍一"},
		Album:       "Þeir Hafa Sloppið",
		AlbumArtist: "Sigur Rós",
		TrackNumber: 7,
		TrackTotal:  10,
		DiscNumber:  1,
		DiscTotal:   1,
		Year:        1999,
		ISRC:        "ISA123456789",
	}, provider.MediaSource{
		Provider: "ytmusic",
		ID:       "üñïcödé-id",
		URL:      "https://music.youtube.com/watch?v=abc",
	})

	if err := NewTagger(nil).Apply(context.Background(), path, tags, nil); err != nil {
		t.Fatalf("apply: %v", err)
	}
	assertStreamIsWellFormed(t, path)

	stored, err := ReadTags(path)
	if err != nil {
		t.Fatalf("read tags: %v", err)
	}

	single := map[string]string{
		FieldTitle:       "Ágætis byrjun — 日本語 🎧",
		FieldAlbum:       "Þeir Hafa Sloppið",
		FieldAlbumArtist: "Sigur Rós",
		FieldTrackNumber: "7",
		FieldTrackTotal:  "10",
		FieldDate:        "1999",
		FieldISRC:        "ISA123456789",
		FieldSource:      "ytmusic",
		FieldSourceID:    "üñïcödé-id",
	}
	for key, want := range single {
		got := stored[key]
		if len(got) != 1 || got[0] != want {
			t.Errorf("%s = %v, want [%q]", key, got, want)
		}
	}

	wantArtists := []string{"Sigur Rós", "Ólafur Arnalds", "坂本龍一"}
	got := stored[FieldArtist]
	if len(got) != len(wantArtists) {
		t.Fatalf("ARTIST = %v, want %v", got, wantArtists)
	}
	for i, want := range wantArtists {
		if got[i] != want {
			t.Errorf("ARTIST[%d] = %q, want %q", i, got[i], want)
		}
	}

	assertPlayable(t, path)
}

// TestApplyOpusRejectsForeignContainers pins that the native writer refuses a
// file that is not an Ogg Opus stream instead of corrupting it.
func TestApplyOpusRejectsForeignContainers(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/not-really.opus"
	if err := os.WriteFile(path, []byte("this is not an Ogg stream at all"), 0o600); err != nil {
		t.Fatalf("write file: %v", err)
	}

	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read file: %v", err)
	}
	if err := NewTagger(nil).Apply(context.Background(), path, sampleTags(), nil); err == nil {
		t.Fatal("a file that is not an Ogg stream was accepted")
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read file: %v", err)
	}
	if !bytes.Equal(before, after) {
		t.Fatal("the rejected file was modified")
	}
}

// TestLacingTerminatesPackets covers the rule that trips up most Ogg writers:
// a packet whose length is an exact multiple of 255 needs an explicit zero
// segment, or the next packet would be read as its continuation.
func TestLacingTerminatesPackets(t *testing.T) {
	tests := []struct {
		length int
		want   []byte
	}{
		{0, []byte{0}},
		{1, []byte{1}},
		{254, []byte{254}},
		{255, []byte{255, 0}},
		{256, []byte{255, 1}},
		{510, []byte{255, 255, 0}},
	}
	for _, test := range tests {
		got := lacing(test.length)
		if !bytes.Equal(got, test.want) {
			t.Errorf("lacing(%d) = %v, want %v", test.length, got, test.want)
		}
		total := 0
		for _, s := range got {
			total += int(s)
		}
		if total != test.length {
			t.Errorf("lacing(%d) encodes %d bytes", test.length, total)
		}
		if got[len(got)-1] == maxSegmentSize {
			t.Errorf("lacing(%d) does not terminate the packet", test.length)
		}
	}
}

// assertPlayable runs ffprobe over the finished file: the stream has to remain
// a readable Opus stream after the comment header was replaced.
func assertPlayable(t *testing.T, path string) {
	t.Helper()
	if _, err := exec.LookPath("ffprobe"); err != nil {
		t.Skip("ffprobe is not installed")
	}
	out, err := exec.Command("ffprobe", "-v", "error",
		"-select_streams", "a:0",
		"-show_entries", "stream=codec_name,sample_rate,channels",
		"-of", "default=noprint_wrappers=1", path).Output()
	if err != nil {
		t.Fatalf("ffprobe rejected the tagged file: %v", err)
	}
	report := string(out)
	if !strings.Contains(report, "codec_name=opus") {
		t.Fatalf("ffprobe reports %q, want an opus stream", strings.TrimSpace(report))
	}

	// Decoding the whole file proves the framing survived, not just the header.
	decode := exec.Command("ffmpeg", "-v", "error", "-i", path, "-f", "null", "-")
	var stderr bytes.Buffer
	decode.Stderr = &stderr
	decode.Stdout = io.Discard
	if err := decode.Run(); err != nil {
		t.Fatalf("the tagged file could not be decoded: %v\n%s", err, stderr.String())
	}
	if message := strings.TrimSpace(stderr.String()); message != "" {
		t.Fatalf("decoding the tagged file reported problems:\n%s", message)
	}
}

func TestUpdateOpusArtwork_PreservesAudioPayloadAndStreamInvariants(t *testing.T) {
	path := makeOpusFile(t, "4")

	initialTags := TagsFor(music.Track{
		Title:       "Papercut",
		Artists:     []string{"Linkin Park"},
		Album:       "Hybrid Theory",
		AlbumArtist: "Linkin Park",
		TrackNumber: 1,
		TrackTotal:  12,
		Year:        2000,
	}, provider.MediaSource{
		Provider: "ytmusic",
		ID:       "84J_XmGkX48",
	})

	initialCover := makeArtwork(t, 400, 400)
	if err := NewTagger(nil).Apply(context.Background(), path, initialTags, initialCover); err != nil {
		t.Fatalf("initial apply: %v", err)
	}

	audioBefore := extractAudioPages(t, path)
	tagsBefore, err := ReadTags(path)
	if err != nil {
		t.Fatalf("read tags before: %v", err)
	}

	// Create a new distinct replacement cover
	newCover := makeArtwork(t, 1200, 1200)

	// Update the artwork using UpdateOpusArtwork / Tagger.UpdateArtwork
	if err := NewTagger(nil).UpdateArtwork(context.Background(), path, newCover); err != nil {
		t.Fatalf("update artwork: %v", err)
	}

	// 1. Verify stream is well-formed according to RFC 3533 & RFC 7845
	assertStreamIsWellFormed(t, path)

	// 2. Invariant: Audio payload must be 100% byte-for-byte identical (zero re-encode)
	audioAfter := extractAudioPages(t, path)
	if !bytes.Equal(audioAfter, audioBefore) {
		t.Fatal("audio payload changed during artwork update; audio must remain bit-identical")
	}

	// 3. Verify text tags are preserved
	tagsAfter, err := ReadTags(path)
	if err != nil {
		t.Fatalf("read tags after: %v", err)
	}
	if tagsAfter[FieldTitle][0] != "Papercut" || tagsAfter[FieldAlbum][0] != "Hybrid Theory" {
		t.Fatalf("text tags corrupted: %+v, want %+v", tagsAfter, tagsBefore)
	}

	// 4. Verify embedded artwork is updated to newCover
	pictures := tagsAfter[FieldPicture]
	if len(pictures) != 1 {
		t.Fatalf("got %d picture blocks, want 1", len(pictures))
	}
	rawPic, err := base64.StdEncoding.DecodeString(pictures[0])
	if err != nil {
		t.Fatalf("picture decode: %v", err)
	}
	if !bytes.HasSuffix(rawPic, newCover.Data) {
		t.Fatal("embedded picture does not match replacement cover data")
	}

	assertPlayable(t, path)
}
