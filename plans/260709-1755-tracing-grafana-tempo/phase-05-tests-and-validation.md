# Phase 05 — Tests + validation

**Priority:** medium | **Status:** complete | **Depends:** 01-04

Verify tracing works end-to-end AND does not regress the redirect hot path.

## Unit tests (Go)
- `pkg/observability`: `SetupTracing` returns no-op shutdown + sets no global when `Enabled=false`; sets provider when enabled (use a stub/in-memory exporter or just assert non-nil provider).
- `slog_trace_handler`: given a ctx with a valid span, the emitted record carries `trace_id`/`span_id`; without a span, it does not.
- otelecho skipper: unit-assert the Skipper func returns true for `/:code`, false for `/api/...`.

No broker/DB needed — keep tests hermetic. Use `sdktrace` with an in-memory span recorder (`tracetest.NewSpanRecorder`) where asserting spans.

## Integration / manual validation (cluster)
1. `TRACING_ENABLED=true`, deploy new image (server+consumer+bulk-worker).
2. Create a link via `/api/links`, hit its redirect, confirm in Grafana Tempo:
   - api call → produce → **kafka** → consumer process → PG insert = ONE trace.
3. Trigger an error (e.g. bad `/api/links` payload) → trace exists (kept, 100%).
4. Loki: find the request's log line, confirm `trace_id` present + clickable → opens the Tempo trace.
5. Confirm redirect `GET /:code` produces NO server span (skipper).

## Perf regression (the key guard)
Re-run `test/read-heavy-2.js` (ClusterIP, the L1-cache setup) with tracing ON:
- redirect throughput within ~5% of the pre-tracing L1 result (~4800 rps @ p50 13ms).
- If regressed >5%: confirm redirect exclusion is effective; check redisotel isn't spanning on L1 hits (it shouldn't — L1 skips Redis).

## Todo
- [x] unit: SetupTracing enabled/disabled
- [x] unit: slog trace handler with/without span
- [x] unit: otelecho skipper excludes /:code
- [x] `go test ./...` green (125 pass)
- [x] manual: end-to-end trace across Kafka visible in Tempo
- [x] manual: error trace kept + log↔trace link works
- [x] perf: redirect throughput regression < 5%

## Success criteria
- All unit tests pass; full suite green.
- End-to-end async trace visible; correlation link works.
- Redirect hot-path throughput not meaningfully degraded.

## Notes (post-completion)
- Kotel consume-span API confirmed; kafka_tracing helpers implemented via franz-go kotel hooks.
- Datasource UIDs resolved; Loki → Tempo derived field jump works end-to-end.
