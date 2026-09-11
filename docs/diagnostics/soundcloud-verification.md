# SoundCloud preview verification

SoundCloud sources with only preview formats must be rejected as candidate-scoped
`TRACK_NOT_FOUND`. The installed yt-dlp SoundCloud extractor identifies previews
using transcoding `snipped` or explicit preview stream-path markers and appends
`_preview` to `formats[].format_id`. The resolver uses this source marker, never
a blanket duration limit. Mixed lists keep full audio formats; normal short full
tracks remain valid. Candidate fallback limits, matching thresholds, session
recovery and file cleanup are unchanged.

Two previously examined public source IDs illustrate contradictory duration
metadata. Their only advertised formats were `hls_mp3_1_0_preview` and
`http_mp3_1_0_preview`:

| Media ID | Catalogue duration | yt-dlp duration | ffprobe duration | Codec/container | Size |
| --- | ---: | ---: | ---: | --- | ---: |
| 254407911 | 195 s | 30 s | 195.653 s | mp3/mp3 | 476472 bytes |
| 254407916 | 212 s | 30 s | 212.040 s | mp3/mp3 | 476472 bytes |

yt-dlp takes the source-response duration; ffprobe inspects the file. The worker
uses the nonzero resolved duration for verification. The observed differences
therefore fail the unchanged 15-second tolerance. The exact MP3/header or demuxer
mechanism behind the long ffprobe values remains **unresolved**: original file
bytes, frame counts, decoded playback lengths and the separate stream/format
duration values were not retained. Nor was the particular underlying preview
condition recorded. The extractor-generated format IDs establish preview
classification, independently of that duration uncertainty. No new live
reproduction is needed to port the reviewed fix.

`media verification failed` contains provider/media ID, raw/final stage, expected
and measured duration, tolerance, codec, container and file size. Reasons are
`missing_audio_stream`, `invalid_duration`, `empty_file`, `duration_mismatch`,
`ffprobe_error` and `unexpected_video_stream` (see
[audio format classification](./audio-format-classification.md)). Unknown probe
measurements remain null. Identifiers are bounded
and sanitized; URLs, paths, sessions/cookies, source titles and raw tool output
are excluded. Rejected final files and staging directories retain their existing
cleanup behavior. Preview-only sources are rejected before download and do not
produce a file-verification event or systemic cooldown.

Offline regression tests cover both mismatches, preview rejection regardless of
claimed duration, a valid 30-second full track, mixed formats, missing formats,
secret-free diagnostics and cleanup. The real resolver-to-orchestrator fixture
checks zero cooldown triggers, unchanged candidate limits/score filtering/provider
fallback, and neutral YouTube session health.

After a separately authorized future rollout, inspect at most the first five
naturally occurring verification failures or 24 hours, whichever comes first.
Group the safe fields by media ID/reason. Investigate unexpected stream/duration,
empty-file or probe errors without relaxing verification. Do not generate
production retries, change cookies, reset cooldowns or retain raw tool output to
create evidence. No events means insufficient observation, not proven recovery.
Stable promotion, release qualification and production rollout remain separate
from development integration.
