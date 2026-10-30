-- Add GPU/CPU hardware selection and allow up to 3600s training jobs.
ALTER TABLE colab_jobs ADD COLUMN IF NOT EXISTS hardware TEXT NOT NULL DEFAULT 'CPU';
ALTER TABLE colab_jobs DROP CONSTRAINT IF EXISTS colab_jobs_timeout_seconds_check;
ALTER TABLE colab_jobs ADD CONSTRAINT colab_jobs_timeout_seconds_check CHECK(timeout_seconds BETWEEN 30 AND 3600);
