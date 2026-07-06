# Phase 09 — Frontend Bulk-Upload UI

## Context Links
- Plan: [plan.md](plan.md)
- Backend contract (done): `internal/handler/bulk_job_handler.go`, `internal/service/bulk_job_service.go`, `internal/router/router.go:93-99`
- Frontend to extend: `web/index.html`, `web/static/app.js`, `web/static/styles.css`
- Prior pattern: `wireLinks`/`wireCreateForm` in `app.js` (same `api()` Bearer helper)

## Overview
- **Priority:** P2 (feature already usable via API; this is the UX layer)
- **Status:** done (md5.js vendored+verified, bulk.js/index.html/app.js wired, R2 CORS documented, reviewed)
- **Backend:** unchanged. Pure frontend + one vendored JS file + one ops note.

The full bulk backend exists (presign → confirm → outbox → worker → result). There is no UI;
today the only client is curl. Add a "Bulk upload" section to the existing vanilla page that walks
the 4-step flow and polls job status.

## Key Insights (from code, not assumptions)
1. **No MD5 in the browser.** `crypto.subtle.digest` supports only SHA-*. Backend
   `GetUploadURL` returns 422 when `content_md5` is empty and R2 rejects the PUT if the
   `Content-MD5` header doesn't match the signature (`bulk_job_service.go:54`, `r2_client.go:52`).
   → Must compute MD5 client-side. Vendor a tiny standalone MD5 (no npm/build step exists — project
   is `go:embed` vanilla). Output must be **base64 of the 16 raw bytes**, not hex.
2. **R2 PUT is cross-origin + preflighted.** URL host is `<account>.r2.cloudflarestorage.com`; the
   `Content-MD5` request header is non-simple → browser sends a CORS preflight `OPTIONS`. curl skips
   CORS (why curl "worked" modulo TLS); the browser will hard-fail without bucket CORS.
   → **Ops prerequisite:** configure R2 bucket CORS to allow `PUT`, `AllowedHeaders: [content-md5, content-type]`,
   and the app origin. Not code — document in deployment guide. Without it the upload PUT fails.
3. **XLSX row-count needs a heavy parser.** To send `row_count` for xlsx the client would have to
   parse the workbook (SheetJS ~ hundreds of KB). CSV row-count = count non-empty lines − header.
   → **UI upload is CSV-only in v1.** Template download still offers csv **and** xlsx; xlsx upload stays
   available via API/curl. Flag as YAGNI-deferred.
4. **Bulk list shape differs from links.** `ListJobs` returns `data` as a **bare array**
   (`bulk_job_handler.go:167`), whereas `/api/links` returns `data:{items,total}`. Don't copy the
   links pager logic blindly.
5. The R2 PUT must **not** go through the `api()` helper — no Bearer to R2, and it's cross-origin.
   Use a plain `fetch(presignedUrl, {method:"PUT", headers:{"Content-MD5":md5}, body:file})`.

## API Contract (all same-origin, Bearer, except the R2 PUT)
| Step | Call | Body / Params | Response |
|------|------|---------------|----------|
| Template | `GET /api/bulk-jobs/template?format=csv\|xlsx` | – | file blob (cols `url,result`) |
| 1. Presign | `POST /api/bulk-jobs/upload-url` | `{filename, row_count, content_md5}` | `data:{presigned_url, file_key}` |
| 2. Upload | `PUT <presigned_url>` (R2, cross-origin) | header `Content-MD5: <b64>`, body = file bytes | 200, empty |
| 3. Confirm | `POST /api/bulk-jobs` | `{file_key, filename, row_count}` | 201 `data:{id,status,total_rows,created_at}` |
| 4. Poll | `GET /api/bulk-jobs/:id` | – | `data:{id,status,done_rows,total_rows,result_url,created_at}` |
| List | `GET /api/bulk-jobs?limit&offset` | – | `data:[ {…job} ]` (bare array) |

Limits: `row_count` 1..10000; presign TTL 15m; result URL TTL 1h.
Terminal statuses: `completed` (has `result_url`), `failed`. Non-terminal: `pending`, `processing`.

