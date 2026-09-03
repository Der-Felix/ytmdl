-- Artist subscriptions: the artists whose catalogue is watched.
--
-- The provider identity is stored here rather than referenced from the artists
-- table. artists is the library's own catalogue and GET /library/artists reads
-- it directly, so a foreign key would force an artists row for every watched
-- artist and every subscribed-but-never-downloaded artist would show up in the
-- library with no music. The unique key below is the same identity tuple
-- artists uses, so the two can still be joined on demand.

CREATE TABLE artist_subscriptions (
    id               text        PRIMARY KEY,
    provider         text        NOT NULL,
    artist_source_id text        NOT NULL,
    artist_name      text        NOT NULL,
    artist_image_url text        NOT NULL DEFAULT '',

    enabled          boolean     NOT NULL DEFAULT true,
    auto_download    boolean     NOT NULL DEFAULT false,

    -- last_sync_at stays NULL until the first run finishes. next_sync_at is
    -- never NULL: a subscription always knows when it is due again, which is
    -- what makes the scheduler's query a plain index scan.
    last_sync_at     timestamptz,
    next_sync_at     timestamptz NOT NULL,
    last_sync_status text        NOT NULL DEFAULT 'pending',
    -- last_error only ever describes an actual sync failure and is cleared by
    -- the next run that finishes.
    last_error       text        NOT NULL DEFAULT '',

    created_at       timestamptz NOT NULL,
    updated_at       timestamptz NOT NULL,

    UNIQUE (provider, artist_source_id),
    CONSTRAINT artist_subscriptions_status_known CHECK (
        last_sync_status IN ('pending', 'success', 'partial', 'failed')
    )
);

-- The scheduler asks for "enabled and due" on every tick; the partial index
-- keeps that lookup off the disabled rows entirely.
CREATE INDEX idx_artist_subscriptions_due
    ON artist_subscriptions (next_sync_at)
    WHERE enabled;

CREATE INDEX idx_artist_subscriptions_name ON artist_subscriptions (artist_name);
