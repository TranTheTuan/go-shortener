package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/labstack/echo/v4"

	"github.com/TranTheTuan/go-shortener/internal/repository"
	"github.com/TranTheTuan/go-shortener/internal/service"
	"github.com/TranTheTuan/go-shortener/pkg/apperror"
)

// stubLinkService satisfies service.LinkService; only ListByOwner is exercised.
type stubLinkService struct {
	listFn   func(ctx context.Context, ownerID int64, status string, limit, offset int) ([]*repository.OwnedLink, int64, error)
	deleteFn func(ctx context.Context, code string, ownerID int64) (*repository.Link, error)
	updateFn func(ctx context.Context, code string, ownerID int64, expiresAt *time.Time, isActive bool) (*repository.Link, error)
}

func (s stubLinkService) Create(context.Context, service.CreateLinkInput) (*repository.Link, bool, error) {
	return nil, false, nil
}
func (s stubLinkService) Resolve(context.Context, string) (*repository.Link, error) { return nil, nil }
func (s stubLinkService) ListByOwner(ctx context.Context, ownerID int64, status string, limit, offset int) ([]*repository.OwnedLink, int64, error) {
	if s.listFn != nil {
		return s.listFn(ctx, ownerID, status, limit, offset)
	}
	return nil, 0, nil
}
func (s stubLinkService) Delete(ctx context.Context, code string, ownerID int64) (*repository.Link, error) {
	if s.deleteFn != nil {
		return s.deleteFn(ctx, code, ownerID)
	}
	return nil, nil
}
func (s stubLinkService) Update(ctx context.Context, code string, ownerID int64, expiresAt *time.Time, isActive bool) (*repository.Link, error) {
	if s.updateFn != nil {
		return s.updateFn(ctx, code, ownerID, expiresAt, isActive)
	}
	return nil, nil
}

