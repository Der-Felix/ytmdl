-- Additive library tools. Original media, identities and memberships remain intact.
ALTER TABLE playlists ADD COLUMN smart_rules jsonb;
ALTER TABLE playlists ADD CONSTRAINT playlists_smart_rules_object CHECK (smart_rules IS NULL OR jsonb_typeof(smart_rules) = 'object');
CREATE TABLE track_overrides (
 track_id text PRIMARY KEY REFERENCES tracks(id) ON DELETE CASCADE,
 album text, album_artist text, artists_json jsonb, year integer CHECK(year BETWEEN 0 AND 9999)
);
CREATE TABLE library_artwork (
 kind text NOT NULL CHECK(kind IN ('artists','releases')), entity_id text NOT NULL,
 image bytea NOT NULL CHECK(octet_length(image) <= 8388608), updated_at timestamptz NOT NULL DEFAULT now(),
 PRIMARY KEY(kind,entity_id)
);
CREATE TABLE playback_events (
 user_id text NOT NULL REFERENCES users(id) ON DELETE CASCADE,
 event_id text NOT NULL, track_id text NOT NULL REFERENCES tracks(id) ON DELETE CASCADE,
 played_at timestamptz NOT NULL DEFAULT now(), PRIMARY KEY(user_id,event_id)
);
CREATE INDEX playback_events_user_time ON playback_events(user_id,played_at DESC);
CREATE TABLE listening_history (
 user_id text NOT NULL REFERENCES users(id) ON DELETE CASCADE,
 track_id text NOT NULL REFERENCES tracks(id) ON DELETE CASCADE,
 play_count bigint NOT NULL DEFAULT 1 CHECK(play_count > 0), last_played_at timestamptz NOT NULL DEFAULT now(),
 PRIMARY KEY(user_id,track_id)
);
CREATE INDEX listening_history_recent ON listening_history(user_id,last_played_at DESC);
CREATE INDEX listening_history_frequent ON listening_history(user_id,play_count DESC);
CREATE TABLE track_loudness (
 track_id text PRIMARY KEY REFERENCES tracks(id) ON DELETE CASCADE,
 file_id text NOT NULL REFERENCES files(id) ON DELETE CASCADE,
 size_bytes bigint NOT NULL, mtime_ns bigint NOT NULL,
 gain_db double precision NOT NULL CHECK(gain_db BETWEEN -24 AND 6),
 integrated_lufs double precision NOT NULL, true_peak_db double precision NOT NULL,
 analyzed_at timestamptz NOT NULL DEFAULT now()
);
-- Polymorphic artwork ownership is cleaned up alongside the catalog entity.
CREATE FUNCTION remove_library_artwork() RETURNS trigger AS $$
BEGIN
 DELETE FROM library_artwork WHERE kind=TG_TABLE_NAME AND entity_id=OLD.id;
 RETURN OLD;
END;
$$ LANGUAGE plpgsql;
CREATE TRIGGER remove_artist_artwork AFTER DELETE ON artists FOR EACH ROW EXECUTE FUNCTION remove_library_artwork();
CREATE TRIGGER remove_release_artwork AFTER DELETE ON releases FOR EACH ROW EXECUTE FUNCTION remove_library_artwork();
