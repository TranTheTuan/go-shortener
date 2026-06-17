package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/labstack/echo/v4"
)

// newContext builds an Echo context whose request carries the given API key
// header (omitted when empty).
func newContext(e *echo.Echo, key string) (echo.Context, *httptest.ResponseRecorder) {
	req := httptest.NewRequest(http.MethodPost, "/api/links", nil)
	if key != "" {
		req.Header.Set(apiKeyHeader, key)
	}
	rec := httptest.NewRecorder()
	return e.NewContext(req, rec), rec
}

// okHandler is a trivial downstream handler that records whether it ran.
func okHandler(ran *bool) echo.HandlerFunc {
	return func(c echo.Context) error {
		*ran = true
		return c.NoContent(http.StatusOK)
	}
}

func TestAPIKey_ValidKeyPassesThrough(t *testing.T) {
	e := echo.New()
	var ran bool
	mw := APIKey([]string{"good-key", "another"})

	ctx, rec := newContext(e, "good-key")
	if err := mw(okHandler(&ran))(ctx); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !ran {
		t.Error("downstream handler did not run for valid key")
	}
	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", rec.Code)
	}
}

func TestAPIKey_Rejects(t *testing.T) {
	cases := map[string]struct {
		keys []string
		sent string
	}{
		"missing header":   {keys: []string{"good-key"}, sent: ""},
		"wrong key":        {keys: []string{"good-key"}, sent: "bad-key"},
		"empty key set":    {keys: nil, sent: "anything"},
		"empty configured": {keys: []string{"", "  "}, sent: "x"},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			e := echo.New()
			var ran bool
			mw := APIKey(tc.keys)

			ctx, rec := newContext(e, tc.sent)
			if err := mw(okHandler(&ran))(ctx); err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if ran {
				t.Error("downstream handler ran despite invalid auth")
			}
			if rec.Code != http.StatusUnauthorized {
				t.Errorf("status = %d, want 401", rec.Code)
			}
		})
	}
}
