# Phase 03 — Repository Layer (job + outbox tx)

## Context Links
- Design spec: `../reports/design-260704-1708-bulk-url-upload.md` (bulk_job_repository.go)
- Pattern refs: `internal/repository/link_repository.go`, `internal/repository/click_repository.go`, `pkg/apperror`

## Overview
- **Priority:** P1
- **Status:** pending
- **Description:** GORM-backed `BulkJobRepository` with entities `BulkJob` + `BulkJobOutbox`. Key operation: `CreateWithOutbox` (single tx). Plus status/result updates and outbox poll/mark.

## Key Insights
- Follow exact repo shape: exported interface + unexported struct + `NewX(db)` constructor; `r.db.WithContext(ctx)`.
- GORM tags must match phase-01 SQL. Table names inferred: `bulk_jobs`, `bulk_job_outboxes` — **override** outbox table name via `TableName()` to `bulk_job_outbox` (singular, matches migration).
- `CreateWithOutbox` uses `db.Transaction(func(tx) error {...})` — insert job, then outbox row referencing `job.ID`. Atomicity is the whole point (design requirement).
- Status constants live here (single source of truth).

## Requirements
- **Functional:** CRUD + tx create + outbox poll/mark.
- **Non-functional:** poll bounded (`LIMIT 50`), ordered by id; updates set `updated_at`.

## Architecture / Design
```go
const (
    StatusPending    = "pending"
    StatusProcessing = "processing"
    StatusCompleted  = "completed"
    StatusFailed     = "failed"
)

type BulkJob struct {
    ID        int64  `gorm:"primaryKey" json:"id"`
    OwnerID   int64  `gorm:"index;not null" json:"owner_id"`
    FileKey   string `gorm:"not null" json:"file_key"`
    Filename  string `gorm:"not null" json:"filename"`
    ResultKey string `json:"result_key,omitempty"`
    Status    string `gorm:"size:20;not null;default:pending" json:"status"`
    TotalRows int    `gorm:"not null;default:0" json:"total_rows"`
    DoneRows  int    `gorm:"not null;default:0" json:"done_rows"`
    CreatedAt time.Time `json:"created_at"`
    UpdatedAt time.Time `json:"updated_at"`
}

type BulkJobOutbox struct {
    ID        int64 `gorm:"primaryKey" json:"id"`
    JobID     int64 `gorm:"not null" json:"job_id"`
    Published bool  `gorm:"not null;default:false" json:"published"`
    CreatedAt time.Time `json:"created_at"`
}
func (BulkJobOutbox) TableName() string { return "bulk_job_outbox" }

type BulkJobRepository interface {
    CreateWithOutbox(ctx context.Context, job *BulkJob) (*BulkJob, error)
    GetByID(ctx context.Context, id int64) (*BulkJob, error)
    ListByOwner(ctx context.Context, ownerID int64, limit, offset int) ([]*BulkJob, error)
    UpdateStatus(ctx context.Context, id int64, status string, totalRows int) error
    UpdateResult(ctx context.Context, id int64, resultKey string, doneRows int) error
    PendingOutbox(ctx context.Context) ([]*BulkJobOutbox, error)
    MarkOutboxPublished(ctx context.Context, id int64) error
}
```

## Related Code Files
- **Create:** `internal/repository/bulk_job_repository.go`

## Implementation Steps
1. Define constants, `BulkJob`, `BulkJobOutbox` (+ `TableName`), interface, struct, `NewBulkJobRepository(db)`.
2. `CreateWithOutbox`:
   ```go
   err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
       if err := tx.Create(job).Error; err != nil { return err }
       return tx.Create(&BulkJobOutbox{JobID: job.ID}).Error
   })
   if err != nil { return nil, err }
   return job, nil
   ```
3. `GetByID`: `First(&job, id)`; map `gorm.ErrRecordNotFound` → `ErrNotFound`.
4. `ListByOwner`: `Where("owner_id = ?", ownerID).Order("created_at DESC").Limit(limit).Offset(offset).Find(&out)`.
5. `UpdateStatus`: `Model(&BulkJob{}).Where("id = ?", id).Updates(map[string]any{"status": status, "total_rows": totalRows, "updated_at": time.Now()})`. (GORM auto-updates `updated_at` on struct updates, but map updates need explicit set.)
6. `UpdateResult`: set `result_key`, `done_rows`, `status=completed`, `updated_at`.
7. `PendingOutbox`: `Where("published = ?", false).Order("id").Limit(50).Find(&rows)`.
8. `MarkOutboxPublished`: `Model(&BulkJobOutbox{}).Where("id = ?", id).Update("published", true)`.
9. `go build ./...`.

## Todo List
- [ ] Entities + status constants + `TableName()`
- [ ] Interface + constructor
- [ ] `CreateWithOutbox` (tx)
- [ ] `GetByID` / `ListByOwner`
- [ ] `UpdateStatus` / `UpdateResult`
- [ ] `PendingOutbox` / `MarkOutboxPublished`
- [ ] `go build ./...` passes

## Success Criteria
- Compiles; `CreateWithOutbox` inserts both rows or neither (verified in phase-08 integration test).

## Risk Assessment
- **Table-name mismatch:** GORM pluralizes to `bulk_job_outboxes`; `TableName()` override prevents "relation does not exist". Double-check.
- **`ErrNotFound` mapping** consistent with existing repos so service/handler map to 404.

## Unresolved Questions
- None.
