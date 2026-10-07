# Download source restrictions and filename collisions

A healthy YouTube session may still lack access to one age-restricted or
Premium-only recording. A flat search result can contain its title while omitting
artist and duration. Such a result does not provide enough evidence to relax the
matching threshold.

When a directly identified source explicitly reports an age or Premium gate,
resolution can try another already configured, immediately eligible session.
Each session is checked once, up to the lower of the candidate limit and five.
Cookie files are not replaced, session health is not penalised, and the winning
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
