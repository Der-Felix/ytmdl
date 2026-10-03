# Library tools

The browser library supports manual and intelligent playlists, selection actions,
custom artwork, possible duplicate recordings, listening history and per-track
loudness adjustment. These tools use the local catalog and authenticated API.

## Intelligent playlists

Enable **Intelligente Playlist** when creating or editing a playlist. Rules can
combine one artist, an exact artist genre, your favorites, and an added-within
period. All chosen conditions must match. Choose title, newest, your most played
or your last played order, and a limit of 1–500 tracks. Membership is evaluated
when opening or playing the list; the currently playing queue remains a snapshot.
Smart lists do not accept manual additions, removals or ordering. Converting a
manual list to a smart list keeps its old membership, so converting back restores
that list. Favorites and listening statistics are scoped to the list owner.

## Selection and metadata

Checkboxes select the displayed tracks (up to 100 per library page). Selected
tracks can be queued together or added atomically to a manual playlist. Existing
members are skipped while new members retain selection order. Administrators can
edit album, album artist, artist names and year together. Only checked fields
change; an empty checked field clears that value. Changes are catalog overrides
and survive provider metadata refresh. Original tags, recording identities and
media paths are unchanged. Genre remains assigned per artist.

## Artwork

Administrators can upload or remove a custom artist portrait or release cover
from the detail page or **Bibliothek → Werkzeuge → Cover verwalten**. JPEG/PNG
uploads up to 8 MiB and 4096 pixels per edge are decoded, resized to at most 1600
pixels and re-encoded as JPEG. Embedded metadata and active formats are not kept.
Images are stored in the database and included in database backups. Release
artwork is shared by its tracks. Removing an upload restores the usual provider
portrait/local cover fallback. The manager can show missing local artwork on the
current page; it does not initiate provider refreshes.

## Duplicate candidates and history

**Mögliche Duplikate** groups downloaded tracks by normalized title and artist
credit. This is a review aid, not an audio fingerprint: different album, live or
remix versions can be intentional. Each page has up to 20 groups, and the first
100 tracks of each group are shown for comparison. Review never deletes before a user makes a choice.

The **Wischvergleich** compares two versions at a time. Swipe left to keep the
incumbent or right to prefer the challenger; buttons and focused-card arrow keys
provide the same choices. Compare duration, album, year, codec and bitrate.
Hörproben use a shared start point and pause the main player without replacing its
queue. Undo is available within an unfinished comparison, or skip a group for
later. **Alle Versionen behalten** marks intentionally different versions as
reviewed. Decisions belong to the signed-in user and survive reloads. Changed
group membership, comparison metadata or file records make the decision stale.
Cursor pagination prevents reviewing a page from skipping later groups.

Administrators confirm deletion in a separate dialog by default. The dialog
lists every removable version; the winner cannot be selected. The switch
**Vor dem Löschen nachfragen** can explicitly disable repeated confirmation for
this account and browser. In direct mode the final comparison immediately moves
all losing versions to the seven-day trash without a dialog; the screen explains this before selection.
The setting can be changed between decisions. During a card transition or a
save/removal, confirmation and keep/skip/view controls are locked until the action
finishes. Other users and browsers default
to confirmation. Skipping and **Alle Versionen behalten** never delete. Removal hides catalog tracks, audio and unshared lyric
sidecars from the active library. A journal keeps media and a metadata snapshot
for seven-day restoration, including favorites and playlist memberships for all
remaining users. Expiration or explicitly confirmed purge deletes the archive. It does not transfer memberships or edit subscriptions; a later
subscription download may restore a removed recording. The server requires a
matching saved preference and current group snapshot, locks every affected track,
checks storage and all paths before mutation, and blocks unfinished track/release
download jobs. Shared files and lyrics referenced by retained tracks are protected.
Filesystem and database operations cannot be atomic together: a failure stops the
batch and reports completed track IDs plus the failed item. Reload the group
before retrying; interrupted journal entries can be recovered from the trash panel.
Groups exceeding 100 versions remain available in the table without bulk deletion.

