# Workspace compute on feat/codelab

The platform separates API, storage and compute navigation. Each product has
user screens and administrator screens. General profile, billing and settings
remain outside those groups. Compute uses a chip icon distinct from proxies.

## User contract

`/compute` shows eligible hardware, GPU memory, the signed-in user's platform
balance, their task counts and recorded allocation time. No upstream account
identifiers or CU balance are exposed. Compute billing is not enabled yet: the
existing platform balance is displayed, but no compute rates or deductions are
invented. Administrators at `/admin/compute` can see account entitlements,
health, allocation time and all jobs. Hardware eligibility is not a guarantee
of immediate capacity. Memory marked nominal is not a measurement of the next
runtime. A100 40 GiB was measured in the deployed account; unknown models remain
unmeasured. Upstream CU balance is not available in the consumer API.

## Command line

Download `/compute/v1/cli` to a location outside the submitted project:

```sh
curl -fsS https://YOUR-HOST/compute/v1/cli -o "$HOME/compute.py"
export SUB2API_BASE_URL=https://YOUR-HOST
# Set SUB2API_API_KEY securely, or let the CLI prompt without echo.
cd my-project
python3 "$HOME/compute.py" run --dry-run -- python main.py
python3 "$HOME/compute.py" run --hardware A100 --output outputs --wait -- python main.py
python3 "$HOME/compute.py" resources
python3 "$HOME/compute.py" status JOB_UUID
```

The argv runs in a fresh provider runtime with the project as cwd. Dependencies
can be installed by the submitted entry script (for example a setup-and-run
shell script); the platform does not execute them on its gateway. Repeat
`--output` for multiple paths. Artifacts are zipped into the owner's virtual
Drive under `/Colab-JOB_UUID/artifacts.zip`, alongside stdout, stderr and execution
metadata. Hidden files, symlinks, common credential files, environment folders
and output directories are excluded from input. `.computeignore` adds relative
path/glob exclusions. Always review `--dry-run` before submitting private work.
Limits: 10,000 files, 256 MiB uncompressed / 64 MiB compressed input, 512 MiB
outputs, 30–3600 seconds including setup and writeback. API-only Python jobs
retain the 64 KiB code limit. The CLI prints a UUID before submission; `--id`
checks that job before uploading or executing again. Inputs stay in the user's
Drive for review and can be removed after the job finishes.

## Transfer and isolation

1. The client requests a file-scoped Drive resumable upload session using its
   platform API key. Input bytes go directly from client to Google Drive.
2. The job refers to a user-owned immutable Drive node. The server issues a
   random, hashed, job-scoped read credential valid only while that job runs.
   The runtime reads the input through the gateway. This leg still consumes
   gateway bandwidth; no public sharing or account-wide OAuth token is used.
3. The provider executes the user's argv in its isolated runtime. Afterward the
   gateway creates one exact-size Drive upload session for artifacts.zip.
   Artifact bytes go directly from runtime to Drive. The gateway verifies and
   commits the object into the user's tree.
4. File downloads and WebDAV currently remain relayed. Log text is also relayed.
   Complete direct downloads need a provider-supported per-object capability;
   sharing a pooled Drive OAuth token would breach tenant isolation.

Google's CLI file management document describes a Jupyter proxy token design,
but it is not a confirmed scoped download capability in this deployed API flow.
The implementation does not expose those runtime credentials to users.

## Operations and recovery

Independent Colab consent uses `colaboratory`, `openid`, `email`, and encrypted
refresh credentials. Set `COLAB_REDIRECT_URI` to the public HTTPS callback and
optionally `COLAB_CLIENT_ID` / `COLAB_CLIENT_SECRET`. Drive client registration
can be reused, but grants and credentials remain separate.

PostgreSQL persists job ownership, hardware, workspace, submission hashes,
operation/runtime IDs and allocation/release timestamps. Summary queries are
owner-scoped for users. Historical jobs without allocation timestamps display
no measured runtime rather than counting queue time as GPU usage. Allocation
time includes writeback and cleanup; it is not provider-billed GPU hours.

A DB advisory lock currently allows one job globally and up to 20 queued/active
jobs. Lost locks cancel execution. Interrupted jobs become unknown and are never
automatically replayed; cleanup must finish before the next allocation. Runtime
and transfer credentials are revoked/cleaned up on termination. The current
pool is not a distributed fair-share scheduler. Deployment must wait for active
jobs, retain an image rollback and take a database backup before migrations.
