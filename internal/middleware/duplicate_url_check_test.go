package middleware

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/labstack/echo/v4"
	"github.com/redis/go-redis/v9"

	"github.com/TranTheTuan/go-shortener/internal/service"
	"github.com/TranTheTuan/go-shortener/pkg/database"
	"github.com/TranTheTuan/go-shortener/pkg/redisbreaker"
)

func newDedup(t *testing.T) *service.DedupCache {
	t.Helper()
	mr, err := miniredis.Run()
	if err != nil {
		t.Fatalf("miniredis: %v", err)
	}
	t.Cleanup(mr.Close)
	rdb := &database.RedisClient{Client: redis.NewClient(&redis.Options{Addr: mr.Addr()})}
	return service.NewDedupCache(rdb, redisbreaker.New(10, time.Minute), time.Hour)
}

func dupReq(e *echo.Echo, body string, withUser bool) (echo.Context, *httptest.ResponseRecorder) {
	req := httptest.NewRequest(http.MethodPost, "/api/links", strings.NewReader(body))
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	if withUser {
		c.Set(ctxUserID, int64(1))
	}
	return c, rec
}

func TestDuplicateURLCheck_HitReturnsEarly(t *testing.T) {
	dedup := newDedup(t)
	dedup.Remember(t.Context(), 1, "https://example.com", "http://sho.rt/abc", time.Minute)

	c, rec := dupReq(echo.New(), `{"url":"https://example.com"}`, true)
	var ran bool
	_ = DuplicateURLCheck(dedup)(func(c echo.Context) error { ran = true; return c.NoContent(http.StatusCreated) })(c)

	if ran {
		t.Error("handler must not run on a dedup hit")
	}
	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "http://sho.rt/abc") {
		t.Errorf("body should contain cached short url, got %s", rec.Body.String())
	}
}

func TestDuplicateURLCheck_MissCallsNext(t *testing.T) {
	dedup := newDedup(t)
	c, _ := dupReq(echo.New(), `{"url":"https://nope.com"}`, true)
	var ran bool
	_ = DuplicateURLCheck(dedup)(func(c echo.Context) error { ran = true; return c.NoContent(http.StatusCreated) })(c)
	if !ran {
		t.Error("handler should run on a dedup miss")
	}
}

func TestDuplicateURLCheck_NoUserSkips(t *testing.T) {
	dedup := newDedup(t)
	dedup.Remember(t.Context(), 1, "https://example.com", "http://sho.rt/abc", time.Minute)

	c, _ := dupReq(echo.New(), `{"url":"https://example.com"}`, false) // no user_id
	var ran bool
	_ = DuplicateURLCheck(dedup)(func(c echo.Context) error { ran = true; return c.NoContent(http.StatusCreated) })(c)
	if !ran {
		t.Error("unowned request must skip the per-owner dedup fast-path")
	}
}
