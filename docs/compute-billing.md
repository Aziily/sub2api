# Compute inventory, platform pricing and storage

Administrator compute accounts display Google account-wide CU snapshots, hourly consumption and active assignments. Values come from the same session endpoint used by Google's `colab usage` CLI. Refreshing an account and completing a task updates the snapshot. Failed reads retain the last result with its timestamp and error; an unknown balance is never displayed as zero. CU cannot be converted into guaranteed GPU hours or capacity.

Platform pricing is separate from Google CU. `/api/v1/admin/compute/pricing` accepts an administrator-only PUT with `{enabled, version, rates: {CPU: "...", A100: "..."}}`. Rates use the existing API user-balance unit per hour, up to six decimals. A version conflict returns 409. Missing rates block billable submissions for that hardware. The initial configuration is disabled with no invented prices.

When enabled, submission reserves `hourly_rate * timeout / 3600` atomically with creating the job. Insufficient funds returns 402. Idempotent submission does not reserve again. Each job saves its own price snapshot, reservation and final charge. After runtime release, settlement uses allocated-to-released seconds, rounded up and capped by the requested timeout. Unused funds are returned atomically and user balance caches invalidated. Queue cancellations and rejected allocations cost zero. Failed or cancelled executions pay for actual runtime occupancy. Recovery retries settlement without re-running code or refunding twice. Existing historical jobs remain unbilled. Storage and compute routes use their own quota/balance checks, so a fully reserved platform balance does not block status, cancellation or downloads in standard mode; active, unexpired, non-exhausted keys are still required. API-key spend limits currently remain API-specific; compute checks the user's platform balance and key validity.

The submitting user's virtual drive owns the input archive, artifacts and logs. The physical backing is the administrator-connected Google Drive storage pool, not an implicit personal Google OAuth grant for every platform user. Input objects are pinned by node ID; output uploads reserve quota before writing. Runtime scratch data is temporary unless selected as output. Retained inputs and outputs continue using quota until deleted. Submission checks usable storage, but cannot guarantee capacity for arbitrarily large future output; output quota is enforced at write time.

CLI: `compute.py run --hardware CPU --output outputs -- python main.py`, then `wait JOB_ID` and `fetch JOB_ID --directory ./results/JOB_ID`. Input upload and artifact writeback go directly to Google; runtime input fetch and client downloads currently pass through the service.

## A100 measurement and initial production rate (2026-10-30)

Live job `7c8b6c4b-e6e1-401d-9cf8-71bd4f77a2c3` allocated an NVIDIA
A100-SXM4-40GB. It loaded a synthetic 1,024-row dataset from the submitting
user's Drive, trained a Torch model for 90 seconds, and wrote the model,
metrics and logs back to that same virtual drive. SHA-256 checks passed.
The input and outputs used 154,713 bytes across five files; cleanup returned
usage to its baseline. The runtime was released and Google reported zero
active assignments.

Google CU decreased from 199.90196229732615 to 199.6157617187562:
**0.28620057856994663 CU**. The reported running rate was 6.73 CU/hour,
while our allocated-to-released interval was 126.135668 seconds. Dividing the
balance delta by this interval gives an effective **8.168364 CU/hour**.
These are different measurements: the short-task balance delta can include
allocation overhead outside our recorded interval. This is not a claim that
Google's steady-state A100 price is 8.168364 CU/hour.

The official https://colab.research.google.com/signup page displayed USD
9.99 per 100 Pay As You Go CU. This is a replacement-cost reference, not the
connected account's regional subscription invoice. At USD 0.0999/CU, this
test cost about USD 0.02859144. With no margin, the initial A100 rate is
**0.816020 platform balance units/hour**, rounding upward to six decimals.
This is a provisional short-task rate; longer measurements may justify a
revision. Prices are not changed automatically by a CU snapshot.

Production pricing is enabled at version 4 with A100 only. Other hardware
has no invented rate and cannot accept paid submissions until priced. The
measurement job was submitted before enabling billing and remains free to
the platform user; Google's CU was consumed. Existing user balances were
not changed. An account with zero platform balance must be funded before
submitting a paid task.

Dashboard storage/compute summaries are cumulative, independently of API
charts' date filters. Administrators see the whole platform, Google CU
coverage and snapshot age, usable storage-pool capacity, platform-indexed
files, job states, GPU runtime, charges and outstanding reservations. Users
see only their own files, quota, job totals and platform charges; Google
account identities and CU are not exposed. Refreshing the dashboard reads
saved upstream snapshots and current database totals, not Google's API.
