# Phase 06 — Tests

## Overview
- **Priority:** P1 · **Status:** ✅ done · Depends on: 01–05
- Cover ownership, cache/dedup eviction, and the redirect-active rule. Mock-based (existing style) — no real Redis/DB unless the repo tests already use a harness.

## Related files
- `internal/service/link_service_test.go` (extend)
- `internal/handler/link_handler_test.go` (extend, if present)

## Cases
**Service (`LinkService`)**
- `Delete`: owner → repo.Delete called + cache.Delete(code) + dedup.Forget(owner,url) called; returns nil.
- `Delete` non-owner / unowned (`user_id=nil`) / missing → `apperror.NotFound`; repo.Delete NOT called.
- `Update`: owner → repo.Update called with map containing `is_active` + `expires_at` (incl. nil-clear case); cache.Delete(code) called; returns updated link.
- `Update` non-owner/missing → NotFound.
- `Resolve` with `is_active=false` (DB path) → `apperror.Gone`; `cache.Set` NOT called (never cache inactive).
- `Resolve` active + not expired → caches + returns link (regression).
- Expiry precedence: inactive AND expired → Gone (either reason acceptable; assert Gone).

- `ListByOwner` status filter: assert repo called with the passed `status` + `now`; (repo-level, if a repo test harness exists) `active`/`disabled`/`expired` return the expected subset and the matching `total`.

**Handler**
- `GET /api/links?status=disabled` → forwards `status` to service; `?status=bogus` → 400; no `status` → unfiltered.
- `DELETE /:code` owner → 204; non-owner → 404.
- `PUT /:code` valid body → 200 with updated `is_active`/`expires_at`; missing `is_active` → 400; bad JSON → 400.
- Both require auth → 401 without user in context.

**Mocks**
- Extend existing fake `LinkRepository`/cache/dedup mocks with `Delete`/`Update`/`Forget` + call-assertions. Follow whatever mock pattern the current `link_service_test.go` uses.

## Todo
- [x] service Delete (happy + 3 negative)
- [x] service Update (happy + clear-expiry + negative)
- [x] service Resolve inactive→Gone + not-cached
- [x] ListByOwner status filter (subset + total)
- [x] handler DELETE/PUT (204/200/400/401/404) + List ?status (forward + 400)
- [x] `make test` green; no skipped/failing

## Success criteria
- All new tests pass with `-race`; existing suite still green.
- Eviction calls asserted (guards the stale-cache risk).
- No fake data / no test-only shortcuts in production code.
