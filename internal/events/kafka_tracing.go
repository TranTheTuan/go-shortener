package events

import (
	"go.opentelemetry.io/otel"

	"github.com/twmb/franz-go/pkg/kgo"
	"github.com/twmb/franz-go/plugin/kotel"
)

// newKafkaTracer builds a kotel tracer bound to the global TracerProvider +
// propagator (installed by observability.SetupTracing). When tracing is
// disabled the globals are no-ops, so this stays cheap.
func newKafkaTracer() *kotel.Tracer {
	return kotel.NewTracer(
		kotel.TracerProvider(otel.GetTracerProvider()),
		kotel.TracerPropagator(otel.GetTextMapPropagator()),
	)
}

// tracingHooks returns kgo hooks that inject trace context into records on
// produce and extract it (into rec.Context) on fetch, so a trace continues
// across the Kafka boundary. Applied to every Kafka client via buildKGOOpts.
func tracingHooks() []kgo.Hook {
	return kotel.NewKotel(kotel.WithTracer(newKafkaTracer())).Hooks()
}
