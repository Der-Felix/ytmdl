# Artist Subscriptions

Artist subscriptions automate library growth by periodically polling metadata providers for newly released singles, EPs, and studio albums.

![YTMDL Artist Subscriptions](/screenshots/subscriptions.webp)

## How Subscriptions Work

1. **Periodic Synchronization:** The backend scheduler checks active artist subscriptions against provider APIs.
2. **Release Selection:** The releases the provider lists for the artist are narrowed down by the subscription's release filter and, when appearances are excluded, to the artist's own releases.
3. **Release Detection:** New release IDs are compared against existing library albums and pending download jobs.
4. **Automatic Queueing:** If `auto_download` is enabled, missing releases are queued automatically with low-priority background scheduling.
5. **Import & Export:** Subscriptions can be imported or exported in bulk via standard JSON payload structures.

## Release Filter

The release filter (`release_filter`) selects the release types a subscription
considers: albums, singles, EPs, live releases, compilations and remixes. The
default is albums, singles and EPs.

A release's type comes from the provider and from its title:

- A release the provider declares a compilation stays a compilation.
- Otherwise a live marker in the title makes it a live release, and a remix
  marker makes it a remix release, whatever the provider calls it. "I'm Good
  (Blue) (Oliver Heldens Remix)" is a remix even though Deezer files it as a
  single, and "Say My Name (Remixes)" is a remix even though Deezer files it as
  an EP. Remix markers are the whole words "Remix", "Remixes" and "Remixed";
  other version markers such as "(Extended)" or "(Acoustic)" do not change the
  type.
- Live markers are "(Live)", "[Live ...]", "Live at/in/from/on ...",
  "Unplugged", "In Concert", "- Live", "Live" followed by a year, or a release
  called just "Live". A title such as "Long Live" or "Live Your Life" is not
  a live release.
- Everything else keeps the provider's album, single or EP type.

A filter without remixes therefore leaves out remix singles and remix EPs as
well.

Releases downloaded before this rule existed keep the type, folder and files
they were stored with. A sync never rewrites a stored release and never
downloads one twice: it recognises a stored release by provider and release
id and its tracks by ISRC or track identity, and a remix type does not change
a track's identity. If the filter excludes remixes, such a release is simply
no longer part of the sync; it stays in the library until you remove it. Its
stored type changes only when the release itself is downloaded again (a manual
download, or a sync whose filter includes remixes and finds a track without a
file); the library audit then reports the existing files as a path mismatch
with the new "[Remix]" folder and offers to move them.

## Appearances

A provider lists more than an artist's own releases under the artist: singles
of other artists the artist is featured on, another artist's remix packages,
joint releases. The switch `include_appearances` decides what a subscription
does with them:

- **On (default):** the sync considers every release the provider lists for the
  artist, as before.
- **Off:** the sync keeps only the releases whose primary artist is the
  subscribed artist. A joint release that names the subscribed artist as one
  of its primary artists counts as the artist's own. A release whose primary
  artist cannot be determined is kept, never dropped.

The primary artist is compared by the provider's artist id where the provider
supplies one, and otherwise by the normalised artist name:

| Provider | How the primary artist is determined |
|---|---|
| Deezer | The album's own artist (`album.artist`). Deezer's artist listing names no artist, so the album itself is looked up once per release and sync. Deezer marks a featured guest as a main contributor too, so the contributor list is not used, except when the album artist is a joined name such as "Artist A & Artist B". |
| Spotify | The album artists of the listing, by id. Spotify's artist listing already leaves out releases the artist only appears on. |
| YouTube Music | The artist names of the listing. The artist page lists the artist's own releases, so in practice nothing is left out. |

The switch only affects subscription syncs. Downloading an artist's
discography by hand still considers every release.

Existing subscriptions keep `include_appearances` on after the update. An
export file written by an earlier version has no `include_appearances` field;
importing it turns the switch on.
