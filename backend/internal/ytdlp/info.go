package ytdlp

import (
	"strings"
)

// DefaultFormatSelector prefers a native Opus stream and falls back to the
// best audio only stream the platform offers. Nothing is ever re-encoded here;
// the selector only decides which stream is fetched.
const DefaultFormatSelector = "bestaudio[acodec^=opus]/bestaudio/best"

// Info is the subset of yt-dlp's JSON output the backend uses.
type Info struct {
	Type        string  `json:"_type"`
	ID          string  `json:"id"`
	Title       string  `json:"title"`
	Track       string  `json:"track"`
	Artist      string  `json:"artist"`
	Creator     string  `json:"creator"`
	Album       string  `json:"album"`
	AlbumArtist string  `json:"album_artist"`
	Uploader    string  `json:"uploader"`
	Channel     string  `json:"channel"`
	ChannelID   string  `json:"channel_id"`
	Duration    float64 `json:"duration"`
	WebpageURL  string  `json:"webpage_url"`
	URL         string  `json:"url"`
	Extractor   string  `json:"extractor"`
	IsLive      bool    `json:"is_live"`
	LiveStatus  string  `json:"live_status"`
	ReleaseYear int     `json:"release_year"`
	TrackNumber int     `json:"track_number"`
	Thumbnail   string  `json:"thumbnail"`
	ViewCount   int64   `json:"view_count"`

	Formats []Format `json:"formats"`
}

// Format is one downloadable stream.
type Format struct {
	FormatID       string  `json:"format_id"`
	Ext            string  `json:"ext"`
	ACodec         string  `json:"acodec"`
	VCodec         string  `json:"vcodec"`
	ABR            float64 `json:"abr"`
	TBR            float64 `json:"tbr"`
	ASR            int     `json:"asr"`
	AudioChannels  int     `json:"audio_channels"`
	Filesize       int64   `json:"filesize"`
	FilesizeApprox int64   `json:"filesize_approx"`
	Container      string  `json:"container"`
	Protocol       string  `json:"protocol"`
}

// yt-dlp describes each stream of a format with three states: a codec name
// when the stream is known to be present, the literal "none" when it is known
// to be absent, and no value at all when it does not know. A missing codec is
// therefore unknown, never absent: yt-dlp's own bestaudio accepts any format
// whose video is "none", whatever it knows about the audio codec. Its HLS
// parser, for example, emits separate audio renditions with vcodec "none" and
// no acodec.

// codecAbsent reports a stream yt-dlp knows to be absent.
func codecAbsent(codec string) bool {
	return strings.EqualFold(strings.TrimSpace(codec), "none")
}

// codecKnown reports a stream yt-dlp knows to be present.
func codecKnown(codec string) bool {
	trimmed := strings.TrimSpace(codec)
	return trimmed != "" && !codecAbsent(trimmed)
}

// IsAudioOnly reports whether the format is an audio only stream: it carries
// no known video and its audio is not known to be absent. Either the video is
// explicitly "none" - the audio codec may then still be unknown - or the audio
// codec is known while the video is unknown. A format about which neither is
// known says nothing about carrying audio and does not qualify.
//
// An unknown field is not taken on trust: the downloaded file is inspected
// before it is kept, which settles the audio codec and rejects a stream that
// turns out to contain video.
func (f Format) IsAudioOnly() bool {
	if codecKnown(f.VCodec) || codecAbsent(f.ACodec) {
		return false
	}
	return codecAbsent(f.VCodec) || codecKnown(f.ACodec)
}

// HasAudio reports whether the format is known to carry an audio stream.
func (f Format) HasAudio() bool {
	return codecKnown(f.ACodec)
}

// AudioCodecKnown reports whether yt-dlp named the audio codec.
func (f Format) AudioCodecKnown() bool {
	return codecKnown(f.ACodec)
}

// IsOpus reports whether the audio stream is native Opus.
func (f Format) IsOpus() bool {
	return strings.HasPrefix(strings.ToLower(f.ACodec), "opus")
}

// Bitrate returns the audio bitrate in kbit/s, falling back to the total
// bitrate for audio only formats.
func (f Format) Bitrate() float64 {
	if f.ABR > 0 {
		return f.ABR
	}
	if f.IsAudioOnly() {
		return f.TBR
	}
	return 0
}

// Size returns the known or estimated file size.
func (f Format) Size() int64 {
	if f.Filesize > 0 {
		return f.Filesize
	}
	return f.FilesizeApprox
}

// DurationMS returns the duration in milliseconds.
func (i Info) DurationMS() int {
	if i.Duration <= 0 {
		return 0
	}
	return int(i.Duration*1000 + 0.5)
}

// PageURL returns the canonical page URL, falling back to a YouTube watch URL
// built from the id when yt-dlp only delivered a flat entry.
func (i Info) PageURL() string {
	if u := strings.TrimSpace(i.WebpageURL); u != "" {
		return u
	}
	if id := strings.TrimSpace(i.ID); id != "" {
		return "https://www.youtube.com/watch?v=" + id
	}
	return ""
}

// DisplayTitle returns the track title, preferring the structured music
// metadata YouTube Music delivers over the free form video title.
func (i Info) DisplayTitle() string {
	if t := strings.TrimSpace(i.Track); t != "" {
		return t
	}
	return strings.TrimSpace(i.Title)
}

// DisplayArtist returns the credited artist, preferring structured metadata.
func (i Info) DisplayArtist() string {
	for _, candidate := range []string{i.Artist, i.Creator, i.AlbumArtist} {
		if c := strings.TrimSpace(candidate); c != "" {
			return c
		}
	}
	return ""
}

// UploaderName returns the channel or uploader name.
func (i Info) UploaderName() string {
	if u := strings.TrimSpace(i.Uploader); u != "" {
		return u
	}
	return strings.TrimSpace(i.Channel)
}

// AudioFormats returns the audio only formats of an item.
func (i Info) AudioFormats() []Format {
	out := make([]Format, 0, len(i.Formats))
	for _, f := range i.Formats {
		if f.IsAudioOnly() {
			out = append(out, f)
		}
	}
	return out
}
