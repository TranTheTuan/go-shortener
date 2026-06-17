# Phase 05 — Tests + Docs

**Priority:** P1 · **Status:** pending · **Depends:** 04

Unit tests on pure/mockable logic + manual e2e verification. Update README.

## Files

- Create: `pkg/shortcode/shortcode_test.go`
- Create: `internal/service/link_service_test.go`
- Create: `internal/service/analytics_service_test.go`
- Create: `internal/middleware/api_key_test.go`
- Modify: `README.md` (API table + env vars)

## 1. `shortcode_test.go`

- Length == n; all chars in alphabet; many iterations no panic.
- Two calls very likely differ (collision sanity, not strict).

## 2. `link_service_test.go`

Use a hand-written mock implementing `repository.LinkRepository` (table-driven), inject fixed `now`.
- Create: valid http/https URL → success, returns code of configured length.
- Create: invalid URL ("not-a-url", "ftp://x", "") → BadRequest.
- Create: past `expires_at` → BadRequest.
- Create: repo returns `ErrConflict` once then success → retried (assert call count ≥ 2).
- Create: repo `ErrConflict` always → Internal after max retries.
- Resolve: not found → NotFound (apperror.Status 404).
- Resolve: expired (`ExpiresAt` < now) → Gone (410).
- Resolve: valid future/nil expiry → returns link.

Assert via `apperror.As(err)` on `.Status`/`.Code`.

## 3. `analytics_service_test.go`

Mock both repos.
- Record: builds Click with given fields + sets ClickedAt → repo.Create called once.
- Stats: unknown code → NotFound. Known code → TotalClicks + RecentClicks wired from mocks.

## 4. `api_key_test.go`

Echo test (`httptest`):
- valid key → next handler runs (200).
- missing header → 401, code `UNAUTHORIZED`.
- wrong key → 401.
- empty key set → any request 401 (fail-closed).

## 5. Manual e2e (document commands in report, run if DB available)

```bash
make migrate-up
make run &
curl -s -XPOST localhost:8080/api/links -H 'X-API-Key: dev-key-1' \
  -H 'Content-Type: application/json' -d '{"url":"https://example.com"}'
# → grab short_code, then:
curl -sI localhost:8080/<code>          # 302, Location: https://example.com
curl -s localhost:8080/api/links/<code>/stats -H 'X-API-Key: dev-key-1'
curl -sI localhost:8080/api/links -XPOST # no key → 401
curl -sI localhost:8080/healthz          # still 200 (not shadowed)
```

## 6. README

Add shortener endpoints to API table, new env vars to config table.

## Todo

- [ ] shortcode tests
- [ ] link_service tests (incl. retry + expiry)
- [ ] analytics_service tests
- [ ] api_key middleware tests
- [ ] `make test` green
- [ ] Manual e2e (if DB) + README update

## Success Criteria

- `make test` passes, no skipped/failing tests
- Core paths (create/resolve/expiry/retry/auth) covered
- No fake data or cheats to force green (per development rules)
