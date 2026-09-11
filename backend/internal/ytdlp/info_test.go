package ytdlp

import "testing"

// The classification follows yt-dlp's three states per stream: a codec name
// (present), "none" (absent) and no value (unknown). JSON null decodes to the
// same empty string as an omitted field; yt-dlp's --dump-json omits unknown
// codecs.
func TestFormatClassificationFollowsYTDLPCodecStates(t *testing.T) {
	for _, tc := range []struct {
		name            string
		f               Format
		audioOnly       bool
		hasAudio, known bool
	}{
		{"audio only with codec", Format{ACodec: "opus", VCodec: "none"}, true, true, true},
		// yt-dlp's HLS parser emits audio renditions exactly like this, and its
		// own bestaudio selects them. They used to be rejected.
		{"audio rendition without codec", Format{VCodec: "none"}, true, false, false},
		{"audio codec known, video unknown", Format{ACodec: "mp4a.40.2"}, true, true, true},
		{"muxed", Format{ACodec: "mp4a.40.2", VCodec: "avc1.4d401f"}, false, true, true},
		{"video only", Format{ACodec: "none", VCodec: "vp9"}, false, false, false},
		{"video, audio unknown", Format{VCodec: "avc1"}, false, false, false},
		{"storyboard images", Format{ACodec: "none", VCodec: "none"}, false, false, false},
		{"nothing known", Format{}, false, false, false},
		{"case and space tolerant", Format{ACodec: " NONE ", VCodec: "None"}, false, false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.f.IsAudioOnly(); got != tc.audioOnly {
				t.Errorf("IsAudioOnly = %v, want %v", got, tc.audioOnly)
			}
			if got := tc.f.HasAudio(); got != tc.hasAudio {
				t.Errorf("HasAudio = %v, want %v", got, tc.hasAudio)
			}
			if got := tc.f.AudioCodecKnown(); got != tc.known {
				t.Errorf("AudioCodecKnown = %v, want %v", got, tc.known)
			}
		})
	}
}

func TestAudioFormatsKeepsRenditionsWithoutCodec(t *testing.T) {
	info := Info{Formats: []Format{
		{FormatID: "234", VCodec: "none"},
		{FormatID: "232", ACodec: "none", VCodec: "avc1.4D401F"},
		{FormatID: "sb0", ACodec: "none", VCodec: "none"},
		{FormatID: "18", ACodec: "mp4a.40.2", VCodec: "avc1.42001E"},
	}}
	got := info.AudioFormats()
	if len(got) != 1 || got[0].FormatID != "234" {
		t.Fatalf("audio formats = %+v, want the rendition only", got)
	}
}
