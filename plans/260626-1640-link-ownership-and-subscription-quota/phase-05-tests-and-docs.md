# Phase 05 — Tests & Docs

**Context:** [plan.md](plan.md) · [spec](../reports/spec-260626-1538-link-ownership-and-subscription-quota.md)

## Overview
- **Priority:** High (DoD gate)
- **Status:** pending
- Unit tests (Part A + Part B) + README + Swagger. No fake/skipped tests.

## Related Code Files
- **Create:** `internal/middleware/authn_test.go`, `internal/service/quota_service_test.go`,
  `internal/service/dedup_cache_test.go`, `internal/middleware/quota_check_test.go`,
  `internal/middleware/duplicate_url_check_test.go`
- **Modify:** `internal/service/link_service_test.go` + `mocks_test.go` (per-owner dedup, dedup cache),
  `README.md`, Swagger (`make swag`), `go.mod` (add `alicebob/miniredis/v2` test dep)

## Implementation Steps

1. **Add test dep:** `go get github.com/alicebob/miniredis/v2` && `go mod tidy`.

2. **Part A tests:**
   - `authn_test.go`: valid JWT → `user_id` set + next; valid API key → next, no `user_id`; neither → 401; bad JWT → 401.
   - `link_service_test.go`: per-owner dedup — same owner+URL reuses; different owner → new link; null-owner group dedups independently. Update `mockLinkRepo` for `GetByOwnerAndURL`.

3. **Part B tests (Redis via miniredis):**
   - `quota_service_test.go`: `DailyLimit` basic-fallback vs active pro sub; `Allow` under limit (true) / at limit (false + counter decremented back) ; `Release` decrements; **breaker-open → fail-open allow**; date-key resets across days (inject `now`).
   - `dedup_cache_test.go`: `Remember` then `Lookup` hit; miss; unavailable → found=false.
   - `quota_check_test.go`: skip when no `user_id`; 429 over limit; refund (`Release`) when downstream returns ≥400; refund on `link_reused`; pass under limit.
   - `duplicate_url_check_test.go`: hit returns early (handler not called); miss calls next; no `user_id` → next.

4. **README** — add: link ownership note (JWT-created links owned; API-key links unowned), quota behavior (10/day basic, 429, resets 00:00 UTC), `QUOTA_*` env vars, plans/subscriptions migration note.

5. **Swagger** — `make swag`; confirm `POST /api/links` documents 429; bump any changed schemas.

6. **Gate:** `make build` && `make test` (all green) && `make lint`/`gofmt`. Fix per recommendations — do NOT weaken assertions.

## Todo
- [ ] add miniredis test dep
- [ ] Authn middleware tests
- [ ] per-owner dedup tests (+ mock update)
- [ ] QuotaService tests (miniredis, fail-open, day reset)
- [ ] DedupCache tests
- [ ] QuotaCheck + DuplicateURLCheck middleware tests
- [ ] README + Swagger updates
- [ ] `make build` + `make test` green

## Success Criteria
- All tests pass; coverage of quota happy + limit + refund + fail-open paths and per-owner dedup.
- Docs reflect ownership + quota + new env vars; existing API-key flow documented as still valid.

## Next
Plan complete → mark phases done; optional `/plan archive`. Billing remains future work.
