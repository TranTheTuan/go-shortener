# Phase 02 — HTTP RED Middleware

## Overview
- **Priority:** P2 · **Status:** done · Depends on: 01
- Echo middleware recording request count + duration + in-flight for every route. RED = Rate, Errors, Duration.

## Related files
- Create: `internal/middleware/metrics.go`
- Modify: `internal/router/router.go` (register early, after Recover)
- Modify: `pkg/metrics/metrics.go` (HTTP instruments)

## Steps
1. **Instruments** (in `pkg/metrics`, created from the package `meter`):
   - `http.server.request.duration` — Float64Histogram, unit `s`.
   - `http.server.active_requests` — Int64UpDownCounter (in-flight gauge).
   - (request count is derivable from the histogram's `_count`; no separate counter.)
2. **Middleware** `Metrics() echo.MiddlewareFunc`:
   ```go
   return func(next echo.HandlerFunc) echo.HandlerFunc {
     return func(c echo.Context) error {
       start := time.Now()
       inflight.Add(ctx, 1, attrs...) ; defer inflight.Add(ctx, -1, attrs...)
       err := next(c)
       route := c.Path()            // template — low cardinality
       if route == "" { route = "unmatched" }
       status := c.Response().Status // set by handler/error handler
       attrs := metric.WithAttributes(
         attribute.String("http.method", c.Request().Method),
         attribute.String("http.route", route),
         attribute.Int("http.status_code", status),
       )
       duration.Record(ctx, time.Since(start).Seconds(), attrs)
       return err
     }
   }
   ```
   - Use `c.Path()` (route template) — the `:code`/`:id` params collapse, avoiding cardinality explosion.
   - `status`: after `next`, Echo's error handler has run so `c.Response().Status` is final. If 0, default 200.
   - in-flight attrs: keep minimal (method only, or none) to avoid churn.
3. **Register** in `router.New` right after `Recover()` so it wraps everything (incl. the error path). Must NOT wrap `/metrics` (different server, so N/A).

## Todo
- [x] HTTP instruments in pkg/metrics
- [x] `Metrics()` middleware (route template, status, duration, in-flight)
- [x] registered after Recover
- [x] `go vet`; `curl` a few routes → `http_server_request_duration_seconds_bucket{http_route="/api/links",...}` present

## Success criteria
- Every request produces a duration observation labelled by method/route-template/status.
- No per-path (raw) or per-id series; `/:code` collapses to one series.

## Notes
- Prometheus metric name from OTel: dots→underscores, unit suffix `_seconds`. Confirm the exported name in `/metrics` for the dashboard queries.
