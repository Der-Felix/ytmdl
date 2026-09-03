-- Migration 0008: Library Integrity & Audit Runs
-- Stores asynchronous read-only audit runs and structured findings with pagination.

CREATE TABLE IF NOT EXISTS library_audit_runs (
    id VARCHAR(32) PRIMARY KEY,
    mode VARCHAR(16) NOT NULL,
    status VARCHAR(16) NOT NULL,
    started_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    finished_at TIMESTAMPTZ,
    scanned INTEGER NOT NULL DEFAULT 0,
    total INTEGER NOT NULL DEFAULT 0,
    findings_count INTEGER NOT NULL DEFAULT 0,
    error_summary TEXT,
    created_by VARCHAR(32) REFERENCES users(id) ON DELETE SET NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);

ALTER TABLE library_audit_runs DROP CONSTRAINT IF EXISTS library_audit_runs_mode_check;
ALTER TABLE library_audit_runs ADD CONSTRAINT library_audit_runs_mode_check CHECK (mode IN ('quick', 'deep'));

ALTER TABLE library_audit_runs DROP CONSTRAINT IF EXISTS library_audit_runs_status_check;
ALTER TABLE library_audit_runs ADD CONSTRAINT library_audit_runs_status_check CHECK (status IN ('running', 'completed', 'failed', 'cancelled'));

CREATE TABLE IF NOT EXISTS library_audit_findings (
    id VARCHAR(32) PRIMARY KEY,
    run_id VARCHAR(32) NOT NULL REFERENCES library_audit_runs(id) ON DELETE CASCADE,
    finding_code VARCHAR(32) NOT NULL,
    severity VARCHAR(16) NOT NULL,
    relative_path TEXT NOT NULL,
    artist_id VARCHAR(32) REFERENCES artists(id) ON DELETE SET NULL,
    release_id VARCHAR(32) REFERENCES releases(id) ON DELETE SET NULL,
    track_id VARCHAR(32) REFERENCES tracks(id) ON DELETE SET NULL,
    evidence JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);

ALTER TABLE library_audit_findings DROP CONSTRAINT IF EXISTS library_audit_findings_severity_check;
ALTER TABLE library_audit_findings ADD CONSTRAINT library_audit_findings_severity_check CHECK (severity IN ('error', 'warning', 'info'));

CREATE INDEX IF NOT EXISTS idx_audit_runs_started
    ON library_audit_runs (started_at DESC, id DESC);

CREATE INDEX IF NOT EXISTS idx_audit_runs_status
    ON library_audit_runs (status);

CREATE INDEX IF NOT EXISTS idx_audit_findings_run_sev
    ON library_audit_findings (run_id, severity, id ASC);

CREATE INDEX IF NOT EXISTS idx_audit_findings_run_code
    ON library_audit_findings (run_id, finding_code, id ASC);

CREATE INDEX IF NOT EXISTS idx_audit_findings_track
    ON library_audit_findings (track_id)
    WHERE track_id IS NOT NULL;
