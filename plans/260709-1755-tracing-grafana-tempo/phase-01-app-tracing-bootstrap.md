# Phase 01 — App tracing bootstrap (shared)

**Priority:** high | **Status:** complete | **Depends:** none

Shared OTel tracing setup usable by all 3 entrypoints (server/consumer/bulk-worker). Mirrors the existing `pkg/metrics.Setup` pattern; shares ONE Resource between meter + tracer.

## Context
- Existing: `pkg/metrics/metrics.go` — `Setup(breakerOpen) (*prometheus.Registry, func(ctx)error, error)` builds MeterProvider + Prometheus exporter, calls `otel.SetMeterProvider`.
- otel v1.44.0 already in go.mod; `otel/trace`, `otel/sdk` currently indirect.
- 3 run funcs in one binary: `cmd/server/{server.go,consumer.go,bulk_worker.go}`.

## Design
New `pkg/observability/tracing.go` (keep metrics.go as-is; do NOT merge — separation clearer). Shared Resource helper so meter+tracer agree on service identity.

```go
// pkg/observability/resource.go
func NewResource(serviceName, version string) (*resource.Resource, error) {
    return resource.Merge(resource.Default(), resource.NewWithAttributes(
        semconv.SchemaURL,
        semconv.ServiceName(serviceName),
        semconv.ServiceVersion(version),
        semconv.DeploymentEnvironment(env), // from cfg.Env
    ))
}

// pkg/observability/tracing.go
// SetupTracing builds a TracerProvider with an OTLP gRPC exporter -> Alloy.
// Returns a shutdown func (flushes BatchSpanProcessor). No-op (nil shutdown,
// no global set) when cfg.Enabled is false, so tracing is fully opt-in.
func SetupTracing(ctx context.Context, cfg TracingConfig, res *resource.Resource) (func(context.Context) error, error) {
    if !cfg.Enabled {
        return func(context.Context) error { return nil }, nil
    }
    exp, err := otlptracegrpc.New(ctx,
        otlptracegrpc.WithEndpoint(cfg.Endpoint), // alloy svc host:4317
        otlptracegrpc.WithInsecure(),             // in-cluster, no mTLS (homelab)
    )
    if err != nil { return nil, err }
    tp := sdktrace.NewTracerProvider(
        sdktrace.WithBatcher(exp),
        sdktrace.WithResource(res),
        sdktrace.WithSampler(sdktrace.ParentBased(
            sdktrace.TraceIDRatioBased(cfg.SampleRatio), // default 1.0
        )),
    )
    otel.SetTracerProvider(tp)
    otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
        propagation.TraceContext{}, propagation.Baggage{},
    )) // W3C traceparent + baggage — carries sampling decision across HTTP+Kafka
    return tp.Shutdown, nil
}
```

## Config (configs/config.go)
```go
type TracingConfig struct {
    Enabled     bool          `env:"ENABLED" envDefault:"false"`
    Endpoint    string        `env:"OTLP_ENDPOINT" envDefault:"alloy.monitoring.svc.cluster.local:4317"`
    SampleRatio float64       `env:"SAMPLE_RATIO" envDefault:"1.0"` // 1.0=keep all; lower for load tests
}
// add to Config: Tracing TracingConfig `envPrefix:"TRACING_"`
// add ServiceVersion string `env:"SERVICE_VERSION" envDefault:"dev"` (git sha at build/deploy) for Resource
```

## Wiring (each entrypoint)
In `server.go` / `consumer.go` / `bulk_worker.go`, near the top of the run func:
```go
res, err := observability.NewResource("go-shortener-server", cfg.ServiceVersion) // -consumer / -bulk-worker
tpShutdown, err := observability.SetupTracing(ctx, cfg.Tracing, res)
defer func() { _ = tpShutdown(context.Background()) }() // flush spans on exit (before os.Exit paths)
```
Ensure shutdown runs in the graceful-shutdown block alongside metricsShutdown (server.go lines ~194).

## Related files
- create: `pkg/observability/tracing.go`, `pkg/observability/resource.go`
- modify: `configs/config.go`, `cmd/server/server.go`, `cmd/server/consumer.go`, `cmd/server/bulk_worker.go`

## Todo
- [x] add deps: `otlptracegrpc`, promote `sdk`
- [x] TracingConfig + ServiceVersion in config
- [x] resource.go + tracing.go
- [x] wire SetupTracing + deferred shutdown in all 3 run funcs
- [x] `go build ./...`

## Success criteria
- `TRACING_ENABLED=false` → zero behavior change, no global tracer set (default off).
- `TRACING_ENABLED=true` with a reachable OTLP endpoint → app starts, spans flushed on shutdown.

## Security
- OTLP gRPC insecure — acceptable in-cluster only. Endpoint must be a cluster-internal svc, never ingress.
