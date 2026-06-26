# Phase 03 — Part B: QuotaService, DedupCache & Circuit Breaker

**Context:** [plan.md](plan.md) · [spec](../reports/spec-260626-1538-link-ownership-and-subscription-quota.md)

## Overview
- **Priority:** High (core quota logic)
- **Status:** pending
- Redis-backed quota counter + per-user-URL dedup cache, both behind a circuit breaker.

## Related Code Files
- **Create:** `pkg/redisbreaker/redisbreaker.go`, `internal/service/quota_service.go`,
  `internal/service/dedup_cache.go` (or `repository/dedup_cache_repository.go`)
- **Modify:** `configs/config.go` (`QUOTA_`/breaker knobs), `go.mod` (add `sony/gobreaker`)

## Implementation Steps

1. **Add dep:** `go get github.com/sony/gobreaker` && `go mod tidy`.

2. **Config** — `QuotaConfig` (`envPrefix:"QUOTA_"`): `DefaultPlanCode "basic"`,
   `BasicFallbackLimit 10`, `BreakerMaxFailures 10`, `BreakerOpenTimeout 5m`.

3. **`pkg/redisbreaker`** — thin wrapper:
   ```go
   type Breaker struct{ cb *gobreaker.CircuitBreaker }
   func New(maxFailures int, openTimeout time.Duration) *Breaker // ReadyToTrip: consecutive >= maxFailures; Timeout: openTimeout
   func (b *Breaker) Do(fn func() (any, error)) (any, error)      // returns gobreaker.ErrOpenState when open
   func IsUnavailable(err error) bool                              // true for ErrOpenState / ErrTooManyRequests / non-nil redis err
   ```

4. **`dedup_cache.go`** (encapsulates `user:links:{uid}:{urlHash}`; breaker-wrapped):
   ```go
   func (d *DedupCache) Lookup(ctx, userID int64, url string) (shortURL string, found bool)
   func (d *DedupCache) Remember(ctx, userID int64, url, shortURL string, ttl time.Duration)
   ```
   `urlHash = hex(sha256(TrimSpace(url)))`. Breaker-open / redis err → `Lookup` returns `found=false`; `Remember` is best-effort (ignore error). Reuse `SHORTENER_CACHE_TTL` default for TTL when no link expiry.

5. **`quota_service.go`** — interface + impl (breaker-wrapped Redis):
   ```go
   type QuotaService interface {
       Allow(ctx, userID int64) (bool, error)  // true = within quota (or fail-open)
       Release(ctx, userID int64)               // refund (DECR)
   }
   ```
   - `DailyLimit(ctx, userID)`: `subs.GetActiveByUserID` → `plans.GetByID(plan_id).DailyLinkQuota`; on `ErrNotFound` → `plans.GetByCode("basic")`; on any error → `cfg.BasicFallbackLimit` (log warn).
   - `Allow`: `limit := DailyLimit(...)`; `key := fmt.Sprintf("user:quota:%d:%s", userID, s.now().UTC().Format("2006-01-02"))`;
     `n := INCR key`; if `n==1` → `EXPIRE key 48h`; if `n > limit` → `DECR key` + return `false`; else `true`.
   - Wrap the Redis ops in `redisbreaker.Do`. If `IsUnavailable(err)` → **fail-open**: `Allow` returns `true` (log warn), `Release` no-op.
   - `Release`: `DECR key` (breaker-wrapped, best-effort).
   - Inject `now func() time.Time`, `rdb`, `breaker`, repos, `cfg`.

6. `go build ./...`.

## Key Insights
- Atomic `INCR` solves TOCTOU. `DECR`-on-reject keeps the counter = successful creates.
- Calendar-day correctness comes from the UTC date in the key; the 48h TTL is cleanup only.
- Fail-open via breaker: a Redis outage never blocks link creation, and the breaker stops per-request timeout stalls.

## Todo
- [ ] add `sony/gobreaker`, tidy
- [ ] `QuotaConfig` knobs
- [ ] `pkg/redisbreaker`
- [ ] `DedupCache` (Lookup/Remember)
- [ ] `QuotaService` (DailyLimit/Allow/Release, breaker, fail-open)
- [ ] build passes

## Success Criteria
- Unit-testable (Phase 5): Allow under/at limit, Release, fail-open on breaker-open, limit resolution.

## Security / Reliability
Fail-open is deliberate for quota (availability > strict cap); revisit when billing adds hard caps.

## Next
Phase 04 wires these behind middlewares on the create route.
