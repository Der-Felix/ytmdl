-- Performance indexes for library sorting and filtering.
--
-- These indexes accelerate:
-- 1. "Recently added" library sorts (ORDER BY created_at DESC) on tracks and releases
-- 2. Release sorting by release year and title (ORDER BY year DESC, title)
-- 3. Filtering library tracks by lyrics state (WHERE lyrics_state = ...)
--
-- The migration is purely additive: no existing schema or data is altered.

CREATE INDEX IF NOT EXISTS idx_tracks_created_at ON tracks (created_at DESC);
CREATE INDEX IF NOT EXISTS idx_releases_created_at ON releases (created_at DESC);
CREATE INDEX IF NOT EXISTS idx_releases_year ON releases (year DESC, title);
CREATE INDEX IF NOT EXISTS idx_tracks_lyrics_state ON tracks (lyrics_state);
