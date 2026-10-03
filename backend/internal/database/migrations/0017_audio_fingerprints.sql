-- Derived measurements only. Media and original catalog metadata are unchanged.
CREATE TABLE audio_fingerprints (
 track_id text PRIMARY KEY REFERENCES tracks(id) ON DELETE CASCADE,
 file_id text NOT NULL REFERENCES files(id) ON DELETE CASCADE,
 file_updated_at timestamptz NOT NULL, generation text NOT NULL,
 size_bytes bigint NOT NULL, mtime_ns bigint NOT NULL,
 state text NOT NULL CHECK(state IN ('ready','inconclusive','failed')),
 words jsonb NOT NULL DEFAULT '[]' CHECK(jsonb_typeof(words)='array' AND jsonb_array_length(words)<=1200),
 buckets integer[] NOT NULL DEFAULT '{}', analyzed_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX audio_fingerprints_buckets ON audio_fingerprints USING gin(buckets);
CREATE TABLE audio_duplicate_pairs (
 first_id text NOT NULL REFERENCES tracks(id) ON DELETE CASCADE,
 second_id text NOT NULL REFERENCES tracks(id) ON DELETE CASCADE,
 first_generation text NOT NULL, second_generation text NOT NULL,
 similarity double precision NOT NULL CHECK(similarity BETWEEN 0 AND 1),
 PRIMARY KEY(first_id,second_id), CHECK(first_id<second_id)
);
