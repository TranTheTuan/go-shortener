# Phase 03 — Async trace propagation (consumer + bulk-worker)

**Priority:** high | **Status:** complete | **Depends:** 01, 02

Continue the trace across the Kafka boundary so producer→consumer→PG is ONE trace. Without this, the consumer starts a disconnected trace and the async link (the main reason for tracing here) is lost.

## Context
- Consumers: `internal/events/click_consumer.go`, `internal/events/bulk_job_consumer.go` (franz-go `PollFetches`).
- Entrypoints: `cmd/server/consumer.go`, `cmd/server/bulk_worker.go` (same binary, separate run funcs).
- Phase 02 injected `traceparent` into Kafka record headers on produce.

## Design

### TracerProvider in consumer + bulk-worker
Each run func calls `observability.SetupTracing` with its own service.name (`go-shortener-consumer`, `go-shortener-bulk-worker`) — done in phase 01 wiring; confirm both are wired.

### kotel on the consume client — extract + start consume span
```go
tracer := kotel.NewTracer(kotel.TracerProvider(otel.GetTracerProvider()))
kt := kotel.NewKotel(kotel.WithTracer(tracer))
opts = append(opts, kgo.WithHooks(kt.Hooks()...)) // on the consumer kgo.NewClient
```
Then per-record, start a span with the extracted parent context so the consumer span attaches to the producer's trace:
```go
for _, rec := range fetches.Records() {
    ctx := kt.Tracer(...).WithProcessSpan(rec) // kotel helper: extracts traceparent header, returns ctx+span
    // ... process rec using ctx (so DB insert span nests under it)
    span.End()
}
```
(Confirm exact kotel API for consume-span; franz-go kotel provides record hooks + a process-span helper.)

### GORM in consumer path
`db.Use(otelgorm.NewPlugin())` on the consumer's db handle (if separate from server's) so the click/bulk INSERT shows as a child span. Pass the extracted ctx into repository calls (`clicks.Create(ctx, ...)`) — verify ctx threads through.

### slog trace_id
Reuse the phase-02 traceHandler for consumer/bulk-worker loggers too.

## Related files
- modify: `internal/events/click_consumer.go`, `internal/events/bulk_job_consumer.go`, `cmd/server/consumer.go`, `cmd/server/bulk_worker.go`
- verify ctx propagation into `internal/repository` Create calls on the consume path.

## Todo
- [x] confirm SetupTracing wired in consumer + bulk-worker run funcs (phase 01)
- [x] kotel hooks on both consume clients
- [x] extract parent + start process span per record
- [x] thread extracted ctx into repository Create calls
- [x] otelgorm on consumer db handle
- [x] slog traceHandler on consumer/bulk loggers
- [x] `go build ./...`

## Success criteria
- A single redirect that produces a click event yields ONE Tempo trace spanning: (api span if via api) → produce → kafka → consumer process span → PG insert span.
- bulk-worker job processing shows as a trace linked to the bulk producer.

## Risk
- If ctx isn't threaded into repo calls, DB spans detach — the link silently degrades. Explicit check required.
