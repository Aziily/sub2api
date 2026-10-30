# Colab CPU task service

Implementation: `feat/google-drive`. This service uses the consumer Colab API,
not Vertex AI / Colab Enterprise. Deployment and account acceptance evidence
belongs in the deployment repository; a built image alone is not a successful
provider execution.

## User and administrator interfaces

- `/compute`: submit Python (up to 64 KiB), name and 30–300 second deadline;
  list the current user's latest 100 jobs; view captured logs and output folder;
  request cancellation.
- `/admin/compute`: separately authorize Colab accounts, check their subscription
  and CPU eligibility, enable/disable scheduling, view and cancel all jobs.
- `/compute/v1/jobs`: API-key submission, list, details and cancellation. Use
  `tools/compute.py --base https://YOUR-HOST submit task.py`. It prompts for the
  user's API key unless `SUB2API_API_KEY` is set. A submission UUID is printed
  before sending; reuse `--id UUID` after a lost response. Changing the request
  body with the same UUID is rejected.
- `/auth/colab/callback`: public HTTPS OAuth callback. Independent PKCE/state,
  scope and encrypted refresh credential; Drive grants are never reused.

## Configuration

Enable `colaboratory.googleapis.com` in the Google Cloud project. Register the
exact HTTPS callback. Set `COLAB_REDIRECT_URI`; optionally set a separate
`COLAB_CLIENT_ID` and `COLAB_CLIENT_SECRET`. If omitted, the existing Drive OAuth
client registration is reused, but the **grant and stored credentials remain
independent**. Consent requests `colaboratory`, `openid`, `email`; verify the
Google account in the consent screen. Persistent `TOTP_ENCRYPTION_KEY` is
required. The account is schedulable only after successful subscription and
standard-CPU eligibility checks. OAuth testing-mode refresh expiry also applies.

The provider returns subscription tier and eligible hardware, not remaining
compute units. The UI explicitly says unavailable; it does not substitute model
credits, Google Cloud trial credits or an invented zero.

## Execution, output and recovery

PostgreSQL stores accounts, jobs, ownership, submission hashes, runtime names,
operation names, state, logs and output paths. A session advisory lock serializes
this initial CPU pool across processes. One job owns one runtime; tenants do not
share kernels or machines. A connection heartbeat cancels work if the worker
loses its database lock. No submitted code is executed on the Sub2API server.

A job provisions `VARIANT_CPU` / `NONE` / `SHAPE_STANDARD`, calls the official
`execute_code` MCP tool once, captures its terminal result, then writes
`stdout.txt`, `stderr.txt`, `execution.json` into `/Colab-<job UUID>` in the owner's
virtual drive. This version saves **text outputs**, not arbitrary runtime files,
notebooks or input datasets. The existing Drive service enforces ownership and
storage quota. Runtime code receives no Drive OAuth token or Sub2API API key.

Statuses: queued, provisioning, running, writing, succeeded, failed, cancelled,
unknown. Cancellation is polled during blocked provider calls. Deadline includes
provisioning, execution and output writeback. Runtime deletion is attempted after
success/failure/cancellation, and retried by the reconciler. No new runtime is
scheduled while an earlier owned runtime awaits cleanup. The UI shows cleanup
separately from execution success.

After a crash, in-flight jobs become unknown and their persisted runtime names
are cleaned up. Code is never automatically replayed after an uncertain outcome;
partial output may exist. Operations that are still provisioning are polled
before cleanup. Queue bound: 20 active jobs. Submission UUID + body hash prevents
accidental replay after a lost HTTP response. This is a deliberately serial CPU
implementation, not a distributed fair-share GPU scheduler.

## Provider references

- https://developers.google.com/colab/api/reference/rest
- https://developers.google.com/colab/api/reference/rest/v1beta/runtimespecs/list
- https://developers.google.com/colab/api/reference/rest/v1beta/TopLevel/getSubscription
- https://developers.google.com/colab/api/reference/mcp/tools_list/execute_code
- https://github.com/googlecolab/google-colab-cli
