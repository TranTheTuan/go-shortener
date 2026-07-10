# Code Review — OpenTelemetry Distributed Tracing (branch feat/tracing)

Date: 2026-07-10 | Reviewer: code-reviewer | Scope: tracing wiring only

## Scope
- pkg/observability/{tracing.go, slog_trace_handler.go}
- configs/config.go (TracingConfig)
- cmd/server/{main,server,consumer,bulk_worker}.go
- internal/router/router.go, internal/events/{kafka_tracing.go, click_producer.go, bulk_job_consumer.go}
- Verified: `go build ./...` OK, `go vet` clean, observability/router/events tests pass.

## Overall Assessment
Solid, idiomatic OTel wiring. All six agreed design decisions are implemented correctly. Two real defects: one HIGH (server loses spans on error-path exit), one MEDIUM (redirect exclusion is incomplete on cache-miss). No Critical, no nil-deref, no data race. Score 7.5/10, mergeable after the HIGH fix.

---

## Critical
None.

## High

### H1 — server.go: TracerProvider never flushed on error-path exit (`cmd/server/server.go:44,215`)
`tpShutdown` is captured at :44 but only invoked at :215 on the happy path. Every `return` between them skips it: server runtime error (`return err` :194), `e.Shutdown` failure (:204), plus config/setup errors (:51 redis, :56 redisotel, :89 kafka, :114 metrics, :138/:147 bulk). On those exits, batched spans are dropped — you lose traces exactly when a crash/error makes them most valuable. consumer.go (:30) and bulk_worker.go (:36) already guard with `defer`; server is the odd one out.

Fix — mirror the other two entrypoints; add right after :47 and delete the manual :215 call:
```go
tpShutdown, err := setupTracing(context.Background(), cfg, "go-shortener-server")
if err != nil {
    return err
}
defer func() {
    ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
    defer cancel()
    _ = tpShutdown(ctx)
}()
```
LIFO ordering keeps it running last (after metrics shutdown), so span flush still happens after the MeterProvider teardown as intended.

## Medium

### M1 — Redirect exclusion is incomplete: L1-miss still emits orphan Redis/DB spans (`internal/router/router.go:54-77`, `internal/handler/redirect_handler.go:41`)
The otelecho skipper correctly suppresses the *HTTP* span for `/:code`, and L1 cache hits touch neither Redis nor GORM — so the hot path is genuinely span-free (design goal met). But on an L1 **miss**, `Redirect` passes `c.Request().Context()` (no active span) into `Resolve`, and the global redisotel + otelgorm instrumentations each call `tracer.Start`. With `ParentBased(TraceIDRatioBased(1.0))` and no parent, the root sampler fires → each redirect miss produces rootless single-span traces (Redis GET, then a DB query on Redis miss). Result: cold-path export overhead + Tempo noise (orphan traces) that the "redirect excluded" intent implies shouldn't exist.

Options:
- **Accept + document** (recommended, YAGNI): misses are low-volume and cheap; note in the plan that redirect misses yield orphan Redis/DB spans by construction.
- **Suppress** if noise matters: on the redirect path derive a context whose span context is valid-but-not-sampled so `ParentBased` short-circuits to "don't record", threading that into `Resolve`.

Not a blocker — hot path is protected as designed.

## Low

### L1 — server.go sets up tracing *after* GORM plugin install (`cmd/server/server.go:39` before `:44`)
`openPostgres` runs `db.Use(otelgorm.NewPlugin())`, and otelgorm captures `otel.GetTracerProvider()` at construction (otelgorm.go:38-41). Here that runs before `SetTracerProvider`. It still works because `otel.GetTracerProvider()` returns the global *delegating* provider and `SetTracerProvider` back-fills the delegate into already-created tracers — so DB spans do record on the server. But it's inconsistent with consumer.go/bulk_worker.go (which set up tracing first) and fragile: swapping to a concrete provider would silently drop server DB spans. Move `setupTracing` above `openPostgres` for consistency.

