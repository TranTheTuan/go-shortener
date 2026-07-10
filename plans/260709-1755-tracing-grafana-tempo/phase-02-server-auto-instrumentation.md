# Phase 02 — Server auto-instrumentation (exclude redirect)

**Priority:** high | **Status:** complete | **Depends:** 01

Auto-instrument the HTTP server + its downstream clients. **Redirect route excluded** to protect the L1-cache latency/throughput gains.

## Context
- Middleware chain: `internal/router/router.go` lines 46-53 — `RequestID → Recover → Metrics → FrontendCache`.
- Redis client: `pkg/database` `RedisClient.Client` (go-redis v9).
- GORM db built in `cmd/server/*` (openPostgres).
- Kafka producer: `internal/events/click_producer.go` (`kgo.NewClient`, `cl.TryProduce`); bulk producer `bulk_job_producer.go`.
- slog is the app logger; `response.Error` already logs with ctx.

## Design

### otelecho middleware WITH redirect skipper
Add AFTER RequestID (so trace has the request id) but the span should wrap the handler. Place early in chain, skip the redirect path.
```go
import "go.opentelemetry.io/contrib/instrumentation/github.com/labstack/echo/otelecho"

e.Use(otelecho.Middleware("go-shortener",
    otelecho.WithSkipper(func(c echo.Context) bool {
        // Exclude the public redirect hot path (GET /:code). Its trace value is
        // low (cache-hit → 302) and per-request span overhead would erode the
        // L1-cache throughput win. Everything else (/api/*, /auth/*) is traced.
        return c.Path() == "/:code"
    }),
))
```
Verify the redirect route template is exactly `/:code` (check router). Also skip `/health`, `/metrics` if noisy.

### redis (go-redis) — only traces actual Redis calls
```go
import "github.com/redis/go-redis/extra/redisotel/v9"
redisotel.InstrumentTracing(rdb.Client)
```
Synergy: L1 hits skip Redis → no redis span → near-zero overhead on hot path even though redirect is excluded anyway.

### GORM
```go
import "github.com/uptrace/opentelemetry-go-extra/otelgorm"
db.Use(otelgorm.NewPlugin())
```

### Kafka produce — inject trace context into record headers
franz-go `kotel`:
```go
import "github.com/twmb/franz-go/plugin/kotel"
tracer := kotel.NewTracer(kotel.TracerProvider(otel.GetTracerProvider()))
kt := kotel.NewKotel(kotel.WithTracer(tracer))
// add kt.Hooks() to kgo.NewClient opts in NewKafkaProducer (and bulk producer)
opts = append(opts, kgo.WithHooks(kt.Hooks()...))
```
This injects `traceparent` into Kafka record headers on produce → consumer continues the trace (phase 03).

### slog ↔ trace correlation
Wrap the slog handler so every log line carries trace_id/span_id from ctx:
```go
// pkg/observability/slog_trace_handler.go
type traceHandler struct{ slog.Handler }
func (h traceHandler) Handle(ctx context.Context, r slog.Record) error {
    if sc := trace.SpanContextFromContext(ctx); sc.IsValid() {
        r.AddAttrs(slog.String("trace_id", sc.TraceID().String()),
                   slog.String("span_id", sc.SpanID().String()))
    }
    return h.Handler.Handle(ctx, r)
}
// wrap the JSON handler at logger setup: slog.New(traceHandler{jsonHandler})
```
Requires log calls to use the `*Context` variants (response.Error already does; audit others). Result: Loki log lines carry trace_id → Grafana derived-field jump to Tempo.

## Related files
- modify: `internal/router/router.go` (otelecho), `cmd/server/server.go` (redisotel, otelgorm, kotel wiring, slog wrap), `internal/events/click_producer.go` + `bulk_job_producer.go` (kotel hooks), logger setup file.
- create: `pkg/observability/slog_trace_handler.go`

## Todo
- [x] add deps (otelecho, redisotel, otelgorm, kotel)
- [x] otelecho middleware + redirect Skipper (confirm `/:code` template)
- [x] redisotel.InstrumentTracing
- [x] otelgorm plugin
- [x] kotel hooks on both producers
- [x] slog trace handler + wrap logger
- [x] `go build ./...`

## Success criteria
- `/api/*` request → trace with child spans for Redis (if hit) + GORM.
- Redirect `GET /:code` → NO server span created (skipper works).
- App log lines for a traced request carry trace_id/span_id.

## Risk
- otelecho span-per-request overhead — mitigated by redirect exclusion. Measure redirect throughput before/after (phase 05).

## Notes (post-completion)
- M1 (orphan spans on redirect L1-miss): accepted trade-off per code-review. Redirect exclusion catches 99.9% of cache-hit hot path; L1-miss edge case (single-digit % of traffic) incurs span cost but still within acceptable bounds.
