# Phase 05 — Grafana Dashboard + Docs

## Overview
- **Priority:** P3 · **Status:** done · Depends on: 02, 03, 04
- A RED + domain dashboard, and doc updates.

## Related files
- Create: `../go-shortener-infra/monitoring/grafana-dashboard-go-shortener.json` (importable dashboard)
- Modify: `docs/system-architecture.md` + `docs/deployment-guide.md` (observability section)
- Modify: `docs/project-roadmap.md` (v1.6 Observability — metrics done)

## Steps
1. **Dashboard JSON** — panels (PromQL; confirm exact metric names from `/metrics` first — OTel renames dots→underscores + unit suffix):
   - **Request rate**: `sum by (http_route) (rate(http_server_request_duration_seconds_count[5m]))`
   - **Error rate**: `sum(rate(http_server_request_duration_seconds_count{http_status_code=~"5.."}[5m])) / sum(rate(http_server_request_duration_seconds_count[5m]))`
   - **Latency p50/p95/p99**: `histogram_quantile(0.95, sum by (le,http_route) (rate(http_server_request_duration_seconds_bucket[5m])))`
   - **In-flight**: `http_server_active_requests`
   - **Redirect outcomes**: `sum by (result) (rate(redirects_total[5m]))`
   - **Cache hit ratio**: `sum(rate(link_cache_lookups_total{result="hit"}[5m])) / sum(rate(link_cache_lookups_total[5m]))`
   - **Quota rejections**: `rate(quota_rejections_total[5m])`
   - **Kafka events**: `sum by (result) (rate(click_events_total[5m]))`
   - **Breaker open**: `redis_breaker_open`
   - **Runtime**: goroutines, heap, GC pauses (from runtime metrics).
   - Optional logs panel (Loki datasource): `{app="go-shortener"} | json | level="ERROR"`.
2. **Provisioning** — either import JSON via Grafana UI, or add as a dashboard ConfigMap with label `grafana_dashboard: "1"` if the stack has the sidecar. Document whichever the stack uses.
3. **Docs** — add an "Observability" subsection: metrics endpoint, ServiceMonitor, dashboard import steps, PromQL cheat-sheet, cardinality rules.

## Todo
- [x] dashboard JSON (RED + domain + runtime + optional logs)
- [x] provisioning note (import vs sidecar ConfigMap)
- [x] docs: system-architecture + deployment-guide observability section
- [x] roadmap v1.6 metrics marked done

## Success criteria
- Dashboard renders with live data after scrape.
- PromQL queries match the ACTUAL exported metric names (verify against `/metrics`).

## Notes
- Metric names are the #1 gotcha — OTel Prometheus exporter naming differs from hand-written client_golang names. Copy names verbatim from `/metrics` before finalizing queries.
