# YTMDL for Apple

Native SwiftUI listening client for iPhone, iPad, Mac and Apple TV. The current
preview is **0.3.0, build 30** and requires OS 27 and Xcode 27. It is a client of the
existing YTMDL API, not a second downloader or a replacement database.

See [HANDOFF.md](HANDOFF.md) for the code map and [FEATURES.md](FEATURES.md)
for the mobile/offline feature matrix, review steps, limits and open work.
The build 27 [functional audit](AUDIT.md) separates verified behavior from
pending hardware checks, including the corrected lock-screen artwork path.
Build 29 adds playback, playlist hit-area and delayed-response fixes. See
[RELEASE-CHECKLIST.md](RELEASE-CHECKLIST.md) for the remaining stable-release gates.
Build 30 gives mobile collections larger play/shuffle controls and one playlist
menu, and embeds a WidgetKit extension for Home/Lock Screen navigation shortcuts.
Widget placement is user-controlled; live song display/control widgets are not
implemented. Review steps and limits are in [FEATURES.md](FEATURES.md).

## Open and run

Open `apple/YTMDL.xcodeproj` in Xcode 27. Choose `YTMDL-iOS`,
`YTMDL-macOS`, or `YTMDL-tvOS`, select your destination and run. The iOS target
supports both iPhone and iPad. Choose your own Apple development team and a
unique bundle identifier before installing on physical devices. A simulator
build needs no paid developer membership. Nothing is submitted to TestFlight
or the App Store by these commands.

Enter your server's **HTTPS origin**, then sign in using an existing account.
The native client already works with the v1.2.0 library/authentication API.
Device-code login additionally requires the new device routes in this branch.
No server data migration is needed. The server version is not changed by
building the app. Use the root repository workflow for backend integration and
deployment; pending code requests are invalidated by a backend restart.

Debug builds offer an explicit local HTTP switch for literal RFC1918 IPv4 or
loopback addresses. This transmits credentials and music without encryption;
use it only for an intentional local development test. HTTP consent is not
remembered. Restored HTTP cookies omit the Secure property; HTTPS cookies and
explicitly secure cookies retain it. Authentication response cookies are
adopted before building the next CSRF request. Failed server checks keep the
login form locked, and invalid credentials have their own error message. Release builds reject HTTP, regardless of the switch's state.
The app does not disable certificate verification or enable arbitrary ATS loads.

## Listening and layout

- Compact iPhone tab navigation, expandable player and persistent mini-player.
- Mobile playlist/favorites headers combine artwork, title, duration and large
  play/shuffle buttons. Playlist download/edit/add/reload/delete live in one menu.
  The iOS app embeds `YTMDLWidgets.appex`; its bundle identifier must remain a child
  of the app identifier when changing signing settings. Both use version 0.3.0 (30).
  Home and Lock Screen widgets link to app views without starting audio or sharing
  authentication. No App Group capability is required.
- iPad/Mac sidebar navigation and adaptive album grids.
  Switching sidebar sections closes the previous album/artist/playlist drilldown;
  clicking the selected Mac section again returns to its overview. The shared
  playback queue and session remain alive across navigation.
  Mac has readable album,
  artist and track labels, search (⌘F) in the single window toolbar, and a centered
  transport bar with seeking, app volume, mute and AirPlay. The full player uses
  two areas on large windows: a unified listening card with a centered cover
  capped at 520 points and large transport/volume controls; queue/lyrics/sound
  and playback options in one continuous right-hand panel. **Warteschlange**, **Lyrics**,
  **Wiedergabe** and **Klang** each have their own tab, with no permanent options
  footer. Queue and lyrics can use the full panel height. Tabs scroll horizontally
  when larger text or a smaller window needs more space. Transport sits near the
  bottom of the listening card, with flexible space beneath the title. Small
  windows stack artwork and context above a persistent compact transport.
  Panels scroll independently.
  Compact playback options use aligned rows without nested tool tiles. The
  adjustment button beside **Visualizer** opens all its display choices; the
  adjustment button beside crossfade opens album transition protection. EQ
  headroom lives in the EQ menu and detailed sound settings. Popovers close with
  Escape before the player navigation shortcut runs.
  EQ profiles, headroom, crossfade, speed and sleep timer remain accessible;
  the **Klang** tab exposes all ten EQ bands and detailed playback settings.
  The queue can be filtered without changing order. Each occurrence has actions
  to play now/next, toggle favorites, copy metadata or remove it from the queue.
  The current occurrence cannot be removed; clearing upcoming entries never
  deletes library files. Escape returns to the library; expanded artwork closes
  with Escape first.
