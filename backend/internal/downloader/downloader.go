package downloader

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"time"

	"ytdm/backend/internal/apperr"
	"ytdm/backend/internal/ffmpeg"
	"ytdm/backend/internal/logging"
	"ytdm/backend/internal/provider"
	"ytdm/backend/internal/ytdlp"
)

// Progress is a download progress report handed to the caller.
type Progress struct {
	DownloadedBytes int64   `json:"downloaded_bytes"`
	TotalBytes      int64   `json:"total_bytes"`
	Percent         float64 `json:"percent"`
	SpeedBytesPerS  float64 `json:"speed_bytes_per_s"`
	ETASeconds      int     `json:"eta_seconds"`
}

// ProgressCallback receives progress updates. It may be nil.
type ProgressCallback func(Progress)

// Result describes the file a download produced.
type Result struct {
	// Path is the finished audio file.
	Path string `json:"path"`
	// Info is the verified stream description.
	Info AudioInfo `json:"info"`
	// Plan records how the raw stream was treated.
	Plan Plan `json:"plan"`
	// SourceCodec is the codec the platform delivered.
	SourceCodec string `json:"source_codec"`
	// NativeOpus reports whether the stored audio is the platform's own Opus
	// stream, untouched by any re-encoding.
	NativeOpus bool `json:"native_opus"`
}

// Downloader fetches a resolved media source and stores it as a verified
// audio file.
type Downloader interface {
	Download(ctx context.Context, source provider.MediaSource, destination string, progress ProgressCallback) (*Result, error)
}

// Options configures the yt-dlp based downloader.
type Options struct {
	YTDLP  *ytdlp.Client
	FFmpeg *ffmpeg.Runner
	Prober *Prober

	// AllowTranscode permits a lossy re-encode when the platform offers no
	// native Opus stream. It is off by default on purpose.
	AllowTranscode bool

	// DurationToleranceMS bounds how far the downloaded audio may deviate from
	// the runtime the catalogue promised.
	DurationToleranceMS int

	// Retries bounds yt-dlp's internal retry attempts.
	Retries int

	Logger *slog.Logger
}

// YTDLPDownloader implements Downloader on top of yt-dlp and ffmpeg.
type YTDLPDownloader struct {
	ytdlp          *ytdlp.Client
	ffmpeg         *ffmpeg.Runner
	prober         *Prober
	allowTranscode bool
	toleranceMS    int
	retries        int
	rateLimit      atomic.Pointer[string]
	logger         *slog.Logger
}

// New builds a downloader.
func New(opts Options) (*YTDLPDownloader, error) {
	if opts.YTDLP == nil {
		return nil, apperr.New(apperr.CodeInternal, "The downloader needs a yt-dlp client.")
	}
	if opts.FFmpeg == nil {
		return nil, apperr.New(apperr.CodeInternal, "The downloader needs an ffmpeg runner.")
	}
	if opts.Prober == nil {
		return nil, apperr.New(apperr.CodeInternal, "The downloader needs an ffprobe prober.")
	}
	tolerance := opts.DurationToleranceMS
	if tolerance <= 0 {
		tolerance = 15000
	}
	logger := opts.Logger
	if logger == nil {
		logger = slog.Default()
	}
	return &YTDLPDownloader{
		ytdlp:          opts.YTDLP,
		ffmpeg:         opts.FFmpeg,
		prober:         opts.Prober,
		allowTranscode: opts.AllowTranscode,
		toleranceMS:    tolerance,
		retries:        opts.Retries,
		logger:         logger,
	}, nil
}

// SetRateLimit updates the download bandwidth limit (e.g. "2M", "5M", "10M", or "" for unlimited).
func (d *YTDLPDownloader) SetRateLimit(limit string) {
	d.rateLimit.Store(&limit)
}

// RateLimit returns the current download bandwidth limit.
func (d *YTDLPDownloader) RateLimit() string {
	ptr := d.rateLimit.Load()
	if ptr == nil {
		return ""
	}
	return *ptr
}

