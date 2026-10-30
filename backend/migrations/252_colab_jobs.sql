-- Independent compute credentials: never reuse Drive or model OAuth grants.
CREATE TABLE colab_accounts (
 id BIGSERIAL PRIMARY KEY,
 email TEXT NOT NULL UNIQUE,
 refresh_token_encrypted TEXT NOT NULL,
 enabled BOOLEAN NOT NULL DEFAULT TRUE,
 subscription JSONB NOT NULL DEFAULT '{}',
 runtime_specs JSONB NOT NULL DEFAULT '{}',
 last_error TEXT NOT NULL DEFAULT '',
 checked_at TIMESTAMPTZ,
 created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE TABLE colab_jobs (
 id UUID PRIMARY KEY,
 user_id BIGINT NOT NULL REFERENCES users(id),
 account_id BIGINT REFERENCES colab_accounts(id),
 title TEXT NOT NULL,
 code TEXT NOT NULL,
 request_hash TEXT NOT NULL,
 status TEXT NOT NULL DEFAULT 'queued',
 timeout_seconds INT NOT NULL CHECK(timeout_seconds BETWEEN 30 AND 300),
 runtime_name TEXT NOT NULL DEFAULT '',
 operation_name TEXT NOT NULL DEFAULT '',
 runtime_released BOOLEAN NOT NULL DEFAULT FALSE,
 cancel_requested BOOLEAN NOT NULL DEFAULT FALSE,
 logs TEXT NOT NULL DEFAULT '',
 error TEXT NOT NULL DEFAULT '',
 output_path TEXT NOT NULL DEFAULT '',
 created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
 updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX colab_jobs_user_created ON colab_jobs(user_id,created_at DESC);
CREATE INDEX colab_jobs_queue ON colab_jobs(created_at) WHERE status='queued';
