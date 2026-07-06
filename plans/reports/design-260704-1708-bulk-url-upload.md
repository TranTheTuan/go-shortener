# Design Spec: Bulk URL Upload

**Branch:** feat/handle-sheet-upload  
**Date:** 2026-07-04  
**Status:** Approved for planning

---

## Problem Statement

Users need to shorten large batches of URLs (up to 10k) at once. Manual one-by-one creation is impractical. Upload a spreadsheet → get back the same sheet with short URLs filled in.

---

## User Stories

- As a user, I upload a CSV/XLSX file containing URLs and receive a result file with short codes filled in.
- As a user, I can check the processing status of my batch job.
- As a user, I can download the result file when the job completes.

---

## File Template (Fixed Format)

Standard 2-column format. Users must follow this structure — no column detection logic needed.

```
url,result
https://example.com,
https://google.com,
https://invalid-url,
```

- Row 1: header (`url`, `result`) — always present
- `url` column: user fills in (required)
- `result` column: user leaves blank; worker fills in short URL or error

**Worker writes:**
- Success: `https://sho.rt/AbCd123`
- Duplicate (existing short code): same as success (reuse)
- Invalid URL: `url không hợp lệ`
- Other error: `lỗi xử lý`

**Accepted formats:** `.csv`, `.xlsx`  
**Max rows:** 10,000 URLs (excludes header)  
**Template download:** `GET /api/bulk-jobs/template?format=csv|xlsx`

---

## Architecture

```
Frontend
  │
  ├─ 1. Select file → count rows locally (reject if > 10k) → compute MD5 (spark-md5)
  │      MD5 format: base64(binary_md5), e.g. "xMpCOKC5I4INzFCab3WEmw=="
  │
  ├─ 2. POST /api/bulk-jobs/upload-url { filename, row_count, content_md5 }
  │         └─ Backend: presign PUT URL with ContentMD5 constraint → {presignedURL, fileKey}
  │
  ├─ 3. PUT presignedURL (direct to R2)
  │         Header: Content-MD5: <base64>  ← R2 enforces this matches signed value
  │         R2: 400 InvalidDigest if file was swapped → upload blocked at edge
  │
  ├─ 4. POST /api/bulk-jobs { fileKey, filename, row_count }
  │         └─ Backend: INSERT bulk_jobs + INSERT bulk_job_outbox (same tx)
  │              └─ Outbox relay goroutine: polls outbox → publish Kafka → mark published
  │
  └─ 5. GET /api/bulk-jobs/:id → status, result presigned download URL

Bulk Worker (separate process: main bulk-worker)
  └─ Consume Kafka topic bulk-link-jobs
       └─ Download file from R2
            └─ Parse rows → linkSvc.Create() for each URL
                 └─ Write result file → upload to R2
                      └─ UPDATE bulk_jobs SET status=completed, result_key=...
```

---

## Data Models

### bulk_jobs table

```sql
CREATE TABLE bulk_jobs (
    id          BIGSERIAL PRIMARY KEY,
    owner_id    BIGINT NOT NULL REFERENCES users(id),
    file_key    TEXT NOT NULL,
    filename    TEXT NOT NULL,
    result_key  TEXT,
    status      TEXT NOT NULL DEFAULT 'pending',
    total_rows  INT NOT NULL DEFAULT 0,
    done_rows   INT NOT NULL DEFAULT 0,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- status enum values: pending | processing | completed | failed
```

### bulk_job_outbox table

```sql
CREATE TABLE bulk_job_outbox (
    id          BIGSERIAL PRIMARY KEY,
    job_id      BIGINT NOT NULL REFERENCES bulk_jobs(id),
    published   BOOLEAN NOT NULL DEFAULT false,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);
```

---

## API Contracts

### GET /api/bulk-jobs/template
- Query: `format=csv` (default) | `format=xlsx`
- Auth: required
- Response: file download

### POST /api/bulk-jobs/upload-url
- Auth: required
- Body: `{"filename": "urls.csv", "row_count": 500, "content_md5": "xMpCOKC5I4INzFCab3WEmw=="}`
- `content_md5`: base64(binary_md5) of the file — computed by frontend via `spark-md5`
- `row_count`: used for pre-validation (≤ 10k) and stored on job record
- Response: `{"presigned_url": "...", "file_key": "bulk/{userID}/{uuid}.csv"}`
- Validation: filename must end in `.csv` or `.xlsx`; row_count ≤ 10000
- Backend: passes `content_md5` as `ContentMD5` in AWS SDK presign → R2 enforces header on upload

### POST /api/bulk-jobs
- Auth: required
- Body: `{"file_key": "bulk/7/abc123.csv", "filename": "urls.csv"}`
- Response: `{"id": 42, "status": "pending", "created_at": "..."}`
- Side effect: INSERT bulk_jobs + INSERT bulk_job_outbox in one transaction

