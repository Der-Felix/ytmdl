-- Users: local accounts with credentials, role and status.
--
-- The username is unique case-insensitively via idx_users_username_lower.
-- display_name preserves user-chosen casing.
-- role is either 'admin' or 'user'.

CREATE TABLE users (
    id            text        PRIMARY KEY,
    username      text        NOT NULL,
    display_name  text        NOT NULL DEFAULT '',
    password_hash text        NOT NULL,
    role          text        NOT NULL DEFAULT 'user',
    enabled       boolean     NOT NULL DEFAULT true,
    created_at    timestamptz NOT NULL,
    updated_at    timestamptz NOT NULL,
    last_login_at timestamptz,

    CONSTRAINT users_role_known CHECK (role IN ('admin', 'user'))
);

CREATE UNIQUE INDEX idx_users_username_lower ON users (lower(username));

-- Sessions: active user logins authenticated by random session tokens.
--
-- The raw session token is never stored in the database. token_hash stores
-- SHA-256(raw_token). If a user is deleted, all associated sessions cascade.

CREATE TABLE sessions (
    id           text        PRIMARY KEY,
    user_id      text        NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    token_hash   text        NOT NULL UNIQUE,
    user_agent   text        NOT NULL DEFAULT '',
    ip_address   text        NOT NULL DEFAULT '',
    created_at   timestamptz NOT NULL,
    expires_at   timestamptz NOT NULL,
    last_seen_at timestamptz NOT NULL
);

CREATE INDEX idx_sessions_user_id ON sessions (user_id);
CREATE INDEX idx_sessions_expires_at ON sessions (expires_at);
