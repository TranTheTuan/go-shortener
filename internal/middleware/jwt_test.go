package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/labstack/echo/v4"

	"github.com/TranTheTuan/go-shortener/pkg/token"
)

// newJWTContext builds an Echo context with the given Authorization header
// (omitted when empty).
func newJWTContext(e *echo.Echo, authHeader string) (echo.Context, *httptest.ResponseRecorder) {
	req := httptest.NewRequest(http.MethodGet, "/auth/me", nil)
	if authHeader != "" {
		req.Header.Set("Authorization", authHeader)
	}
	rec := httptest.NewRecorder()
	return e.NewContext(req, rec), rec
}

func TestJWT_ValidTokenPassesThrough(t *testing.T) {
	iss := token.NewIssuer("secret", time.Hour)
	tok, _ := iss.Issue(99)

	e := echo.New()
	var gotID int64
	handler := func(c echo.Context) error {
		id, ok := UserIDFrom(c)
		if !ok {
			t.Error("UserIDFrom returned ok=false in downstream handler")
		}
		gotID = id
		return c.NoContent(http.StatusOK)
	}

	ctx, rec := newJWTContext(e, "Bearer "+tok)
	if err := JWT(iss)(handler)(ctx); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", rec.Code)
	}
	if gotID != 99 {
		t.Errorf("user_id = %d, want 99", gotID)
	}
}

func TestJWT_Rejects(t *testing.T) {
	iss := token.NewIssuer("secret", time.Hour)
	expired := token.NewIssuer("secret", -time.Minute)
	expiredTok, _ := expired.Issue(1)

	cases := map[string]string{
		"missing header":   "",
		"no bearer prefix": "Token abc",
		"empty bearer":     "Bearer ",
		"malformed token":  "Bearer not-a-jwt",
		"expired token":    "Bearer " + expiredTok,
	}

	for name, header := range cases {
		t.Run(name, func(t *testing.T) {
			e := echo.New()
			var ran bool
			handler := func(c echo.Context) error {
				ran = true
				return c.NoContent(http.StatusOK)
			}

			ctx, rec := newJWTContext(e, header)
			if err := JWT(iss)(handler)(ctx); err != nil {
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
