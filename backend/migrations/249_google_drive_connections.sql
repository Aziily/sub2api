-- One Google Drive connection belongs to one Sub2API user. Only encrypted
-- refresh credentials are persisted; access tokens stay in memory.
CREATE TABLE IF NOT EXISTS google_drive_connections (
    user_id BIGINT PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
    label TEXT NOT NULL DEFAULT '',
    refresh_token_encrypted TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
