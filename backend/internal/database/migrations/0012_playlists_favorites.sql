-- Migration 0012: Persistent user playlists and track favorites.
-- Provides user-owned playlists with persistent ordering and per-user favorite tracks.

CREATE TABLE IF NOT EXISTS playlists (
    id          text        PRIMARY KEY,
    user_id     text        NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    name        text        NOT NULL,
    description text        NOT NULL DEFAULT '',
    created_at  timestamptz NOT NULL,
    updated_at  timestamptz NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_playlists_user_id ON playlists (user_id);
CREATE INDEX IF NOT EXISTS idx_playlists_user_updated ON playlists (user_id, updated_at DESC);

CREATE TABLE IF NOT EXISTS playlist_tracks (
    playlist_id text        NOT NULL REFERENCES playlists (id) ON DELETE CASCADE,
    track_id    text        NOT NULL REFERENCES tracks (id) ON DELETE CASCADE,
    position    integer     NOT NULL,
    added_at    timestamptz NOT NULL,
    PRIMARY KEY (playlist_id, track_id),
    CONSTRAINT uq_playlist_tracks_position UNIQUE (playlist_id, position) DEFERRABLE INITIALLY DEFERRED,
    CONSTRAINT chk_playlist_tracks_position CHECK (position >= 1)
);

CREATE INDEX IF NOT EXISTS idx_playlist_tracks_order ON playlist_tracks (playlist_id, position);
CREATE INDEX IF NOT EXISTS idx_playlist_tracks_track ON playlist_tracks (track_id);

CREATE TABLE IF NOT EXISTS favorite_tracks (
    user_id    text        NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    track_id   text        NOT NULL REFERENCES tracks (id) ON DELETE CASCADE,
    created_at timestamptz NOT NULL,
    PRIMARY KEY (user_id, track_id)
);

CREATE INDEX IF NOT EXISTS idx_favorite_tracks_user ON favorite_tracks (user_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_favorite_tracks_track ON favorite_tracks (track_id);

-- Automatically compact playlist_tracks positions into a contiguous 1..N
-- sequence upon track deletion (both direct DELETE and foreign key cascades).
CREATE OR REPLACE FUNCTION compact_playlist_tracks_after_delete()
RETURNS TRIGGER AS $$
BEGIN
    WITH affected_playlists AS (
        SELECT DISTINCT playlist_id FROM deleted_rows
    ),
    numbered AS (
        SELECT pt.playlist_id, pt.track_id,
               ROW_NUMBER() OVER (PARTITION BY pt.playlist_id ORDER BY pt.position ASC)::integer AS new_pos
        FROM playlist_tracks pt
        JOIN affected_playlists ap ON ap.playlist_id = pt.playlist_id
    )
    UPDATE playlist_tracks pt
    SET position = n.new_pos
    FROM numbered n
    WHERE pt.playlist_id = n.playlist_id
      AND pt.track_id = n.track_id
      AND pt.position != n.new_pos;
    RETURN NULL;
END;
$$ LANGUAGE plpgsql;

DROP TRIGGER IF EXISTS trg_compact_playlist_tracks_after_delete ON playlist_tracks;
CREATE TRIGGER trg_compact_playlist_tracks_after_delete
AFTER DELETE ON playlist_tracks
REFERENCING OLD TABLE AS deleted_rows
FOR EACH STATEMENT
EXECUTE FUNCTION compact_playlist_tracks_after_delete();
