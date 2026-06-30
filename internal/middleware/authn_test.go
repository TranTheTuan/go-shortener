package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/labstack/echo/v4"

	"github.com/TranTheTuan/go-shortener/pkg/token"
)

func newAuthnContext(e *echo.Echo, header, value string) (echo.Context, *httptest.ResponseRecorder) {
	req := httptest.NewRequest(http.MethodPost, "/api/links", nil)
	if header != "" {
		req.Header.Set(header, value)
	}
	rec := httptest.NewRecorder()
	return e.NewContext(req, rec), rec
}

func TestAuthn_JWTSetsUserID(t *testing.T) {
	iss := token.NewIssuer("secret", time.Hour)
	tok, _ := iss.Issue(7)

	e := echo.New()
	var gotID int64
	var ran bool
	h := func(c echo.Context) error {
		ran = true
		gotID, _ = UserIDFrom(c)
		return c.NoContent(http.StatusOK)
	}

	ctx, rec := newAuthnContext(e, "Authorization", "Bearer "+tok)
	_ = Authn(iss, []string{"key1"})(h)(ctx)
	if !ran || rec.Code != http.StatusOK {
		t.Fatalf("handler should run for valid JWT; status=%d", rec.Code)
	}
	if gotID != 7 {
		t.Errorf("user_id = %d, want 7", gotID)
	}
}

func TestAuthn_APIKeyHasNoUserID(t *testing.T) {
	iss := token.NewIssuer("secret", time.Hour)
	e := echo.New()
	var hadUser bool
	h := func(c echo.Context) error {
		_, hadUser = UserIDFrom(c)
		return c.NoContent(http.StatusOK)
	}

	ctx, rec := newAuthnContext(e, apiKeyHeader, "key1")
	_ = Authn(iss, []string{"key1"})(h)(ctx)
	if rec.Code != http.StatusOK {
		t.Fatalf("handler should run for valid API key; status=%d", rec.Code)
	}
	if hadUser {
		t.Error("API-key auth must not set a user_id")
	}
}

func TestAuthn_Rejects(t *testing.T) {
	iss := token.NewIssuer("secret", time.Hour)
	cases := map[string]struct{ header, value string }{
		"no credentials": {"", ""},
		"bad api key":    {apiKeyHeader, "wrong"},
		"bad jwt":        {"Authorization", "Bearer not-a-jwt"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			e := echo.New()
			var ran bool
			h := func(c echo.Context) error { ran = true; return c.NoContent(http.StatusOK) }

			ctx, rec := newAuthnContext(e, tc.header, tc.value)
			_ = Authn(iss, []string{"key1"})(h)(ctx)
			if ran {
				t.Error("handler ran despite invalid credentials")
			}
			if rec.Code != http.StatusUnauthorized {
				t.Errorf("status = %d, want 401", rec.Code)
			}
		})
	}
}
