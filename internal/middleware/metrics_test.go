package middleware

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/labstack/echo/v4"

	"github.com/TranTheTuan/go-shortener/pkg/metrics"
)

// TestMetrics_RecordsRouteTemplate verifies the middleware labels requests by
// the Echo route template (e.g. /api/links/:code), not the request path, ensuring
// cardinality is bounded even with path params. Also verifies the middleware:
// - calls next and returns its result
// - reads status after next() (handlers write synchronously)
// - does not panic when metrics are un-initialized (Setup not called)
func TestMetrics_RecordsRouteTemplate(t *testing.T) {
	// Setup metrics once for the test. This registers the OTel provider globally
	// and builds all instruments. All subsequent requests use this same provider.
	reg, shutdown, err := metrics.Setup(nil)
	if err != nil {
		t.Fatalf("metrics.Setup: %v", err)
	}
	defer shutdown(context.Background())

	e := echo.New()
	handlerRan := false
	handler := func(c echo.Context) error {
		handlerRan = true
		code := c.Param("code")
		if code != "abc123" {
			t.Errorf("param code = %q, want abc123", code)
		}
		return c.NoContent(http.StatusFound)
	}

	// Register the route with the template. Echo stores the template in c.Path()
	// so the middleware can read it after next() returns.
	e.GET("/api/links/:code", handler, Metrics())

	// Serve a request to a concrete path. The middleware will extract the
	// template /api/links/:code from c.Path().
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/links/abc123", nil)
	e.ServeHTTP(rec, req)

	// Verify the handler ran and the response status was recorded.
	if !handlerRan {
		t.Error("handler must run (middleware must call next)")
	}
	if rec.Code != http.StatusFound {
		t.Errorf("status = %d, want %d (middleware must return next's result)", rec.Code, http.StatusFound)
	}

	// Scrape /metrics and verify the route template label is present (not the
	// concrete path /api/links/abc123). This proves cardinality is bounded: all
	// requests to /api/links/{any-code} collapse into one series.
	metricsRec := httptest.NewRecorder()
	metricsReq := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	metrics.Handler(reg).ServeHTTP(metricsRec, metricsReq)

	if metricsRec.Code != http.StatusOK {
		t.Fatalf("GET /metrics: status = %d, want 200", metricsRec.Code)
	}

	body := metricsRec.Body.String()

	// Assert the route label is the template.
	if !strings.Contains(body, `http_route="/api/links/:code"`) {
		t.Errorf("metrics must label by route template /api/links/:code, not by request path")
		t.Logf("metrics output (relevant lines):\n%s", filterMetrics(body, "http_"))
	}

	// Assert the status was recorded.
	if !strings.Contains(body, `http_status_code="302"`) {
		t.Errorf("metrics must record http_status_code=302")
	}
}

// TestMetrics_NoOpWhenUninitialized verifies the middleware does not panic and
// passes requests through correctly even when metrics.Setup has not been called
// (metrics are disabled/no-op state).
func TestMetrics_NoOpWhenUninitialized(t *testing.T) {
	e := echo.New()
	handlerRan := false
	handler := func(c echo.Context) error {
		handlerRan = true
		return c.JSON(http.StatusOK, map[string]string{"status": "ok"})
	}

	e.GET("/api/test", handler, Metrics())

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/test", nil)
	e.ServeHTTP(rec, req)

	// Verify the handler ran (middleware was transparent) and the response is correct.
	if !handlerRan {
		t.Error("handler must run when metrics are uninitialized")
	}
	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), `"status":"ok"`) {
		t.Errorf("response body = %q, want json with status ok", rec.Body.String())
	}
}

// TestMetrics_PassesErrorsThrough verifies the middleware passes handler errors
// through unchanged and still records metrics on error responses.
func TestMetrics_PassesErrorsThrough(t *testing.T) {
	reg, shutdown, err := metrics.Setup(nil)
	if err != nil {
		t.Fatalf("metrics.Setup: %v", err)
	}
	defer shutdown(context.Background())

	e := echo.New()
	handler := func(c echo.Context) error {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "test"})
	}

	e.GET("/api/error", handler, Metrics())

	// Request the error endpoint.
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/error", nil)
	e.ServeHTTP(rec, req)

	// Verify the error response is passed through.
	if rec.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusInternalServerError)
	}

	// Verify status 500 was recorded in metrics.
	metricsRec := httptest.NewRecorder()
	metricsReq := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	metrics.Handler(reg).ServeHTTP(metricsRec, metricsReq)

	body := metricsRec.Body.String()
	if !strings.Contains(body, `http_status_code="500"`) {
		t.Errorf("metrics must record error status codes")
	}
}

// filterMetrics extracts lines from Prometheus output that contain the given prefix
// for readability in test failure logs.
func filterMetrics(body, prefix string) string {
	var lines []string
	for _, line := range strings.Split(body, "\n") {
		if strings.Contains(line, prefix) {
			lines = append(lines, line)
		}
	}
	return strings.Join(lines, "\n")
}
