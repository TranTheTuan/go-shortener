---
title: "Observability — OTel Metrics"
description: "OpenTelemetry metrics via Prometheus exporter, scraped by kube-prometheus-stack; RED + domain metrics + Grafana dashboard."
status: complete
priority: P2
effort: ~8h
branch: master
tags: [backend, observability, otel, prometheus, grafana, infra]
created: 2026-07-07
completed: 2026-07-07
---

# Observability — OTel Metrics — Implementation Plan

Instrument the Go/Echo backend with **OpenTelemetry metrics** exported via the
**Prometheus exporter** (pull), scraped by the existing **kube-prometheus-stack**
through a **ServiceMonitor**. Expose RED (rate/errors/duration) + a small set of
domain metrics, plus a Grafana dashboard.

## Context
- Design: `plans/reports/brainstorm-260707-1459-observability-metrics-logs.md` (READ IT).
- **Decided:** OTel SDK + Prometheus exporter, **NO Collector** (stack is pull-based). Metrics port **9464**. kube-prometheus-stack release **`proxy-monitor`** → ServiceMonitor needs label `release: proxy-monitor`.
- **Already shipped (out of scope):** structured error logging at `pkg/response/response.go` (request_id/route/cause) + service error-wrapping; Loki via Alloy (`hdp-infra/monitoring/alloy-values.yaml`). This plan is **metrics only**.

## Architecture (one line)
`OTel MeterProvider + Prometheus exporter → /metrics on :9464 (0.0.0.0) → ServiceMonitor(release=proxy-monitor) → Prometheus → Grafana`

## Cardinality rules (NON-NEGOTIABLE)
- HTTP label `route` = Echo template (`c.Path()`, e.g. `/api/links/:code`), NEVER raw path.
- Domain labels bounded to small enums (result=hit|miss, etc.). NEVER label by user_id / short_code / URL.

## Phases
| # | Phase | Depends on | Status |
|---|-------|-----------|--------|
| 01 | [Metrics bootstrap (provider, exporter, /metrics server, runtime)](phase-01-metrics-bootstrap.md) | – | done |
| 02 | [HTTP RED middleware](phase-02-http-red-middleware.md) | 01 | done |
| 03 | [Domain instruments (redirect/cache/quota/breaker/kafka)](phase-03-domain-instruments.md) | 01 | done |
| 04 | [Infra: chart port + Service + ServiceMonitor](phase-04-infra-servicemonitor.md) | 01 | done |
| 05 | [Grafana dashboard + docs](phase-05-grafana-dashboard.md) | 02,03,04 | done |
| 06 | [Tests](phase-06-tests.md) | 01–03 | done |

## New dependencies
- `go.opentelemetry.io/otel`, `.../sdk/metric`, `.../exporters/prometheus`
- `go.opentelemetry.io/contrib/instrumentation/runtime` (Go runtime metrics)
- `github.com/prometheus/client_golang/prometheus` + `promhttp` (registry + handler for the exporter)

## Cross-cutting principles
- YAGNI: no OTel Collector, no tracing (Tempo) yet, no push, no per-worker metrics initially (server only).
- KISS: one `pkg/metrics` package owns instrument definitions + record helpers; call sites stay one-liners.
- DRY: instruments defined once, reused; reuse existing graceful-shutdown pattern in `cmd/server/server.go`.

## Global risks
- **Cardinality blow-up** → enforced by the rules above; reviewer must check labels.
- **ServiceMonitor not selected** if the `release: proxy-monitor` label is missing (silent no-scrape).
- `/metrics` must bind `0.0.0.0` (Prometheus scrapes pod IP) — unlike pprof's loopback — but must NOT be routed via public ingress.
- Instrument import from service/handler layers couples them to `pkg/metrics` — acceptable (cross-cutting, like slog); keep the package dependency-free of business logic.

## Unresolved questions
- Instrument the `analyze`/`bulk-worker` processes too, or server only for v1? (Plan assumes **server only**; workers later.)
