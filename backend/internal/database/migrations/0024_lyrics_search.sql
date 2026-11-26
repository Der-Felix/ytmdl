-- Search index for lyrics text.
--
-- The lyrics themselves stay in the .lrc/.txt sidecar next to the audio file
-- (see 0004): that file is what Plex, Jellyfin and Emby read and remains the
-- source of truth. This table is a derived, rebuildable index of it, filled
-- by a background indexer from the sidecars of tracks whose lyrics_state
-- says lyrics are available, and refreshed when lyrics_checked_at changes.
-- Dropping its rows loses nothing but search hits until the indexer has run
-- again.
--
-- body is the plain text (timestamps of synchronised lyrics removed) for the
-- snippet around a hit. document is built from that text lowercased and with
-- diacritics removed in the application (no unaccent extension needed), with
-- the 'simple' configuration so German and English words are matched as
-- written, without stemming or stop words.
--
-- A new, empty table: the GIN index costs nothing at migration time.
CREATE TABLE track_lyrics_search (
 track_id text PRIMARY KEY REFERENCES tracks(id) ON DELETE CASCADE,
 body text NOT NULL,
 document tsvector NOT NULL,
 -- tracks.lyrics_checked_at the row was built from; a different value means
 -- the sidecar was rewritten and the row is rebuilt.
 source_checked_at timestamptz,
 indexed_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX idx_track_lyrics_search_document ON track_lyrics_search USING gin(document);
