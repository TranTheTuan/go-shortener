---
phase: 4
status: pending
priority: P1
depends_on: phase-02
---

# Phase 4: Bulk Upload Quota Integration

## Context Links
- Spec: [spec-260714-1418-billing-subscription.md](../reports/spec-260714-1418-billing-subscription.md)
- Phase 2: [phase-02-quota-billing-service.md](./phase-02-quota-billing-service.md)

## Overview
Add quota gate to `BulkJobService.ConfirmUpload` (primary check) and
`BulkJobWorker.Process` (safety net). Add `IsStatus` helper to `apperror`
so workers can distinguish permanent vs transient failures for Kafka commit logic.

## Key Insights
- `ConfirmUpload` is the UX gate — catches most cases before job is created
- `BulkJobWorker.Process` safety net catches race: link creation between ConfirmUpload and worker run
- **Permanent failure (quota) → return nil** so Kafka commits offset and doesn't redeliver
- **Transient failure (DB/storage) → return error** so Kafka redelivers
- `IsStatus` helper missing from `pkg/apperror` — needs to be added
- No quota pre-reservation at ConfirmUpload time — no refund logic needed on job failure
- `bulkJobService` needs `quota QuotaService` field added (constructor change)

## Requirements
- Add `IsStatus(err error, status int) bool` to `pkg/apperror/apperror.go`
- Add `quota QuotaService` field to `bulkJobService` + update `NewBulkJobService`
- `ConfirmUpload`: check `quota.Remaining()` → 429 if `rowCount > remaining`
- `BulkJobWorker.Process`: safety net check + nil return for permanent quota failure

## Architecture

```
BulkJobService.ConfirmUpload
    └── quota.Remaining(ctx, ownerID)
        └── rowCount > remaining → apperror.TooManyRequests (429)

BulkJobWorker.Process
    └── process(ctx, job)
        ├── quota.Remaining() safety net check
        └── BatchCreate per-URL via Allow() [existing]

    if apperror.IsStatus(err, 429):
        UpdateStatus(failed)
        return nil  // Kafka commits offset, no retry
    else if err != nil:
        return err  // Kafka redelivers
```

## Related Code Files

Modify:
- `pkg/apperror/apperror.go`
- `internal/service/bulk_job_service.go`
- `internal/worker/bulk_job_worker.go`

## Implementation Steps

### 1. Add IsStatus to pkg/apperror/apperror.go

```go
// IsStatus reports whether err is an *Error with the given HTTP status code.
func IsStatus(err error, status int) bool {
    if appErr, ok := As(err); ok {
        return appErr.Status == status
    }
    return false
}
```

### 2. bulkJobService — add quota field

Current constructor:
```go
func NewBulkJobService(jobs repository.BulkJobRepository, s storage.R2Client, baseURL string) BulkJobService
```

Updated:
```go
func NewBulkJobService(
    jobs    repository.BulkJobRepository,
    s       storage.R2Client,
    baseURL string,
    quota   QuotaService,
) BulkJobService {
    return &bulkJobService{jobs: jobs, storage: s, baseURL: baseURL, quota: quota}
}
```

Update `bulkJobService` struct and call site in `cmd/server/main.go`.

### 3. BulkJobService.ConfirmUpload — primary quota gate

Add at the top of `ConfirmUpload`, after input validation:
```go
remaining := s.quota.Remaining(ctx, ownerID)
if rowCount > remaining {
    return nil, apperror.TooManyRequests(
        fmt.Sprintf("quota insufficient: need %d slots, only %d remaining today", rowCount, remaining),
    )
}
```

### 4. BulkJobWorker — add quota field

Current worker struct needs `quota QuotaService` field. Update constructor accordingly.

### 5. BulkJobWorker.process — safety net check

Add at start of `process()` inner function:
```go
remaining := w.quota.Remaining(ctx, job.OwnerID)
if job.TotalRows > remaining {
    // ponytail: safety net — quota may have changed since ConfirmUpload
    return apperror.TooManyRequests("quota exceeded at processing time")
}
```

### 6. BulkJobWorker.Process — permanent vs transient failure

```go
if err := w.process(ctx, job); err != nil {
    _ = w.jobs.UpdateStatus(ctx, jobID, repository.BulkJobStatusFailed, job.TotalRows)
    if apperror.IsStatus(err, http.StatusTooManyRequests) {
        // Permanent: quota exceeded. Return nil → Kafka commits offset, no retry.
        return nil
    }
    return err // Transient: Kafka redelivers.
}
```

### 7. Update constructor call in cmd/server/main.go

Pass `quotaSvc` to `NewBulkJobService(...)`.

## Todo List

- [ ] Add `IsStatus(err error, status int) bool` to `pkg/apperror/apperror.go`
- [ ] Add `quota QuotaService` field to `bulkJobService` struct
- [ ] Update `NewBulkJobService` signature + all call sites
- [ ] Add quota check in `ConfirmUpload` (primary gate)
- [ ] Add `quota QuotaService` field to `BulkJobWorker` struct + update constructor
- [ ] Add safety net quota check in `BulkJobWorker.process()`
- [ ] Add permanent/transient failure split in `BulkJobWorker.Process()`
- [ ] Run `go build ./...` — no compile errors

## Success Criteria
- `ConfirmUpload` returns 429 when `rowCount > quota.Remaining()`
- `BulkJobWorker.Process` returns nil (no Kafka retry) on quota exceeded
- `BulkJobWorker.Process` returns error (Kafka retries) on DB/storage failures
- No change to existing `Allow()` per-URL flow inside `BatchCreate`

## Risk Assessment
- Constructor signature change: `NewBulkJobService` is called once in `main.go` — low blast radius
- `Remaining()` fails open to `math.MaxInt` on Redis down — bulk upload proceeds, consistent with `Allow()`

## Security Considerations
- No quota bypass: safety net in worker ensures quota is checked even if ConfirmUpload was bypassed

## Next Steps
- Phase 5: tests covering all new logic paths
