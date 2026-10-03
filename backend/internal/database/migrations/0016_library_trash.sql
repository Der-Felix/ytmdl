-- A journal persists before moving media. No existing song is automatically removed.
CREATE TABLE library_trash (
 id text PRIMARY KEY, track_id text NOT NULL UNIQUE, title text NOT NULL,
 deleted_by text REFERENCES users(id) ON DELETE SET NULL,
 created_at timestamptz NOT NULL DEFAULT now(),
 expires_at timestamptz NOT NULL DEFAULT now()+interval '7 days',
 state text NOT NULL CHECK(state IN ('preparing','ready','restoring','purging')),
 payload jsonb NOT NULL CHECK(jsonb_typeof(payload)='object'),
 moves jsonb NOT NULL CHECK(jsonb_typeof(moves)='array')
);
CREATE INDEX library_trash_expiry ON library_trash(expires_at) WHERE state='ready';
