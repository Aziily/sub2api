-- Keep historical jobs unknown rather than treating total lifecycle time as GPU use.
ALTER TABLE colab_jobs ADD COLUMN IF NOT EXISTS allocated_at TIMESTAMPTZ;
ALTER TABLE colab_jobs ADD COLUMN IF NOT EXISTS released_at TIMESTAMPTZ;
ALTER TABLE colab_jobs ADD COLUMN IF NOT EXISTS workspace JSONB;
ALTER TABLE colab_jobs ADD COLUMN IF NOT EXISTS input_token_hash TEXT NOT NULL DEFAULT '';