## Related Code Files
**Create**
- `web/static/md5.js` — vendored minimal MD5. Export `md5Base64(arrayBuffer) -> string`.
  Use a known-good tiny implementation (e.g. SparkMD5's `ArrayBuffer` path, `.end(true)` → raw
  binary string → `btoa`). ~ single file, no deps. `ponytail:` vendored, upgrade to Web Crypto if
  it ever ships MD5 (it won't) or if backend drops the MD5 requirement.

**Modify**
- `web/index.html` — add a `<section id="bulk">` inside `#signed-in` (after the links section):
  - "Download template" row: two buttons (csv / xlsx) → hit template endpoint, trigger browser download.
  - Upload form: `<input type="file" accept=".csv">`, submit button, `#bulk-status` line.
  - `#bulk-jobs` table: id, status, progress (`done/total`), created, result (download link when completed).
- `web/static/app.js` — add `wireBulk(api)`; call it from `renderSignedIn` alongside `wireLinks`.
- `web/static/styles.css` — reuse existing classes; add a `.progress`/small tweak only if needed (optional).

## Implementation Steps
1. **md5.js**: drop in the vendored MD5. Verify `md5Base64` against a known vector
   (`"" -> 1B2M2Y8AsgTpgAmY7PhCfg==`, `"abc" -> kAFQmDzST7DWlj99KOF/cg==`).
2. **Template download** (`downloadTemplate(format)`): `const res = await api(url)` → `res.blob()` →
   object URL → click a temp `<a download>`. Revoke the URL after.
3. **Upload flow** (`uploadCsv(file)`):
   a. Read file as ArrayBuffer (`await file.arrayBuffer()`).
   b. `row_count`: decode as text, split on `\n`, drop empty lines, subtract 1 for the header. Guard
      `1..10000`; show inline error otherwise (don't round-trip a doomed request).
   c. `content_md5 = md5Base64(buf)`.
   d. POST `upload-url` `{filename:file.name, row_count, content_md5}` → `{presigned_url, file_key}`.
   e. `PUT presigned_url` (plain fetch, `Content-MD5` header, body = `buf`/`file`). On non-2xx surface
      "Upload failed (check R2 CORS / link expiry)".
   f. POST confirm `{file_key, filename:file.name, row_count}` → job id.
   g. Kick off polling for that id; reload the jobs list.
4. **Poll** (`pollJob(id)`): `setInterval` every 3s; stop on `completed`/`failed` or after 40 ticks
   (~2 min) → then reload list. Update `#bulk-status` with `processing done/total`.
5. **Jobs list** (`loadJobs`): `GET /api/bulk-jobs?limit=20&offset=0`; `json.data` is the array.
   Render rows; when `status==="completed"` and `result_url`, render a download `<a>` (textContent, no
   innerHTML — match the XSS-safe pattern already in `app.js`).
6. Wire `wireBulk(api)` in `renderSignedIn`. Section only meaningful when backend has R2 enabled; if
   `GET /api/bulk-jobs` 404s (routes absent), hide the section quietly.
7. **Ops:** add R2 CORS config to `docs/deployment-guide.md` (allowed origin, `PUT`, `Content-MD5`).

## Todo
- [ ] `web/static/md5.js` vendored + self-check against known vectors
- [ ] `index.html` bulk section markup
- [ ] `app.js` `wireBulk`: template download
- [ ] `app.js` upload flow (row-count + md5 + presign + PUT + confirm)
- [ ] `app.js` poll + jobs list render
- [ ] hide section when routes absent (R2 disabled)
- [ ] `docs/deployment-guide.md` R2 CORS note

## Success Criteria
- Signed-in user downloads a template, fills it, uploads a CSV, sees the job appear as `pending`→
  `processing`→`completed`, and downloads the result file — all in-browser, no curl.
- Empty/oversized CSV rejected client-side with a clear message (no server round-trip).
- No new backend code; no build step; XSS-safe DOM (textContent only).

## Risks & Mitigations
- **R2 CORS missing** → PUT fails in browser. Mitigation: ops note + explicit error message pointing at CORS.
- **MD5 mismatch** (encoding bug) → R2 400 on PUT. Mitigation: self-check md5.js against known vectors before wiring.
- **Presign expiry (15m)** if user dawdles between presign and PUT. Mitigation: presign immediately before the PUT, single click.
- **Poll never terminates** (worker down) → capped at 40 ticks, then stop + "still processing, refresh later".

## Out of Scope (YAGNI)
- XLSX upload from the UI (template + API path remain). 
- SSE/websocket live progress (polling is enough).
- Client-side URL validation of each row (worker + `linkSvc.Create` already validate).
- Drag-and-drop, multi-file, resumable upload.

## Unresolved Questions
- None. **Decided:** MD5 stays mandatory on the client — vendor `md5.js`, backend `content_md5`
  requirement unchanged.
