// Package metrics wires OpenTelemetry metrics to a Prometheus exporter and
// exposes one-line record helpers used across the app. The MeterProvider is set
// up once via Setup(); record helpers are no-ops until then, so instrumented
// call sites are safe even when metrics are disabled (workers, tests).
//
// Cardinality rule: labels are bounded enums or route templates only — never
// user_id / short_code / URL.
package metrics

import (
	"context"
	"net/http"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"go.opentelemetry.io/contrib/instrumentation/runtime"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	otelprom "go.opentelemetry.io/otel/exporters/prometheus"
	"go.opentelemetry.io/otel/metric"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
)

const scopeName = "github.com/TranTheTuan/go-shortener"

// instruments holds every metric the app records. Built once in Setup.
type instruments struct {
	httpDuration    metric.Float64Histogram
	httpInflight    metric.Int64UpDownCounter
	redirects       metric.Int64Counter
	cacheLookups    metric.Int64Counter
	quotaRejections metric.Int64Counter
	clickEvents     metric.Int64Counter
}

// inst is set once by Setup, before the server starts serving — reads after are
// race-free. nil means metrics are disabled; all helpers become no-ops.
var inst *instruments

// Setup builds the OTel MeterProvider backed by a Prometheus exporter, creates
// all instruments, starts Go runtime metrics, and returns the Prometheus
// registry (for the /metrics handler) plus a shutdown func. breakerOpen, when
// non-nil, feeds an observable gauge for the Redis circuit-breaker state.
func Setup(breakerOpen func() bool) (*prometheus.Registry, func(context.Context) error, error) {
	reg := prometheus.NewRegistry()
	exporter, err := otelprom.New(otelprom.WithRegisterer(reg))
	if err != nil {
		return nil, nil, err
	}

	// HTTP-latency histogram buckets (seconds).
	view := sdkmetric.NewView(
		sdkmetric.Instrument{Name: "http.server.request.duration"},
		sdkmetric.Stream{Aggregation: sdkmetric.AggregationExplicitBucketHistogram{
			Boundaries: []float64{0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10},
		}},
	)

	provider := sdkmetric.NewMeterProvider(
		sdkmetric.WithReader(exporter),
		sdkmetric.WithView(view),
	)
	otel.SetMeterProvider(provider)

	if err := runtime.Start(runtime.WithMeterProvider(provider)); err != nil {
		return nil, nil, err
	}
	if err := buildInstruments(provider, breakerOpen); err != nil {
		return nil, nil, err
	}
	return reg, provider.Shutdown, nil
}

func buildInstruments(provider *sdkmetric.MeterProvider, breakerOpen func() bool) error {
	m := provider.Meter(scopeName)
	i := &instruments{}
	var err error
	if i.httpDuration, err = m.Float64Histogram("http.server.request.duration",
		metric.WithUnit("s"), metric.WithDescription("HTTP server request duration")); err != nil {
		return err
	}
	if i.httpInflight, err = m.Int64UpDownCounter("http.server.active_requests",
		metric.WithDescription("in-flight HTTP requests")); err != nil {
		return err
	}
	// Counter names OMIT the _total suffix — the Prometheus exporter appends it,
	// so these export as redirects_total, link_cache_lookups_total, etc.
	if i.redirects, err = m.Int64Counter("redirects",
		metric.WithDescription("redirect outcomes by result")); err != nil {
		return err
	}
	if i.cacheLookups, err = m.Int64Counter("link_cache_lookups",
		metric.WithDescription("link cache lookups by result")); err != nil {
		return err
	}
	if i.quotaRejections, err = m.Int64Counter("quota_rejections",
		metric.WithDescription("requests rejected by the daily quota")); err != nil {
		return err
	}
	if i.clickEvents, err = m.Int64Counter("click_events",
		metric.WithDescription("click events by produce result")); err != nil {
		return err
	}

	if breakerOpen != nil {
		if _, err = m.Int64ObservableGauge("redis_breaker_open",
			metric.WithDescription("redis circuit breaker: 1=open, 0=closed"),
			metric.WithInt64Callback(func(_ context.Context, o metric.Int64Observer) error {
				if breakerOpen() {
					o.Observe(1)
				} else {
					o.Observe(0)
				}
				return nil
			}),
		); err != nil {
			return err
		}
	}

	inst = i
	return nil
}

// Handler returns the Prometheus /metrics HTTP handler for the registry.
func Handler(reg *prometheus.Registry) http.Handler {
	return promhttp.HandlerFor(reg, promhttp.HandlerOpts{})
}

// --- record helpers (no-op until Setup runs) ---

// RecordHTTP observes one request's duration, labelled by method/route/status.
// route MUST be the Echo template (e.g. /api/links/:code), not the raw path.
func RecordHTTP(ctx context.Context, method, route string, status int, seconds float64) {
	if inst == nil {
		return
	}
	inst.httpDuration.Record(ctx, seconds, metric.WithAttributes(
		attribute.String("http.method", method),
		attribute.String("http.route", route),
		attribute.Int("http.status_code", status),
	))
}

// TrackInflight increments the in-flight gauge and returns a func to decrement it.
func TrackInflight(ctx context.Context, method string) func() {
	if inst == nil {
		return func() {}
	}
	a := metric.WithAttributes(attribute.String("http.method", method))
	inst.httpInflight.Add(ctx, 1, a)
	return func() { inst.httpInflight.Add(ctx, -1, a) }
}

// RecordRedirect counts a redirect outcome (ok|notfound|expired|disabled).
func RecordRedirect(ctx context.Context, result string) {
	if inst == nil {
		return
	}
	inst.redirects.Add(ctx, 1, metric.WithAttributes(attribute.String("result", result)))
}

// RecordCacheLookup counts a link-cache hit or miss.
func RecordCacheLookup(ctx context.Context, hit bool) {
	if inst == nil {
		return
	}
	result := "miss"
	if hit {
		result = "hit"
	}
	inst.cacheLookups.Add(ctx, 1, metric.WithAttributes(attribute.String("result", result)))
}

// RecordQuotaRejection counts a request rejected by the daily quota.
func RecordQuotaRejection(ctx context.Context) {
	if inst == nil {
		return
	}
	inst.quotaRejections.Add(ctx, 1)
}

// RecordClickEvent counts a click event by produce result (produced|dropped).
func RecordClickEvent(ctx context.Context, result string) {
	if inst == nil {
		return
	}
	inst.clickEvents.Add(ctx, 1, metric.WithAttributes(attribute.String("result", result)))
}
