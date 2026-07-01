package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/labstack/echo/v4"

	"github.com/TranTheTuan/go-shortener/internal/repository"
	"github.com/TranTheTuan/go-shortener/internal/service"
)

// stubLinkService satisfies service.LinkService; only ListByOwner is exercised.
type stubLinkService struct {
	listFn func(ctx context.Context, ownerID int64, limit, offset int) ([]*repository.OwnedLink, int64, error)
}

func (s stubLinkService) Create(context.Context, service.CreateLinkInput) (*repository.Link, bool, error) {
	return nil, false, nil
}
func (s stubLinkService) Resolve(context.Context, string) (*repository.Link, error) { return nil, nil }
func (s stubLinkService) ListByOwner(ctx context.Context, ownerID int64, limit, offset int) ([]*repository.OwnedLink, int64, error) {
	return s.listFn(ctx, ownerID, limit, offset)
}

func TestLinkHandler_List(t *testing.T) {
	var gotOwner int64
	var gotLimit, gotOffset int
	svc := stubLinkService{listFn: func(_ context.Context, ownerID int64, limit, offset int) ([]*repository.OwnedLink, int64, error) {
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
