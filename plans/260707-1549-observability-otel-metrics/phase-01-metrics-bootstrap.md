# Phase 01 — Metrics Bootstrap

## Overview
- **Priority:** P2 · **Status:** done · Depends on: none
- Stand up the OTel MeterProvider with a Prometheus exporter, serve `/metrics` on a dedicated port, register Go runtime metrics, wire into server startup + graceful shutdown.

## Related files
- Create: `pkg/metrics/metrics.go` (provider setup + shared instrument registry)
- Modify: `configs/config.go` (`ServerConfig.MetricsAddr`)
- Modify: `cmd/server/server.go` (init provider, start `/metrics` server, shutdown)
- `go.mod` (new deps)

## Steps
1. **Config** — add to `ServerConfig` (mirror `PprofAddr`):
   ```go
   // MetricsAddr serves the Prometheus /metrics endpoint. Empty disables it.
   // Bind 0.0.0.0 (Prometheus scrapes the pod IP) — do NOT expose via ingress.
   MetricsAddr string `env:"METRICS_ADDR" envDefault:"0.0.0.0:9464"`
   ```
2. **pkg/metrics** — `Init() (*prometheus.Registry, error)` or `Setup()`:
   - Create a `prometheus.NewRegistry()`.
   - `exporter, _ := otelprom.New(otelprom.WithRegisterer(reg))` (`go.opentelemetry.io/otel/exporters/prometheus`).
   - `mp := sdkmetric.NewMeterProvider(sdkmetric.WithReader(exporter))`; `otel.SetMeterProvider(mp)`.
   - Return the registry (for the handler) + the provider (for shutdown). Keep a package-level `meter = otel.Meter("github.com/TranTheTuan/go-shortener")` used by instrument constructors in later phases.
   - Add default histogram buckets suited to HTTP latencies via a `sdkmetric.View` (explicit bucket boundaries e.g. 5ms..10s) so duration histograms are useful.
3. **Runtime metrics** — `runtime.Start(runtime.WithMeterProvider(mp))` from `go.opentelemetry.io/contrib/instrumentation/runtime` (Go GC/heap/goroutines).
4. **Serve /metrics** — in `server.go`, when `cfg.Server.MetricsAddr != ""`, start a separate `http.Server` whose mux serves `/metrics` via `promhttp.HandlerFor(reg, promhttp.HandlerOpts{})`. Run in a goroutine (like pprof). Add it to graceful shutdown (Shutdown on signal, alongside the main server).
5. **Shutdown** — `mp.Shutdown(ctx)` + metrics `http.Server.Shutdown(ctx)` in the existing shutdown path.

## Todo
- [x] `ServerConfig.MetricsAddr` (+ .env.example, values later)
- [x] `pkg/metrics` provider + Prometheus exporter + registry + meter + latency buckets view
- [x] runtime metrics started
- [x] `/metrics` http.Server wired + graceful shutdown
- [x] `go mod tidy`; `go vet ./...`; hit `curl localhost:9464/metrics` shows `go_*`/`target_info`

## Success criteria
- `/metrics` returns Prometheus text with runtime metrics; main API unaffected.
- Provider shuts down cleanly on SIGTERM (no goroutine leak).

## Notes
- Keep `pkg/metrics` free of business types — only OTel/prometheus wiring + instrument accessors. Later phases add instruments here.
- Do NOT serve `/metrics` on the main 8080 mux (keep it off the public ingress surface).
