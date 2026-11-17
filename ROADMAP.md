# Roadmap

What is planned for YTMDL, in the order it is meant to happen. This is a statement
of intent, not a promise: items move when something more urgent turns up, and
nothing here has a date. Shipped work is in the [changelog](CHANGELOG.md).

Last updated: 2026-11-17.

## Now

Work that is in progress or blocks a release.

- **Check the new theme with real content.** The dashboard, the library, the
  release view and the player page were checked with a filled library at desktop
  width and at 375 px. Downloads, subscriptions, settings and playlists were
  brought to the same standard in 1.6.0 but have only been tested, not looked at
  with a filled library. What remains is that look, from 360 to 1920 px and in
  both themes, and new documentation screenshots, which still show the old look.

## Next

Agreed work that starts when the items above are done.

- **Live recordings the provider does not label.** A live album is recognised by
  its title or, since 1.4.1, by the live markers in its track titles (at least
  three tracks and three quarters of the list). One that has fewer marked tracks
  still shares its tracks' identity with the studio recordings, and its plainly
  titled tracks can still be matched to a studio recording.
- **Show Live and Remix in the library's release-type filter?** The fixed chip
  list offers Album, EP, Single and Compilation only. To decide whether the
  other release types belong there.
- **Apple app.**
  - Move the macOS session items to the data protection keychain. This depends
    on how the app is signed; the DMG is ad-hoc signed today.
  - Decide whether opening a playlist should reload every playlist preview.
- **Smaller findings from the code review that need a decision.**
  - Restarts use up retry attempts of a download.
  - The health endpoint shows the server version without sign-in; `ytmdlctl` and
    the Apple app read it.
  - The pool of pending device sign-ins can be filled from outside.
  - `ytmdlctl recover restore` cannot be reached after a successful update.
  - Database errors are not mapped to specific API errors everywhere.

## Later

Directions that are wanted but not yet planned in detail.

- **Remove the older identity key forms.** Since 1.6.0 stored keys are carried
  over to the current scheme in the background. The code that still reads the
  older remix-title form can go once a release requires a finished migration;
  the earliest is the release after next. Tracks held back because their stored
  key no longer follows from their title have no way to be accepted yet.
- **Branch model.** Changes have gone to `main` directly, with `dev` moved up
  afterwards, although the contribution guide names `dev` as the target. Either
  the guide or the practice should change, and `main` should get required checks.

## How this list is maintained

Each release moves what it shipped out of this file and into the changelog. An
item is added here when it is agreed, not when it is first mentioned. Bugs go to
the issue tracker; this file is for work that changes what YTMDL does or how it
is built.
