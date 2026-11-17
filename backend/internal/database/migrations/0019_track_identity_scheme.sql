-- Every track records the identity scheme its identity_key was built under,
-- so that a change to how the key is built is carried over to stored rows by
-- the background identity migration (internal/identitymigration) instead of
-- by lookups that know every older key form.
--
-- Existing rows are scheme 1: whatever the versions up to 1.5.0 wrote. The
-- default stays 1 on purpose: a row inserted without the column (a trash
-- restore of an older payload, an older binary after a downgrade) is treated
-- as unknown and examined by the next run, never assumed current. The
-- backend writes the current scheme explicitly.
--
-- A constant default makes this a catalogue-only change in PostgreSQL 11 and
-- later; no row is rewritten.
ALTER TABLE tracks ADD COLUMN identity_scheme smallint NOT NULL DEFAULT 1;

CREATE INDEX idx_tracks_identity_scheme ON tracks (identity_scheme, id);
