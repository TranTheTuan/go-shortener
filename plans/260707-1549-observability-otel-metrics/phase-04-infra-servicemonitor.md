# Phase 04 — Infra: metrics port + Service + ServiceMonitor

## Overview
- **Priority:** P2 · **Status:** done · Depends on: 01
- Expose the metrics port in the chart and let kube-prometheus-stack (`proxy-monitor`) scrape it via a ServiceMonitor.
- **Repo:** `../go-shortener-infra` (Helm chart), NOT the app repo.

## Related files
- Modify: `templates/deployment.yaml` (metrics containerPort + env)
- Modify: `templates/service.yaml` (metrics port)
- Create: `templates/servicemonitor.yaml`
- Modify: `values.yaml` (metrics block)
- Modify: `templates/configmap.yaml` (SERVER_METRICS_ADDR)

## Steps
1. **values.yaml** — add:
   ```yaml
   metrics:
     enabled: true
     port: 9464
     path: /metrics
     # Helm release name of kube-prometheus-stack — the ServiceMonitor MUST carry
     # this as `release:` label or Prometheus's default selector ignores it.
     prometheusRelease: proxy-monitor
     interval: 30s
   ```
2. **configmap.yaml** — add `SERVER_METRICS_ADDR: "0.0.0.0:{{ .Values.metrics.port }}"` (app reads METRICS_ADDR via SERVER_ prefix).
3. **deployment.yaml** — add a second containerPort:
   ```yaml
   - name: metrics
     containerPort: {{ .Values.metrics.port }}
   ```
   (main port stays 8080.)
4. **service.yaml** — add a named metrics port so the ServiceMonitor can target it:
   ```yaml
   - name: metrics
     port: {{ .Values.metrics.port }}
     targetPort: {{ .Values.metrics.port }}
   ```
   (keep the existing `port: 80 → 8080`.)
5. **servicemonitor.yaml** (new, gated by `.Values.metrics.enabled`):
   ```yaml
   apiVersion: monitoring.coreos.com/v1
   kind: ServiceMonitor
   metadata:
     name: {{ .Release.Name }}
     labels:
       release: {{ .Values.metrics.prometheusRelease }}   # REQUIRED for selection
   spec:
     selector:
       matchLabels:
         app: {{ .Release.Name }}          # must match the Service's selector/labels
     endpoints:
     - port: metrics                        # matches the Service port NAME
       path: {{ .Values.metrics.path }}
       interval: {{ .Values.metrics.interval }}
   ```
   - **Check:** the Service must have a label the selector matches. Current `service.yaml` sets only `spec.selector`, not `metadata.labels`. Add `metadata.labels.app: {{ .Release.Name }}` to the Service so the ServiceMonitor `selector.matchLabels.app` finds it.

## Todo
- [x] values `metrics` block (port 9464, prometheusRelease proxy-monitor)
- [x] configmap SERVER_METRICS_ADDR
- [x] deployment metrics containerPort
- [x] service named `metrics` port + `metadata.labels.app`
- [x] servicemonitor.yaml with `release: proxy-monitor` label
- [x] `helm template` renders; ServiceMonitor + Service port present

## Success criteria
- After `helm upgrade`, Prometheus Targets shows the go-shortener endpoint UP.
- `/metrics` NOT reachable via public ingress (only ClusterIP + scrape).

## Risks
- **Silent no-scrape** if `release:` label ≠ stack release, or the ServiceMonitor namespace isn't watched by Prometheus (default: all namespaces in kube-prometheus-stack unless restricted). Verify in Prometheus → Status → Targets.
- CRD `monitoring.coreos.com/v1` must exist (it does — kube-prometheus-stack installs it).
