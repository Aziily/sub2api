-- Cloud drive (网盘). Administrators connect Google accounts as a shared
-- storage pool. Every user sees only a virtual tree kept in drive_nodes:
-- folders exist solely in this database, and each file points at one object
-- stored in one pool account. Only encrypted refresh credentials are stored.
CREATE TABLE IF NOT EXISTS drive_storage_accounts (
    id BIGSERIAL PRIMARY KEY,
    google_email TEXT NOT NULL,
    refresh_token_encrypted TEXT NOT NULL,
    root_folder_id TEXT NOT NULL,
    enabled BOOLEAN NOT NULL DEFAULT TRUE,
    quota_limit BIGINT,
    quota_usage BIGINT,
    quota_checked_at TIMESTAMPTZ,
    last_error TEXT NOT NULL DEFAULT '',
    -- Set for legacy per-user folders whose files still need importing.
    import_user_id BIGINT REFERENCES users(id) ON DELETE SET NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE UNIQUE INDEX IF NOT EXISTS drive_storage_accounts_email_unique
    ON drive_storage_accounts (lower(google_email));

CREATE TABLE IF NOT EXISTS drive_nodes (
    id BIGSERIAL PRIMARY KEY,
    user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    parent_id BIGINT REFERENCES drive_nodes(id) ON DELETE CASCADE,
    name TEXT NOT NULL,
    is_dir BOOLEAN NOT NULL,
    size BIGINT NOT NULL DEFAULT 0 CHECK (size >= 0),
    mime_type TEXT NOT NULL DEFAULT '',
    storage_account_id BIGINT REFERENCES drive_storage_accounts(id),
    google_file_id TEXT,
    -- 'pending' files reserve space while a client uploads straight to Google.
    state TEXT NOT NULL DEFAULT 'ready' CHECK (state IN ('ready', 'pending')),
    pending_expires_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CHECK (NOT is_dir OR (storage_account_id IS NULL AND google_file_id IS NULL AND state = 'ready')),
    CHECK (is_dir OR storage_account_id IS NOT NULL),
    CHECK (is_dir OR state = 'pending' OR google_file_id IS NOT NULL)
);
CREATE UNIQUE INDEX IF NOT EXISTS drive_nodes_ready_name_unique
    ON drive_nodes (user_id, COALESCE(parent_id, 0), name) WHERE state = 'ready';
CREATE INDEX IF NOT EXISTS drive_nodes_children ON drive_nodes (user_id, parent_id);
CREATE INDEX IF NOT EXISTS drive_nodes_storage
    ON drive_nodes (storage_account_id) WHERE storage_account_id IS NOT NULL;
CREATE INDEX IF NOT EXISTS drive_nodes_pending
    ON drive_nodes (pending_expires_at) WHERE state = 'pending';

-- Optional per-user limit; users without a row share the pool freely.
CREATE TABLE IF NOT EXISTS drive_user_quotas (
    user_id BIGINT PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
    quota_bytes BIGINT NOT NULL CHECK (quota_bytes >= 0),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Former per-user connections become pool storage; their folders are reused
-- as storage roots and existing files are imported for the former owner.
INSERT INTO drive_storage_accounts
    (google_email, refresh_token_encrypted, root_folder_id, import_user_id, created_at, updated_at)
SELECT label, refresh_token_encrypted, root_folder_id, user_id, created_at, updated_at
FROM google_drive_connections
WHERE root_folder_id IS NOT NULL AND label <> ''
ON CONFLICT DO NOTHING;

DROP TABLE IF EXISTS google_drive_connections;
