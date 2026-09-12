package provider

import (
	"context"
	"strings"

	"ytdm/backend/internal/music"
)

// MediaCandidate is one possible audio source for a wanted track. Candidates
// are scored by the matching engine before anything is downloaded.
type MediaCandidate struct {
	Provider string `json:"provider"`
	ID       string `json:"id"`
	URL      string `json:"url"`

	Title   string   `json:"title"`
	Artists []string `json:"artists,omitempty"`
	Album   string   `json:"album,omitempty"`

	DurationMS int    `json:"duration_ms"`
	ISRC       string `json:"isrc,omitempty"`

	// Uploader is the channel or account that published the item. It is used
	// as a fallback artist when no structured credit is available.
	Uploader string `json:"uploader,omitempty"`

	// IsMusicService reports whether the candidate comes from a dedicated
	// music catalogue rather than a general video platform.
	IsMusicService bool `json:"is_music_service"`
}

// Label renders a candidate for logs and API responses.
func (c MediaCandidate) Label() string {
	artist := music.PrimaryArtist(c.Artists)
	if artist == music.UnknownArtist && c.Uploader != "" {
		artist = c.Uploader
	}
	return artist + " - " + c.Title
}

// AudioFormat describes one audio stream offered by a media source.
type AudioFormat struct {
	ID          string  `json:"id"`
	Codec       string  `json:"codec"`
	Container   string  `json:"container"`
	BitrateKbps float64 `json:"bitrate_kbps"`
	SampleRate  int     `json:"sample_rate"`
	Channels    int     `json:"channels"`
	Filesize    int64   `json:"filesize"`

	// Combined marks a format that carries the audio inside a stream that also
	// carries video. Such a format is only ever offered when the item has no
	// audio only stream at all, and the downloader must strip the video before
	// the file may be stored. Codec then names the audio codec, never the
	// video one.
	Combined bool `json:"combined,omitempty"`
	// VideoCodec names the video stream of a combined format. It is empty for
	// an audio only format.
	VideoCodec string `json:"video_codec,omitempty"`
	// TransferBitrateKbps is what the whole format costs to transfer, audio
	// and video together. For an audio only format it equals the audio
	// bitrate; for a combined format it is the figure that decides whether the
	// transfer is worth it.
	TransferBitrateKbps float64 `json:"transfer_bitrate_kbps,omitempty"`
}

// IsOpus reports whether the format carries a native Opus stream.
func (f AudioFormat) IsOpus() bool {
	return strings.HasPrefix(strings.ToLower(f.Codec), "opus")
}

// MediaSource is a resolved, downloadable audio source.
type MediaSource struct {
	Provider string `json:"provider"`
	ID       string `json:"id"`
	URL      string `json:"url"`

	Title      string `json:"title"`
	Uploader   string `json:"uploader,omitempty"`
	DurationMS int    `json:"duration_ms"`

	// Formats lists the audio formats the source offers. It may be empty when
	// the provider does not enumerate formats up front; the downloader then
	// falls back to its own format selection.
	Formats []AudioFormat `json:"formats,omitempty"`

	// SessionID is the opaque identifier of the session used to resolve this source,
	// allowing the downloader to use the affine session without holding a control-plane lease.
	SessionID string `json:"session_id,omitempty"`
}

// MediaProvider finds and resolves audio sources for a wanted track.
type MediaProvider interface {
	// Name returns the stable provider identifier, e.g. "ytmusic".
	Name() string

	// Search returns candidates that might carry the wanted track.
	Search(ctx context.Context, track music.Track) ([]MediaCandidate, error)

	// Resolve turns a candidate into a concrete, downloadable source.
	Resolve(ctx context.Context, candidate MediaCandidate) (*MediaSource, error)
}

// SearchQuery builds the free text query used to look for a track on a media
// platform.
func SearchQuery(track music.Track) string {
	parts := make([]string, 0, 2)
	if artist := music.PrimaryArtist(track.Artists); artist != music.UnknownArtist {
		parts = append(parts, artist)
	}
	if title := strings.TrimSpace(track.Title); title != "" {
		parts = append(parts, title)
	}
	return strings.Join(parts, " ")
}

// canonicalAudioCodec reduces a codec name to the family it belongs to.
// Platforms and ffprobe spell the same codec differently: yt-dlp reports
// "mp4a.40.2" where ffprobe reports "aac", and both spellings must reach the
// same decision.
func canonicalAudioCodec(codec string) string {
	name := strings.ToLower(strings.TrimSpace(codec))
	if idx := strings.IndexByte(name, '.'); idx > 0 {
		name = name[:idx]
	}
	switch {
	case name == "aac" || name == "mp4a":
		return "aac"
	case name == "alac":
		return "alac"
	case name == "mp3" || name == "mp4a-mp3":
		return "mp3"
	case strings.HasPrefix(name, "opus"):
		return "opus"
	case name == "vorbis":
		return "vorbis"
	case name == "flac":
		return "flac"
	}
	return name
}

// extractableContainers maps an audio codec onto the container its packets can
// be copied into without re-encoding. It is an allow list on purpose: a codec
// that is not listed is rejected rather than guessed into a container that
// cannot hold it, which is what renaming a file extension would amount to.
var extractableContainers = map[string]string{
	"aac":    "m4a",
	"alac":   "m4a",
	"mp3":    "mp3",
	"opus":   "opus",
	"vorbis": "ogg",
	"flac":   "flac",
}

// ExtractableAudioCodec reports the container an audio codec can be copied
// into without a second lossy encode, and whether this backend supports it at
// all. It is the single authority for that question: the resolver uses it to
// decide whether a combined format is worth transferring, and the downloader
// uses it again on the codec ffprobe actually measured.
func ExtractableAudioCodec(codec string) (container string, ok bool) {
	container, ok = extractableContainers[canonicalAudioCodec(codec)]
	return container, ok
}
