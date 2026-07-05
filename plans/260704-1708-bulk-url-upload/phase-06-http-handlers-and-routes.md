# Phase 06 — HTTP Handlers & Routes

## Context Links
- Design spec: `../reports/design-260704-1708-bulk-url-upload.md` (API Contracts, bulk_job_handler.go)
- Pattern refs: `internal/handler/link_handler.go`, `internal/router/router.go`, `internal/middleware` (`UserIDFrom`), `pkg/response`, `pkg/apperror`

## Overview
- **Priority:** P1
- **Status:** pending
- **Description:** Echo handler with 5 endpoints under `/api/bulk-jobs` (auth required via existing `keycloakMW`). Wire into router `Handlers`/`registerRoutes`.

## Key Insights
- Auth: `appmw.UserIDFrom(c)` returns `(int64, bool)` — same as link handler. All endpoints require it → 401 if absent.
- Responses use `response.Success(c, status, data)` / `response.Error(c, err)` envelope.
- **422:** `apperror` lacks an UnprocessableEntity helper. Decision: use `apperror.BadRequest` (400) for filename/row_count validation to avoid touching shared pkg (KISS). If 422 semantics required, add one helper `apperror.UnprocessableEntity(msg)` mirroring `BadRequest` — small, contained. **Chosen: add the helper** (spec explicitly says 422).
- Template download: return raw bytes with `Content-Disposition` attachment header via `c.Blob(200, contentType, data)`.

## Requirements
- **Functional:** 5 endpoints matching API contracts.
- **Non-functional:** ownership enforced in service (`GetJob`); handler stays thin.

## Architecture / Design
```go
type BulkJobHandler struct { svc service.BulkJobService }
func NewBulkJobHandler(svc service.BulkJobService) *BulkJobHandler

// GET  /api/bulk-jobs/template?format=csv|xlsx   -> DownloadTemplate
// POST /api/bulk-jobs/upload-url {filename,row_count,content_md5} -> GetUploadURL
// POST /api/bulk-jobs {file_key,filename,row_count} -> ConfirmUpload
// GET  /api/bulk-jobs?limit&offset               -> ListJobs
// GET  /api/bulk-jobs/:id                         -> GetJob
```

## Related Code Files
- **Create:** `internal/handler/bulk_job_handler.go`
- **Modify:** `internal/router/router.go` (add `BulkJob` to `Handlers`, register routes), `pkg/apperror` (add `UnprocessableEntity` helper)

## Implementation Steps
1. Add to `pkg/apperror`:
   ```go
   func UnprocessableEntity(message string) *Error {
       return New(http.StatusUnprocessableEntity, "UNPROCESSABLE", message)
   }
   ```
2. Handler request/response structs:
   ```go
   type uploadURLRequest  struct { Filename string `json:"filename"`; RowCount int `json:"row_count"`; ContentMD5 string `json:"content_md5"` }
   type uploadURLResponse struct { PresignedURL string `json:"presigned_url"`; FileKey string `json:"file_key"` }
   type confirmRequest    struct { FileKey string `json:"file_key"`; Filename string `json:"filename"`; RowCount int `json:"row_count"` }
   ```
3. Each handler: bind → `owner, ok := appmw.UserIDFrom(c)` (else 401) → call service → `response.Success/Error`.
   - `GetUploadURL`: validate `content_md5` non-empty (else `UnprocessableEntity`), pass through; service validates ext + rowCount.
   - `ConfirmUpload`: returns `{id, status, created_at}` (201).
   - `GetJob`: `id := strconv.ParseInt(c.Param("id"))`; returns job + `result_url` (null until completed).
   - `ListJobs`: parse `limit`/`offset` (reuse `atoiDefault`-style; note `atoiDefault` is unexported in handler pkg → reuse it directly, same package).
   - `DownloadTemplate`: `format := c.QueryParam("format")`; service returns bytes+ct+filename; set `c.Response().Header().Set(echo.HeaderContentDisposition, "attachment; filename=\""+fn+"\"")`; `c.Blob(200, ct, data)`.
4. Add swagger annotations (match link_handler style) — optional but consistent.
5. Router: add `BulkJob *handler.BulkJobHandler` to `Handlers`; in `registerRoutes` under existing `api := e.Group("/api", keycloakMW)`:
   ```go
   bulk := api.Group("/bulk-jobs")
   bulk.GET("/template", h.BulkJob.DownloadTemplate)
   bulk.POST("/upload-url", h.BulkJob.GetUploadURL)
   bulk.POST("", h.BulkJob.ConfirmUpload)
   bulk.GET("", h.BulkJob.ListJobs)
   bulk.GET("/:id", h.BulkJob.GetJob)
   ```
   Register `/template` before `/:id`? Different path segments (`/template` vs `/:id`) — Echo distinguishes literal from param; literal wins. OK.
6. `go build ./...`.

## Todo List
- [ ] Add `apperror.UnprocessableEntity`
- [ ] Handler structs + `NewBulkJobHandler`
- [ ] 5 handler methods (thin, auth-guarded)
- [ ] Add `BulkJob` to router `Handlers` + register routes
- [ ] `go build ./...` passes

## Success Criteria
- All 5 routes resolve; 401 without token; validation returns 422; ownership enforced (404 for others' jobs).

## Risk Assessment
- **Route ordering:** `/api/bulk-jobs/:id` vs `/api/bulk-jobs/template` — literal precedence in Echo handles it; verify with a quick route test.
- **Shared pkg edit:** `apperror` change is additive/backward-compatible.

## Unresolved Questions
- Should `ListJobs` return total count (like links `listResponse`)? Spec omits it. Default: return `{data:[...]}` without total (YAGNI) — confirm with frontend.
