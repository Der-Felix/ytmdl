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

// IsAudioOnly reports whether the format carries audio and no video.
func (f Format) IsAudioOnly() bool {
	return f.HasAudio() && (f.VCodec == "" || f.VCodec == "none")
}

// HasAudio reports whether the format carries an audio stream.
func (f Format) HasAudio() bool {
	return f.ACodec != "" && f.ACodec != "none"
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