Listening history counts actual playing wall time, excluding pause, buffering,
seeking and long background callback gaps. A play counts after 30 seconds, or
half the duration of short tracks adjusted for playback speed. Repeated network
submissions use the same event identifier. The server stores per-user counts and
last played times; up to 100 recent/frequent tracks are displayed. Existing
browser-only history is retained, but is not imported as verified plays. Users
can clear their own server history explicitly.

## Loudness adjustment

Enable **Lautstärke angleichen** under Player → Audio. The current and next track
are measured sequentially with FFmpeg's [loudnorm filter](https://ffmpeg.org/ffmpeg-filters.html#loudnorm).
The browser applies a static gain toward −16 LUFS, limited by a −1.5 dB true-peak
ceiling and a −24…+6 dB gain range. Separate gain nodes for both decks preserve
crossfades. EQ and preamp may still change resulting loudness; the safety limiter
remains independent. Unmeasured tracks play at unity until their measurement is
available; silence keeps unity gain. Disabling adjustment restores unity.

Analysis is read-only, bounded to regular catalog files of at most 512 MiB and a
known duration of at most one hour, with a two-minute process timeout. Only one
analysis runs at a time; errors are shown in Audio settings. Files are opened
through a confined root and passed as an inherited descriptor, never a caller
URL. Only supported audio/container demuxers and local file/pipe protocols are
allowed. Measurements are cached by file identity, byte size and modification
time. First-time analysis has a CPU cost; it is disabled by default and no whole
library scan is automatically scheduled. The opt-in persists in this browser.

## Database upgrade

Migration 0014 adds rules, metadata overrides, artwork, listening data and loudness
measurements. Migration 0015 adds per-user duplicate review decisions. Both are
additive: installing the feature preserves existing identities, audio and
playlists and never automatically deletes candidates. Upgrades from schema 8–14
to 15 require a verified backup for schema rollback. Schema 15 is neutral only
when already on 15. The old application can still run with the additive tables
present, but will not expose these new features. Backup restoration is required
for a full schema rollback; restoring audio deleted by an explicit user action
also requires a separate media backup.

## Papierkorb

Administratoren finden den Papierkorb unter **Bibliothek → Deine Bibliothek →
Papierkorb**. Im Duplikatvergleich entfernte Versionen werden sieben Tage
aufbewahrt; die Bestätigung vor dem Verschieben kann weiterhin pro Konto und
Browser ausgeschaltet werden. Die Originaldateien und Lyrics bleiben auf dem
Bibliotheksspeicher. Danach werden abgelaufene Einträge automatisch endgültig
entfernt. Ein ausdrücklich bestätigtes endgültiges Löschen ist vorher möglich.

Wiederherstellen erhält die ursprünglichen Titel- und Datei-IDs, eigene
Metadaten, Favoriten aller noch vorhandenen Konten und Zuordnungen zu noch
vorhandenen Playlists. Nachbarpositionen erlauben das Zurückholen mehrerer Titel
in beliebiger Reihenfolge, ohne inzwischen hinzugefügte Titel zu entfernen.
Vorhandene Dateien werden niemals überschrieben. Inzwischen gelöschte
Playlists/Konten werden nicht neu angelegt; Hörverlauf und abgeleitete Messungen
werden nicht wiederhergestellt. Erneut heruntergeladene Aufnahmen können eine
Wiederherstellung durch Identitäts- oder Dateipfadkonflikte blockieren.

Ein persistentes Journal schützt unterbrochene Datei-/Datenbankaktionen.
**Unterbrochene Aktionen wiederherstellen** prüft offene Vorgänge; automatische
Wartung versucht dies ebenfalls. Nicht sicher auflösbare Konflikte bleiben
sichtbar. Der Papierkorb verwendet atomare, nicht überschreibende Hardlinks auf
demselben Dateisystem. Speicher ohne Hardlink-Unterstützung lehnt Verschieben
sicher ab. `.ytmdl-trash` muss auch aus externen Media-Server-Scans ausgeschlossen
werden; YTMDL überspringt dieses reservierte Verzeichnis bereits.