- The original repository logo appears in the sidebar and connection screen.
  Mac/iOS app icons use that same artwork; regenerate them with
  `swift apple/Scripts/generate-icons.swift` from the repository root.
- Optional **Visualizer** uses a real 2048-sample Hann-windowed FFT with Apple
  Accelerate. It displays 32 logarithmic frequency bands in six styles: **Spiegel-Spektrum**
  (mirrored bars with falling peaks), **Säulen**, **Orbit**, **Ringe**,
  **Lichtpunkte** and **Frequenzband**. The four rings aggregate frequency ranges;
  the filled envelope represents frequency, not a PCM waveform. Under **Player →
  Wiedergabe** using the adjustment button beside Visualizer, or in detailed sound
  settings, choose beneath-cover placement,
  a cover overlay or a cover-free view on the music-color background. Intensity,
  overlay opacity and peak markers are configurable. Colors offer **Coverfarben**,
  **Theme-Farbe**, **Aurora**, **Sonnenuntergang**, **Ozean**, **Neon** and **Eigene Farben**.
  Native color pickers select a custom solid color or two-color gradient. Colors
  affect all six styles and the expanded visualizer, independently of cover and
  control accents. Preferences persist locally; an absent or unknown color-mode
  value respects the earlier cover/theme boolean instead of resetting it. Disabling the visualizer restores the normal cover.
  Frequency is shown instead of a scrolling volume history. The larger live view
  includes pause/play and closes with Escape. Style and activation persist on the
  device; it defaults to off. Audio-callback storage is preallocated and FFT work
  is gated by activation. Main-actor attack/release and peak decay update around
  30 Hz, stop on teardown and decay to silence on pause/stall. With EQ bypassed,
  samples remain unchanged; with both features off there is no processing tap.
  Analysis uses the first decoded channel including AVPlayer gain, not a
  calibrated final speaker signal. During crossfade the shared display receives
  either title's tap results rather than an FFT of their final mixed output.
  Unsupported PCM retains ordinary playback. No microphone access is used.
  Implementation references: Apple's [windowing guidance](https://developer.apple.com/documentation/accelerate/reducing-spectral-leakage-with-windowing)
  and [audio processing taps](https://developer.apple.com/documentation/MediaToolbox/MTAudioProcessingTap).
- Mac **Playlists** uses adaptive collection cards, a name filter and optional
  A–Z sorting. Cards display names, track counts, duration and a clear open action;
  the index makes no extra per-playlist detail requests. **Favoriten** and an
  opened playlist use a cover preview/collage built from loaded tracks, readable
  title rows, play/shuffle, and filters for title, artist or album. Favorites load
  100 tracks per page; their header explicitly labels partial totals as loaded
  tracks. Filtering and playback apply to loaded matching tracks, and sorting
  changes only the local display; playlist order stored on the server is untouched.
  Mac track rows show favorite and enqueue buttons directly, with a labeled
  **Aktionen** menu for play/copy. Favorite requests cannot be double-submitted
  while pending; removed favorites disappear from the loaded collection and
  adjust its pagination offset. Empty filters disable play/shuffle.
- Mac opens on **Start** by default: recently added albums, favorite tracks,
  playlists, artists and recently played tracks, with quick links to search and
  collections. Settings can choose a different launch page and hide feed sections.
  The larger grouped sidebar keeps Settings accessible below its scrolling list.
- Six Mac themes (Rose, Ocean, Forest, Amber, Lavender and Graphite) change
  backgrounds, surfaces and accents in light/dark/system appearance. Three text
  sizes also apply to navigation; the default is Large. Album cover size is
  adjustable from 220 to 340 points. **Settings → Darstellung** contains these
  controls; **Startseite**, **Wiedergabe**, **Klang** and **Konto** group the other options.
- Mac recent listening records at most 40 track metadata entries after playback
  begins. History stays in local preferences, separately scoped to server and
  account. Optional server history uses idempotent events and can be disabled;
  mobile offline storage is documented separately in FEATURES.md. Local history can be disabled or cleared in **Settings → Wiedergabe**. Disabling retains earlier
  entries, while logout removes them from the current view. Test fixtures never
  persist listening history.
- Mac volume and mute control AVPlayer output and persist across app launches;
  moving the volume slider unmutes. Device/system volume remains separate.
  ⌘↑/⌘↓ adjust app volume; ⇧⌘M toggles mute. Mac Settings use top-aligned cards
  for playback, appearance, server, device approval and account controls.
- The Mac player derives its background and control accents from the current
  cover on the device. Pale covers are darkened for readable white transport
  glyphs; missing/monochrome artwork uses the selected theme. Disable this in
  **Settings → Darstellung**. The sidebar has a 300-point minimum width.
- **Mac Settings → Klang**, also available in the player's **Klang** tab:
  actual ten-band audio EQ (31.5 Hz–16 kHz, ±12 dB), five built-in profiles,
  persistent custom tuning, preamp (−12 to +6 dB), bypass and optional headroom
  compensation. The OS 27 AVPlayer mixed-output processing tap filters decoded
  PCM; unsupported output formats or attachment failure leave audio playing
  without EQ and show a message. When both EQ and visualization are off, no processing tap is attached.
- **Mac Settings → Wiedergabe**: crossfade from 0 to 12 seconds (off by default),
  next-track preparation, fast start, 0.5–2× speed with pitch correction when
  speed differs from 1×, and a sleep timer (15/30/60 minutes, track or album end).
  Repeat cycles through off, queue and one track. Settings stay on this device;
  active timers and repeat modes are not restored on launch.
- Crossfade uses two authenticated AVPlayers and complementary linear gains.
  It starts only when the incoming item is ready and caps the overlap at half of
  each track. Album protection skips overlaps for adjacent entries with the same
  album name and artist list. Pause/mute/volume apply to both players; seeking,
  stop, logout and timer completion cancel the incoming transition. A late or
  failed incoming item keeps the normal title-end path. Prepared titles retain
  their position when promoted. This is not a guarantee of sample-perfect
  gapless playback. Timer album boundaries are determined by the current queue.
- For known tracks, library duration drives the timeline, seek limits and both
  sides of the crossfade. AVPlayer duration is only a fallback when library
  duration is missing; inflated stream estimates do not override known metadata.
- Fast start avoids a separate playable-asset preflight and begins with available
  audio; lyric and cover loading run independently. Turn it off on weak networks
  to allow more buffering. Native loading state reflects actual playback.
- Apple TV native tabs/focus, readable covers, remote-operated transport and seek.
- Local library search, artists, albums, genre filtering, favorites and playlists.
  Mac search shows existing library albums before a query, then groups results
  into artist/album cards and playable track rows. Search is cleared on logout
  or server changes.
- Bounded paginated library reads, queue up to 500 tracks, shuffle upcoming
  songs, repeat queue, seeking, lyrics, explicit retry and format error messages.
- Native audio-session/background configuration, system media controls and
  AirPlay output selection on iPhone/iPad/Mac. TV uses system output controls.
- Administration stays in the existing website, accessed through Settings.

Audio is streamed in its existing container/codec. No transcoding, library
rewrites, external-provider requests or automatic download retries occur.
Actual format support must be checked on each target device. OS 27 simulator
UI tests play an authenticated synthetic Opus/Ogg stream with advancing time.
A local Mac asset check opens Opus/Ogg and AAC/M4A, but rejects Opus/WebM.
These checks do not replace playback checks on physical devices with actual
library media. Unsupported media is
reported rather than silently skipped. iOS offline storage, LRC highlighting and
explicit server playback handoff are available in preview 0.2.0. Automatic device
discovery, seamless remote control and physical-codec qualification remain open.

## Apple TV code login

After connecting the TV to your server, it requests an eight-character code
and QR code. On an already signed-in device open native **Settings → Anderes
Gerät anmelden**, or web **Profile & Security → Apple TV & Geräte anmelden**.
The QR links to `/profile?device_code=…` on the same server. It never approves
automatically. First inspect the device name, then explicitly confirm.

Only approve a code requested on your own device. Confirmation grants an
ordinary session with your account's permissions, including administrator
permissions if you approve as an administrator. The app presents listening
controls, but this is not a new server-side reduced-privilege role. Revoke the
device session through the existing profile session list when needed.

## Security and Apple guidance

See [SECURITY-DESIGN.md](SECURITY-DESIGN.md) for credential handling, threat model,
privacy and the App Store review requirements. This is a review build, not an
Apple-reviewed or App Store-approved application. There is no telemetry,
advertising SDK, analytics SDK or third-party account integration.

Apple's bundled `swiftui-whats-new-27` skill and its AsyncImage and State-macro
references were read from Xcode 27 before implementation. Authenticated artwork
uses the new `AsyncImage(request:)` and a custom ephemeral URLSession. No shared
cookie jar or persistent private artwork cache is used.

## Verification

```sh
swift test --package-path apple --scratch-path /tmp/ytmdl-apple-tests
xcodebuild -project apple/YTMDL.xcodeproj -scheme YTMDL-macOS CODE_SIGNING_ALLOWED=NO build
xcodebuild -project apple/YTMDL.xcodeproj -scheme YTMDL-iOS -destination 'generic/platform=iOS Simulator' CODE_SIGNING_ALLOWED=NO build
xcodebuild -project apple/YTMDL.xcodeproj -scheme YTMDL-tvOS -destination 'generic/platform=tvOS Simulator' CODE_SIGNING_ALLOWED=NO build
```

Use Xcode 27's `DEVELOPER_DIR` if another Xcode is the system default; changing
the global `xcode-select` setting is unnecessary. Native UI tests use only a
loopback fixture started by `python3 apple/Scripts/fixture-server.py` on port
59583. Choose an iPhone/iPad or TV simulator and run Test for that scheme. The
shared test schemes set `YTMDL_FIXTURE_URL` to the local fixture. They never
authenticate to a production host. `--fixture-server` is accepted only in Debug
and only for literal loopback; its cookies are not saved to the keychain.

Audio regression tests use generated PCM files and actual AVPlayers to verify
EQ response, bypass, live activation, real RMS metering with EQ bypassed,
queue occurrence edits and prefetch invalidation, crossfade gain/position, pause/seek/stop,
repeat and timer boundaries. Palette and session-invalidation tests use isolated
images/sessions. Local compressed-stream probes are loopback-only. These checks
are not measurements of production network start latency.
Transport fixtures are silent by default. Two tests that require an audible
signal are skipped unless `YTMDL_AUDIBLE_AUDIO_TESTS=1` is explicitly supplied;
do not enable that flag during unattended local verification. CPU-only EQ and
spectrum tests still run, alongside silent AVPlayer transport tests. Supply a
loopback `YTMDL_OFFLINE_FIXTURE_URL` to exercise the authenticated offline transfer.

The DSP uses [Apple’s OS 27 mixed-output audio tap](https://developer.apple.com/streaming/Whats-new-HLS.pdf)
and the [W3C Audio EQ Cookbook](https://www.w3.org/TR/audio-eq-cookbook/).
Render callbacks use preallocated channel state and atomic settings; automatic
headroom compensation estimates the combined filter response, and processed
samples are bounded to the PCM range. This is not server-side ReplayGain or
library normalization.

The included iPhone/iPad/Mac icon adapts the repository music-note mark.
Regenerate it with `swift apple/Scripts/generate-icons.swift` from the repo root.
For an App Store build, supply final layered TV icons, signing/provisioning, privacy
policy, support contact, screenshots, accurate metadata and a reviewer account
using authorized sample music. Verify interruption, AirPlay, background audio,
accessibility, format support and remote focus on physical devices.

## Native playlists (preview build 21)

Open **Playlists → Neue Playlist** to create a manual collection or enable
**Intelligente Playlist**. Edit name, description and smart rules using
**Playlist bearbeiten** (or the Playlist toolbar menu on mobile/TV).
**Titel hinzufügen** searches the existing library and accepts up to 100 selected
titles. Track action menus add songs to existing or new playlists; the Mac
player's current-title menu can also save its queue. Playlist row edit menus move
titles up/down or remove membership. Clear local filters and select original
order before persisting a reorder. Deletion asks for confirmation and preserves
library audio. The same user-owned playlists appear in the web client.

Smart presets and rules are evaluated by the existing server on opening:
genre/artist, favorite-only, recent additions, sort and maximum 1–500 titles.
All filters combine; frequent/recent-play sorting uses server listening history.
Manual membership/order is disabled for smart collections. Turning rules off
restores earlier stored manual membership, without copying the dynamic selection.
Native playlist features require the existing playlist/rules/bulk API routes.

Use silent isolated fixtures for verification. `PlaylistTests.swift` exercises
typed rule payloads, CSRF, clearing rules with JSON null, order/removal, batching,
partial failures and discarding mutations across server/session replacement.
Run only these non-audio tests locally with:

```sh
swift test --package-path apple --filter 'nativePlaylistMutations|smartRulesDecode'
```
