# Download source restrictions and filename collisions

A healthy YouTube session may still lack access to one age-restricted or
Premium-only recording. A flat search result can contain its title while omitting
artist and duration. Such a result does not provide enough evidence to relax the
matching threshold.

When a directly identified source explicitly reports an age or Premium gate,
resolution can try another already configured, eligible session.
Each session is checked once, up to the lower of the candidate limit and five.
The restricted lease is released before waiting for alternate capacity, so
concurrent downloads cannot deadlock by holding all session slots. Cookie files
are not replaced, session health is not penalised, and the winning
session remains attached to the download. Bot challenges, authentication failures
and rate limits stop the attempt immediately; they never trigger this rotation.
If no session can access the recording, normal candidate search still runs. When
no alternative was resolved, the known restriction remains the item's error.
Unavailable sources and genuine mismatches can still fail permanently.

Different verified recordings can map to the same conventional library filename
because providers disagree about credits or runtime. The existing file is not
removed or adopted. On a filename conflict, a source with a known provider and ID
receives a stable ` [source-<digest>]` suffix. The alternative is committed with
the same size/checksum verification and atomic no-overwrite operation. Repeating
an identical interrupted commit recovers the same file; different bytes at that
alternative path still return `PATH_CONFLICT`. Ordinary filenames and deliberate
replacement of the recording's own registered file retain their existing rules.
Lyrics follow the final audio path. No database migration is required.

After deploying a reviewed fix, retry only the affected failed items. Preserve
completed/skipped items and operator-paused jobs. Back up the affected job state
first, and review final item outcomes rather than interpreting a queued job as a
successful download. Never lower the matching threshold or delete existing music
just to clear the failure list.

## Recording runtime validation

A direct-source search can fall back to ordinary text results if the source
runtime is implausible. The orchestrator trusts only the exact requested source
ID on its fast path; every replacement must pass normal matching. Known
runtimes differing by more than 15 seconds are rejected even if title, credits,
album or ISRC otherwise match. Unknown candidate runtime still requires the
normal matching threshold and downloaded-audio verification. This is a runtime
comparison, not a minimum-length rule; valid short full recordings remain usable.

The worker verifies audio against the requested recording's runtime rather than
the selected source's own runtime. Skip-existing requires a file with a compatible
measured runtime when the request carries one. An incompatible or unmeasured old
file is preserved; a verified correction is committed under the stable source
suffix instead of replacing that file. Old incorrect catalog associations are
not silently deleted or automatically re-downloaded by the code change. They
need a scoped audit/retry with a before-state backup.

When the original source fails and no replacement is attempted, its safe error
message is retained. Failures of actual source resolutions take precedence over
weak, unrelated search hits, which must not turn unavailable or restricted
sources into a misleading MATCH_FAILED or zero-source summary. Provider
protection and cooldown behavior is unchanged.
