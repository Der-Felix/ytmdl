# Library tools

The browser library supports manual and intelligent playlists, selection actions,
custom artwork, possible duplicate recordings, listening history and per-track
loudness adjustment. These tools use the local catalog and authenticated API.

## Intelligent playlists

Enable **Intelligente Playlist** when creating or editing a playlist. Rules can
combine one artist, an exact artist genre, your favorites, and an added-within
period. All chosen conditions must match. Choose title, newest, your most played
or your last played order, and a limit of 1–500 tracks. Membership is evaluated
when opening or playing the list; the currently playing queue remains a snapshot.
Smart lists do not accept manual additions, removals or ordering. Converting a
manual list to a smart list keeps its old membership, so converting back restores
that list. Favorites and listening statistics are scoped to the list owner.

## Selection and metadata

Checkboxes select the displayed tracks (up to 100 per library page). Selected
tracks can be queued together or added atomically to a manual playlist. Existing
members are skipped while new members retain selection order. Administrators can
edit album, album artist, artist names and year together. Only checked fields
change; an empty checked field clears that value. Changes are catalog overrides
and survive provider metadata refresh. Original tags, recording identities and
media paths are unchanged. Genre remains assigned per artist.

## Artwork

Administrators can upload or remove a custom artist portrait or release cover
from the detail page or **Bibliothek → Werkzeuge → Cover verwalten**. JPEG/PNG
uploads up to 8 MiB and 4096 pixels per edge are decoded, resized to at most 1600
pixels and re-encoded as JPEG. Embedded metadata and active formats are not kept.
Images are stored in the database and included in database backups. Release
artwork is shared by its tracks. Removing an upload restores the usual provider
portrait/local cover fallback. The manager can show missing local artwork on the
current page; it does not initiate provider refreshes.

## Duplicate candidates and history

**Mögliche Duplikate** groups downloaded tracks by normalized title and artist
credit. This is a review aid, not an audio fingerprint: different album, live or
remix versions can be intentional. Each page has up to 20 groups, and the first
20 tracks of each group are shown for comparison. No automatic deletion occurs.

Listening history counts actual playing wall time, excluding pause, buffering,
seeking and long background callback gaps. A play counts after 30 seconds, or
half the duration of short tracks adjusted for playback speed. Repeated network
submissions use the same event identifier. The server stores per-user counts and
last played times; up to 100 recent/frequent tracks are displayed. Existing
browser-only history is retained, but is not imported as verified plays. Users
can clear their own server history explicitly.

## Loudness adjustment

Enable **Lautstärke angleichen** under Player → Audio. The current and next track
are measured sequentially with FFmpeg's [loudnorm filter](https://ffmpeg.org/ffmpeg-filters.html#loudnorm).
The browser applies a static gain toward −16 LUFS, limited by a −1.5 dB true-peak
ceiling and a −24…+6 dB gain range. Separate gain nodes for both decks preserve
crossfades. EQ and preamp may still change resulting loudness; the safety limiter
remains independent. Unmeasured tracks play at unity until their measurement is
available; silence keeps unity gain. Disabling adjustment restores unity.

Analysis is read-only, bounded to regular catalog files of at most 512 MiB and a
known duration of at most one hour, with a two-minute process timeout. Only one
analysis runs at a time; errors are shown in Audio settings. Files are opened
through a confined root and passed as an inherited descriptor, never a caller
URL. Only supported audio/container demuxers and local file/pipe protocols are
allowed. Measurements are cached by file identity, byte size and modification
time. First-time analysis has a CPU cost; it is disabled by default and no whole
library scan is automatically scheduled. The opt-in persists in this browser.

## Database upgrade

Migration 0014 adds rules, metadata overrides, artwork, listening data and loudness
measurements. Existing identities, audio and playlists are preserved. Upgrades
from schema 8–13 to 14 require a verified backup for schema rollback. Schema 14
is neutral only when already on 14. The old application can still run with the
additive tables present, but will not expose these new features.
