// Package observability wires OpenTelemetry tracing for the service. Metrics
// live in pkg/metrics (Prometheus pull); this package owns the push-based trace
// pipeline (OTLP gRPC → Alloy → Tempo) plus the slog↔trace correlation handler.
package observability

import (
	"context"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.26.0"
)

// Config carries everything SetupTracing needs. Kept here (not in configs) so
// this package has no dependency on the config loader; the caller maps its own
// env-driven struct onto this.
type Config struct {
	Enabled     bool
	Endpoint    string  // OTLP gRPC endpoint host:port (the Alloy forwarder)
	SampleRatio float64 // 1.0 = keep every trace; lower only for load tests
	ServiceName string  // e.g. go-shortener-server / -consumer / -bulk-worker
	Version     string  // build/deploy version (git sha) — service.version
	Env         string  // deployment.environment
}

// SetupTracing installs a global TracerProvider backed by an OTLP gRPC exporter
// and returns a shutdown func that flushes buffered spans. When c.Enabled is
// false it installs nothing and returns a no-op shutdown, so tracing is fully
// opt-in — the binary runs identically without a collector.
//
// The sampling decision is head-based (ParentBased(TraceIDRatioBased)): made
// once at the root span and propagated via W3C tracecontext to every downstream
// service (HTTP + Kafka), so no centralized collector is needed. Alloy only
// forwards; Tempo reassembles spans by trace_id at storage.
func SetupTracing(ctx context.Context, c Config) (func(context.Context) error, error) {
	noop := func(context.Context) error { return nil }
	if !c.Enabled {
		return noop, nil
	}

	// Schemaless ("") so Merge never conflicts with resource.Default()'s own
	// schema URL (which tracks the SDK's semconv version, not ours). The
	// attribute keys are stable regardless of schema.
	res, err := resource.Merge(resource.Default(), resource.NewWithAttributes(
		"",
		semconv.ServiceName(c.ServiceName),
		semconv.ServiceVersion(c.Version),
		semconv.DeploymentEnvironment(c.Env),
	))
	if err != nil {
		return noop, err
	}

	exp, err := otlptracegrpc.New(ctx,
		otlptracegrpc.WithEndpoint(c.Endpoint),
		otlptracegrpc.WithInsecure(), // in-cluster only; never exposed via ingress
	)
	if err != nil {
		return noop, err
	}

	tp := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exp),
		sdktrace.WithResource(res),
		sdktrace.WithSampler(sdktrace.ParentBased(sdktrace.TraceIDRatioBased(c.SampleRatio))),
	)
	otel.SetTracerProvider(tp)
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{}, propagation.Baggage{},
	))
	return tp.Shutdown, nil
}
