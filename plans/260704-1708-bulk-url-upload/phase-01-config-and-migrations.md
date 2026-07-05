# Phase 01 — Config & Migrations

## Context Links
- Design spec: `../reports/design-260704-1708-bulk-url-upload.md` (Data Models, New Config)
- Pattern refs: `configs/config.go`, `migrations/000008_create_subscriptions_table.up.sql`, `.env.example`

## Overview
- **Priority:** P1 (foundation — all later phases depend on it)
- **Status:** pending
- **Description:** Add R2 config + Kafka bulk-topic config; create `bulk_jobs` and `bulk_job_outbox` tables (migration `000010`). No new deps here.

## Key Insights
- Config uses `caarlos0/env` with `envPrefix`; add `R2 R2Config \`envPrefix:"R2_"\`` to top-level `Config`.
- Kafka config extended in-place (same struct) — mirrors `ClickTopic`/`ConsumerGroup`.
- Migrations are manual (`make migrate-up`), NOT run on startup. Next index is `000010`.
- GORM entities (phase 03) must match this SQL exactly.

## Requirements
- **Functional:** New env vars parse with defaults; tables exist with FKs to `users(id)`.
- **Non-functional:** `bulk_jobs` list-by-owner query fast → index on `owner_id`; outbox poll fast → index on `published`.

## Architecture / Design
`R2Config` (endpoint built from `AccountID`), bucket default `bulk-uploads`. Kafka gains `BulkJobTopic`, `BulkConsumerGroup`.

## Related Code Files
- **Create:** `migrations/000010_create_bulk_jobs.up.sql`, `migrations/000010_create_bulk_jobs.down.sql`
- **Modify:** `configs/config.go`, `.env.example`

## Implementation Steps
1. In `configs/config.go`, add struct:
   ```go
   // R2Config holds Cloudflare R2 (S3-compatible) settings. Endpoint is derived
   // from AccountID: https://<AccountID>.r2.cloudflarestorage.com
   type R2Config struct {
       AccountID       string `env:"ACCOUNT_ID"`
       AccessKeyID     string `env:"ACCESS_KEY_ID"`
       SecretAccessKey string `env:"SECRET_ACCESS_KEY"`
       Bucket          string `env:"BUCKET" envDefault:"bulk-uploads"`
   }
   func (r R2Config) Enabled() bool { return r.AccountID != "" && r.AccessKeyID != "" }
   func (r R2Config) Endpoint() string {
       return fmt.Sprintf("%s.r2.cloudflarestorage.com", r.AccountID) // minio wants host only
   }
   ```
2. Add `R2 R2Config \`envPrefix:"R2_"\`` to the top-level `Config` struct.
3. Extend `KafkaConfig`:
   ```go
   BulkJobTopic      string `env:"BULK_JOB_TOPIC" envDefault:"bulk-link-jobs"`
   BulkConsumerGroup string `env:"BULK_CONSUMER_GROUP" envDefault:"bulk-job-consumer"`
   ```
4. Create `000010_create_bulk_jobs.up.sql`:
   ```sql
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
   ```
5. Create `000010_create_bulk_jobs.down.sql`: `DROP TABLE IF EXISTS bulk_job_outbox; DROP TABLE IF EXISTS bulk_jobs;`
6. Append to `.env.example` under new `# Cloudflare R2` + existing Kafka block:
   ```
   R2_ACCOUNT_ID=
   R2_ACCESS_KEY_ID=
   R2_SECRET_ACCESS_KEY=
   R2_BUCKET=bulk-uploads
   KAFKA_BULK_JOB_TOPIC=bulk-link-jobs
   KAFKA_BULK_CONSUMER_GROUP=bulk-job-consumer
   ```
7. `go build ./...` compiles (config parses).

## Todo List
- [ ] Add `R2Config` + `Enabled()`/`Endpoint()` helpers
- [ ] Wire `R2` into top-level `Config`
- [ ] Extend `KafkaConfig` with bulk topic/group
- [ ] Create up/down migration `000010`
- [ ] Update `.env.example`
- [ ] `go build ./...` passes; `make migrate-up` applies cleanly

## Success Criteria
- `configs.Load()` returns R2 + bulk Kafka fields with defaults.
- `make migrate-up` then `make migrate-down` round-trips without error.

## Risk Assessment
- **FK CASCADE:** deleting a user cascades jobs+outbox — acceptable.
- **Partial index** on outbox keeps poll cheap even as published rows accumulate; a later cleanup job can prune (out of scope).

## Unresolved Questions
- Should published outbox rows be auto-deleted after publish instead of flagged? (Flag chosen for audit/debug simplicity.)
