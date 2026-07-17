package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/labstack/echo/v4"

	"github.com/TranTheTuan/go-shortener/web"
)

func TestParseIssuer(t *testing.T) {
	cases := map[string][2]string{
		"http://auth.cd.me/realms/nine-realms": {"http://auth.cd.me", "nine-realms"},
		"https://auth.cd.me/realms/r/":         {"https://auth.cd.me", "r"},
		"https://no-realm.example":             {"https://no-realm.example", ""},
	}
	for in, want := range cases {
		authURL, realm := parseIssuer(in)
		if authURL != want[0] || realm != want[1] {
			t.Errorf("parseIssuer(%q) = (%q, %q), want (%q, %q)", in, authURL, realm, want[0], want[1])
		}
	}
}

func TestFrontendHandler_Config(t *testing.T) {
	e := echo.New()
	h := NewFrontendHandler("http://auth.cd.me/realms/nine-realms", "go-shortener", "", "1.0")

	req := httptest.NewRequest(http.MethodGet, "/app-config.json", nil)
	rec := httptest.NewRecorder()
	if err := h.Config(e.NewContext(req, rec)); err != nil {
		t.Fatalf("Config: %v", err)
	}

	var got appConfig
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.AuthURL != "http://auth.cd.me" || got.Realm != "nine-realms" || got.ClientID != "go-shortener" || got.TermsVersion != "1.0" {
		t.Errorf("config = %+v", got)
	}
}

// TestFrontendRouting verifies the SPA routes resolve and take precedence over
// the /:code redirect catch-all (a real short code still reaches the catch-all).
func TestFrontendRouting(t *testing.T) {
	e := echo.New()
	h := NewFrontendHandler("http://auth.cd.me/realms/nine-realms", "go-shortener", "", "1.0")
	e.FileFS("/", "index.html", web.Files)
	e.StaticFS("/static", echo.MustSubFS(web.Files, "static"))
	e.GET("/app-config.json", h.Config)

	var redirectHit bool
	e.GET("/:code", func(c echo.Context) error {
		redirectHit = true
		return c.NoContent(http.StatusOK)
	})

	cases := []struct {
		path         string
		wantRedirect bool
	}{
		{"/", false},
		{"/static/app.js", false},
		{"/static/styles.css", false},
		{"/app-config.json", false},
		{"/Ab3xY7q", true}, // a real short code → catch-all
	}
	for _, tc := range cases {
		redirectHit = false
		req := httptest.NewRequest(http.MethodGet, tc.path, nil)
		rec := httptest.NewRecorder()
		e.ServeHTTP(rec, req)

		if redirectHit != tc.wantRedirect {
			t.Errorf("%s: redirect catch-all hit = %v, want %v (status %d)", tc.path, redirectHit, tc.wantRedirect, rec.Code)
		}
		if !tc.wantRedirect && rec.Code != http.StatusOK {
			t.Errorf("%s: status = %d, want 200", tc.path, rec.Code)
		}
	}
}
