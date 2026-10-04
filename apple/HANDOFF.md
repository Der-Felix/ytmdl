# Apple app handoff — preview 0.1.0, build 12

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

Build 12 replaces the three-column Mac player with a unified listening card
and a right-hand queue/options area. The cover stays bounded; seeking, volume,
AirPlay and big transport controls sit beneath it. Queue menus address
occurrences by index, preserve the playing occurrence and invalidate prefetch
when order changes. Removing from the queue never deletes media.

Visualization is optional, defaults to off and uses measured PCM RMS history.
It is not a spectrum analyzer. During crossfade the shared meter receives
callbacks from either title rather than a final mixed-output measurement.
The bypass path leaves samples unchanged. With both EQ and visualization off,
no processing tap is attached. EQ headroom is not title loudness normalization.

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
