-- A subscription can leave out the releases its artist only appears on: a
-- single filed under another artist, another artist's remix package. When the
-- switch is off, the sync keeps only the releases whose primary artist is the
-- subscribed one; a release whose primary artist cannot be told is kept.
--
-- Existing subscriptions keep their behaviour, which considered every release
-- the provider lists for the artist: the default is true. A constant default
-- makes this a catalogue-only change in PostgreSQL 11 and later; no row is
-- rewritten.
ALTER TABLE artist_subscriptions ADD COLUMN include_appearances boolean NOT NULL DEFAULT true;
