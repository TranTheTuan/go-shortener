CREATE TABLE IF NOT EXISTS bulk_jobs (
    id         BIGSERIAL PRIMARY KEY,
    owner_id   BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    file_key   TEXT NOT NULL,
    filename   TEXT NOT NULL,
    result_key TEXT,
    status     VARCHAR(20) NOT NULL DEFAULT 'pending',
    total_rows INT NOT NULL DEFAULT 0,
    done_rows  INT NOT NULL DEFAULT 0,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_bulk_jobs_owner_id ON bulk_jobs (owner_id, created_at DESC);

CREATE TABLE IF NOT EXISTS bulk_job_outbox (
    id         BIGSERIAL PRIMARY KEY,
    job_id     BIGINT NOT NULL REFERENCES bulk_jobs(id) ON DELETE CASCADE,
    published  BOOLEAN NOT NULL DEFAULT false,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_bulk_job_outbox_unpublished ON bulk_job_outbox (id) WHERE published = false;
