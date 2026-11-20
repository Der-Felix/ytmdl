# Artist Subscriptions

Artist subscriptions automate library growth by periodically polling metadata providers for newly released singles, EPs, and studio albums.

![YTMDL Artist Subscriptions](/screenshots/subscriptions.webp)

## How Subscriptions Work

1. **Periodic Synchronization:** The backend scheduler checks active artist subscriptions against provider APIs.
2. **Release Selection:** The releases the provider lists for the artist are narrowed down by the subscription's release filter and, when appearances are excluded (server-wide or for this subscription), to the artist's own releases.
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
joint releases. One server-wide setting decides what every subscription does
with them, and a single subscription can make an exception:

- **Server-wide:** **Settings → Downloads → Beteiligungen laden**
  (`subscription_include_appearances` in `GET`/`PUT /api/v1/settings`), on by
  default. It is read when a sync starts, so changing it applies to every
  subscription that follows it from its next sync on, without editing any of
  them.
- **Per subscription:** the subscription's settings dialog offers "Wie in den
  Einstellungen", "Immer laden" and "Nie laden" (`include_appearances` is
  `null`, `true` or `false`). Every subscription starts with `null` and follows
  the setting. `effective_include_appearances` on each subscription reports the
  value that applies right now.

What the effective value means:

- **On:** the sync considers every release the provider lists for the
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

Appearances only affect subscription syncs. Downloading an artist's
discography by hand still considers every release.

After the update every existing subscription follows the setting, which is
on, so nothing changes until you turn it off. A `PATCH` with
`"include_appearances": null` puts a subscription back to following the
setting; a `PATCH` that leaves the field out keeps the current choice.

The export writes `include_appearances` only for a subscription with its own
choice. On import, a missing field or `null` means "follow the setting" of the
importing server; this is also how a file written by an earlier version reads.
The server-wide setting itself is not part of the export.
