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
| items ending `UNSUPPORTED_MEDIA_FORMAT` | `job_items.error_code` | whether the item is kept instead of burned |
| items ending permanently `TRACK_NOT_FOUND` | `job_items.error_code` | the permanent-loss rate |
| family cooldowns by cause | backend log | must not rise because of this change |
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

## What the result cannot say

- It cannot separate the switch from provider conditions or from the shrinking
  queue; only a repeated run under different conditions can.
- It cannot establish a fixed hourly YouTube quota, and no such quota should be
  inferred from it.
- It says nothing about bandwidth cost at other resolutions than the ones the
  platform happened to offer during the window.
