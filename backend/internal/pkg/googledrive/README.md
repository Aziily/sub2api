# Google Drive integration development status

This branch is a work in progress. It does not yet expose a Drive management
page or a WebDAV endpoint and must not be deployed as a finished integration.

The current primitives include a PKCE authorization flow, encrypted
per-user refresh-token storage, a separate root-folder identifier for each
user, Drive file operations and a Basic-auth adapter for the existing API-key
checks. A namespace resolver walks from the configured root and rejects
ambiguous names and traversal segments. These components are not yet wired
into routes or the application lifecycle.

The integration uses the `drive.file` scope. It can access files created by
the application or explicitly opened with it; this is not access to every
existing file in the account. Google OAuth applications in Testing mode may
issue refresh tokens that expire after seven days. Production use also needs
a stable, explicitly configured encryption key and a registered callback URI.

Before deployment, complete the authenticated admin authorization and file
management flow, enforce per-user root traversal for every file operation,
implement and test WebDAV methods and limits, and verify cross-user isolation
and key revocation against the real application. Test both file integrity and
error handling with the target Drive account. Do not log OAuth credentials,
API keys or request bodies.
