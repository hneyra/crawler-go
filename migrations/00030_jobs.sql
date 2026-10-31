-- +goose Up
-- postgres work queue (skip locked + lease). same design as costea-backend.
CREATE TABLE jobs (
  id           bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  kind         text NOT NULL CHECK (length(kind) BETWEEN 1 AND 64),
  payload      jsonb NOT NULL DEFAULT '{}' CHECK (jsonb_typeof(payload) = 'object'),
  status       text NOT NULL DEFAULT 'queued' CHECK (status IN ('queued', 'running', 'succeeded', 'failed', 'cancelled')),
  dedupe_key   text CHECK (length(dedupe_key) <= 255),
  run_at       timestamptz NOT NULL DEFAULT now(),
  attempts     integer NOT NULL DEFAULT 0 CHECK (attempts >= 0),
  max_attempts integer NOT NULL DEFAULT 5 CHECK (max_attempts BETWEEN 1 AND 50),
  locked_by    text,
  locked_until timestamptz,
  last_error   text CHECK (length(last_error) <= 4000),
  created_at   timestamptz NOT NULL DEFAULT now(),
  updated_at   timestamptz NOT NULL DEFAULT now(),
  finished_at  timestamptz
);
CREATE INDEX jobs_ready ON jobs (run_at) WHERE status IN ('queued', 'running');
CREATE UNIQUE INDEX jobs_dedupe ON jobs (kind, dedupe_key) WHERE dedupe_key IS NOT NULL AND status IN ('queued', 'running');
CREATE INDEX jobs_finished ON jobs (finished_at) WHERE finished_at IS NOT NULL;

-- run <-> job link for status pages.
ALTER TABLE crawl_run ADD COLUMN job_id bigint REFERENCES jobs (id) ON DELETE SET NULL;
CREATE INDEX crawl_run_active ON crawl_run (site_id, kind) WHERE status IN ('QUEUED', 'RUNNING');

-- +goose Down
DROP INDEX crawl_run_active;
ALTER TABLE crawl_run DROP COLUMN job_id;
DROP TABLE jobs;
