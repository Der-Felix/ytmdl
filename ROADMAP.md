# Roadmap

What is planned for YTMDL, in the order it is meant to happen. This is a statement
of intent, not a promise: items move when something more urgent turns up, and
nothing here has a date. Shipped work is in the [changelog](CHANGELOG.md).

Last updated: 2026-11-16.

## Now

Work that is in progress or blocks a release.

- **Get the maintenance release out.** The fixes from the full code review and the
  correction for live recordings are on `main`, but neither v1.3.1 nor v1.3.2 was
  released: both runs stopped in the upgrade qualification after their container
  images had been pushed. The cause is in the test harness. Under Podman, the
  network of one test stack still holds its subnet when the next stack starts.
  The harness is being fixed and gets a way to run without a release tag.
- **Stop a failed release run from using up its version.** The release workflow
  pushes the images before it runs the upgrade qualification, and it never
  replaces a published image tag. A failure in between costs the version number.
  The qualification should run against the candidate images before anything is
  published.
- **Review the new web theme.** A flat, token-driven theme is open as a pull
  request. It builds and passes the checks, but nobody has looked at it yet. It is
  merged once it has been seen on real screens and corrected where needed.

## Next

Agreed work that starts when the items above are done.

- **File names.** Decide how a track is named on disk when the provider's title
  carries an edition note ("Remastered 2011", "Single Version"), when the title
  contains characters a filesystem refuses, and for multi-disc releases, where
  disc 2, track 2 is currently stored as `202 - Title`. Existing libraries need a
  way to move to a new scheme without breaking media servers.
- **Live recordings the provider does not label.** A live album is recognised by
  its title. One that is not recognised still shares its tracks' identity with
  the studio recordings. The audio match for a plainly titled track of a live
  album also still prefers a studio recording.
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
- **Screenshots and documentation for the new look**, once the theme is merged.
- **Branch model.** Changes have gone to `main` directly, with `dev` moved up
  afterwards, although the contribution guide names `dev` as the target. Either
  the guide or the practice should change, and `main` should get required checks.

## How this list is maintained

Each release moves what it shipped out of this file and into the changelog. An
item is added here when it is agreed, not when it is first mentioned. Bugs go to
the issue tracker; this file is for work that changes what YTMDL does or how it
is built.
