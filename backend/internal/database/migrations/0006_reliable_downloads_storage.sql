-- Migration 0006: Reliable downloads, retry scheduling, local persistent staging, and network storage states.
--
-- Adds support for:
-- 1. Fine-grained item states: finalizing, retry_wait, waiting_for_storage, waiting_for_space
-- 2. Extended job states: retry_wait, waiting_for_storage, waiting_for_space
-- 3. Persistent attempt tracking, retry backoff scheduling, staging relative paths, and staged SHA-256 validation.

ALTER TABLE jobs DROP CONSTRAINT IF EXISTS jobs_status_known;
ALTER TABLE jobs ADD CONSTRAINT jobs_status_known CHECK (
    status IN (
        'queued', 'resolving_artist', 'resolving_releases', 'resolving_tracks',
        'deduplicating', 'matching', 'downloading', 'tagging', 'finalizing',
        'retry_wait', 'waiting_for_storage', 'waiting_for_space',
        'completed', 'failed', 'cancelled'
    )
);

ALTER TABLE job_items DROP CONSTRAINT IF EXISTS job_items_status_known;
ALTER TABLE job_items ADD CONSTRAINT job_items_status_known CHECK (
    status IN (
        'pending', 'matching', 'downloading', 'tagging', 'finalizing',
        'retry_wait', 'waiting_for_storage', 'waiting_for_space',
        'completed', 'failed', 'skipped', 'cancelled'
    )
);

ALTER TABLE job_items
    ADD COLUMN IF NOT EXISTS max_attempts     integer     NOT NULL DEFAULT 5,
    ADD COLUMN IF NOT EXISTS next_retry_at    timestamptz,
    ADD COLUMN IF NOT EXISTS staging_relpath  text        NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS staged_size      bigint      NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS staged_sha256    text        NOT NULL DEFAULT '';

CREATE INDEX IF NOT EXISTS idx_job_items_retry_due
    ON job_items (next_retry_at)
    WHERE status = 'retry_wait';

CREATE INDEX IF NOT EXISTS idx_job_items_waiting_storage
    ON job_items (status)
    WHERE status IN ('waiting_for_storage', 'waiting_for_space');