### GET /api/bulk-jobs
- Auth: required
- Query: `limit`, `offset`
- Response: `{"data": [{id, filename, status, total_rows, done_rows, created_at}]}`

### GET /api/bulk-jobs/:id
- Auth: required; must own the job
- Response: `{"id", "status", "total_rows", "done_rows", "result_url": "<presigned GET>", "created_at"}`
- `result_url` is null until status=completed; presigned URL expires in 1h

---

## New Config

```go
// configs/config.go

type R2Config struct {
    AccountID       string `env:"ACCOUNT_ID"`
    AccessKeyID     string `env:"ACCESS_KEY_ID"`
    SecretAccessKey string `env:"SECRET_ACCESS_KEY"`
    Bucket          string `env:"BUCKET" envDefault:"bulk-uploads"`
    // Endpoint: https://<accountID>.r2.cloudflarestorage.com (built from AccountID)
}

// Add to KafkaConfig:
BulkJobTopic      string `env:"BULK_JOB_TOPIC"      envDefault:"bulk-link-jobs"`
BulkConsumerGroup string `env:"BULK_CONSUMER_GROUP" envDefault:"bulk-job-consumer"`
```

Config env prefix: `R2_` → `R2_ACCOUNT_ID`, `R2_ACCESS_KEY_ID`, etc.

---

## Components

### pkg/storage/r2_client.go
- Wraps `aws-sdk-go-v2/service/s3` with Cloudflare R2 endpoint
- `PresignedPutURL(key, contentMD5 string, ttl) (string, error)` — includes ContentMD5 in signature
- `PresignedGetURL(key, ttl) (string, error)` — for result download
- `Download(ctx, key) (io.ReadCloser, error)` — worker downloads file
- `Upload(ctx, key string, r io.Reader, contentType string) error` — worker uploads result (with `SendContentMd5: true`)

**ContentMD5 presign (MinIO SDK v7 — `PresignHeader`):**
```go
headers := http.Header{}
headers.Set("Content-MD5", contentMD5) // base64 — baked into URL signature

presignedURL, err := minioClient.PresignHeader(
    ctx, http.MethodPut, cfg.Bucket, key,
    15*time.Minute, url.Values{}, headers,
)
// R2 returns 403 SignatureDoesNotMatch if frontend sends wrong/missing Content-MD5
```

### internal/repository/bulk_job_repository.go
```go
type BulkJobRepository interface {
    Create(ctx, job *BulkJob) (*BulkJob, error)
    CreateWithOutbox(ctx, job *BulkJob) (*BulkJob, error) // tx: INSERT job + outbox
    GetByID(ctx, id int64) (*BulkJob, error)
    ListByOwner(ctx, ownerID int64, limit, offset int) ([]*BulkJob, error)
    UpdateStatus(ctx, id int64, status string, totalRows, doneRows int) error
    UpdateResult(ctx, id int64, resultKey string, doneRows int) error
    PendingOutbox(ctx) ([]*BulkJobOutbox, error)        // WHERE published=false ORDER BY id LIMIT 50
    MarkOutboxPublished(ctx, id int64) error
}
```

### internal/service/bulk_job_service.go
```go
type BulkJobService interface {
    GetUploadURL(ctx, ownerID int64, filename string) (presignedURL, fileKey string, err error)
    ConfirmUpload(ctx, ownerID int64, fileKey, filename string) (*repository.BulkJob, error)
    GetJob(ctx, id, ownerID int64) (*repository.BulkJob, string, error) // job + result presigned URL
    ListJobs(ctx, ownerID int64, limit, offset int) ([]*repository.BulkJob, error)
    DownloadTemplate(format string) ([]byte, string, error) // content, contentType
}
```

### internal/events/bulk_job_producer.go
- Publishes `BulkJobEvent{JobID, FileKey, OwnerID}` to `bulk-link-jobs` topic
- Called by outbox relay goroutine (not by HTTP handler directly)

### internal/events/bulk_job_consumer.go
- Consumes `bulk-link-jobs` topic (same franz-go pattern as ClickConsumer)
- One message per job → calls `BulkWorker.Process(ctx, jobID)`

### internal/worker/bulk_job_worker.go
```go
type BulkJobWorker struct {
    jobs    repository.BulkJobRepository
    links   service.LinkService
    storage storage.R2Client
}

func (w *BulkJobWorker) Process(ctx, jobID int64) error
// 1. GetByID → validate status=pending
// 2. UpdateStatus → processing
// 3. storage.Download(fileKey)
// 4. Detect format by extension (.csv/.xlsx)
// 5. Parse: CSV via encoding/csv, XLSX via excelize/v2
// 6. For each URL row (skip header): linkSvc.Create() → fill result cell
// 7. Write result to bytes buffer
// 8. storage.Upload(resultKey, buffer, contentType)
// 9. UpdateResult(resultKey, doneRows) → status=completed
// On any fatal error: UpdateStatus(failed)
```

