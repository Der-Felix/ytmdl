# Apple app handoff — preview 0.3.0, build 31

The Apple client lives on `feat/apple-native-player` in draft
[PR #42](https://github.com/Der-Felix/ytmdl/pull/42), based on `dev`.
Do not promote it to a stable server release just because it builds locally.
The server release and native preview have independent version numbers.

## Build 31: offline library browsing

`OfflineLibraryView` in `App/MobileListeningViews.swift` opens on visible local
collections: playlists, favorites, albums and artists, with cached cover collages,
ready/total counts and collection drilldowns. “Alle Titel” retains legacy and
individually downloaded music. A playlist opens in saved membership order;
`OfflineCatalog` in `App/OfflineLibrary.swift` handles local search and deterministic
artist/album/title/date sorting with ID tie-breaks. All-title sorting is remembered.
A persistent mini-player in the offline root keeps transport reachable while browsing.

The manifest format and audio files are unchanged. No collection is inferred from
individual downloads: an existing playlist must have been saved as a collection.
If an older download has no collection metadata, use “Alle Titel”; when online,
choose the playlist's “Offline speichern” action to save its membership. Already
ready tracks are reused. No automatic server requests run in offline browsing.
Local-file removal also affects other collections sharing that file; confirmation
now explains this, while saved membership and server playlists remain intact.

Verification (7 October): signed iOS Debug build and two silent iPhone simulator
flows pass, including playlist download, logout, local-only entry, cover preview,
original order, alphabetical sorting and no-match search. Offline tests cover
restart, account isolation, shared membership and old manifests without collections.
The Swift tests were also run individually: 44 pass, three optional fixture tests
are skipped. An initial concurrent run exposed existing shared fixture/Now Playing
interference; both affected cases pass in isolation. Do not treat that concurrent
run as green. Physical-phone installation/review is pending: the paired iPhone
was unavailable during this change.

## Build 30: mobile collection design and widgets

`MobileCollectionHeader` in `App/MobileListeningViews.swift` owns the iOS cover,
title/metadata and play/shuffle layout. `CollectionView` in `App/Views.swift` uses
it as one list row, removes the duplicate large navigation heading and groups
playlist download/edit/add/reload/delete in one toolbar menu. Regular buttons
are equal width with 52-point minimum height; accessibility sizes stack them.
The existing mini-player regression verifies button size, menu access and
navigation without starting playback.

`Widgets/MusicWidgets.swift` and `Widgets/Info.plist` define `YTMDLWidgets.appex`,
embedded only in `YTMDL-iOS`. It supports small/medium Home widgets and three
Lock Screen accessory families. These are static navigation shortcuts, not live
Now Playing widgets. The extension has no API client, credentials, network
requests or shared container. `Resources/Info.plist` registers `ytmdl-player`;
`Sources/YTMDLCore/MusicWidgetRoute.swift` whitelists only player/favorites/playlists
with no query, fragment, user info, port or extra path. The root retains a
pending route until sign-in, dismisses the player before a different route,
and never starts audio. Parser rejection has a core unit test.

Keep app/extension version and signing identifiers aligned. Add core files to
the package as usual; widget source belongs only to the extension, not the app
or support target. The app and extension compile together through the existing
iOS scheme. A build does not verify Home/Lock Screen placement or tinted widget
rendering on hardware; these remain explicit review steps in FEATURES.md.

## Build 29: stability candidate

Playlist and artist selection buttons accept taps across the whole row, including
its empty space. Manual playlist creation, adding two titles, persistent ordering,
removal, rename and confirmed deletion are covered by an iPhone UI regression.
Repeated tab switching retains access to the paused mini-player.
The fixed-height system mini-player limits only its compact metadata scaling;
full metadata remains scaled in the expanded view. Symbols retain bounded sizes
and 44-point hit areas, including secondary transport in the expanded player.
Accessibility text switches Home shortcuts/mixes to one
column and removes decorative hero art so it cannot squeeze the heading.

Player seeks requested before AVPlayerItem readiness are retained and applied
after loading. Next/previous preserve pause, and a paused queue boundary cannot
start radio. A playback activity revision prevents delayed mix/radio results or
handoff acknowledgements from replacing or pausing a newer playback decision.
Delayed catalog snapshots cannot undo acknowledged favorite mutations.
Logout invalidates unfinished offline transfer attempts; a completion already
queued by URLSession cannot publish a cancelled download afterward. Ordinary
user pause remains distinct and can accept a completed transfer.

Server form results are tied to the checked origin and HTTP consent. Account
changes close expanded player presentation. Offline track menus disable
operations that require a server session. The privacy manifest also declares
app-container file metadata access, reason C617.1.

Use [RELEASE-CHECKLIST.md](RELEASE-CHECKLIST.md) before stable promotion.
This candidate does not establish physical-device route/background qualification,
distribution signing or Release connectivity to an HTTP-only server.

## Build 28: calmer mobile collections

Start, Library and Playlists use consistent neutral surfaces, a single full-width
primary action and large labeled collection shortcuts. Native tab navigation and
the anchored player remain available. Refresh uses pull-to-refresh; Playlist
sorting/refresh live in one labeled options menu instead of several toolbar icons.
Playlists have an inline filter, cover previews and readable hour/minute durations.

`MobileActionStyle`, `MobileLibraryShortcuts` and `MobilePlaylistRow` live in
`App/MobileListeningViews.swift`; the mobile playlist overview is in `App/Views.swift`.
`AppModel.loadPlaylistPreviews` shares at most twelve previews per playlist revision,
loads each batch serially, retains up to four distinct covers and clears on account changes.
An unavailable preview uses a placeholder; it must never prevent opening/editing a
playlist. The existing API response-size limit still applies.

Review Start, Library and Playlists on the phone, filter a playlist, clear the
filter, open it, create/edit a playlist and return through the tab bar. No playback
is triggered by navigation, artwork previews, filters or refreshing.

Design reference: [Apple buttons](https://developer.apple.com/design/human-interface-guidelines/buttons)
(minimum 44-point hit regions and consistent control sizing).

## Code map

| Area | Files |
| --- | --- |
| Schemes, targets, minimum OS and signing | `YTMDL.xcodeproj`, `Resources/Info.plist`, `Resources/Mac.entitlements` |
| App lifecycle and media shortcuts | `App/YTMDLApp.swift` |
| Library, search, authentication, favorites | `App/AppModel.swift`, `App/APIClient.swift` |
| Mobile Home/player, offline views and sound controls | `App/MobileListeningViews.swift` |
| Scoped offline files/background transfer | `App/OfflineLibrary.swift` |
| Timed lyric parser and playback transfer wire models | `Sources/YTMDLCore/ListeningFeatures.swift` |
| Sessions, keychain and logout | `App/SessionVault.swift` |
| Navigation, shared iOS/iPadOS/tvOS views, authenticated artwork | `App/Views.swift` |
| Mac player, queue actions and compact playback options | `App/DesktopViews.swift` |
| Mac Start feed and appearance | `App/DesktopHomeView.swift`, `App/Appearance.swift`, `App/ListeningHistory.swift` |
| Streaming, prefetch, timers, two-player crossfade, metering | `App/PlayerModel.swift` |
| Audio processing and ten-band EQ | `App/Equalizer.swift`, `App/AudioSettingsViews.swift` |
| Cover-derived colors | `App/ArtworkPalette.swift` |
| Native playlist editing, library track selection and smart rules | `App/PlaylistViews.swift` |
| Settings | `App/SettingsViews.swift` |
| API models, origin policy, bounded queue | `Sources/YTMDLCore/` |
| Isolated audio/network/queue regression tests | `Tests/` |
| Synthetic UI server and simulator checks | `Scripts/fixture-server.py`, `UITests/PlayerUITests.swift` |

All app source files are shared with the Swift support test target. If adding
files, ensure both the Xcode project and package include them.

## Build 27: functional audit

[AUDIT.md](AUDIT.md) records each mobile feature's actual verification level,
confirmed corrections and pending physical-device review. Keep this distinction
when extending the preview; a build or fixture result is not an audio-route qualification.
`Tests/YTMDLAppleSupportTests/AuditTests.swift` exercises lock-screen artwork,
delayed account/logout replies and serialized favorites without creating audio items.
The offline integration test also checks local Now Playing artwork without a login
request or playback. `MPMediaItemArtwork` is cached per image, preserved across
progress updates and cleared before the next title's asynchronous image loads.
The callback captures immutable CGImage data rather than main-actor player state.

The iPhone UI suite uses stateful playlist/favorite fixtures and optional
`--audit-failures` for a recoverable collection error. The codec test requires
`YTMDL_CODEC_FIXTURE_URL`; it fully decodes synthetic silent files with AVAssetReader,
without starting AVPlayer. Do not replace them with library media or audible signals.
The expanded mobile transport has `mobile-player-toggle`: a generic first match
for Pause can select the mini-player beneath a sheet and produce a false UI failure.

## Build 26: reachable mobile player

The compact iPhone TabView owns `tabViewBottomAccessory`; do not put a bottom
safe-area inset inside each tab's NavigationStack because floating tabs can cover it.
`MobileMiniPlayer` keeps cover/title, play/pause and next controls reachable in
pushed playlist views. The toolbar also opens the player before any track starts.
The collection header includes a bounded four-cover collage and duration.
`testMiniPlayerRemainsReachableInPlaylistWithoutPlayback` tests tab switching,
pushed playlists and expansion through a paused loopback-only Debug fixture.
The fixture does not create an AVPlayerItem or start audio. Preserve this property.

## Mobile preview 0.2.0

Build 25 added the iPhone/iPad listening feature set documented in
[FEATURES.md](FEATURES.md), including download/restart review steps and explicit
limits. Preserve the existing bundle ID, keychain and preferences during updates.
The background session registers during launch and delegates system wake completion
through `OfflineAppDelegate`. Test fixtures use a distinct temporary store and an
ephemeral transfer session. No task description or manifest contains credentials.

## Build and review

Use Xcode 27, OS 27 SDKs and Swift 6.4. If multiple Xcodes are installed, set
`DEVELOPER_DIR` for each command; do not change global toolchain selection.
Build commands, fixtures, device requirements and codec limits are in
[README.md](README.md). Authentication, local HTTP consent, privacy and Apple
review requirements are in [SECURITY-DESIGN.md](SECURITY-DESIGN.md).

Build 24 binds split-view detail navigation to an explicit NavigationPath.
Albums, artists, playlists and home cards use value-based collection routes;
the selection handler clears the path on section changes or repeated clicks.
Opened album, artist and playlist drilldowns cannot conceal another section's
root. Clicking the already selected Mac sidebar item returns to its overview.
Home shortcuts, mini-player, search and player Escape use the same selection
handler. Search focus is cleared when leaving search and typing does not recreate
its stack per keystroke. Focus is requested after the new search field appears.
Navigation replacement retains the shared AppModel,
PlayerModel, queue and authentication state. Session changes discard drilldowns.
The iPad-only silent UI regression opens an album and playlist, switches sections
and verifies the library overview returns without starting playback.

The Mac layout uses a unified listening card
and a right-hand queue/options area. Artwork is capped at 520 points, with
transport near the bottom and flexible space beneath metadata. Known library
duration takes precedence over unreliable stream estimates for seeking and
crossfade; missing metadata falls back to valid AVPlayer duration. The cover stays bounded; seeking, volume,
AirPlay and big transport controls sit beneath it. Queue menus address
occurrences by index, preserve the playing occurrence and invalidate prefetch
when order changes. Removing from the queue never deletes media.

Queue, lyrics, playback and sound share one right-hand surface with four
underlined tabs: **Warteschlange**, **Lyrics**, **Wiedergabe**, **Klang**. Playback
options are in their own tab, so other content uses the full panel height.
Tabs scroll horizontally when needed and follow the selected tab. Transparent
queue rows reduce competing surfaces; only the playing occurrence is tinted.
Playback options use matching menu rows for EQ, timer and speed. Visualizer
choices and transition protection live in named popovers with aligned fields.
EQ headroom remains reachable in the EQ menu and detailed sound settings.
Shorter windows scroll. Player overlays suppress the underlying toolbar's Escape shortcut
so their close action runs first; a later Escape returns to the library. Sidebar and sign-in use the original repository logo,
and Mac/iOS icons are generated from it by `Scripts/generate-icons.swift`.

Mac favorites and playlist details now use a collection header with cover
preview/collage from already loaded tracks, play/shuffle and local filtering and
sorting. The playlist index uses adaptive cards with a name filter and clear
open action, without extra requests to fetch individual playlists. Long names
reserve three title lines so cards remain aligned. Mac track rows expose favorite
and enqueue controls directly; a labeled **Aktionen** menu replaces the ellipsis.
Favorite mutations stay explicit and are blocked while that row is pending;
removing a loaded favorite adjusts the offset for the next 100-track page.
Loaded totals are labeled when more pages exist. Filtered empty lists disable
transport actions; sorting/filtering do not change stored playlist order or
remove library media. Shared non-Mac list layouts remain available.

Playlist management is native on all three targets: create, rename, describe,
delete with confirmation, add/remove titles and persist order. Track action menus
in collections/search and Mac player/queue offer **Zur Playlist hinzufügen**;
the Mac current-title menu can save its queue as a manual playlist. Library
selection adds up to 100 tracks; queue additions support up to 500 unique tracks
in sequential server batches of 100. Already acknowledged batches remain visible
if a later request fails; errors explain partial changes without deleting music.
Metadata and rules are separate server writes; a rule failure preserves and
reports the successful metadata edit. Reordering is enabled only in the original,
unfiltered order. Deleting a playlist removes its collection, not library media.

Smart playlists use existing server rules: genre, artist, favorites, added-days,
recent/title/frequent/last-played ordering and 1–500 titles. Presets provide a
starting point; all criteria combine with AND. They resolve when opened, and
manual membership changes are disabled. Turning rules off restores earlier
manual membership rather than materializing the current smart result. Listening
sorts use server history, not the Mac's local recent-listening cache. Artist search
is not limited to the first loaded library page. No recommendation model or new
backend migration is introduced. Session/origin/CSRF guards remain shared;
stale catalog reloads cannot overwrite newer playlist mutations.

The optional Visualizer now uses a Hann-windowed 2048-point real FFT (Accelerate)
and 32 logarithmic bands rather than RMS-history graphics. **Spiegel-Spektrum** draws
mirrored bars and falling peaks; **Säulen**, **Orbit**, **Ringe**, **Lichtpunkte**
and **Frequenzband** provide five other frequency-based renderers. Rings summarize
four frequency ranges; the envelope is a frequency display, not a PCM waveform.
The player supports placement beneath the cover, a cover overlay with adjustable
opacity, or a cover-free view over the music-color background. Intensity, peak
markers and colors are device-local settings, shared between player options
and detailed playback settings. Color modes include cover, theme, Aurora,
sunset, ocean, neon and custom. Custom RGB color pickers support a solid color
or two-color gradient; all six renderers and the expanded view use the selected
palette. These preferences do not recolor the rest of the player. Unknown or
absent `playerVisualizerColorMode` respects the old `playerVisualizerCoverColors`
boolean. RGB values persist as validated six-digit strings; invalid values
fall back to defaults. Custom color and gradient choices remain saved when
another palette is selected. Turning visualization off restores
the ordinary cover. Existing bars/curve preference values remain compatible. Main-actor attack,
release and peak decay run near 30 Hz. The large live view closes with Escape
while playback continues. Style, placement and enabled state persist; default/style
migration retain existing preferences. Render callback storage/setup is allocated
before rendering, analysis is activation-gated, and disabling both EQ and
visualization removes the processing tap. EQ bypass leaves samples unchanged.
The first decoded channel is analyzed, including AVPlayer gain. During crossfade
callbacks from either title feed the shared display, not a final mixed-output FFT.
EQ headroom is not title loudness normalization. Reference links are in README.

Local verification must remain silent: the default suite uses zero-valued PCM
for actual AVPlayer transport checks and silent loopback audio for UI checks.
Two signal-dependent tap/meter tests require explicit `YTMDL_AUDIBLE_AUDIO_TESTS=1`
and are skipped by default. Never enable them through local speakers during
unattended verification. Stateless `SpectrumCanvas` allows
rendering checks with CPU-analyzed samples, without scheduling audio playback.

Review wide/narrow windows, long titles, light/dark themes, queue filtering with
removal and duplicates, next-track order, mute/volume, pause, seeking, changing
EQ, crossfade and timers. Use fixtures for automated checks. Do not sign in to
production or mutate its data incidentally during verification.

## Open work

- Measure real device/network startup latency; fixture timings do not establish
  live-server performance. Check actual media formats on physical devices.
- Real per-title loudness normalization needs reliable loudness metadata or
  analysis and bounded processing. The current UI does not pretend it exists.
- Physical-device background completion, offline codec playback and live handoff
  still require qualification. Automatic discovery, sample-perfect gapless, loudness
  analysis, CarPlay entitlement, Siri/widgets and third-party services remain open.
- TV device-code backend routes exist in this branch but require reviewed backend
  integration and deployment before use on an older server. Code approval is
  explicit; approving an admin account grants that account's permissions.
- Confirm final-commit CI, physical iPhone/iPad/TV behavior and accessibility.
  Distribution signing, final TV artwork, privacy disclosures, TestFlight and
  App Store review are separate unfinished steps.

Preserve the bundle identifier, app preferences and keychain when replacing a
local preview. Keep a rollback app bundle, stage and verify signing before
stopping the installed app, then reopen the replacement. Machine paths, private
review captures and local server addresses belong in the local handoff, not in
public repository files.
