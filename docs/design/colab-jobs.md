# Colab task service: implementation design (2026-10-30)

Status: researched design, not an enabled execution service. Existing Drive authorization has only drive.file and cannot authorize Colab. No compute balance or successful Colab execution has been verified for the connected account.

## Verified provider interfaces

- https://developers.google.com/colab/api/reference/rest — consumer Colab runtime create/get/list/delete, eligible runtime specs and subscription tier.
- https://developers.google.com/colab/api/reference/rest/v1beta/TopLevel/getSubscription — requires colaboratory or colaboratory.readonly scope; returns tier, not remaining compute units.
- https://developers.google.com/colab/api/reference/mcp/tools_list/execute_code — Python execution with incremental logs and cancellation. A stable executionId plus session allows reattachment after streaming deadline, but replaying after a final result starts a NEW execution. Persist terminal state before retrying.
- https://github.com/googlecolab/google-colab-cli — official CLI supports ephemeral `colab run`, runtime lifecycle and artifact retrieval. Pin a reviewed version for an isolated worker; do not execute user scripts on the Sub2API host.
- https://docs.cloud.google.com/colab/docs/reference/rest — Enterprise notebook execution jobs are a separate Google Cloud product and billing path.

## Product and API

User page `/compute`: submit Python/notebook plus input paths, CPU/GPU choice, deadline and budget ceiling; list only own jobs; display queued/provisioning/running/succeeded/failed/cancelled/unknown, logs and output links in the user's drive. Show compute units only when returned by a verified provider endpoint with timestamp. Unknown is not zero. Provider compute units, platform credits and model API spend are separate measures.

Administrator page `/admin/compute`: independently authorized Colab accounts, subscription, eligible hardware, provider balance when obtainable, freshness/error, concurrency limit, enabled status and all job records. Do not expose tokens. Drive and compute accounts need not be the same.

Proposed client: `pool compute submit train.py --input /data --output /runs --timeout 1800`; returns job ID. `status`, `logs`, `cancel`, `download` call `/compute/v1/jobs` with existing API-key authentication and user ownership checks. A notebook link remains a fallback, with explicit `manual` state rather than pretending a background execution was submitted.

## Durable execution

Persist compute_accounts, compute_jobs, compute_attempts and append-only job events. Use transaction-based leasing (`FOR UPDATE SKIP LOCKED`), lease expiry, per-account concurrency, idempotency key plus body hash and account/budget reservation. A job owns one isolated runtime. Never reuse a runtime between tenants. A worker provisions a runtime, copies scoped inputs, executes code, uploads outputs through a job-scoped Drive capability, records terminal status and releases the runtime in cleanup. A reconciler detects leaked runtimes and unknown outcomes after crashes.

Credentials remain in the control plane; runtime code must not receive pool OAuth refresh tokens or a user's unrestricted API key. Issue short-lived capabilities bound to job/user/path/operation/byte limit for inputs and outputs. Enforce CPU default, explicit GPU selection, maximum elapsed time and output bytes. No purchase or automatic balance top-up.

## Acceptance gates before enabling submission

1. Independent Colab consent with minimal scope and verified account identity; refresh survives restart.
2. Read subscription and eligible runtimes; determine whether a supported balance endpoint exists. Keep balance unavailable otherwise.
3. CPU hello-world using official CLI/API; verify logs, artifacts, cancellation and runtime cleanup, including failure.
4. Queue restart/idempotency, cross-user access rejection, timeout and resource release tests.
5. Only then enable the user submit button and production scheduler. Claims that this consumes the account's Google AI plan benefits require an actual account-level check.