// Download fetches source and writes the finished audio next to destination.
// The extension of destination is replaced by the one that matches the actual
// codec, so the caller learns from Result.Path what was really produced.
func (d *YTDLPDownloader) Download(ctx context.Context, source provider.MediaSource, destination string, progress ProgressCallback) (*Result, error) {
	if strings.TrimSpace(source.URL) == "" {
		return nil, apperr.New(apperr.CodeInvalidRequest, "The media source has no URL.")
	}
	if strings.TrimSpace(destination) == "" {
		return nil, apperr.New(apperr.CodeInternal, "The download has no destination.")
	}

	workDir := filepath.Dir(destination)
	if err := os.MkdirAll(workDir, 0o755); err != nil {
		return nil, apperr.Wrap(apperr.CodeInternal, "The working directory could not be created.", err)
	}

	logger := d.logger.With(
		logging.KeyProvider, source.Provider,
		logging.KeyOperation, "download",
		"media_id", source.ID,
	)

	started := time.Now()
	rawPath, err := d.ytdlp.Download(ctx, ytdlp.DownloadRequest{
		URL:            source.URL,
		Dir:            workDir,
		FormatSelector: FormatSelector(source.Formats),
		Retries:        d.retries,
		RateLimit:      d.RateLimit(),
	}, wrapProgress(progress))

	if err != nil {
		return nil, err
	}

	rawInfo, err := d.prober.Probe(ctx, rawPath)
	if err != nil {
		return nil, err
	}

	plan, ext := PlanFor(*rawInfo, d.allowTranscode)
	target := replaceExtension(destination, ext)

	switch plan {
	case PlanKeep:
		if rawPath != target {
			if err := os.Rename(rawPath, target); err != nil {
				return nil, apperr.Wrap(apperr.CodeInternal, "The downloaded file could not be moved.", err)
			}
		}
	case PlanRemux:
		if err := d.remux(ctx, rawPath, target); err != nil {
			return nil, err
		}
		if rawPath != target {
			_ = os.Remove(rawPath)
		}
	case PlanTranscode:
		if err := d.transcode(ctx, rawPath, target, rawInfo.BitrateKbps); err != nil {
			return nil, err
		}
		if rawPath != target {
			_ = os.Remove(rawPath)
		}
	default:
		return nil, apperr.Newf(apperr.CodeInternal, "Unknown download plan %q.", plan)
	}

	finalInfo, err := d.prober.Probe(ctx, target)
	if err != nil {
		_ = os.Remove(target)
		return nil, err
	}
	if err := Verify(finalInfo, source.DurationMS, d.toleranceMS); err != nil {
		_ = os.Remove(target)
		return nil, apperr.Wrap(apperr.CodeMediaVerifyFailed, "Downloaded audio failed duration/stream verification.", err)
	}

	result := &Result{
		Path:        target,
		Info:        *finalInfo,
		Plan:        plan,
		SourceCodec: rawInfo.Codec,
		NativeOpus:  finalInfo.IsOpus() && plan != PlanTranscode,
	}

	logger.Info("download finished",
		"plan", string(plan),
		"source_codec", rawInfo.Codec,
		"codec", finalInfo.Codec,
		"bitrate_kbps", finalInfo.BitrateKbps,
		"sample_rate", finalInfo.SampleRate,
		"channels", finalInfo.Channels,
		"duration_ms", finalInfo.DurationMS,
		"native_opus", result.NativeOpus,
		"elapsed_ms", time.Since(started).Milliseconds(),
	)
	return result, nil
}

// remux copies the audio stream into a new container without touching the
// samples. This is what turns a WebM delivered Opus stream into an .opus file.
func (d *YTDLPDownloader) remux(ctx context.Context, source, target string) error {
	return d.ffmpeg.Run(ctx, apperr.CodeDownloadFailed,
		"-i", source,
		"-vn",
		"-map", "0:a:0",
		"-c:a", "copy",
		"-map_metadata", "-1",
		target,
	)
}

// transcode re-encodes to Opus. It is only reached when the operator allowed
// it explicitly.
func (d *YTDLPDownloader) transcode(ctx context.Context, source, target string, sourceKbps float64) error {
	bitrate := TranscodeBitrate(sourceKbps)
	return d.ffmpeg.Run(ctx, apperr.CodeDownloadFailed,
		"-i", source,
		"-vn",
		"-map", "0:a:0",
		"-c:a", "libopus",
		"-b:a", fmt.Sprintf("%dk", bitrate),
		"-vbr", "on",
		"-application", "audio",
		"-map_metadata", "-1",
		target,
	)
}

func wrapProgress(callback ProgressCallback) ytdlp.ProgressFunc {
	if callback == nil {
		return nil
	}
	return func(p ytdlp.Progress) {
		callback(Progress{
			DownloadedBytes: p.DownloadedBytes,
			TotalBytes:      p.TotalBytes,
			Percent:         p.Percent,
			SpeedBytesPerS:  p.SpeedBytesPerS,
			ETASeconds:      p.ETASeconds,
		})
	}
}

// replaceExtension swaps the extension of path for ext.
func replaceExtension(path, ext string) string {
	current := filepath.Ext(path)
	if current == "" {
		return path + ext
	}
	return strings.TrimSuffix(path, current) + ext
}
