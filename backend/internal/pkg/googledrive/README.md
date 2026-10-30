# Google Drive storage client

This package contains the low-level Google pieces behind the cloud drive
(网盘): the PKCE authorization flow with the `drive.file` scope, token refresh,
file operations (create, download, delete, quota) and resumable upload
sessions that a browser can complete directly against Google.

The pooled drive itself lives in `internal/service`:

- `drive_storage_accounts` holds the Google accounts that administrators
  connect as shared storage. Only encrypted refresh tokens are stored.
- `drive_nodes` is each user's virtual tree. Folders exist only in the
  database; every file points at one object in one pool account. Users never
  see Google accounts or Drive IDs.
- `drive_user_quotas` holds optional per-user limits.

`GoogleDriveService` picks the account with the most free space for each new
file, reserves space for direct uploads, expires abandoned reservations and
refreshes account quotas in a background loop. The same tree is exposed to
the web UI (`/api/v1/user/drive`), to API-key clients (`/drive/v1`) and to
WebDAV (`/webdav`, Basic auth with the user's email and one of their API
keys).

With `drive.file`, the application can only see files it created. OAuth
clients in Google's Testing mode may issue refresh tokens that expire after
seven days; administrators then reauthorize the account from the admin page.
Do not log OAuth credentials, API keys or request bodies.
