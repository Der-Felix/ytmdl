-- A recording is stored once in tracks, with one home release (tracks.release_id)
-- and the files of that release in files. The same recording often appears on
-- further releases: a single and its album, an album and a compilation, a
-- deluxe reissue. Media servers read folders and tags, so each of those
-- releases needs a file of its own in its own folder, tagged with that
-- release's album, disc and track number.
--
-- track_placements holds only these additional appearances: one row per
-- further release a recording is placed on, pointing at a copy of an existing
-- library file. The copies are kept out of files on purpose: files rows of one
-- track are versions of the recording (playback, duplicate detection,
-- fingerprints, the canonical-path audit), and a placement is none of that.
--
-- The home release and its files stay exactly where they are. The table
-- starts empty; no existing row is rewritten.
CREATE TABLE track_placements (
    id             text        PRIMARY KEY,
    track_id       text        NOT NULL REFERENCES tracks (id) ON DELETE CASCADE,
    release_id     text        NOT NULL REFERENCES releases (id) ON DELETE CASCADE,
    -- Relative to the library root, like files.path.
    path           text        NOT NULL UNIQUE,
    size_bytes     bigint      NOT NULL DEFAULT 0,
    sha256         text        NOT NULL DEFAULT '',
    -- Zero rather than NULL for an unknown position, as in tracks, so the
    -- unique key below holds for every row.
    disc_number    integer     NOT NULL DEFAULT 0,
    track_number   integer     NOT NULL DEFAULT 0,
    -- The title as this release lists it.
    title          text        NOT NULL DEFAULT '',
    -- The files row the copy was made from. It is provenance only: the copy
    -- stays valid when that file is replaced or removed.
    source_file_id text,
    created_at     timestamptz NOT NULL,
    updated_at     timestamptz NOT NULL,
    UNIQUE (track_id, release_id, disc_number, track_number)
);

CREATE INDEX idx_track_placements_release ON track_placements (release_id);
CREATE INDEX idx_track_placements_track ON track_placements (track_id);