Bereits endgültig gelöschte Dateien aus älteren Versionen werden nicht
wiederhergestellt. Der Papierkorb ersetzt kein separates Backup der Medien.

## Audioerkennung für Duplikate

Administratoren finden unter **Bibliothek → Deine Bibliothek → Audioerkennung**
einen ausdrücklich gestarteten, abbrechbaren Lauf für 10, 50 oder 100 Titel.
Chromaprint untersucht maximal 90 Sekunden pro Datei, lokal und ohne externe
Fingerabdruckdienste. Das offizielle Backend-Image enthält `fpcalc`.
Bereits gemessene Dateigenerationen werden gespeichert; ein neuer Durchlauf
setzt mit offenen Titeln fort. Das Verlassen des Bereichs beendet den Lauf.

Ähnliche Aufnahmen mit unterschiedlichen Namen erscheinen im normalen
Duplikatvergleich als **Ähnliche Audioaufnahme · bitte anhören**. Gleiche
Namen bleiben im bisherigen Metadatenvergleich. Treffer sind Hinweise:
Kurze, gleichförmige oder nicht lesbare Dateien können unklar bleiben, und
Versionen mit identischem Anfang können sich später unterscheiden. Keine
Analyse löscht Musik oder beeinflusst die Download-Eignung.

Ein Dateiwechsel macht die alte Messung und Entscheidung ungültig. **Analyseindex
zurücksetzen** entfernt ausschließlich abgeleitete Messungen und Audiotreffer;
Musik, Favoriten und Playlists bleiben erhalten. Der nächste Lauf prüft erneut.

## Lokales Song-Radio

Unter **Bibliothek → Deine Bibliothek → Song-Radio** entsteht ein Mix mit bis
zu 50 verfügbaren lokalen Titeln. Favoriten und Hörverlauf beeinflussen nur
für dein Konto die Auswahl; ähnliche Künstler und Genres des aktuellen Songs
können als Ausgangspunkt dienen. Ein Genre lässt sich gezielt auswählen.
Kürzlich gehörte Titel werden nach hinten gestellt, verschiedene Künstler
bevorzugt und gleich benannte Versionen eines Künstlers nicht mehrfach gewählt.

**Neuen Mix zusammenstellen** zeigt die Auswahl zuerst. **Titel abspielen**
ersetzt die Warteschlange; **Zur Warteschlange** hängt den Mix an und lässt die
aktuelle Wiedergabe weiterlaufen. Es werden keine Provider abgefragt und keine
neuen Songs heruntergeladen. Für Genres braucht die Bibliothek gepflegte
Genre-Zuordnungen; ohne Hörverlauf oder Favoriten entsteht ein abwechslungsreicher
lokaler Startmix.

## Geräteübergabe

**Bibliothek → Deine Bibliothek → Geräteübergabe** speichert bis zu 500 Titel
in ihrer aktuellen Reihenfolge, die Position und den Wiederholmodus für 15
Minuten. **Hier pausieren und übertragen** pausiert das Ausgangsgerät erst,
wenn der Server die Übergabe angenommen hat. Auf dem Zielgerät mit demselben
Konto **Aktualisieren**, dann **Übernehmen und abspielen** wählen. Der Player
setzt Warteschlange und Position gemeinsam; Lautstärke und Klang bleiben lokal.

Es gibt keine Hintergrundüberwachung der Geräte und keine automatische
Fernsteuerung. Beide Geräte brauchen eine Serververbindung. Ein neuer Eintrag
ersetzt die vorige Übergabe; fehlende Titel und veraltete Entscheidungen werden
abgewiesen. Eine Übergabe kann während ihrer Gültigkeit erneut übernommen oder
bewusst verworfen werden. Sleep-Timer und „Stopp nach …“ werden beim Übernehmen
ausgeschaltet, damit eine alte lokale Einstellung die neue Sitzung nicht beendet.
