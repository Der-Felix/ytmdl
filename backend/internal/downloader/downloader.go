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
	"ytdm/backend/internal/throughput"
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
	// FormatKind says whether the audio came from an audio only stream or was
	// copied out of a combined audio/video stream.
	FormatKind string `json:"format_kind"`
	// TransferredBytes is what the transfer really moved, as far as it could
	// be measured. It is the figure the combined fallback has to be judged by.
	TransferredBytes int64 `json:"transferred_bytes"`
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

	// CombinedAudioFallback permits copying the audio out of a combined
	// audio/video stream when the resolver found no audio only stream. It is
	// off by default: the video bytes are transferred and discarded, and the
	// transfer holds the media session for its whole duration.
	CombinedAudioFallback bool
	// CombinedMaxBytes bounds what one combined transfer may move. Zero
	// selects DefaultCombinedMaxBytes.
	CombinedMaxBytes int64
	// CombinedTimeout bounds how long one combined transfer may run. Zero
	// selects DefaultCombinedTimeout.
	CombinedTimeout time.Duration

	// DurationToleranceMS bounds how far the downloaded audio may deviate from
	// the runtime the catalogue promised.
	DurationToleranceMS int

	// Retries bounds yt-dlp's internal retry attempts.
	Retries int

	// CookieResolver resolves an opaque session ID to a cookie file path.
	CookieResolver func(sessionID string) string

	// ExecutionGateResolver binds the same per-session yt-dlp gate used during
	// search and resolution to the later download process.
	ExecutionGateResolver func(sessionID string) ytdlp.ExecutionGate

	// Recorder counts acquisitions by the kind of stream they came from and
	// the bytes they moved, so the hourly summary can weigh the combined
	// fallback against the audio only path. It may be nil.
	Recorder *throughput.Recorder

	Logger *slog.Logger
}

