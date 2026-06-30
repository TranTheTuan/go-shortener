package middleware

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/labstack/echo/v4"

	"github.com/TranTheTuan/go-shortener/internal/repository"
	"github.com/TranTheTuan/go-shortener/internal/service"
	"github.com/TranTheTuan/go-shortener/pkg/keycloak"
)

// mockVerifier is a test double for keycloak.TokenVerifier.
type mockVerifier struct {
	id  *keycloak.Identity
	err error
}

func (m *mockVerifier) Verify(_ context.Context, _ string) (*keycloak.Identity, error) {
	return m.id, m.err
}

// mockSyncer is a test double for the middleware's userSyncer.
type mockSyncer struct {
	user *repository.User
	err  error
	got  service.SyncInput
}

func (m *mockSyncer) SyncFromKeycloak(_ context.Context, in service.SyncInput) (*repository.User, error) {
	m.got = in
	return m.user, m.err
}

func newKCContext(e *echo.Echo, authHeader string) (echo.Context, *httptest.ResponseRecorder) {
	req := httptest.NewRequest(http.MethodGet, "/auth/me", nil)
	if authHeader != "" {
		req.Header.Set("Authorization", authHeader)
	}
	rec := httptest.NewRecorder()
	return e.NewContext(req, rec), rec
}

func TestKeycloak_ValidTokenProvisionsAndSetsUserID(t *testing.T) {
	v := &mockVerifier{id: &keycloak.Identity{Sub: "kc-uuid-1", Email: "a@b.com", Username: "alice"}}
	s := &mockSyncer{user: &repository.User{ID: 77}}

	e := echo.New()
	var gotID int64
	var ran bool
	h := func(c echo.Context) error {
		ran = true
		gotID, _ = UserIDFrom(c)
		return c.NoContent(http.StatusOK)
	}

	ctx, rec := newKCContext(e, "Bearer valid")
	_ = Keycloak(v, s)(h)(ctx)

	if !ran || rec.Code != http.StatusOK {
		t.Fatalf("handler should run for a valid token; status=%d", rec.Code)
	}
	if gotID != 77 {
		t.Errorf("user_id = %d, want 77 (local id from JIT sync)", gotID)
	}
	if s.got.Sub != "kc-uuid-1" || s.got.Email != "a@b.com" || s.got.Username != "alice" {
		t.Errorf("sync input = %+v, want claims passed through", s.got)
	}
}

func TestKeycloak_Rejects(t *testing.T) {
	okUser := &repository.User{ID: 1}
	cases := map[string]struct {
		header   string
		verifier *mockVerifier
		syncer   *mockSyncer
		want     int
	}{
		"missing header": {"", &mockVerifier{id: &keycloak.Identity{Sub: "x"}}, &mockSyncer{user: okUser}, http.StatusUnauthorized},
		"no bearer":      {"Token abc", &mockVerifier{id: &keycloak.Identity{Sub: "x"}}, &mockSyncer{user: okUser}, http.StatusUnauthorized},
		"verify fails":   {"Bearer bad", &mockVerifier{err: errors.New("invalid")}, &mockSyncer{user: okUser}, http.StatusUnauthorized},
		"sync db error":  {"Bearer ok", &mockVerifier{id: &keycloak.Identity{Sub: "x"}}, &mockSyncer{err: errors.New("db down")}, http.StatusInternalServerError},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			e := echo.New()
			var ran bool
			h := func(c echo.Context) error { ran = true; return c.NoContent(http.StatusOK) }

			ctx, rec := newKCContext(e, tc.header)
			_ = Keycloak(tc.verifier, tc.syncer)(h)(ctx)
			if ran {
				t.Error("handler ran despite auth failure")
			}
			if rec.Code != tc.want {
				t.Errorf("status = %d, want %d", rec.Code, tc.want)
			}
		})
	}
}
