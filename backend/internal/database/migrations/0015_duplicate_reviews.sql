-- Decisions are private to the listener; original files and memberships are unchanged.
CREATE TABLE duplicate_reviews (
 user_id text NOT NULL REFERENCES users(id) ON DELETE CASCADE,
 group_key text NOT NULL CHECK(group_key ~ '^[0-9a-f]{64}$'),
 fingerprint text NOT NULL CHECK(fingerprint ~ '^[0-9a-f]{64}$'),
 outcome text NOT NULL CHECK(outcome IN ('preferred','distinct')),
 preferred_track_id text REFERENCES tracks(id) ON DELETE CASCADE,
 updated_at timestamptz NOT NULL DEFAULT now(),
 PRIMARY KEY(user_id,group_key),
 CHECK((outcome='preferred' AND preferred_track_id IS NOT NULL) OR
       (outcome='distinct' AND preferred_track_id IS NULL))
);
