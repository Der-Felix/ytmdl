-- Migration 0010: Add very_high priority (3 = Sehr hoch) to jobs and artist_subscriptions.
-- Priority rank: 0 = low, 1 = normal, 2 = high, 3 = very_high.

ALTER TABLE jobs DROP CONSTRAINT IF EXISTS jobs_priority_check;
ALTER TABLE jobs ADD CONSTRAINT jobs_priority_check CHECK (priority IN (0, 1, 2, 3));

ALTER TABLE artist_subscriptions DROP CONSTRAINT IF EXISTS artist_subscriptions_priority_check;
ALTER TABLE artist_subscriptions ADD CONSTRAINT artist_subscriptions_priority_check CHECK (download_priority IN (0, 1, 2, 3));
