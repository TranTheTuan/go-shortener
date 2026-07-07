# Phase 03 — Domain Instruments

## Overview
- **Priority:** P2 · **Status:** done · Depends on: 01
- A small set of business metrics beyond RED. Instruments defined in `pkg/metrics`, recorded via one-line helpers at the event sites.

## Related files
- Modify: `pkg/metrics/metrics.go` (instruments + record helpers)
- Modify: `internal/handler/redirect_handler.go` (redirect outcome)
- Modify: `internal/service/link_service.go` (cache hit/miss in `cacheGet`/`Resolve`)
- Modify: `internal/middleware/quota_check.go` or `internal/service/quota_service.go` (quota rejection)
- Modify: `pkg/redisbreaker/*` or the breaker owner (breaker state gauge)
- Modify: `internal/events/click_producer.go` (produce ok/dropped)

## Metric set (KISS — bounded labels only)
| Metric | Type | Labels | Site |
|--------|------|--------|------|
| `redirects_total` | Int64Counter | `result` = ok\|notfound\|expired\|disabled | redirect handler (map Resolve outcome) |
| `link_cache_lookups_total` | Int64Counter | `result` = hit\|miss | linkService cache path |
| `quota_rejections_total` | Int64Counter | – | where 429 is returned |
| `click_events_total` | Int64Counter | `result` = produced\|dropped | click producer TryProduce |
| `redis_breaker_open` | Int64ObservableGauge | – | callback reads breaker state (1=open,0=closed) |

## Steps
1. **pkg/metrics** — construct the above from `meter`; expose helpers, e.g.:
   ```go
   func RecordRedirect(ctx, result string) { redirectsTotal.Add(ctx, 1, metric.WithAttributes(attribute.String("result", result))) }
   func RecordCacheLookup(ctx, hit bool) { ... "result" hit|miss }
   func RecordQuotaRejection(ctx) { ... }
   func RecordClickEvent(ctx, result string) { ... }
   ```
   Observable gauge for breaker: register a callback that reads the breaker's state (needs a state accessor on the breaker — add `IsOpen() bool` if absent).
2. **Redirect** — in the handler, after `Resolve`, map result → `metrics.RecordRedirect`: success=ok; apperror code/status → notfound (404) / expired+disabled (410; distinguish by message or add distinct app-error codes). Prefer distinguishing expired vs disabled — the service already returns different messages; cleanest is separate apperror codes (e.g. `GONE_EXPIRED` vs `GONE_DISABLED`) — small change, optional; else bucket both as `gone`.
3. **Cache** — in `linkService.cacheGet` (or Resolve), record hit when cache returns non-nil, miss otherwise. One call site.
4. **Quota** — at the point the 429 is produced (quota middleware/service) call `RecordQuotaRejection`.
5. **Kafka** — in the producer's TryProduce path: `produced` on enqueue, `dropped` on ErrMaxBuffered / error.
6. **Breaker gauge** — observable callback; ensure thread-safe read of breaker state.

## Todo
- [x] instruments + helpers in pkg/metrics
- [x] redirect outcome recorded (decide expired vs disabled granularity)
- [x] cache hit/miss recorded (single site)
- [x] quota rejection recorded
- [x] kafka produced/dropped recorded
- [x] breaker-open observable gauge
- [x] `go vet`; verify counters increment in `/metrics` after exercising flows

## Success criteria
- Each domain event increments its counter with a bounded label set.
- No high-cardinality labels introduced.

## Risks
- Redirect result mapping depends on apperror granularity; if not distinguishing expired/disabled, document the bucket. Keep it a deliberate choice.
- Breaker state accessor must be race-free (observable callbacks run concurrently with breaker updates).
