package observability

import (
	"context"
	"testing"

	"go.opentelemetry.io/otel"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
)

func TestSetupTracing_DisabledIsNoop(t *testing.T) {
	before := otel.GetTracerProvider()

	shutdown, err := SetupTracing(context.Background(), Config{Enabled: false})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// No provider should have been installed.
	if otel.GetTracerProvider() != before {
		t.Fatal("disabled tracing must not replace the global TracerProvider")
	}
	// Shutdown must be a safe no-op.
	if err := shutdown(context.Background()); err != nil {
		t.Fatalf("noop shutdown returned error: %v", err)
	}
}

func TestSetupTracing_EnabledInstallsProvider(t *testing.T) {
	// A bad endpoint is fine: otlptracegrpc.New does not dial eagerly, so setup
	// succeeds and installs the provider; export failures happen later in the bg.
	shutdown, err := SetupTracing(context.Background(), Config{
		Enabled:     true,
		Endpoint:    "127.0.0.1:4317",
		SampleRatio: 1.0,
		ServiceName: "test-svc",
		Version:     "test",
		Env:         "test",
	})
	if err != nil {
		t.Fatalf("setup failed: %v", err)
	}
	defer func() { _ = shutdown(context.Background()) }()

	if _, ok := otel.GetTracerProvider().(*sdktrace.TracerProvider); !ok {
		t.Fatalf("expected an sdktrace.TracerProvider to be installed, got %T", otel.GetTracerProvider())
	}
}
