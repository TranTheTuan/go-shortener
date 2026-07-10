# Brainstorm: Distributed Tracing with Grafana Tempo

**Date:** 2026-07-09 | **Status:** agreed, pending plan
**Stack:** Go 1.26, Echo v4, GORM/Postgres, go-redis, franz-go/Kafka; k3s homelab; monitoring ns (Grafana + kube-prometheus-stack + Loki + Alloy)

## Problem statement

App already emits **metrics** (OTel MeterProvider + Prometheus pull exporter) and **logs** (slog JSON → Loki via Alloy). Missing the third pillar: **traces**. Want request causality + the **async Kafka flow** (redirect → produce click → consumer → PG; bulk-worker) visualized end-to-end, plus log↔trace↔metric correlation in Grafana.

Traces MUST push (OTLP) — cannot reuse the metrics pull path. New pipeline required.

## Decisions (agreed — FINAL, revised to B after debate)

| Axis | Choice |
|---|---|
| Export path | App → **existing Alloy DaemonSet** (OTLP receiver, **forward only, no tail**) → Tempo (OTLP gRPC). **0 new pods.** |
| Sampling | **Head / keep 100%** at real (low) traffic; decision made in-app at root span, propagated. Lower head ratio only during load tests. NO tail → no central collector needed. |
| Scope | **Auto full** (HTTP+Redis+GORM+Kafka) + **trace propagation across Kafka** (producer→consumer). **Redirect route EXCLUDED** from tracing (hot path, low trace value, protects L1 perf gains). |
| Storage | Tempo single-binary, **filesystem local-path PVC, 72h retention** (mirror Loki) |
| Service graph (metrics-generator) | **Phase 2 / deferred** — not in initial implementation |

**Why B over tail:** tail sampling would force a centralized single-replica Alloy Deployment (SPOF + ship-all-spans + coupling), justified only by the 4800 rps *load-test* volume — not real homelab traffic. At low real traffic, keeping 100% via head is simpler, captures ALL errors anyway, needs no central collector, and lets the existing logs DaemonSet double as a plain OTLP forwarder (Tempo reassembles spans by trace_id at storage — forwarding needs no whole-trace view). Add tail + dedicated collector later only if trace volume actually hurts (YAGNI).

## Architecture

```
[go-shortener-server] ┐
[go-shortener-consumer]├─OTLP gRPC→ [Alloy-traces (1 replica)] ─tail_sampling→ OTLP→ [Tempo] ←── Grafana
[go-shortener-bulk-worker]┘              (dedicated Deployment)                 (filesystem)   (datasource)
```

### ⚠️ Gotcha #1 (critical): tail sampling needs a CENTRALIZED collector
Tail sampling decides AFTER a trace completes, so **all spans of a trace must reach the same collector instance**. The existing **Alloy logs is a DaemonSet (per-node)** — a distributed trace (producer node A, consumer node B) splits across instances → each sees a partial trace → broken/inconsistent decisions.

**Resolution:** deploy a **separate single-replica Alloy Deployment dedicated to traces** (`otelcol.receiver.otlp` → `otelcol.processor.tail_sampling` → `otelcol.exporter.otlp`→Tempo). Do NOT put the trace pipeline on the logs DaemonSet. Single instance = homelab-acceptable SPOF; the "correct" scale-out answer (load-balancing exporter keyed by trace_id → tier-2 samplers) is over-engineering here — YAGNI.