### L2 — main.go doc/mode naming drift (`cmd/server/main.go:3` vs `:44-49`)
Package comment says roles are `"server"`/`"consumer"`, but the switch accepts `"server"`/`"analyze"`/`"bulk-worker"` and maps mode `"analyze"` to `service.name = go-shortener-consumer`. Cosmetic, but the mismatch between the deploy command (`analyze`) and the trace service name (`consumer`) will confuse ops correlating pods to services. Align the comment and consider renaming the mode to `consumer`.

### L3 — tpShutdown uses `context.Background()` in consumer/bulk_worker (`consumer.go:30`, `bulk_worker.go:36`)
No timeout: if Alloy is unreachable at SIGTERM, `BatchSpanProcessor.Shutdown` → exporter export/retry can delay pod termination (bounded by the exporter's internal timeout, but still stalls the drain). Wrap with a `context.WithTimeout(...5s)` like the H1 fix.

### L4 — Click traces are consumer-rooted only (informational)
Clicks are produced on the redirect path (`click_producer.go:102` uses `context.Background()`), which is untraced by design — so the produce→consume link the plan narrative describes ("HTTP → produce → Kafka → consumer") does not form for clicks; the click-consumer's `WithProcessSpan` starts a fresh root. Expected consequence of excluding redirect, not a defect. Bulk-job produce→consume (via traced `/api/*` handlers) does link correctly.

---

## Focus-area verdicts
1. **OTel wiring** — Correct. Resource merge uses schemaless URL to avoid conflict (:44-52). ParentBased+TraceIDRatioBased head sampling, W3C tracecontext+baggage propagator (:71-73). Shutdown returns `tp.Shutdown`. Only gap: server flush-on-error (H1).
2. **bulk_job_consumer.go** — Correct. `defer span.End()` is inside the EachRecord closure → fires per record, no leak (franz-go invokes the callback sequentially per fetch, so `toCommit` append is race-free). `procCtx` is threaded into `worker.Process` (:70) and all slog calls, so DB spans nest under the process span. `WithProcessSpan` starts on `rec.Context` (kotel tracer.go:168), which the fetch hook populated — parent link intact.
3. **Hot-path safety** — Correct. `/:code`,`/healthz`,`/metrics` skipped (:70-77). L1 hit returns before Redis (tiered_link_cache.go:41), redisotel only wraps real client calls → no span on L1 hit. See M1 for the L1-miss caveat.
4. **Error handling** — `SetupTracing` returns the no-op shutdown on every error (:39,54,62) and when disabled (:40). Exporter creation failure surfaced (:61). Good — but caller must still defer the returned shutdown (H1).
5. **Kafka propagation** — Correct. `buildKGOOpts` adds `tracingHooks()` to every client (producers + both consumers) via `kgo.WithHooks` (:52). `WithProcessSpan` uses `rec.Context`. kotel bound to global provider+propagator so it's a no-op when disabled.
6. **Races / nil-deref / config defaults** — None found. Globals never nil; kotel/BatchSpanProcessor/slog handler all concurrency-safe. Defaults correct: `TRACING_ENABLED=false` (opt-in ✓), endpoint `alloy.monitoring.svc.cluster.local:4317`, `SAMPLE_RATIO=1.0`.

## Positive
- Opt-in no-op path is clean and total (disabled ⇒ zero provider installed).
- TraceHandler rewraps on WithAttrs/WithGroup — a subtle correctness point many miss.
- Schemaless resource merge avoids the common semconv schema-URL conflict panic.
- redisotel/otelgorm auto-instrumentation reused instead of hand-rolled spans (DRY).

## Recommended actions (priority order)
1. H1 — add `defer` tpShutdown in server.go, remove manual :215 call. **Required before merge.**
2. M1 — decide accept-vs-suppress for redirect-miss orphan spans; document the choice.
3. L1 — reorder setupTracing before openPostgres in server.go.
4. L3 — add timeout ctx to consumer/bulk_worker shutdown.
5. L2 — fix main.go mode-name/service-name drift.

## Score: 7.5/10 — Mergeable after H1.

## Unresolved questions
- M1: is the orphan Redis/DB span on redirect cache-miss acceptable noise, or should sampling be suppressed on the redirect path?
- L2: is mode `analyze` intentional (legacy click-consumer name) or should it be `consumer` to match service.name?
