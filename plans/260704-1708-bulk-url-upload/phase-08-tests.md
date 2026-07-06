# Phase 08 — Tests

## Context Links
- Design spec: `../reports/design-260704-1708-bulk-url-upload.md` (Testing Strategy)
- Pattern refs: existing `*_test.go` alongside repo/service/handler; Echo test recorder pattern; `miniredis` for cache

## Overview
- **Priority:** P2 (validates all prior phases)
- **Status:** pending
- **Description:** Unit tests for worker (mock linkSvc + mock storage), repository tx integration test, handler tests (Echo recorder), outbox relay retry test. No fakes-to-pass-build.

## Key Insights
- Worker is the highest-value target: pure logic given mocked deps. Mock `LinkService` and `R2Client` interfaces.
- `CreateWithOutbox` atomicity needs a real Postgres (matches existing repo integration tests) — check how existing repo tests get a DB (likely a test helper / dockertest / shared DSN). Reuse that harness.
- Handler tests: build handler with a mock `BulkJobService`, use `httptest` + Echo context, assert status + envelope.
- Relay test: mock producer that errors first N calls → assert `MarkOutboxPublished` only after success.

## Requirements
- All tests pass (`go test ./...`); no skipped/failing.
- Cover: success row, invalid URL row, dedup row, XLSX + CSV parse, worker idempotency (status!=pending → no-op), relay retry.

## Architecture / Design
Define local mocks in `_test.go` files (interfaces already small):
```go
type mockLinkSvc struct{ createFn func(ctx, in) (*repository.Link, bool, error) }
type mockStorage struct{ downloadFn ...; uploadFn ...; ... }
type mockBulkRepo struct{ ... }
type mockProducer struct{ failN int; calls int }
```

## Related Code Files
- **Create:** `internal/worker/bulk_job_worker_test.go`, `internal/worker/bulk_file_parser_test.go`,
  `internal/repository/bulk_job_repository_test.go`, `internal/handler/bulk_job_handler_test.go`,
  `cmd/server/outbox_relay_test.go`

## Implementation Steps
1. **Parser test:** round-trip CSV `[[url,result],[https://a.com,]]` → Read → Write → Read equal. Same for XLSX via `excelize` in-memory.
2. **Worker success:** mock storage returns a 3-row CSV (valid, invalid, dup); mock linkSvc returns code for valid, `apperror.BadRequest` for invalid, existing code for dup. Assert result rows = `["base/AbCd", "url không hợp lệ", "base/DupCode"]`; assert `UpdateResult(completed, doneRows=3)`; assert `Upload` called with correct key.
3. **Worker idempotency:** repo `GetByID` returns `status=processing` → `Process` returns nil, no download/upload calls.
4. **Worker failure:** storage.Download errors → `UpdateStatus(failed)` called, error returned.
5. **Repo tx test (integration):** real PG; `CreateWithOutbox` → assert 1 job + 1 outbox row; force failure (e.g., invalid FK owner) → assert neither row persists.
6. **Handler tests:** for each endpoint — 401 without user, validation 422 (bad ext / rowCount>10k / empty md5), happy path 200/201 with mock svc. Ownership: `GetJob` for non-owner → 404.
7. **Relay test:** 2 pending entries; mock producer fails first call, succeeds after; run one/two ticks (inject interval or call inner step directly — refactor `outboxRelay` to expose a `relayOnce(ctx, repo, producer)` for testability). Assert `MarkOutboxPublished` count matches successes.
8. `go test ./...` green.

## Todo List
- [ ] Parser round-trip tests (CSV + XLSX)
- [ ] Worker: success / idempotent / failure
- [ ] Repo `CreateWithOutbox` tx integration test
- [ ] Handler tests (401/422/happy/ownership) for all 5 endpoints
- [ ] Outbox relay retry test (`relayOnce` seam)
- [ ] `go test ./...` passes

## Success Criteria
- All tests pass locally; worker + handler branches covered; tx atomicity proven.

## Risk Assessment
- **Integration DB availability:** if CI lacks Postgres, gate repo test behind build tag / skip-if-no-DSN like existing tests (mirror current convention — check before writing).
- **excelize in-memory:** use `f.WriteToBuffer()` / `excelize.OpenReader(bytes.NewReader(...))` — no temp files.
- **Refactor for test seam:** extracting `relayOnce` keeps relay testable without sleeping on tickers.

## Unresolved Questions
- Does the repo test harness exist (shared test DSN / helper)? Inspect an existing `*_repository_test.go` before writing phase-08 repo test; reuse, don't invent.
