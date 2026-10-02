# Library artwork and genres

The browser serves archived `cover.jpg`, `cover.jpeg`, `cover.png` and
`cover.webp` through authenticated, same-origin library artwork endpoints.
Releases and playable tracks prefer these files and fall back to their provider
image address. Artist portraits remain preferred; an archived release cover is
used when the portrait is absent or fails. This fallback is an album cover,
not a newly fetched artist portrait. No provider requests or library writes are
triggered by the artwork endpoint. It checks at most 64 distinct catalog-owned
release directories per artist, accepts raster files up to 8 MiB and confines
filesystem access to the music root, including symlinks.

Favorites display up to four distinct release covers. Empty collections retain
the heart placeholder. Escape closes player popovers first, then returns from
the large player to the library without interrupting playback. Native browser
fullscreen consumes its own Escape; a second Escape leaves the player page.
Open dialogs handle Escape themselves.

Administrators can open a library artist and choose **Genres bearbeiten**.
Enter up to 20 comma-separated labels, each up to 80 characters; saving an empty
field removes the assignment. These are shared library metadata, not audio-file
tag edits. They apply to that artist's releases and tracks. The Genre selector
on the library filters artists, releases and tracks before pagination, preserves
the selection between tabs and includes **Ohne Genre** for unassigned entries.
Provider-supplied genres seed unassigned artists but never replace a manual
assignment, including an explicitly cleared assignment. Artist reconciliation
preserves the union of assigned labels.

Migration 0013 adds `genres_json` and `genres_manual` to artists; existing
identities, files, playlists and favorites remain intact. Make a verified backup
before deployment. Application rollback to schema-12 code can leave these
additive columns in place; it does not remove assignments or restore the database.
The public v1.0.0 release remains on its original schema 12. Development release
manifests must advertise schema 13 when built with this migration.
