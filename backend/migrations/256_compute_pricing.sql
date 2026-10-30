-- Google CU is upstream inventory; platform billing uses the existing user balance.
ALTER TABLE colab_accounts ADD COLUMN consumption JSONB;
ALTER TABLE colab_accounts ADD COLUMN consumption_checked_at TIMESTAMPTZ;
ALTER TABLE colab_accounts ADD COLUMN consumption_error TEXT NOT NULL DEFAULT '';
CREATE TABLE compute_pricing (
 id INTEGER PRIMARY KEY CHECK(id=1), enabled BOOLEAN NOT NULL DEFAULT false,
 version BIGINT NOT NULL DEFAULT 1, rates JSONB NOT NULL DEFAULT '{}'
);
INSERT INTO compute_pricing(id) VALUES(1);
ALTER TABLE colab_jobs ADD COLUMN price_per_hour NUMERIC(16,6) NOT NULL DEFAULT 0 CHECK(price_per_hour>=0);
ALTER TABLE colab_jobs ADD COLUMN reserved_cost NUMERIC(20,8) NOT NULL DEFAULT 0 CHECK(reserved_cost>=0);
ALTER TABLE colab_jobs ADD COLUMN charged_cost NUMERIC(20,8) NOT NULL DEFAULT 0 CHECK(charged_cost>=0);
ALTER TABLE colab_jobs ADD COLUMN billable_seconds INTEGER NOT NULL DEFAULT 0 CHECK(billable_seconds>=0);
ALTER TABLE colab_jobs ADD COLUMN billing_status TEXT NOT NULL DEFAULT 'free' CHECK(billing_status IN ('free','reserved','settled'));
CREATE INDEX colab_jobs_unsettled ON colab_jobs(created_at) WHERE billing_status='reserved';
