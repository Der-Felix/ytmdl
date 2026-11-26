-- Steering for personal mixes and the local radio: what a user does not want
-- to hear chosen for them. An exclusion names an artist, a genre or a track
-- ("weniger davon"); excluded items never enter a mix or a radio station,
-- while playing them explicitly (an album, a playlist, a search hit) stays
-- possible. A track exclusion covers every version of that recording (same
-- artist, same normalized title).
--
-- A new table only: no existing row is rewritten, so the migration is
-- instant on a large library. The genre is kept as the user named it and
-- compared case-insensitively, like the genre filters of the library.
CREATE TABLE listening_exclusions (
 id text PRIMARY KEY,
 user_id text NOT NULL REFERENCES users(id) ON DELETE CASCADE,
 kind text NOT NULL CHECK(kind IN ('artist','genre','track')),
 artist_id text REFERENCES artists(id) ON DELETE CASCADE,
 track_id text REFERENCES tracks(id) ON DELETE CASCADE,
 genre text,
 created_at timestamptz NOT NULL DEFAULT now(),
 CONSTRAINT listening_exclusions_target CHECK(
  (kind='artist' AND artist_id IS NOT NULL AND track_id IS NULL AND genre IS NULL) OR
  (kind='track' AND track_id IS NOT NULL AND artist_id IS NULL AND genre IS NULL) OR
  (kind='genre' AND btrim(COALESCE(genre,''))<>'' AND artist_id IS NULL AND track_id IS NULL))
);
CREATE UNIQUE INDEX listening_exclusions_unique ON listening_exclusions(user_id,kind,(COALESCE(artist_id,track_id,lower(btrim(genre) COLLATE "pg_c_utf8"))));
CREATE INDEX listening_exclusions_artist ON listening_exclusions(artist_id) WHERE artist_id IS NOT NULL;
CREATE INDEX listening_exclusions_track ON listening_exclusions(track_id) WHERE track_id IS NOT NULL;
