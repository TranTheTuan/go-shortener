# Phase 04 — Infra: Tempo + Alloy forward + Grafana datasource

**Priority:** high | **Status:** complete | **Depends:** none (parallel with 01-03)

Deploy Tempo, turn the existing Alloy DaemonSet into an OTLP forwarder, wire the Grafana datasource + log↔trace correlation. All in `monitoring` ns, homelab k3s.

**Repo:** `../hdp-infra/monitoring/` (alongside loki-values.yaml, alloy-values.yaml, loki-datasource.yaml).

## Context
- Install pattern: helm values files + datasource ConfigMap (label `grafana_datasource: "1"` for kube-prometheus-stack sidecar discovery).
- Loki: filesystem, local-path PVC, 168h. Mirror for Tempo but 72h.
- Alloy: DaemonSet, currently `loki.source.kubernetes` → `loki.process` → `loki.write`. Add OTLP receive+forward.

## Design

### 1. Tempo (helm grafana/tempo, monolithic)
`../hdp-infra/monitoring/tempo-values.yaml`:
```yaml
# single-binary (monolithic) — NOT tempo-distributed
tempo:
  storage:
    trace:
      backend: local
      local: { path: /var/tempo/traces }
      wal: { path: /var/tempo/wal }
  retention: 72h           # compactor block_retention
  receivers:               # accept OTLP from Alloy
    otlp:
      protocols:
        grpc: { endpoint: 0.0.0.0:4317 }
persistence:
  enabled: true
  storageClassName: local-path
  size: 10Gi               # tune to disk
# metrics-generator: DISABLED (phase 2 — service graph deferred)
```
Install: `helm upgrade --install tempo grafana/tempo -n monitoring -f tempo-values.yaml`.
Service: `tempo.monitoring.svc.cluster.local` (distributor OTLP :4317, query :3200).

### 2. Alloy DaemonSet → add OTLP forwarder (no tail)
Append to `../hdp-infra/monitoring/alloy-values.yaml` Alloy river config:
```river
otelcol.receiver.otlp "default" {
  grpc { endpoint = "0.0.0.0:4317" }
  output { traces = [otelcol.processor.batch.default.input] }
}
otelcol.processor.batch "default" {
  output { traces = [otelcol.exporter.otlp.tempo.input] }
}
otelcol.exporter.otlp "tempo" {
  client {
    endpoint = "tempo.monitoring.svc.cluster.local:4317"
    tls { insecure = true }
  }
}
```
**No tail_sampling** — forward only. Spans of one trace may pass through different DaemonSet pods; Tempo reassembles by trace_id at storage. Head/keep-100% decision already made in-app.

Expose OTLP port on the Alloy Service/DaemonSet (4317) so apps can reach it. App endpoint = `alloy.monitoring.svc.cluster.local:4317` (ClusterIP fronting the DaemonSet).

### 3. Grafana datasource + correlation
`../hdp-infra/monitoring/tempo-datasource.yaml` (mirror loki-datasource.yaml):
```yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: tempo-grafana-datasource
  namespace: monitoring
  labels: { grafana_datasource: "1" }
data:
  tempo-datasource.yaml: |-
    apiVersion: 1
    datasources:
      - name: Tempo
        type: tempo
        access: proxy
        url: http://tempo.monitoring.svc.cluster.local:3200
        jsonData:
          tracesToLogsV2:
            datasourceUid: <loki-uid>
            filterByTraceID: true
            spanStartTimeShift: "-5m"
            spanEndTimeShift: "5m"
```
Add a **Loki derived field** (in loki-datasource.yaml jsonData) so log `trace_id` → Tempo:
```yaml
derivedFields:
  - name: TraceID
    matcherRegex: '"trace_id":"(\w+)"'
    url: '$${__value.raw}'
    datasourceUid: <tempo-uid>
```

## Related files
- create: `../hdp-infra/monitoring/tempo-values.yaml`, `../hdp-infra/monitoring/tempo-datasource.yaml`
- modify: `../hdp-infra/monitoring/alloy-values.yaml` (OTLP receiver+exporter, expose :4317), `../hdp-infra/monitoring/loki-datasource.yaml` (derivedFields)
- modify: go-shortener-infra deployment/configmap — set `TRACING_ENABLED=true`, `TRACING_OTLP_ENDPOINT=alloy.monitoring.svc.cluster.local:4317`, `SERVICE_VERSION` (git sha) for server/consumer/bulk-worker.

## Todo
- [x] tempo-values.yaml + helm install
- [x] alloy-values.yaml OTLP receiver+batch+exporter, expose 4317, helm upgrade
- [x] tempo-datasource.yaml (tracesToLogsV2)
- [x] loki-datasource.yaml derivedFields (trace_id → Tempo)
- [x] app deployment env (TRACING_* + SERVICE_VERSION) in go-shortener-infra
- [x] verify Tempo receives spans (`/api/echo` or curl `/status`)

## Success criteria
- Tempo pod Running, PVC bound (local-path).
- Alloy accepts OTLP on 4317 and forwards to Tempo.
- Grafana shows Tempo datasource; a trace is queryable by trace_id.
- Loki log line with trace_id shows a clickable "Tempo" link.

## Security
- OTLP insecure gRPC in-cluster only. Do NOT expose 4317 via ingress. Cross-check Alloy Service type stays ClusterIP.
