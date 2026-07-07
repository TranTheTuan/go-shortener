# Brainstorm — Backend Observability (Metrics + Logs)

- **Date:** 2026-07-07
- **Type:** technical brainstorm (no implementation)
- **Stack given:** Prometheus + Grafana (kube-prometheus-stack) + Loki, already deployed.
- **Decision status:** architecture agreed; implementation plan deferred.

## Problem
Collect metrics + logs from the Go/Echo backend into the existing Prometheus/Grafana/Loki stack. Backend runs on K8s (Helm chart `go-shortener`), already logs slog JSON to stdout; no metrics yet.

## Current state (scouted)
- Metrics: none (no prometheus/otel deps).
- Logs: slog JSON → stdout. `RequestID()` middleware on; `RequestLogger()` commented (no per-request access log).
- Ports: `8080` only. pprof on `localhost:6060` (loopback, not scrapable).
- Infra: kube-prometheus-stack (Prometheus Operator, **pull**). Loki fed by Alloy/Promtail scraping pod stdout.

## Key decision (brutal-honesty call)
User initially picked "OpenTelemetry (OTLP)", but the stack is **pull-based**. Pure OTLP push needs an **OTel Collector** they don't have — over-engineering for one service (YAGNI).
→ **Chosen: OTel SDK (metric API) + OTel Prometheus exporter.** App exposes `/metrics`; Prometheus Operator scrapes via **ServiceMonitor**. Keeps OTel (vendor-neutral, reusable for tracing later) with **no Collector**, native to the pull stack.
- Deferred alt: OTLP → Collector, only when Tempo tracing / central pipeline is added.

## Agreed architecture

### Metrics
- OTel `MeterProvider` + `exporters/prometheus`; `/metrics` handler on a **dedicated port** (e.g. `:9464`), bound `0.0.0.0` (Prometheus must reach it — unlike pprof loopback). Not routed via ingress.
- Echo RED via a small middleware recording OTel instruments (otelecho is trace-focused; HTTP metrics recorded manually). Add OTel Go runtime/process metrics (free).
- **Initial metric set (KISS):**
  - `http.server.request.duration` histogram {method, route-template, status} + in-flight gauge
  - `redirects_total{result=ok|notfound|expired|disabled}`
  - `link_cache_lookups_total{result=hit|miss}`
  - `quota_rejections_total`, `breaker_state` gauge
  - `click_events_produced_total{result=ok|dropped}`
- **Cardinality rule:** `route` = Echo template (`/:code`), never the raw path; status by exact/class. NEVER label by user_id / short_code / URL.

### Logs (Loki) — mostly already flowing
- Enable `RequestLogger` (currently commented) → per-request access log (JSON, same slog handler): method, route, status, latency, `request_id`, `user_id`, bytes.
- Per-request slog logger carrying `request_id` (from existing RequestID mw); add `trace_id` later when tracing lands.
- **Loki labels low-cardinality only:** `app, namespace, pod, level` (+ maybe `route`). `request_id/user_id/trace_id` are log **fields** (LogQL JSON parse), NOT labels.
- Alloy pipeline: JSON parse stage + `level` label + drop noisy fields (infra, not app code).

### Infra (chart `go-shortener` + kube-prometheus-stack)
- `deployment.yaml`: add metrics `containerPort`; `service.yaml`: add metrics port; new **`ServiceMonitor`** CRD.
- **Gotcha:** kube-prometheus-stack `serviceMonitorSelectorNilUsesHelmValues=true` (default) → the ServiceMonitor must carry a label matching the stack's Helm release (e.g. `release: <kube-prometheus-stack-release>`), else Prometheus ignores it.
- Grafana: RED dashboard from metrics + Loki log panel filtered by `app`; correlate via `request_id`.

## Approaches evaluated
| Option | Verdict |
|--------|---------|
| client_golang + echoprometheus (pull, native) | Simplest, but no path to OTel tracing; rejected in favor of OTel API. |
| OTel SDK + Prometheus exporter (pull) | **CHOSEN** — OTel API + native pull, no Collector. |
| OTel SDK + OTLP + Collector (push) | Deferred — extra workload; revisit with Tempo. |
| In-app Loki push client | Rejected — anti-pattern; stdout+Alloy is K8s-native. |

## Risks / considerations
- Metric cardinality blow-up if route/label hygiene ignored (biggest risk).
- ServiceMonitor label-selector gotcha (silent no-scrape).
- Loki high-cardinality labels → cost/perf; keep IDs as fields.
- `/metrics` must not be exposed via public ingress.

## Success criteria
- Prometheus scrapes `/metrics`; RED + domain metrics visible in Grafana.
- Access logs with `request_id` queryable in Loki; low label cardinality.
- Zero Collector; no app-side log push.

## Next steps
- (Deferred) `/plan` for: OTel metrics bootstrap + Echo RED middleware + domain instruments; enable RequestLogger + request_id slog; metrics port; ServiceMonitor + chart wiring; Alloy JSON pipeline; Grafana dashboard; tests.

## Resolved parameters
1. **kube-prometheus-stack release = `proxy-monitor`** → ServiceMonitor must carry label `release: proxy-monitor` (else the default selector ignores it).
2. **Metrics port = `9464`** (OTel default).
3. **Log agent = Alloy**, current config is **raw forward** (`loki.source.kubernetes → loki.write`, no `loki.process`/JSON stage). Logs reach Loki intact (queryable via LogQL `| json`) — acceptable. Recommended enrichment: `discovery.relabel` to promote low-card labels (`app`, `namespace`, `pod`) + `loki.process` with `stage.json`+`stage.labels` promoting ONLY `level`; keep request_id/user_id as fields. Loki push endpoint: `http://loki-gateway.monitoring.svc.cluster.local/loki/api/v1/push`.

## Unresolved questions
- None. All parameters resolved; ready for `/plan`.
