// Package downloader turns a resolved media source into a verified audio file
// in the library's working directory. It drives yt-dlp and ffmpeg but contains
// no provider, matching or queue logic.
package downloader

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"ytdm/backend/internal/apperr"
)

// AudioInfo is the verified description of an audio file as reported by
// ffprobe.
type AudioInfo struct {
	Codec       string  `json:"codec"`
	Container   string  `json:"container"`
	BitrateKbps float64 `json:"bitrate_kbps"`
	SampleRate  int     `json:"sample_rate"`
	Channels    int     `json:"channels"`
	DurationMS  int     `json:"duration_ms"`
	SizeBytes   int64   `json:"size_bytes"`
	// VideoStreams counts real video streams. An embedded cover is marked as
	// an attached picture and does not count.
	VideoStreams int `json:"video_streams,omitempty"`
}

// IsOpus reports whether the file carries an Opus stream.
func (a AudioInfo) IsOpus() bool {
	return strings.EqualFold(a.Codec, "opus")
}

// Prober inspects audio files with ffprobe.
type Prober struct {
	binary  string
	timeout time.Duration
	logger  *slog.Logger
}

// ProberOptions configures a Prober.
type ProberOptions struct {
	Binary  string
	Timeout time.Duration
	Logger  *slog.Logger
}

// NewProber builds a Prober. An empty binary name falls back to "ffprobe".
func NewProber(opts ProberOptions) *Prober {
	binary := strings.TrimSpace(opts.Binary)
	if binary == "" {
		binary = "ffprobe"
	}
	timeout := opts.Timeout
	if timeout <= 0 {
		timeout = 60 * time.Second
	}
	logger := opts.Logger
	if logger == nil {
		logger = slog.Default()
	}
	return &Prober{binary: binary, timeout: timeout, logger: logger}
}

// Available reports whether ffprobe can be executed.
func (p *Prober) Available(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, p.binary, "-version")
	if err := cmd.Run(); err != nil {
		if errors.Is(err, exec.ErrNotFound) || errors.Is(err, os.ErrNotExist) {
			return apperr.Wrapf(apperr.CodeToolUnavailable, err,
				"ffprobe was not found at %q.", p.binary)
		}
		return apperr.Wrap(apperr.CodeToolUnavailable, "ffprobe could not be started.", err)
	}
	return nil
}

// ffprobeOutput mirrors the parts of ffprobe's JSON output that are used.
type ffprobeOutput struct {
	Streams []struct {
		CodecName   string `json:"codec_name"`
		CodecType   string `json:"codec_type"`
		SampleRate  string `json:"sample_rate"`
		Channels    int    `json:"channels"`
		BitRate     string `json:"bit_rate"`
		Duration    string `json:"duration"`
		Disposition struct {
			AttachedPic int `json:"attached_pic"`
		} `json:"disposition"`
	} `json:"streams"`
	Format struct {
		FormatName string `json:"format_name"`
		Duration   string `json:"duration"`
		BitRate    string `json:"bit_rate"`
		Size       string `json:"size"`
	} `json:"format"`
}

