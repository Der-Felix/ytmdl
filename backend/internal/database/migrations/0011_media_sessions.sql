-- Migration 0011: Media Sessions Foundation for server-managed authentication.
-- Supports multi-session rotation, health tracking, and cooldowns.
-- Sensitive cookie contents and ephemeral lease counts are NEVER stored in the database.

CREATE TABLE IF NOT EXISTS media_sessions (
    id                   UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    provider_family      TEXT        NOT NULL,
    name                 TEXT        NOT NULL,
    cookie_ref           TEXT        NOT NULL,
    enabled              BOOLEAN     NOT NULL DEFAULT TRUE,
    health_status        TEXT        NOT NULL DEFAULT 'unknown',
    consecutive_failures INTEGER     NOT NULL DEFAULT 0,
    last_used_at         TIMESTAMPTZ,
    last_success_at      TIMESTAMPTZ,
    last_failure_at      TIMESTAMPTZ,
    last_failure_reason  TEXT        NOT NULL DEFAULT '',
    cooldown_until       TIMESTAMPTZ,
    created_at           TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at           TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT media_sessions_health_check CHECK (
        health_status IN ('unknown', 'healthy', 'cooldown', 'rate_limited', 'bot_challenge', 'auth_failed')
    ),
    CONSTRAINT media_sessions_failures_check CHECK (
        consecutive_failures >= 0
    )
);

CREATE INDEX IF NOT EXISTS idx_media_sessions_lookup
    ON media_sessions (provider_family, enabled, health_status, cooldown_until);

CREATE INDEX IF NOT EXISTS idx_media_sessions_updated_at
    ON media_sessions (updated_at DESC);
