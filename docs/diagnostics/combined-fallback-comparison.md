# Combined-stream fallback — controlled comparison plan

Prepared, **not executed**. This note fixes the measurements and the abort
criteria *before* the combined-stream audio fallback is switched on anywhere,
so the result cannot be argued after the fact. Running it needs its own
authorization; nothing here is a deployment instruction.

The goal is **more new, complete, verified downloads at a defensible request
and data cost**. Fewer error messages is not the goal and is not a result.

## Preconditions

1. The change is merged into `dev` and built; production still runs whatever
   release it runs. The comparison uses a development-channel build.
2. `YTDM_COMBINED_AUDIO_FALLBACK` is `false` everywhere until the window
   starts. Turning it on requires a restart.
3. Unchanged for the whole comparison, in both phases: worker count, matching
   threshold, verification tolerance, cookies, media sessions, the 32 protected
   jobs, and the storage guard. None of them is a variable of this experiment.
4. A baseline snapshot is taken the way the RC-1 and RC-2 windows took theirs:
   settings, protected jobs, storage guard, queue and session state.
5. **Runtime diagnosis first, in the same supervised window, before phase A.**
   The deployed image contains a matching JavaScript runtime for yt-dlp
   (`deno` via `yt-dlp-ejs-rt-deno`, `yt-dlp-ejs` matching yt-dlp's expected
   script version), but whether YouTube's challenges are actually solved in the
   running container is neither confirmed nor ruled out. A debug header that
   lists the runtime does not prove it. Confirm it with one verbose extraction
   of one known item, with no other diagnosis or download run using the same
   session at that time: the log has to show the challenge solver running with
   the runtime and succeeding, and the item's format list has to be recorded
   (audio-only formats present or not). If challenge solving fails, stop: the
   fallback would only work around that failure, and the comparison would
   measure the wrong thing.
6. With the switch **off**, this build ends an item without an audio-only
   stream exactly as v0.27.2-rc.2 did (`TRACK_NOT_FOUND`, no retry). Phase A
   therefore measures the release-candidate behaviour, not a changed one.

## Design

Two phases in the same installation, back to back, same queue, same time of day
where possible:

| Phase | Duration | `YTDM_COMBINED_AUDIO_FALLBACK` |
|---|---|---|
| A — control | 6 h | `false` |
| B — fallback | 6 h | `true` |

Back-to-back phases in one installation cannot separate the switch from
provider conditions and from the remaining queue, which shrinks as the run
proceeds. That is a known limitation of this design and has to be stated with
the result, exactly as the RC-1/RC-2 comparison stated it. Phase A is therefore
a reference point, not a control group.

## Measurements

Per phase and per hour, from the database and the hourly `throughput summary`:

| Measure | Source | Why |
|---|---|---|
| new verified acquisitions | `files` × `job_items`, first file per track | the only success measure |
| of those, `format_kind` = `combined` | `download.<family>.combined.stored` | how much the fallback contributed |
| provider requests per success | `ytdlp.*.process` | the request cost |
| **transferred bytes per success** | `download.<family>.*.transferred_bytes` | the data cost; process counts do not show it |
| download and extraction duration | `download_ms`, `extract_ms` | how long a session slot is held |
| items ending `UNSUPPORTED_MEDIA_FORMAT` | `job_items.error_code` | phase B only: sources whose format answer offered not even a usable combined stream (permanent) |
| items ending `TRANSFER_BUDGET_EXCEEDED` | `job_items.error_code`, `download.<family>.combined.rejected_over_budget` | phase B only: combined transfers stopped by the local byte or time budget (permanent) |
| items ending permanently `TRACK_NOT_FOUND` | `job_items.error_code` | the permanent-loss rate |
| items waiting or ending with `TRACK_TIMEOUT` | `job_items.error_code` | tracks that outran `YTDM_TRACK_TIMEOUT`, typically while queued behind combined transfers for the only session slot; retried within the attempt limit |
| family cooldowns by cause | backend log | must not rise because of this change; a budget stop never causes one, so any rise is a real provider signal |
| rate limits, bot and auth events | summary counters | the protection signal |
| verification failures by reason | `verification_reason` | whether extracted audio is sound |
| staging peak usage | staging quota check | the disk cost |

Both phases are read the same way, from the same queries, with the phase
boundary taken from the container start — not from the wall clock — and the
summary offset against the observation window disclosed rather than netted out,
as in the RC-2 evaluation.

## Success criterion

Phase B is an improvement only if **all** of these hold:

1. new verified acquisitions per hour are higher than in phase A by more than
   the hour-to-hour spread of phase A itself;
2. transferred bytes per success stay within a factor the operator accepts
   beforehand — the offline measurement suggests 4–10× for combined streams,
   so a run in which most successes are combined will cost several times the
   data per track;
3. family cooldowns, rate limits and bot or auth events per hour do not rise;
4. no verification failure is attributable to the extraction itself
   (`unexpected_video_stream` on a stored file, container or codec mismatch).

A rise in completed items that comes with a fall in *verified* acquisitions is
a failure, not a success.

## Abort criteria

Stop phase B immediately, set the switch back to `false` and restart, if any of
these occurs:

- rate limits, bot challenges or auth failures per hour exceed the phase A rate;
- family cooldowns per hour exceed the phase A rate;
- a stored file is found to contain a real video stream;
- staging usage reaches its quota, or free space falls below the configured
  minimum;
- transferred bytes per success exceed the limit agreed beforehand;
- health checks fail repeatedly, or the container restarts;
- anything touches the 32 protected jobs, the settings or the storage guard.

Aborting is not a failure of the measurement: it is the measurement. Record the
hour, the trigger and the counters at that moment.

## Known limits of this build

This build contains the staging cleanup and the track-timeout handling (PR #9).
They have to be read into the result rather than mistaken for the fallback's
effect:

- A track that waits for the only session's execution slot behind combined
  transfers still counts that wait against its track time limit. When the
  limit passes it waits for a bounded retry as `TRACK_TIMEOUT`; a rising
  `TRACK_TIMEOUT` count in phase B is a cost of the fallback.
- The first start of this build removes the staging directories of tracks
  that already ended, so staging usage drops once at the beginning of the
  window. Take the baseline snapshot after that start.
- An interrupted transfer is not resumed: partial files do not survive their
  attempt or a restart, so a restart during phase B repeats the transfers that
  were running.

## What the result cannot say

- It cannot separate the switch from provider conditions or from the shrinking
  queue; only a repeated run under different conditions can.
- It cannot establish a fixed hourly YouTube quota, and no such quota should be
  inferred from it.
- It says nothing about bandwidth cost at other resolutions than the ones the
  platform happened to offer during the window.