// YTDLPDownloader implements Downloader on top of yt-dlp and ffmpeg.
type YTDLPDownloader struct {
	ytdlp                 *ytdlp.Client
	ffmpeg                *ffmpeg.Runner
	prober                *Prober
	allowTranscode        bool
	combinedFallback      bool
	combinedMaxBytes      int64
	combinedTimeout       time.Duration
	toleranceMS           int
	retries               int
	cookieResolver        func(sessionID string) string
	executionGateResolver func(sessionID string) ytdlp.ExecutionGate
	recorder              *throughput.Recorder
	rateLimit             atomic.Pointer[string]
	logger                *slog.Logger
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
	combinedMaxBytes := opts.CombinedMaxBytes
	if combinedMaxBytes <= 0 {
		combinedMaxBytes = DefaultCombinedMaxBytes
	}
	combinedTimeout := opts.CombinedTimeout
	if combinedTimeout <= 0 {
		combinedTimeout = DefaultCombinedTimeout
	}
	logger := opts.Logger
	if logger == nil {
		logger = slog.Default()
	}
	return &YTDLPDownloader{
		ytdlp:                 opts.YTDLP,
		ffmpeg:                opts.FFmpeg,
		prober:                opts.Prober,
		allowTranscode:        opts.AllowTranscode,
		combinedFallback:      opts.CombinedAudioFallback,
		combinedMaxBytes:      combinedMaxBytes,
		combinedTimeout:       combinedTimeout,
		toleranceMS:           tolerance,
		retries:               opts.Retries,
		cookieResolver:        opts.CookieResolver,
		executionGateResolver: opts.ExecutionGateResolver,
		recorder:              opts.Recorder,
		logger:                logger,
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
		logging.KeyProvider, diagnosticToken(source.Provider),
		logging.KeyOperation, "download",
		"media_id", diagnosticToken(source.ID),
	)

	started := time.Now()
	cookiePath := ""
	sessID := strings.TrimSpace(source.SessionID)
	if d.cookieResolver != nil && sessID != "" {
		cookiePath = d.cookieResolver(sessID)
	}

	client := d.ytdlp
	family := provider.FamilyOf(source.Provider)
	if family != "" {
		// Download counters are reported under the family that serves the source.
		client = client.WithLabel(string(family))
	}
	if cookiePath != "" {
		client = client.WithCookieFile(cookiePath)
	}
	if d.executionGateResolver != nil && sessID != "" {
		client = client.WithExecutionGate(d.executionGateResolver(sessID))
	}

	request := ytdlp.DownloadRequest{
		URL:            source.URL,
		Dir:            workDir,
		FormatSelector: FormatSelector(source.Formats),
		Retries:        d.retries,
		RateLimit:      d.RateLimit(),
	}

	// A combined stream is only fetched when the operator switched the
	// fallback on and the resolver put such a format forward. Everything below
	// that is not guarded by `combined` keeps the audio only path unchanged.
	chosen, combined := combinedFormatFrom(source)
	combined = combined && d.combinedFallback
	formatKind := "audio_only"
	if combined {
		formatKind = "combined"
	}
	logger = logger.With("format_kind", formatKind, "format_id", diagnosticToken(chosen.ID))
	d.count(family, formatKind, "attempted", 1)

	dlCtx := ctx
	var (
		budget   *transferBudget
		attempt  *attemptFiles
		deadline time.Time
	)
	if combined {
		// An announced size that already exceeds the budget is refused before
		// a single byte moves.
		if d.combinedMaxBytes > 0 && chosen.Filesize > d.combinedMaxBytes {
			d.count(family, formatKind, "rejected_over_budget", 1)
			return nil, apperr.Newf(apperr.CodeUnsupportedMediaFormat,
				"The combined stream announces %d bytes, more than the transfer budget of %d bytes.",
				chosen.Filesize, d.combinedMaxBytes)
		}
		attempt = newAttemptFiles(workDir)
		var cancel context.CancelFunc
		dlCtx, cancel, deadline = d.combinedContext(ctx)
		defer cancel()
		budget = newTransferBudget(d.combinedMaxBytes, deadline, cancel)
		request = combinedRequest(request, d.combinedMaxBytes)
	}

	downloadStarted := time.Now()
	rawPath, err := client.Download(dlCtx, request, wrapProgress(progress, budget))
	downloadMS := time.Since(downloadStarted).Milliseconds()

	if err != nil {
		if combined {
			err = budget.classify(ctx, err)
			// Nothing this attempt wrote may survive it; files that were
			// already in the directory are left untouched.
			attempt.removeNew()
			logger.Warn("combined stream transfer failed",
				logging.KeyErrorCode, string(apperr.CodeOf(err)),
				"transferred_bytes", budget.transferred(),
				"download_ms", downloadMS,
				"transfer_budget_bytes", d.combinedMaxBytes,
				"transfer_budget_ms", d.combinedTimeout.Milliseconds(),
			)
		}
		return nil, err
	}

	rawInfo, err := d.prober.Probe(ctx, rawPath)
	if err != nil {
		logVerificationFailure(logger, "raw", nil, source.DurationMS, d.toleranceMS, err)
		if combined {
			attempt.removeNew()
		}
		return nil, err
	}

	// The size on disk is the binding byte limit: a segmented stream announces
	// no total size, and its progress reports are not a guarantee either.
	if combined && d.combinedMaxBytes > 0 && rawInfo.SizeBytes > d.combinedMaxBytes {
		attempt.removeNew()
		d.count(family, formatKind, "rejected_over_budget", 1)
		return nil, apperr.Newf(apperr.CodeUnsupportedMediaFormat,
			"The combined stream transferred %d bytes, more than the transfer budget of %d bytes.",
			rawInfo.SizeBytes, d.combinedMaxBytes)
	}

	// A combined stream is expected to carry video; anything else is not.
	extract := combined && rawInfo.VideoStreams > 0
	if rawInfo.VideoStreams > 0 && !extract {
		// The source was accepted as audio only, but the bytes carry video -
		// possible when the provider left the video codec unknown. Keeping
		// the file would store a video as an audio track. Unknown metadata is
		// settled here by the file itself, never taken on trust.
		err := invalidAudio("unexpected_video_stream", "The downloaded stream contains video instead of audio only.", rawInfo)
		logVerificationFailure(logger, "raw", rawInfo, source.DurationMS, d.toleranceMS, err)
		_ = os.Remove(rawPath)
		return nil, err
	}

	var (
		plan      Plan
		ext       string
		target    string
		extractMS int64
	)
	if extract {
		target, ext, err = combinedTarget(destination, *rawInfo)
		if err != nil {
			attempt.removeNew()
			return nil, err
		}
		if target == rawPath {
			// The container the audio goes into must be a file of its own, so
			// that the combined stream is never the file that survives.
			target = replaceExtension(destination, ".audio"+ext)
		}
		extractStarted := time.Now()
		if err := d.extractAudio(ctx, rawPath, target); err != nil {
			attempt.removeNew()
			return nil, err
		}
		extractMS = time.Since(extractStarted).Milliseconds()
		// The combined stream has served its purpose and must never be
		// published as a music file.
		_ = os.Remove(rawPath)
		plan = PlanExtractAudio
	} else {
		plan, ext = PlanFor(*rawInfo, d.allowTranscode)
		target = replaceExtension(destination, ext)

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
	}

	finalInfo, err := d.prober.Probe(ctx, target)
	if err != nil {
		logVerificationFailure(logger, "final", nil, source.DurationMS, d.toleranceMS, err)
		_ = os.Remove(target)
		if combined {
			attempt.removeNew()
		}
		return nil, err
	}
	// Whatever the path, a real video stream must never reach the library. An
	// embedded cover is an attached picture and is not counted here.
	if finalInfo.VideoStreams > 0 {
		err := invalidAudio("unexpected_video_stream", "The stored file still contains a video stream.", finalInfo)
		logVerificationFailure(logger, "final", finalInfo, source.DurationMS, d.toleranceMS, err)
		_ = os.Remove(target)
		if combined {
			attempt.removeNew()
		}
		return nil, err
	}
	if err := Verify(finalInfo, source.DurationMS, d.toleranceMS); err != nil {
		logVerificationFailure(logger, "final", finalInfo, source.DurationMS, d.toleranceMS, err)
		_ = os.Remove(target)
		if combined {
			attempt.removeNew()
		}
		return nil, apperr.Wrap(apperr.CodeMediaVerifyFailed, "Downloaded audio failed duration/stream verification.", err)
	}

	transferred := rawInfo.SizeBytes
	if observed := budget.transferred(); observed > transferred {
		transferred = observed
	}

	result := &Result{
		Path:             target,
		Info:             *finalInfo,
		Plan:             plan,
		SourceCodec:      rawInfo.Codec,
		NativeOpus:       finalInfo.IsOpus() && plan != PlanTranscode,
		FormatKind:       formatKind,
		TransferredBytes: transferred,
	}

	if combined {
		// Everything this attempt created besides the finished audio goes,
		// including the combined stream if anything still refers to it.
		attempt.removeNew(target)
	}
	d.count(family, formatKind, "stored", 1)
	if transferred > 0 {
		d.count(family, formatKind, "transferred_bytes", uint64(transferred))
	}

	logger.Info("download finished",
		"plan", string(plan),
		"source_codec", rawInfo.Codec,
		"source_video_codec", diagnosticToken(rawVideoCodec(rawInfo, chosen)),
		"codec", finalInfo.Codec,
		"bitrate_kbps", finalInfo.BitrateKbps,
		"sample_rate", finalInfo.SampleRate,
		"channels", finalInfo.Channels,
		"duration_ms", finalInfo.DurationMS,
		"native_opus", result.NativeOpus,
		"transferred_bytes", transferred,
		"stored_bytes", finalInfo.SizeBytes,
		"download_ms", downloadMS,
		"extract_ms", extractMS,
		"verification", "ok",
		"session_lease_ms", sessionLeaseMS(sessID, started),
		"elapsed_ms", time.Since(started).Milliseconds(),
	)
	return result, nil
}

// rawVideoCodec names the video stream a combined transfer carried. ffprobe
// counts the streams of the arrived file but does not name the video codec, so
// the format record the resolver judged supplies the name.
func rawVideoCodec(info *AudioInfo, chosen provider.AudioFormat) string {
	if info != nil && info.VideoStreams == 0 {
		return ""
	}
	return chosen.VideoCodec
}

// sessionLeaseMS reports how long this download occupied a media session. A
// download without a session occupies none, which is a different statement
// from occupying one for no time.
func sessionLeaseMS(sessionID string, started time.Time) any {
	if sessionID == "" {
		return nil
	}
	return time.Since(started).Milliseconds()
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

// wrapProgress forwards progress to the caller and, when a budget is in force,
// lets it watch the bytes as they arrive. The budget needs the reports even
// when the caller wants none, so a nil callback is not the end of it.
func wrapProgress(callback ProgressCallback, budget *transferBudget) ytdlp.ProgressFunc {
	if callback == nil && budget == nil {
		return nil
	}
	return func(p ytdlp.Progress) {
		budget.observe(p.DownloadedBytes)
		if callback == nil {
			return
		}
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
