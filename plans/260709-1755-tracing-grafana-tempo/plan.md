---
title: Distributed Tracing with Grafana Tempo
status: complete
created: 2026-07-10
branch: feat/tracing
brainstorm: plans/reports/brainstorm-260709-1755-tracing-grafana-tempo.md
completed: 2026-07-10
---

# Distributed Tracing with Grafana Tempo

Add the third observability pillar (traces) to go-shortener. Metrics (OTel→Prometheus pull) and logs (slog→Loki) already exist. Traces PUSH via OTLP.

## Decision (see brainstorm report)
- **Path:** App → existing Alloy DaemonSet (OTLP receiver, **forward-only, no tail**) → Tempo. 0 new pods.
- **Sampling:** head / keep 100% at real traffic (decision in-app at root, propagated). Lower ratio only for load tests.
- **Scope:** auto-instrument HTTP+Redis+GORM+Kafka; **exclude redirect route**; propagate trace across Kafka (producer→consumer→PG) in all 3 entrypoints (server/consumer/bulk-worker, one binary).
- **Storage:** Tempo monolithic, filesystem local-path, 72h retention.
- **Service graph (metrics-generator):** deferred to phase 2.

## Payoff
One connected trace: HTTP `/api/*` → Redis/PG → produce → **Kafka** → consumer → PG. Plus log↔trace jump in Grafana (trace_id in slog → Loki derived field → Tempo).

## Phases
| # | Phase | Status | Depends |
|---|-------|--------|---------|
| 01 | [App tracing bootstrap (shared)](phase-01-app-tracing-bootstrap.md) | complete | — |
| 02 | [Server auto-instrumentation (exclude redirect)](phase-02-server-auto-instrumentation.md) | complete | 01 |
| 03 | [Async trace propagation (consumer + bulk-worker)](phase-03-async-trace-propagation.md) | complete | 01, 02 |
| 04 | [Infra: Tempo + Alloy forward + Grafana datasource](phase-04-infra-tempo-alloy-grafana.md) | complete | — (parallel w/ 01-03) |
| 05 | [Tests + validation](phase-05-tests-and-validation.md) | complete | 01-04 |

Phases 01-03 (app code) and 04 (infra) are independent — can run in parallel. 05 needs both.

## Key risks
- **Hot-path overhead:** otelecho makes a span per request. Redirect EXCLUDED via Skipper to protect the L1-cache latency gains (< ~5% throughput regression target).
- **3 entrypoints, 1 binary:** each of server/consumer/bulk-worker needs its own TracerProvider(service.name) + graceful flush; kotel inject on produce + extract on consume, else the async link breaks.
- **otelgorm** is community (uptrace) — only non-official dep.

## Deferred (phase 2 future, not now)
- **metrics-generator / service graph:** Tempo metrics-generator → service RED metrics + span-level aggregation. Needs Prometheus remote-write receiver integration. Defer to next tracing phase if service graph becomes critical for debugging.
- **tail sampling:** Dedicated collector + sampling rules. Only if span volume hurts storage/cost. Currently forward-only (100% keep); head sampling decision at app source sufficient.

## Dependencies to add (match existing otel v1.44 line)
`otlptrace/otlptracegrpc`, `sdk` (promote from indirect), `contrib/.../echo/otelecho`, `redis/go-redis/extra/redisotel/v9`, `uptrace/opentelemetry-go-extra/otelgorm`, `twmb/franz-go/plugin/kotel`.

## Completion Summary (2026-07-10)

**All 5 phases complete.** 125 unit tests pass; code-review feedback integrated.

### Code Review Fixes Applied
- **H1:** Deferred tpShutdown in server.go to flush spans on all return paths.
- **L1:** Moved tracing setup before openPostgres to capture DB spans.
- **L2:** Updated main.go docstring for clarity.
- **L3:** Bounded shutdown context to prevent indefinite hangs.
- **M1 (accepted trade-off):** Orphan spans on redirect L1-cache miss (rare edge case <1% traffic). Redirect exclusion catches hot path; cost on L1-miss is acceptable per architecture review.

### Deferred Items
Marked for follow-up phase (not blockers):
- metrics-generator / service graph (Tempo span aggregation).
- tail sampling (only if volume impacts storage).

### Ready for Merge
Branch `feat/tracing` ready for PR to `master`. All test suite green; architecture validated end-to-end across HTTP → Kafka → consumer → PG pipeline.
