-- Skip signals for personal mixes and the local radio. A skip is the user
-- moving on from a track early, before it counted as a play. Mirrors the
-- playback pattern of 0014: idempotent events keyed by a client event id and
-- a per user/track aggregate. Both are new tables, so the trash snapshots of
-- listening_history and playback_events keep their shape.
CREATE TABLE skip_events (
 user_id text NOT NULL REFERENCES users(id) ON DELETE CASCADE,
 event_id text NOT NULL, track_id text NOT NULL REFERENCES tracks(id) ON DELETE CASCADE,
 skipped_at timestamptz NOT NULL DEFAULT now(), PRIMARY KEY(user_id,event_id)
);
CREATE INDEX skip_events_track ON skip_events(track_id);
CREATE TABLE track_skips (
 user_id text NOT NULL REFERENCES users(id) ON DELETE CASCADE,
 track_id text NOT NULL REFERENCES tracks(id) ON DELETE CASCADE,
 skip_count bigint NOT NULL DEFAULT 1 CHECK(skip_count > 0), last_skipped_at timestamptz NOT NULL DEFAULT now(),
 PRIMARY KEY(user_id,track_id)
);
CREATE INDEX track_skips_track ON track_skips(track_id);
-- "Neu in der Mediathek" looks for files added recently.
CREATE INDEX idx_files_created_at ON files(created_at DESC);
-- One row per artist whose genres were looked up at a metadata provider by
-- the background genre backfill, so it asks once per artist and not again
-- after every restart. found=false rows are asked again after a while;
-- failed rows (the provider answered with an error) sooner.
CREATE TABLE artist_genre_lookups (
 artist_id text PRIMARY KEY REFERENCES artists(id) ON DELETE CASCADE,
 checked_at timestamptz NOT NULL DEFAULT now(),
 found boolean NOT NULL DEFAULT false,
 failed boolean NOT NULL DEFAULT false
);