// Probe inspects the file at path.
func (p *Prober) Probe(ctx context.Context, path string) (*AudioInfo, error) {
	stat, statErr := os.Stat(path)
	if statErr == nil && stat.Size() == 0 {
		return nil, invalidAudio("empty_file", "The downloaded file is empty.", &AudioInfo{})
	}
	ctx, cancel := context.WithTimeout(ctx, p.timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, p.binary,
		"-v", "error",
		"-print_format", "json",
		"-show_format",
		"-show_streams",
		"--", path)

	var stdout bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = io.Discard

	if err := cmd.Run(); err != nil {
		if errors.Is(err, exec.ErrNotFound) || errors.Is(err, os.ErrNotExist) {
			return nil, &verificationFailure{reason: "ffprobe_error", info: probeFileSize(stat, statErr), cause: apperr.New(apperr.CodeToolUnavailable, "ffprobe could not be started.")}
		}
		return nil, invalidAudio("ffprobe_error", "The downloaded file could not be inspected.", probeFileSize(stat, statErr))
	}

	var out ffprobeOutput
	if err := json.Unmarshal(stdout.Bytes(), &out); err != nil {
		return nil, invalidAudio("ffprobe_error", "The ffprobe output could not be decoded.", probeFileSize(stat, statErr))
	}

	info := &AudioInfo{Container: primaryFormatName(out.Format.FormatName)}

	var audioFound bool
	for _, stream := range out.Streams {
		if strings.EqualFold(stream.CodecType, "video") && stream.Disposition.AttachedPic == 0 {
			info.VideoStreams++
		}
	}
	for _, stream := range out.Streams {
		if !strings.EqualFold(stream.CodecType, "audio") {
			continue
		}
		audioFound = true
		info.Codec = strings.ToLower(stream.CodecName)
		info.Channels = stream.Channels
		info.SampleRate = atoiSafe(stream.SampleRate)
		if br := atofSafe(stream.BitRate); br > 0 {
			info.BitrateKbps = br / 1000
		}
		if d := atofSafe(stream.Duration); d > 0 {
			info.DurationMS = int(d*1000 + 0.5)
		}
		break
	}

	if info.DurationMS == 0 {
		if d := atofSafe(out.Format.Duration); d > 0 {
			info.DurationMS = int(d*1000 + 0.5)
		}
	}
	if info.BitrateKbps == 0 {
		if br := atofSafe(out.Format.BitRate); br > 0 {
			info.BitrateKbps = br / 1000
		}
	}
	info.SizeBytes = int64(atofSafe(out.Format.Size))
	if info.SizeBytes == 0 {
		if stat, err := os.Stat(path); err == nil {
			info.SizeBytes = stat.Size()
		}
	}
	if !audioFound {
		return nil, invalidAudio("missing_audio_stream", "The downloaded file contains no audio stream.", info)
	}
	return info, nil
}

// Verify checks a probed file against the expected runtime. A file that is far
// shorter or longer than the catalogue says is rejected instead of being filed
// away as the wanted track.
func Verify(info *AudioInfo, expectedDurationMS, toleranceMS int) error {
	if info == nil {
		return invalidAudio("ffprobe_error", "The downloaded file was not inspected.", nil)
	}
	if info.Codec == "" {
		return invalidAudio("missing_audio_stream", "The downloaded file contains no audio stream.", info)
	}
	if info.DurationMS <= 0 {
		return invalidAudio("invalid_duration", "The downloaded file has no playable duration.", info)
	}
	if info.SizeBytes <= 0 {
		return invalidAudio("empty_file", "The downloaded file is empty.", info)
	}
	if expectedDurationMS > 0 {
		if toleranceMS <= 0 {
			toleranceMS = 15000
		}
		diff := info.DurationMS - expectedDurationMS
		if diff < 0 {
			diff = -diff
		}
		if diff > toleranceMS {
			return invalidAudio("duration_mismatch", fmt.Sprintf(
				"The downloaded audio is %.1fs long, but %.1fs were expected.",
				float64(info.DurationMS)/1000, float64(expectedDurationMS)/1000), info)
		}
	}
	return nil
}

// primaryFormatName reduces ffprobe's comma separated format list to its first
// entry, which is the one that actually describes the container.
func primaryFormatName(name string) string {
	name = strings.TrimSpace(name)
	if idx := strings.IndexByte(name, ','); idx >= 0 {
		return name[:idx]
	}
	return name
}

func atoiSafe(s string) int {
	v, err := strconv.Atoi(strings.TrimSpace(s))
	if err != nil {
		return 0
	}
	return v
}

func atofSafe(s string) float64 {
	v, err := strconv.ParseFloat(strings.TrimSpace(s), 64)
	if err != nil {
		return 0
	}
	return v
}

func probeFileSize(stat os.FileInfo, err error) *AudioInfo {
	if err != nil {
		return nil
	}
	return &AudioInfo{SizeBytes: stat.Size()}
}