### App side (all THREE services, not just the API)
- **TracerProvider** (sdktrace) + OTLP gRPC exporter → Alloy-traces svc; BatchSpanProcessor.
- **Sampler = AlwaysSample** in-app on non-hot paths. Tail decision lives in Alloy, so app must emit spans for Alloy to judge → app ships full span volume (see Gotcha #2).
- Share ONE OTel **Resource** (service.name, service.version=git sha, deployment.environment) between the existing MeterProvider and the new TracerProvider — refactor `pkg/metrics.Setup` into a shared `pkg/observability` bootstrap (or add `metrics.SetupTracing`).
- Auto-instrument:
  - `otelecho` middleware (HTTP server spans)
  - `redisotel.InstrumentTracing(rdb)` (go-redis — official)
  - `otelgorm` plugin (GORM — uptrace community, acceptable)
  - `kotel` (franz-go OTel plugin): inject trace ctx into Kafka record headers on produce, extract on consume → **single trace spans producer→broker→consumer→PG**. This is the payoff; wire it in server (produce) AND consumer + bulk-worker (extract).

### Alloy-traces config
- `otelcol.receiver.otlp` grpc :4317 (http :4318 optional)
- `otelcol.processor.tail_sampling` policies:
  - `status_code = ERROR` → keep
  - `latency > 500ms` (tune) → keep
  - `probabilistic 5%` → remainder
- `otelcol.exporter.otlp` → `tempo.monitoring.svc:4317` (insecure, in-cluster)
- batch processor before export.

### Tempo (helm `grafana/tempo`, monolithic)
- storage.trace.backend: `local` (filesystem), PVC `local-path`.
- `compactor.compaction.block_retention: 72h` (traces heavier than logs → shorter than Loki's 168h).
- distributor OTLP receiver on 4317.
- **Optional (flag, not default): metrics-generator** → service graph + span RED metrics, remote-write to Prometheus. Killer feature (dependency map) but extra config + Prometheus remote-write-receiver enabled. Defer to phase 2.

### Grafana wiring
- `tempo-datasource.yaml` ConfigMap, label `grafana_datasource: "1"`, url `http://tempo.monitoring.svc.cluster.local:3200`.
- `tracesToLogsV2` (Tempo→Loki, match by trace_id + time).
- Loki **derived field** `traceID` → Tempo (logs→trace jump).
- `tracesToMetrics` (optional) → Prometheus.

### Correlation (why tracing earns its keep alongside existing metrics+logs)
- **slog ↔ trace:** custom `slog.Handler` wrapper that reads span context from ctx and adds `trace_id`/`span_id` fields. App already uses `slog.*Context` (ctx in hand) — low effort. Result: every Loki log line carries trace_id → click to open the trace.
- **metrics ↔ trace (optional):** OTel Prometheus exporter exemplars (trace_id on histogram buckets) → Grafana Metrics→Traces. Nice-to-have, phase 2.

## Brutal-honesty risks / trade-offs

1. **Gotcha #2 — tail sampling ships FULL span volume app→Alloy.** To keep all errors, app can't pre-drop (outcome unknown at span start), so at 4800 rps load test the app emits ~4800 traces/s to Alloy even though most are dropped. Network + Alloy CPU cost. Mitigations: BatchSpanProcessor compression; Alloy in-cluster; **real traffic ≪ 4800 rps** so this only bites during load tests. Acceptable.

2. **Tension with the L1-cache perf work.** You just optimized the redirect hot path (L1, p50 20→13ms). `otelecho` creates a span **per request** regardless of sampling (needed for tail) → per-request CPU overhead on the exact path you sped up; app is now CPU-leaning under load. **Recommendation:** head-sample or EXCLUDE the redirect endpoint from tracing (404/410/cache-hit redirects are low trace value), full-trace `/api/*` + async flows. Net: keep hot-path overhead near zero, trace where it matters. (redisotel adds spans only on actual Redis calls — L1 hits skip Redis, so synergy is good there.)

3. **Scope is 3 services, not 1.** server + consumer + bulk-worker all need TracerProvider + kotel to make the async trace continuous. Under-wiring any breaks the producer→consumer link.

4. **SPOF:** single-replica Alloy-traces. If it dies, traces drop (logs/metrics unaffected). Homelab-acceptable; don't scale out (would reintroduce Gotcha #1).

5. **~5 new deps** (otelecho, redisotel, otelgorm, kotel, otlptracegrpc). otelgorm is community — only real caveat.

6. **Storage growth:** even sampled, traces on local-path. 72h retention + 5% + errors bounded. Monitor PVC.

## Success criteria
- One trace in Grafana spans: HTTP redirect → (Redis) → produce → **Kafka** → consumer → PG insert, as ONE connected trace.
- Error request → trace kept (tail) + Loki log line links to it via trace_id.
- Redirect hot path throughput regression < ~5% vs pre-tracing (guarded by excluding/low-sampling that route).
- Tempo PVC stable under 72h retention.

## Config additions (app)
`TRACING_ENABLED`, `OTEL_EXPORTER_OTLP_TRACES_ENDPOINT` (alloy-traces svc:4317), `TRACING_SAMPLE_RATIO` (head ratio for hot path if excluded-path approach not used), `SERVICE_VERSION` (git sha for Resource).

## Open questions
1. metrics-generator (service graph) now or phase 2? (recommend phase 2)
2. Redirect route: exclude from tracing entirely, or head-sample at low ratio (e.g. 1%)? (recommend exclude or 1%)
3. Tail latency threshold for "slow" — 500ms default ok, or tie to SLO?
4. OTLP app→Alloy: gRPC insecure in-cluster ok (no mTLS)? (recommend yes, homelab)
