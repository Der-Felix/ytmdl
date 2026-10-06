# Apple 0.2.1 (29) release checklist

This is a stabilization candidate for the native client. App and server versions
are independent. Builds and isolated tests are evidence for the tested paths;
they do not establish zero defects or certify physical-device behavior.

## Candidate scope

- Whole-row taps in playlist/title/artist selection; manual playlist lifecycle.
- Early seek retention; next/previous while paused; paused queue completion.
- Delayed favorite snapshots, radio/mix results and playback handoff replies.
- Cancelled offline completion after logout and background-task reconciliation.
- Origin-bound login form state and offline action availability.
- Compact transport hit areas and layout at the largest accessibility text size.
- Explicit app-container metadata privacy declaration.

The current feature set and limits are in [FEATURES.md](FEATURES.md). Keep bundle
identifiers, keychain, preferences and offline storage when upgrading. Do not
uninstall the user's app as an update workaround.

## Automated qualification

Use isolated loopback fixtures and temporary stores, never production accounts
or library media. Default transport fixtures contain silence. The two
signal-dependent tests require explicit opt-in and remain unqualified when skipped.

- Swift core/support suite, including silent AVPlayer transport and offline transfer.
- iPhone: favorites/search, collections, error/retry, smart rules, manual playlist
  create/add/order/remove/rename/delete, repeated tabs, player and queue.
  Include the maximum-text-size compact transport/navigation regression.
- iPad: navigation replacement and reachable player controls.
- Release compilation: iOS, macOS and tvOS simulator.
- Verify the final commit's CI and applicable branch rules before integration.

Record dated results in [AUDIT.md](AUDIT.md). Do not replace failed tests with
weaker assertions or mark unexecuted paths verified.

## Required before calling the mobile release stable

1. Install the signed candidate on a real iPhone without removing existing data.
   Verify **Settings → App version: 0.2.1 (29)**. Availability of a build alone
   does not confirm installation.
2. Connect a Release build to the intended server over HTTPS. Release rejects HTTP;
   local HTTP consent belongs to Debug and does not qualify production transport.
3. With the user starting playback, verify lock-screen artwork/title changes,
   pause/seek/next, wired/Bluetooth/AirPlay routes, an interruption and resumption.
4. Download a small playlist, background the app, then restart in flight mode.
   Verify audio, cover, lyrics, queue and storage accounting. Logout must stop
   unfinished transfers; intentionally retained offline copies remain available.
5. Review large text, VoiceOver, long names, landscape and compact player controls
   on physical devices. Check EQ, transition, timer and selected visualizer settings.
6. For remote distribution, finish signing/provisioning and App Store Connect /
   TestFlight setup, privacy/support information and authorized reviewer media.
   Compilation does not upload a build. TV distribution also needs final layered
   artwork and remote-focus/device-pairing qualification.

Any failed gate is a release blocker. Keep the previous signed app for rollback;
do not label an unqualified simulator build or Debug-only HTTP installation as
an App Store-ready stable release. Promotion to `dev`/`main`, tags, server changes
and distribution follow the repository's approval and branch workflow.
