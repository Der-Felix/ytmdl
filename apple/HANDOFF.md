# Apple app handoff — preview 0.1.0, build 24

The Apple client lives on `feat/apple-native-player` in draft
[PR #42](https://github.com/Der-Felix/ytmdl/pull/42), based on `dev`.
Do not promote it to a stable server release just because it builds locally.
The server release and native preview have independent version numbers.

## Code map

| Area | Files |
| --- | --- |
| Schemes, targets, minimum OS and signing | `YTMDL.xcodeproj`, `Resources/Info.plist`, `Resources/Mac.entitlements` |
| App lifecycle and media shortcuts | `App/YTMDLApp.swift` |
| Library, search, authentication, favorites | `App/AppModel.swift`, `App/APIClient.swift` |
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

Local verification must remain silent: compile the full test target, run only
CPU/state tests without audio output, and use the default silent loopback WAV
for layout checks. The full AVPlayer audio suite includes audible fixtures and
must not be run through local speakers. Stateless `SpectrumCanvas` allows
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
- Timed lyric highlighting, native offline downloads, playback transfer between
  devices and automatic server discovery remain open.
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
