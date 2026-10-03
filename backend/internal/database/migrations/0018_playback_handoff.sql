-- Explicit, short-lived handoffs. No device telemetry or background tracking.
CREATE TABLE playback_handoffs (
 user_id text PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
 id text NOT NULL, queue_ids jsonb NOT NULL CHECK(jsonb_typeof(queue_ids)='array' AND jsonb_array_length(queue_ids) BETWEEN 1 AND 500),
 queue_index integer NOT NULL CHECK(queue_index BETWEEN 0 AND 499),
 position_seconds double precision NOT NULL CHECK(position_seconds>=0 AND position_seconds<=86400),
 repeat_mode text NOT NULL CHECK(repeat_mode IN('off','queue','track')),
 source_name text NOT NULL, created_at timestamptz NOT NULL DEFAULT now(),
 expires_at timestamptz NOT NULL DEFAULT now()+interval '15 minutes'
);
