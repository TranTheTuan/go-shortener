package service

import (
	"context"
	"net/http"
	"testing"

	"github.com/TranTheTuan/go-shortener/internal/repository"
)

func TestAnalyticsService_Record(t *testing.T) {
	clicks := &mockClickRepo{}
	links := &mockLinkRepo{}
	svc := NewAnalyticsService(links, clicks, nil, nil)

	in := RecordInput{LinkID: 42, Referrer: "https://ref.example", IPAddress: "1.2.3.4", UserAgent: "curl/8"}
	if err := svc.Record(context.Background(), in); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if clicks.createCalls != 1 {
		t.Fatalf("Create calls = %d, want 1", clicks.createCalls)
	}
	got := clicks.lastClickStored
	if got.LinkID != 42 || got.Referrer != in.Referrer || got.IPAddress != in.IPAddress || got.UserAgent != in.UserAgent {
		t.Errorf("stored click = %+v, want fields from %+v", got, in)
	}
	if got.ClickedAt.IsZero() {
		t.Error("ClickedAt not set")
	}
}

func TestAnalyticsService_Stats_NotFound(t *testing.T) {
	links := &mockLinkRepo{
		getByCodeFn: func(_ context.Context, _ string) (*repository.Link, error) {
			return nil, repository.ErrNotFound
		},
	}
	svc := NewAnalyticsService(links, &mockClickRepo{}, nil, nil)

	_, err := svc.Stats(context.Background(), "missing")
	wantStatus(t, err, http.StatusNotFound)
}

func TestAnalyticsService_Stats_Aggregates(t *testing.T) {
	links := &mockLinkRepo{
		getByCodeFn: func(_ context.Context, code string) (*repository.Link, error) {
			return &repository.Link{ID: 7, ShortCode: code}, nil
		},
	}
	recent := []*repository.Click{{ID: 1, LinkID: 7}, {ID: 2, LinkID: 7}}
	clicks := &mockClickRepo{
		countFn: func(_ context.Context, linkID int64) (int64, error) {
			if linkID != 7 {
				t.Errorf("count linkID = %d, want 7", linkID)
			}
			return 5, nil
		},
		listFn: func(_ context.Context, linkID int64, limit int) ([]*repository.Click, error) {
			if limit != recentClicksLimit {
				t.Errorf("list limit = %d, want %d", limit, recentClicksLimit)
			}
			return recent, nil
		},
	}
	svc := NewAnalyticsService(links, clicks, nil, nil)

	stats, err := svc.Stats(context.Background(), "abc123")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if stats.ShortCode != "abc123" {
		t.Errorf("short code = %q", stats.ShortCode)
	}
	if stats.TotalClicks != 5 {
		t.Errorf("total clicks = %d, want 5", stats.TotalClicks)
	}
	if len(stats.RecentClicks) != 2 {
		t.Errorf("recent clicks = %d, want 2", len(stats.RecentClicks))
	}
}
