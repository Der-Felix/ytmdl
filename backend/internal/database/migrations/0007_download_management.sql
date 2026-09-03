-- Migration 0007: Download management, job prioritization, pause controls, and subscription release rules.

-- 1. Job Priority & Paused flag
-- Priority uses a compact integer rank in the database:
-- 0 = low, 1 = normal, 2 = high
-- This enables fast B-tree index scans on (priority DESC, created_at ASC) without CASE expressions.
ALTER TABLE jobs
    ADD COLUMN IF NOT EXISTS priority integer NOT NULL DEFAULT 1,
    ADD COLUMN IF NOT EXISTS paused boolean NOT NULL DEFAULT false;

ALTER TABLE jobs DROP CONSTRAINT IF EXISTS jobs_priority_check;
ALTER TABLE jobs ADD CONSTRAINT jobs_priority_check CHECK (priority IN (0, 1, 2));

-- 2. Subscriptions Release Filter & Download Priority
-- Default release filter allows Albums, EPs, and Singles.
-- Existing subscriptions get download_priority = 1 (normal) to preserve v0.11 behavior.
ALTER TABLE artist_subscriptions
    ADD COLUMN IF NOT EXISTS release_filter jsonb NOT NULL DEFAULT '{"albums": true, "singles": true, "eps": true, "live": false, "compilations": false, "remixes": false}'::jsonb,
    ADD COLUMN IF NOT EXISTS download_priority integer NOT NULL DEFAULT 1;

ALTER TABLE artist_subscriptions DROP CONSTRAINT IF EXISTS artist_subscriptions_priority_check;
ALTER TABLE artist_subscriptions ADD CONSTRAINT artist_subscriptions_priority_check CHECK (download_priority IN (0, 1, 2));

-- 3. Optimization Indexes for Queue Selection and History
CREATE INDEX IF NOT EXISTS idx_jobs_priority_created
    ON jobs (priority DESC, created_at ASC)
    WHERE status NOT IN ('completed', 'failed', 'cancelled') AND NOT paused;

CREATE INDEX IF NOT EXISTS idx_jobs_history_pagination
    ON jobs (status, created_at DESC, id DESC);