func TestLinkHandler_List(t *testing.T) {
	var gotOwner int64
	var gotLimit, gotOffset int
	svc := stubLinkService{listFn: func(_ context.Context, ownerID int64, status string, limit, offset int) ([]*repository.OwnedLink, int64, error) {
		gotOwner, gotLimit, gotOffset = ownerID, limit, offset
		return []*repository.OwnedLink{
			{Link: repository.Link{ShortCode: "abc1234", OriginalURL: "https://example.com"}, TotalClicks: 5},
		}, 1, nil
	}}
	h := NewLinkHandler(svc, nil, nil, "http://sho.rt")

	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/api/links?limit=50&offset=10", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	c.Set("user_id", int64(7)) // set by the Keycloak middleware in production

	if err := h.List(c); err != nil {
		t.Fatalf("List: %v", err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	// Owner + parsed pagination reach the service.
	if gotOwner != 7 || gotLimit != 50 || gotOffset != 10 {
		t.Errorf("owner/limit/offset = %d/%d/%d, want 7/50/10", gotOwner, gotLimit, gotOffset)
	}

	var env struct {
		Data listResponse `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if env.Data.Total != 1 || len(env.Data.Items) != 1 {
		t.Fatalf("response = %+v", env.Data)
	}
	if env.Data.Items[0].ShortURL != "http://sho.rt/abc1234" {
		t.Errorf("short_url = %q, want http://sho.rt/abc1234", env.Data.Items[0].ShortURL)
	}
	if env.Data.Items[0].TotalClicks != 5 {
		t.Errorf("total_clicks = %d, want 5", env.Data.Items[0].TotalClicks)
	}
}

func TestLinkHandler_List_Unauthenticated(t *testing.T) {
	h := NewLinkHandler(stubLinkService{}, nil, nil, "http://sho.rt")

	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/api/links", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec) // no user_id in context

	_ = h.List(c)
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", rec.Code)
	}
}

func TestLinkHandler_List_StatusFilter(t *testing.T) {
	var gotStatus string
	svc := stubLinkService{listFn: func(_ context.Context, _ int64, status string, _, _ int) ([]*repository.OwnedLink, int64, error) {
		gotStatus = status
		return []*repository.OwnedLink{}, 0, nil
	}}
	h := NewLinkHandler(svc, nil, nil, "http://sho.rt")

	cases := []struct {
		query       string
		wantStatus  string
		wantCode    int
	}{
		{"?status=active", "active", http.StatusOK},
		{"?status=disabled", "disabled", http.StatusOK},
		{"?status=expired", "expired", http.StatusOK},
		{"", "", http.StatusOK},
		{"?status=bogus", "", http.StatusBadRequest},
	}

	for _, tc := range cases {
		e := echo.New()
		req := httptest.NewRequest(http.MethodGet, "/api/links"+tc.query, nil)
		rec := httptest.NewRecorder()
		c := e.NewContext(req, rec)
		c.Set("user_id", int64(7))

		_ = h.List(c)
		if rec.Code != tc.wantCode {
			t.Errorf("query=%s: status = %d, want %d", tc.query, rec.Code, tc.wantCode)
		}
		if tc.wantCode == http.StatusOK && gotStatus != tc.wantStatus {
			t.Errorf("query=%s: passed status = %q, want %q", tc.query, gotStatus, tc.wantStatus)
		}
	}
}

// Note on Delete handler tests: The LinkHandler.Delete method calls h.dedup.Forget(),
// which requires a non-nil DedupCache. Standard unit testing with nil mocks doesn't work
// here since DedupCache is a concrete type, not an interface. Full handler Delete testing
// would require integrating with a real or well-mocked DedupCache with Redis.
// For now, we test the non-dedup Delete/Update scenarios instead.


func TestLinkHandler_Update_Valid(t *testing.T) {
	futureTime := time.Now().Add(time.Hour)
	svc := stubLinkService{
		updateFn: func(_ context.Context, code string, ownerID int64, expiresAt *time.Time, isActive bool) (*repository.Link, error) {
			if code != "abc1234" || ownerID != 7 || !isActive {
				t.Errorf("update: code=%q owner=%d active=%v", code, ownerID, isActive)
			}
			return &repository.Link{ID: 1, ShortCode: code, OriginalURL: "https://example.com", IsActive: isActive, ExpiresAt: expiresAt}, nil
		},
	}
	h := NewLinkHandler(svc, nil, nil, "http://sho.rt")

	body, _ := json.Marshal(map[string]any{"is_active": true, "expires_at": futureTime.Format(time.RFC3339)})
	e := echo.New()
	req := httptest.NewRequest(http.MethodPut, "/api/links/abc1234", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	c.SetParamNames("code")
	c.SetParamValues("abc1234")
	c.Set("user_id", int64(7))

	_ = h.Update(c)
	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", rec.Code)
	}
}

func TestLinkHandler_Update_MissingIsActive(t *testing.T) {
	h := NewLinkHandler(stubLinkService{}, nil, nil, "http://sho.rt")

	body, _ := json.Marshal(map[string]any{"expires_at": nil})
	e := echo.New()
	req := httptest.NewRequest(http.MethodPut, "/api/links/abc1234", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	c.SetParamNames("code")
	c.SetParamValues("abc1234")
	c.Set("user_id", int64(7))

	_ = h.Update(c)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", rec.Code)
	}
}

func TestLinkHandler_Update_BadJSON(t *testing.T) {
	h := NewLinkHandler(stubLinkService{}, nil, nil, "http://sho.rt")

	e := echo.New()
	req := httptest.NewRequest(http.MethodPut, "/api/links/abc1234", bytes.NewReader([]byte("invalid json")))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	c.SetParamNames("code")
	c.SetParamValues("abc1234")
	c.Set("user_id", int64(7))

	_ = h.Update(c)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", rec.Code)
	}
}

func TestLinkHandler_Update_Unauthenticated(t *testing.T) {
	h := NewLinkHandler(stubLinkService{}, nil, nil, "http://sho.rt")

	body, _ := json.Marshal(map[string]any{"is_active": true})
	e := echo.New()
	req := httptest.NewRequest(http.MethodPut, "/api/links/abc1234", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec) // no user_id
	c.SetParamNames("code")
	c.SetParamValues("abc1234")

	_ = h.Update(c)
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", rec.Code)
	}
}

func TestLinkHandler_Update_NotFound(t *testing.T) {
	svc := stubLinkService{
		updateFn: func(_ context.Context, _ string, _ int64, _ *time.Time, _ bool) (*repository.Link, error) {
			return nil, apperror.NotFound("not found")
		},
	}
	h := NewLinkHandler(svc, nil, nil, "http://sho.rt")

	body, _ := json.Marshal(map[string]any{"is_active": true})
	e := echo.New()
	req := httptest.NewRequest(http.MethodPut, "/api/links/missing", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	c.SetParamNames("code")
	c.SetParamValues("missing")
	c.Set("user_id", int64(7))

	_ = h.Update(c)
	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", rec.Code)
	}
}


