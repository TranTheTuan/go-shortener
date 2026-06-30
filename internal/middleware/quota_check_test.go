package middleware

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/labstack/echo/v4"
)

// mockQuota is a test double for service.QuotaService.
type mockQuota struct {
	allow        bool
	allowCalls   int
	releaseCalls int
}

func (m *mockQuota) Allow(_ context.Context, _ int64) (bool, error) {
	m.allowCalls++
	return m.allow, nil
}
func (m *mockQuota) Release(_ context.Context, _ int64) { m.releaseCalls++ }

func quotaCtx(e *echo.Echo, withUser bool) (echo.Context, *httptest.ResponseRecorder) {
	req := httptest.NewRequest(http.MethodPost, "/api/links", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	if withUser {
		c.Set(ctxUserID, int64(1))
	}
	return c, rec
}

func TestQuotaCheck_SkipsWhenNoUser(t *testing.T) {
	q := &mockQuota{allow: true}
	c, rec := quotaCtx(echo.New(), false)
	var ran bool
	_ = QuotaCheck(q)(func(c echo.Context) error { ran = true; return c.NoContent(http.StatusCreated) })(c)

	if q.allowCalls != 0 {
		t.Error("Allow must not be called for API-key/unowned requests")
	}
	if !ran || rec.Code != http.StatusCreated {
		t.Errorf("handler should run; status=%d", rec.Code)
	}
}

func TestQuotaCheck_OverLimitFlagsAndDefersToHandler(t *testing.T) {
	q := &mockQuota{allow: false}
	c, _ := quotaCtx(echo.New(), true)
	var ran, flagged bool
	_ = QuotaCheck(q)(func(c echo.Context) error {
		ran = true
		flagged = c.Get(CtxQuotaExhausted) == true
		return c.NoContent(http.StatusCreated)
	})(c)

	// Over limit no longer rejects in the middleware — it flags the context so a
	// dedup can still succeed; the handler enforces the 429 for a new link.
	if !ran {
		t.Error("handler should run so dedup can resolve before the 429 decision")
	}
	if !flagged {
		t.Error("CtxQuotaExhausted must be set when over limit")
	}
	if q.releaseCalls != 0 {
		t.Errorf("releaseCalls = %d, want 0 (nothing reserved when over limit)", q.releaseCalls)
	}
}

func TestQuotaCheck_PassesUnderLimit(t *testing.T) {
	q := &mockQuota{allow: true}
	c, rec := quotaCtx(echo.New(), true)
	_ = QuotaCheck(q)(func(c echo.Context) error { return c.NoContent(http.StatusCreated) })(c)

	if rec.Code != http.StatusCreated || q.releaseCalls != 0 {
		t.Errorf("under limit: status=%d releaseCalls=%d, want 201 / 0", rec.Code, q.releaseCalls)
	}
}

func TestQuotaCheck_RefundsOnDownstreamError(t *testing.T) {
	q := &mockQuota{allow: true}
	c, _ := quotaCtx(echo.New(), true)
	_ = QuotaCheck(q)(func(c echo.Context) error { return c.NoContent(http.StatusInternalServerError) })(c)

	if q.releaseCalls != 1 {
		t.Errorf("releaseCalls = %d, want 1 (refund on downstream failure)", q.releaseCalls)
	}
}

func TestQuotaCheck_RefundsOnReused(t *testing.T) {
	q := &mockQuota{allow: true}
	c, _ := quotaCtx(echo.New(), true)
	_ = QuotaCheck(q)(func(c echo.Context) error {
		c.Set(CtxLinkReused, true)
		return c.NoContent(http.StatusCreated)
	})(c)

	if q.releaseCalls != 1 {
		t.Errorf("releaseCalls = %d, want 1 (refund when link reused)", q.releaseCalls)
	}
}
