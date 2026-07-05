# Phase 04 — Service & Worker

## Context Links
- Design spec: `../reports/design-260704-1708-bulk-url-upload.md` (bulk_job_service.go, bulk_job_worker.go, Error Handling)
- Pattern refs: `internal/service/link_service.go` (validation, `apperror`, dedup via `Create`), phase-02 `R2Client`, phase-03 repo

## Overview
- **Priority:** P1
- **Status:** pending
- **Description:** Two components. `BulkJobService` — presign URL, confirm (tx create), get/list, template. `BulkJobWorker` — download, parse CSV/XLSX, shorten each row via `linkSvc.Create`, write result, upload, mark completed.

## Key Insights
- `linkSvc.Create` already validates URL + dedups (reuse existing short code) + enforces nothing extra when `QuotaExhausted=false` → worker passes `QuotaExhausted:false` (no quota per spec).
- Result strings: success = `baseURL + "/" + code`; invalid URL → `url không hợp lệ`; other error → `lỗi xử lý`. Worker distinguishes via `apperror.As` (400 = invalid).
- **Idempotency:** worker processes only `status==pending`; else no-op (guards Kafka duplicates + reprocessing).
- File key format: `bulk/{ownerID}/{uuid}.{ext}`. Result key: `bulk/{ownerID}/{uuid}-result.{ext}`.
- Format detection by extension only (fixed template — KISS).
- Template files served from `assets/` (see phase 06). Service `DownloadTemplate` reads embedded bytes.

## Requirements
- **Functional:** presign + confirm + get(+result URL) + list + template; worker end-to-end processing.
- **Non-functional:** cap 10k rows; 60s upload timeout; stream XLSX rows (excelize `Rows()` iterator, not `GetRows` for large files — but 10k×2 cols is fine either way, prefer streaming writer for output).

## Architecture / Design
```go
type BulkJobService interface {
    GetUploadURL(ctx, ownerID int64, filename, contentMD5 string, rowCount int) (url, fileKey string, err error)
    ConfirmUpload(ctx, ownerID int64, fileKey, filename string, rowCount int) (*repository.BulkJob, error)
    GetJob(ctx, id, ownerID int64) (*repository.BulkJob, string, error) // job, resultURL
    ListJobs(ctx, ownerID int64, limit, offset int) ([]*repository.BulkJob, error)
    DownloadTemplate(format string) (data []byte, contentType, filename string, err error)
}

type BulkJobWorker struct {
    jobs    repository.BulkJobRepository
    links   service.LinkService
    storage storage.R2Client
    baseURL string
}
func (w *BulkJobWorker) Process(ctx context.Context, jobID int64) error
```

## Related Code Files
- **Create:** `internal/service/bulk_job_service.go`, `internal/worker/bulk_job_worker.go`,
  `internal/worker/bulk_file_parser.go` (CSV/XLSX read+write helpers — keeps worker <200 lines, DRY)

## Implementation Steps — Service
1. Constants: `maxRows = 10000`, `presignPutTTL = 15*time.Minute`, `resultGetTTL = time.Hour`.
2. `GetUploadURL`: validate ext (`.csv`/`.xlsx`) → else `apperror.BadRequest`; validate `rowCount <= maxRows`; build `fileKey = fmt.Sprintf("bulk/%d/%s%s", ownerID, uuid, ext)`; call `storage.PresignedPutURL(ctx, fileKey, contentMD5, presignPutTTL)`.
3. `ConfirmUpload`: build `BulkJob{OwnerID, FileKey, Filename, Status: pending, TotalRows: rowCount}` → `jobs.CreateWithOutbox`.
4. `GetJob`: `jobs.GetByID`; verify `job.OwnerID == ownerID` else `apperror.NotFound` (don't leak); if `status==completed && ResultKey!=""` → `storage.PresignedGetURL(ctx, ResultKey, resultGetTTL)`, else empty.
5. `ListJobs`: clamp paging (reuse `service.ClampPaging`), `jobs.ListByOwner`.
6. `DownloadTemplate`: switch format → return embedded `assets/template.csv|xlsx` bytes + content-type + filename.

## Implementation Steps — Worker + parser
7. `Process`:
   ```
   job := jobs.GetByID(id); if job.Status != pending { return nil }   // idempotent
   jobs.UpdateStatus(id, processing, job.TotalRows)
   rc := storage.Download(ctx, job.FileKey); defer rc.Close()
   rows := parser.Read(rc, ext)          // [][]string incl header
   for i, row := range rows[1:] {        // skip header
       result := w.shorten(ctx, job.OwnerID, row[0])
       rows[i+1] = []string{row[0], result}; done++
   }
   buf, ct := parser.Write(rows, ext)
   resultKey := deriveResultKey(job.FileKey)
   upCtx, cancel := context.WithTimeout(ctx, 60*time.Second); defer cancel()
   storage.Upload(upCtx, resultKey, buf, int64(buf.Len()), ct)
   jobs.UpdateResult(id, resultKey, done)   // sets completed
   ```
   On any fatal error (download/parse/upload) → `jobs.UpdateStatus(id, failed, job.TotalRows)` and return err.
8. `shorten(ctx, ownerID, rawURL)`:
   ```go
   link, _, err := w.links.Create(ctx, service.CreateLinkInput{URL: rawURL, OwnerID: &ownerID})
   if err == nil { return w.baseURL + "/" + link.ShortCode }
   if ae, ok := apperror.As(err); ok && ae.Status == http.StatusBadRequest { return "url không hợp lệ" }
   return "lỗi xử lý"
   ```
9. `bulk_file_parser.go`: `Read(r io.Reader, ext string) ([][]string, error)` — CSV via `encoding/csv`, XLSX via `excelize.OpenReader` + `GetRows(sheet)`. `Write(rows [][]string, ext string) (*bytes.Buffer, string, error)` — CSV via `csv.Writer`, XLSX via `excelize.NewFile` + `SetSheetRow` + `WriteTo`.
10. `go get github.com/xuri/excelize/v2`; `go build ./...`.

## Todo List
- [ ] Service constants + `GetUploadURL` (ext + rowCount validation)
- [ ] `ConfirmUpload` (CreateWithOutbox)
- [ ] `GetJob` (ownership + result presign) / `ListJobs`
- [ ] `DownloadTemplate`
- [ ] `bulk_file_parser.go` Read/Write (CSV + XLSX)
- [ ] Worker `Process` + `shorten` + idempotency guard + failed-status path
- [ ] `go get excelize/v2`; `go build ./...` passes

## Success Criteria
- Service methods compile and enforce validation.
- Worker processes an in-memory CSV/XLSX end-to-end in unit test (phase 08): success, invalid, dedup rows.

## Risk Assessment
- **Memory:** 10k rows fully in memory ~ small (<10MB). Acceptable; cap enforced upstream.
- **`apperror.As` signature:** confirm it returns `(*Error, bool)` (it does) and `.Status` field exists.
- **UUID:** need a UUID source — use `crypto/rand` hex (avoid new dep) or check if a uuid lib already vendored. Prefer stdlib rand-hex helper (DRY: put in service).
- **Partial failure:** if upload succeeds but `UpdateResult` fails, job stays processing → redelivery reprocesses (idempotent guard blocks since status now processing → **bug**). Mitigation: guard on `pending` only; a processing job that failed to finalize needs manual/cron reset (out of scope, documented).

## Unresolved Questions
- UUID: stdlib rand-hex vs add `google/uuid`? (Lean stdlib unless uuid already present.)
- excelize streaming writer (`NewStreamWriter`) vs `SetSheetRow` — 10k rows fine with either; pick simpler `SetSheetRow`.
