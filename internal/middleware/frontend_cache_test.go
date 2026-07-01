package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"testing/fstest"

	"github.com/labstack/echo/v4"
)

// testFS mimics the embedded frontend layout (index.html + static assets).
var testFS = fstest.MapFS{
	"index.html":       {Data: []byte("<!doctype html>")},
	"static/app.js":    {Data: []byte("console.log(1)")},
	"static/style.css": {Data: []byte("body{}")},
}

func serve(t *testing.T, path, ifNoneMatch string) *httptest.ResponseRecorder {
	t.Helper()
	e := echo.New()
	e.Use(FrontendCache(testFS))
	e.GET(path, func(c echo.Context) error { return c.String(http.StatusOK, "body") })

	req := httptest.NewRequest(http.MethodGet, path, nil)
	if ifNoneMatch != "" {
		req.Header.Set("If-None-Match", ifNoneMatch)
	}
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	return rec
}

func TestFrontendCache_SetsHeaders(t *testing.T) {
	for _, path := range []string{"/", "/static/app.js"} {
		rec := serve(t, path, "")
		if got := rec.Header().Get("Cache-Control"); got != "no-cache" {
			t.Errorf("%s: Cache-Control = %q, want no-cache", path, got)
		}
		if rec.Header().Get("ETag") == "" {
			t.Errorf("%s: ETag not set", path)
		}
	}
}

func TestFrontendCache_NotModified(t *testing.T) {
	first := serve(t, "/static/app.js", "")
	etag := first.Header().Get("ETag")

	second := serve(t, "/static/app.js", etag)
	if second.Code != http.StatusNotModified {
		t.Fatalf("status = %d, want %d", second.Code, http.StatusNotModified)
	}
	if second.Body.Len() != 0 {
		t.Errorf("304 response has body %q, want empty", second.Body.String())
	}
	if second.Header().Get("ETag") != etag {
		t.Errorf("304 ETag = %q, want %q", second.Header().Get("ETag"), etag)
	}
}

func TestFrontendCache_StaleETagServesBody(t *testing.T) {
	rec := serve(t, "/static/app.js", `"stale"`)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
}

func TestFrontendCache_UnknownPathPassthrough(t *testing.T) {
	rec := serve(t, "/api/links", "")
	if rec.Header().Get("ETag") != "" {
		t.Error("ETag set on non-frontend path")
	}
	if rec.Header().Get("Cache-Control") != "" {
		t.Error("Cache-Control set on non-frontend path")
	}
}
