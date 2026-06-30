# Phase 04 — Part B: Middlewares & Wiring

**Context:** [plan.md](plan.md) · [spec](../reports/spec-260626-1538-link-ownership-and-subscription-quota.md)

## Overview
- **Priority:** High
- **Status:** pending
- `DuplicateURLCheck` + `QuotaCheck` middlewares, route wiring, main wiring, 429 error.

## Related Code Files
- **Create:** `internal/middleware/duplicate_url_check.go`, `internal/middleware/quota_check.go`
- **Modify:** `pkg/apperror/apperror.go`, `internal/router/router.go`, `cmd/server/main.go`,
  `internal/service/link_service.go` (Remember dedup cache + "reused" signal)

## Implementation Steps

1. **`apperror`** — add:
   ```go
   func TooManyRequests(message string) *Error { return New(http.StatusTooManyRequests, "QUOTA_EXCEEDED", message) }
   ```

2. **`DuplicateURLCheck(dedup *service.DedupCache)`** (after Authn, on `POST /api/links`):
   - `id, ok := appmw.UserIDFrom(c)`; `!ok` → `next(c)` (ownerless skips fast-path).
   - bind URL (peek body → restore: read `c.Request().Body`, `io.NopCloser` reset) OR re-bind in handler; simplest: read `url` then reset body for downstream bind.
   - `short, found := dedup.Lookup(ctx, id, url)`; found → `response.Success(c, 200, reusedResponse{ShortURL: short, Reused: true})` and return.
   - miss / unavailable → `next(c)`.

3. **`QuotaCheck(quota service.QuotaService)`** (after DuplicateURLCheck):
   - `id, ok := appmw.UserIDFrom(c)`; `!ok` → `next(c)` (API-key/ownerless: no quota).
   - `allowed, _ := quota.Allow(ctx, id)`; `!allowed` → `response.Error(c, apperror.TooManyRequests("daily link quota exceeded"))`.
   - `err := next(c)`; after: if `c.Response().Status >= 400` OR ctx flag `c.Get("link_reused")==true` → `quota.Release(ctx, id)`. Return `err`.

4. **`LinkService.Create`** — on **new** create: `dedup.Remember(ctx, *ownerID, url, shortURL, ttl)` (only when `ownerID != nil`). On **DB dedup backstop hit** (existing returned) for an owner: set a signal the handler propagates to context (`c.Set("link_reused", true)`) so QuotaCheck refunds. (Pass a `reused bool` out of the service → handler sets the flag.)

5. **`router.go`** — create route chain:
   ```go
   links := api.Group("/links")
   links.POST("", h.Link.Create, appmw.DuplicateURLCheck(dedup), appmw.QuotaCheck(quota))
   links.GET("/:code/stats", h.Link.Stats)
   ```
   (Echo runs group middleware, then route middleware left→right, then handler.)
   Pass `dedup` + `quota` into `registerRoutes`/`Handlers` wiring.

6. **`main.go`** — build: `breaker := redisbreaker.New(cfg.Quota.BreakerMaxFailures, cfg.Quota.BreakerOpenTimeout)`;
   `dedup := service.NewDedupCache(rdb, breaker, cfg.Shortener.CacheTTL)`;
   `planRepo := repository.NewPlanRepository(db)`; `subRepo := repository.NewSubscriptionRepository(db)`;
   `quota := service.NewQuotaService(rdb, breaker, planRepo, subRepo, cfg.Quota)`; pass into router. Inject `dedup` into LinkService (new param) for `Remember`.

7. `go build ./...`.

## Edge Cases
- Dedup fast-path hit → no quota touched. DB-dedup backstop hit → QuotaCheck refunds via `link_reused`. Failed insert (≥400) → refund. Redis down → breaker open → both middlewares no-op (fail-open), DB dedup still correct.

## Todo
- [ ] `apperror.TooManyRequests`
- [ ] `DuplicateURLCheck` middleware
- [ ] `QuotaCheck` middleware (Allow + refund on ≥400 / reused)
- [ ] `LinkService` Remember + reused signal
- [ ] Router create-route middleware chain
- [ ] main wiring (breaker, dedup, quota, repos)
- [ ] build passes

## Success Criteria
- Manual E2E: JWT user creates ≤10 links; 11th → 429. Same-URL reuse returns existing, count unchanged. API-key create unaffected.

## Next
Phase 05: tests + docs.
