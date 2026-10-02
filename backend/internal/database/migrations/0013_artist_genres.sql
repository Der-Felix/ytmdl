-- Additive metadata: existing audio, identities and playlists stay unchanged.
ALTER TABLE artists ADD COLUMN genres_json jsonb NOT NULL DEFAULT '[]'::jsonb
    CHECK (jsonb_typeof(genres_json) = 'array' AND NOT jsonb_path_exists(genres_json, '$[*] ? (@.type() != "string")'));
ALTER TABLE artists ADD COLUMN genres_manual boolean NOT NULL DEFAULT false;
