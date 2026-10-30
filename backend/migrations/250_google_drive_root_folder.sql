-- The WebDAV namespace must be confined to a folder created for this user.
-- Existing incomplete connections must be reauthorized to get a root folder.
ALTER TABLE google_drive_connections ADD COLUMN IF NOT EXISTS root_folder_id TEXT;
CREATE UNIQUE INDEX IF NOT EXISTS google_drive_connections_root_folder_unique
    ON google_drive_connections(root_folder_id) WHERE root_folder_id IS NOT NULL;
