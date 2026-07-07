package metrics

import (
	"context"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestSetupExportsMetrics wires the real Prometheus exporter, records one of
// each instrument, and asserts the exported names appear on /metrics. It also
// pins the exact Prometheus names (the OTel exporter renames dots + suffixes),
// which the Grafana dashboard queries depend on.
func TestSetupExportsMetrics(t *testing.T) {
	reg, shutdown, err := Setup(func() bool { return true })
	if err != nil {
		t.Fatalf("Setup: %v", err)
	}
	defer shutdown(context.Background())

	ctx := context.Background()
	RecordHTTP(ctx, "GET", "/api/links/:code", 200, 0.012)
	RecordRedirect(ctx, "ok")
	RecordCacheLookup(ctx, true)
	RecordQuotaRejection(ctx)
	RecordClickEvent(ctx, "produced")
	dec := TrackInflight(ctx, "GET")
	dec()

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/metrics", nil)
	Handler(reg).ServeHTTP(rec, req)
	body := rec.Body.String()

	if rec.Code != 200 {
		t.Fatalf("status = %d", rec.Code)
	}
	// Exact Prometheus names the dashboard relies on.
	want := []string{
		"http_server_request_duration_seconds_bucket",
		"http_server_active_requests",
		"redirects_total",
		"link_cache_lookups_total",
		"quota_rejections_total",
		"click_events_total",
		"redis_breaker_open",
		`http_route="/api/links/:code"`, // route template, not a raw path
		`result="ok"`,
	}
	for _, w := range want {
		if !strings.Contains(body, w) {
			t.Errorf("missing %q in /metrics output", w)
		}
	}
}
