# Roadmap

What is planned for YTMDL, in the order it is meant to happen. This is a statement
of intent, not a promise: items move when something more urgent turns up, and
nothing here has a date. Shipped work is in the [changelog](CHANGELOG.md).

Last updated: 2026-11-17.

## Now

Work that is in progress or blocks a release.

- **Stop a failed release run from using up its version.** 1.3.1 and 1.3.2 were
  lost because the release workflow pushed the version tags before the upgrade
  qualification. It now pushes `candidate-*` tags, qualifies those images,
  upgrades included, and gives the qualified digests the version tag only
  afterwards; published version tags are still never replaced. A rehearsal run
  has exercised this path without a release. What remains: the first real
  release with it, which is the only run that creates the version tag, the draft
  and the release. Failed runs and rehearsals delete their own `candidate-*`
  package versions.
- **Check the new theme with real content.** The theme shipped in 1.4.0 was
  reviewed on an empty library. Covers, long lists, the playing view and narrow
  screens still need a pass, and the documentation screenshots show the old look.

## Next

Agreed work that starts when the items above are done.

- **File names.** Decide how a track is named on disk when the provider's title
  carries an edition note ("Remastered 2011", "Single Version"), when the title
  contains characters a filesystem refuses, and for multi-disc releases, where
  disc 2, track 2 is currently stored as `202 - Title`. Existing libraries need a
  way to move to a new scheme without breaking media servers.
- **Live recordings the provider does not label.** A live album is recognised by
  its title or, since 1.4.1, by the live markers in its track titles (at least
  three tracks and three quarters of the list). One that has fewer marked tracks
  still shares its tracks' identity with the studio recordings, and its plainly
  titled tracks can still be matched to a studio recording.
- **Finish the web interface to the standard the theme sets.**
  - Toasts for results and errors, a command menu, and a queue that can be
    dragged on touch devices.
  - A monospaced face for durations and counters.
  - A pass over every view for loading, empty and error states, keyboard order,
    and the widths from 360 to 1920 px.
  - Less re-rendering while a track plays; the queue and the player page are
    redrawn on every progress tick.
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

- **Finalise without downloading again.** A track that was staged while the
  library was unreachable is downloaded once more when the library returns. The
  worker needs a path that only moves the staged file.
- **A migration framework for stored identities.** The key a track is recognised
  by is written once. Changing how it is built currently needs lookups that know
  the older keys; a way to rewrite stored keys would remove that.
- **Branch model.** Changes have gone to `main` directly, with `dev` moved up
  afterwards, although the contribution guide names `dev` as the target. Either
  the guide or the practice should change, and `main` should get required checks.

## How this list is maintained

Each release moves what it shipped out of this file and into the changelog. An
item is added here when it is agreed, not when it is first mentioned. Bugs go to
the issue tracker; this file is for work that changes what YTMDL does or how it
is built.
