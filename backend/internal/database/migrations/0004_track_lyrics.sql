-- Lyrics state and compilation flag for the media server compatibility release.
--
-- Only the lyrics *state* lives in the catalogue. The lyrics text itself stays
-- in the .lrc/.txt sidecar next to the audio file, because that file is what
-- Plex, Jellyfin and Emby read; a second copy in the database could only ever
-- disagree with it.
--
-- lyrics_checked_at records a *definitive* answer from the providers. A
-- transient failure — a timeout, a 429, an unparsable response — deliberately
-- leaves both columns untouched, so a temporary outage can never be mistaken
-- for "this track has no lyrics" and can never start the cooldown that keeps a
-- bulk run from asking again.
--
-- The migration is purely additive: no existing row is rewritten and no file
-- on disk is touched.

ALTER TABLE tracks
    ADD COLUMN lyrics_state      text        NOT NULL DEFAULT 'unknown',
    ADD COLUMN lyrics_provider   text        NOT NULL DEFAULT '',
    ADD COLUMN lyrics_checked_at timestamptz,
    ADD COLUMN compilation       boolean     NOT NULL DEFAULT false;

ALTER TABLE tracks
    ADD CONSTRAINT tracks_lyrics_state_known CHECK (
        lyrics_state IN ('unknown', 'available_synced', 'available_plain', 'instrumental', 'not_found')
    );

ALTER TABLE releases
    ADD COLUMN compilation boolean NOT NULL DEFAULT false;

-- The backfill asks for the tracks that were never checked or whose check has
-- aged out, and never for tracks that already carry synchronised lyrics.
CREATE INDEX idx_tracks_lyrics_backfill
    ON tracks (lyrics_checked_at NULLS FIRST, created_at)
    WHERE lyrics_state <> 'available_synced';