### internal/handler/bulk_job_handler.go
- Standard Echo handler pattern (matches existing link_handler.go)
- `GetUploadURL`, `ConfirmUpload`, `GetJob`, `ListJobs`, `DownloadTemplate`

### cmd/server/bulk_worker.go
- `runBulkWorker()` — mirrors `runAnalyzeConsumer()`
- Wires: R2 client + DB + linkSvc + bulkRepo + consumer

### cmd/server/main.go changes
```go
case "bulk-worker":
    err = runBulkWorker()
```

### cmd/server/server.go changes
- Wire `BulkJobHandler` + routes
- Start outbox relay goroutine:
  ```go
  go outboxRelay(ctx, bulkJobRepo, bulkJobProducer, 5*time.Second)
  ```

---

## Outbox Relay

```go
func outboxRelay(ctx context.Context, repo BulkJobRepository, producer BulkJobProducer, interval time.Duration) {
    t := time.NewTicker(interval)
    defer t.Stop()
    for {
        select {
        case <-ctx.Done():
            return
        case <-t.C:
            pending, _ := repo.PendingOutbox(ctx)
            for _, entry := range pending {
                job, _ := repo.GetByID(ctx, entry.JobID)
                if err := producer.Publish(BulkJobEvent{...}); err != nil {
                    continue // retry next tick
                }
                repo.MarkOutboxPublished(ctx, entry.ID)
            }
        }
    }
}
```

Polls every 5s. Delivery latency: 0–5s after confirm API returns.

---

## New Dependencies

```
github.com/minio/minio-go/v7   // R2 S3-compatible client (presign via PresignHeader)
github.com/xuri/excelize/v2    // XLSX parsing + generation
```

---

## Files to Create

| File | Purpose |
|------|---------|
| `pkg/storage/r2_client.go` | R2 S3-compatible client |
| `internal/repository/bulk_job_repository.go` | DB layer |
| `internal/service/bulk_job_service.go` | Business logic |
| `internal/worker/bulk_job_worker.go` | File processing logic |
| `internal/events/bulk_job_producer.go` | Kafka producer |
| `internal/events/bulk_job_consumer.go` | Kafka consumer |
| `internal/handler/bulk_job_handler.go` | HTTP handlers |
| `cmd/server/bulk_worker.go` | Worker runner |
| `migrations/000010_create_bulk_jobs.up.sql` | Schema |
| `migrations/000010_create_bulk_jobs.down.sql` | Rollback |
| `assets/template.csv` | Downloadable CSV template |
| `assets/template.xlsx` | Downloadable XLSX template |

## Files to Modify

| File | Change |
|------|--------|
| `configs/config.go` | Add `R2Config`, `BulkJobTopic`, `BulkConsumerGroup` |
| `cmd/server/main.go` | Add `"bulk-worker"` case |
| `cmd/server/server.go` | Wire bulk handler, start outbox relay |
| `internal/router/` | Add bulk job routes under `/api/bulk-jobs` |
| `.env.example` | Add `R2_*` vars, `KAFKA_BULK_JOB_TOPIC` |

---

## Error Handling

| Scenario | Behavior |
|----------|----------|
| File not `.csv`/`.xlsx` | 422 at upload-url endpoint |
| `row_count > 10000` | 422 at upload-url endpoint (frontend also validates) |
| MD5 mismatch on upload | R2 returns 400 InvalidDigest — upload never reaches backend |
| R2 download fails | job → `failed`, no result file |
| URL invalid format | write "url không hợp lệ" in result, continue |
| linkSvc.Create() error | write "lỗi xử lý" in result, continue |
| Worker panics | job stays `processing` → needs manual reset or cron |
| Kafka publish fail | outbox relay retries next tick (5s) |

---

## Testing Strategy

- `BulkJobWorker.Process`: unit test with mock linkSvc + mock storage + in-memory CSV/XLSX
- `BulkJobRepository.CreateWithOutbox`: integration test against real PG (tx atomicity)
- `BulkJobHandler`: Echo test recorder (same pattern as existing handler tests)
- Outbox relay: unit test with mock producer that fails first N calls → verify retry

---

## Risks

| Risk | Mitigation |
|------|-----------|
| XLSX memory usage (10k rows) | `excelize` streams rows; cap at 10k rows |
| R2 upload timeout for result file | Set 60s context timeout on worker uploads |
| Worker crash mid-job | job stuck in `processing`; add cron or admin endpoint to reset |
| Duplicate Kafka messages (at-least-once) | Worker checks `status != pending` before processing → idempotent |

---

## Out of Scope (for now)

- Quota deduction per bulk URL
- Progress streaming (WebSocket/SSE) — poll `GET /api/bulk-jobs/:id`
- Notification service (email/webhook on complete)
- Admin reset for stuck jobs
