-- Migration 0009: Artist Sources (True Canonical Artist Identity)
-- Introduces artist_sources to support multi-provider artist identity.
-- One canonical artist entity in artists can be linked to multiple external
-- provider identities (Deezer, Spotify, YouTube Music, etc.) and legacy
-- synthetic keys without losing real external provider IDs.

CREATE TABLE IF NOT EXISTS artist_sources (
    id          text        PRIMARY KEY,
    artist_id   text        NOT NULL REFERENCES artists (id) ON DELETE CASCADE,
    provider    text        NOT NULL,
    source_kind text        NOT NULL DEFAULT 'external',
    source_id   text        NOT NULL,
    source_url  text        NOT NULL DEFAULT '',
    is_primary  boolean     NOT NULL DEFAULT false,
    created_at  timestamptz NOT NULL,
    updated_at  timestamptz NOT NULL,
    UNIQUE (provider, source_id),
    CONSTRAINT artist_sources_kind_known CHECK (source_kind IN ('external', 'legacy_synthetic'))
);

CREATE INDEX IF NOT EXISTS idx_artist_sources_artist ON artist_sources (artist_id);
CREATE INDEX IF NOT EXISTS idx_artist_sources_lookup ON artist_sources (provider, source_id);

-- Lossless backfill from artists to artist_sources:
-- Every existing artist's provider and source_id is preserved.
-- Real external provider IDs are categorized as 'external',
-- while synthetic 'artist:*' IDs are categorized as 'legacy_synthetic'.
INSERT INTO artist_sources (id, artist_id, provider, source_kind, source_id, source_url, is_primary, created_at, updated_at)
SELECT
    md5(random()::text || clock_timestamp()::text || id)::text,
    id,
    provider,
    CASE WHEN source_id LIKE 'artist:%' THEN 'legacy_synthetic' ELSE 'external' END,
    source_id,
    source_url,
    true,
    created_at,
    updated_at
FROM artists
WHERE provider <> '' AND source_id <> ''
ON CONFLICT (provider, source_id) DO NOTHING;

-- Drop unique constraint on artists(provider, source_id) so that
-- multiple sources or canonical merges do not trigger table-level conflicts.
-- artist_sources now strictly enforces global uniqueness per (provider, source_id).
ALTER TABLE artists DROP CONSTRAINT IF EXISTS artists_provider_source_id_key;
