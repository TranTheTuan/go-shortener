package service

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/TranTheTuan/go-shortener/internal/repository"
	"github.com/TranTheTuan/go-shortener/pkg/apperror"
)

// wantStatus asserts err is an *apperror.Error carrying the given HTTP status.
func wantStatus(t *testing.T, err error, status int) {
	t.Helper()
	if err == nil {
		t.Fatalf("expected error with status %d, got nil", status)
	}
	appErr, ok := apperror.As(err)
	if !ok {
		t.Fatalf("expected *apperror.Error, got %T: %v", err, err)
	}
	if appErr.Status != status {
		t.Errorf("status = %d, want %d", appErr.Status, status)
	}
}

func TestLinkService_Create_Valid(t *testing.T) {
	repo := &mockLinkRepo{
		createFn: func(_ context.Context, link *repository.Link) (*repository.Link, error) {
			link.ID = 1
			return link, nil
		},
	}
	svc := NewLinkService(repo, nil, 7, 0)

	link, _, err := svc.Create(context.Background(), CreateLinkInput{URL: "https://example.com/path"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(link.ShortCode) != 7 {
		t.Errorf("short code length = %d, want 7", len(link.ShortCode))
	}
	if link.OriginalURL != "https://example.com/path" {
		t.Errorf("original url = %q", link.OriginalURL)
	}
}

func TestLinkService_Create_InvalidURL(t *testing.T) {
	repo := &mockLinkRepo{createFn: func(_ context.Context, l *repository.Link) (*repository.Link, error) { return l, nil }}
	svc := NewLinkService(repo, nil, 7, 0)

	for _, raw := range []string{"", "   ", "not-a-url", "ftp://files.example.com", "http://"} {
		_, _, err := svc.Create(context.Background(), CreateLinkInput{URL: raw})
		wantStatus(t, err, http.StatusBadRequest)
	}
}

func TestLinkService_Create_PastExpiry(t *testing.T) {
	repo := &mockLinkRepo{createFn: func(_ context.Context, l *repository.Link) (*repository.Link, error) { return l, nil }}
	svc := NewLinkService(repo, nil, 7, 0)

	past := time.Now().Add(-time.Hour)
	_, _, err := svc.Create(context.Background(), CreateLinkInput{URL: "https://example.com", ExpiresAt: &past})
	wantStatus(t, err, http.StatusBadRequest)
}

func TestLinkService_Create_RetriesOnCollision(t *testing.T) {
	repo := &mockLinkRepo{}
	repo.createFn = func(_ context.Context, link *repository.Link) (*repository.Link, error) {
		if repo.createCalls == 1 {
			return nil, repository.ErrConflict
		}
		return link, nil
	}
	svc := NewLinkService(repo, nil, 7, 0)

	if _, _, err := svc.Create(context.Background(), CreateLinkInput{URL: "https://example.com"}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if repo.createCalls < 2 {
		t.Errorf("createCalls = %d, want >= 2 (collision should retry)", repo.createCalls)
	}
}

func TestLinkService_Create_ExhaustedRetries(t *testing.T) {
	repo := &mockLinkRepo{
		createFn: func(_ context.Context, _ *repository.Link) (*repository.Link, error) {
			return nil, repository.ErrConflict
		},
	}
	svc := NewLinkService(repo, nil, 7, 0)

	_, _, err := svc.Create(context.Background(), CreateLinkInput{URL: "https://example.com"})
	wantStatus(t, err, http.StatusInternalServerError)
	if repo.createCalls != maxCodeGenAttempts {
		t.Errorf("createCalls = %d, want %d", repo.createCalls, maxCodeGenAttempts)
	}
}

func TestLinkService_Create_DeduplicatesExistingURL(t *testing.T) {
	existing := &repository.Link{ID: 1, ShortCode: "abc1234", OriginalURL: "https://example.com"}
	repo := &mockLinkRepo{
		getByOwnerAndURLFn: func(_ context.Context, _ *int64, _ string) (*repository.Link, error) {
			return existing, nil
		},
	}
	cache := &mockLinkCacheRepository{}
	svc := NewLinkService(repo, cache, 7, 24*time.Hour)

	link, reused, err := svc.Create(context.Background(), CreateLinkInput{URL: "https://example.com"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !reused {
		t.Error("expected reused = true for an existing-URL dedup hit")
	}
	if link.ShortCode != "abc1234" {
		t.Errorf("expected existing short code, got %q", link.ShortCode)
	}
	if repo.createCalls != 0 {
		t.Errorf("createCalls = %d, want 0 (should reuse existing)", repo.createCalls)
	}
	if cache.setCalls != 1 {
		t.Errorf("cache.setCalls = %d, want 1 (should warm cache)", cache.setCalls)
	}
}

func TestLinkService_Create_StampsOwnerAndScopesDedup(t *testing.T) {
	owner := int64(42)
	var gotOwner *int64
	repo := &mockLinkRepo{
		getByOwnerAndURLFn: func(_ context.Context, ownerID *int64, _ string) (*repository.Link, error) {
			gotOwner = ownerID // capture the dedup scope
			return nil, repository.ErrNotFound
		},
		createFn: func(_ context.Context, link *repository.Link) (*repository.Link, error) {
			link.ID = 1
			return link, nil
		},
	}
	svc := NewLinkService(repo, nil, 7, 0)

	link, reused, err := svc.Create(context.Background(), CreateLinkInput{URL: "https://example.com", OwnerID: &owner})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if reused {
		t.Error("reused should be false for a new link")
	}
	if gotOwner == nil || *gotOwner != owner {
		t.Errorf("dedup must be scoped to owner %d, got %v", owner, gotOwner)
	}
	if link.UserID == nil || *link.UserID != owner {
		t.Errorf("new link must be stamped with owner %d, got %v", owner, link.UserID)
	}
}

func TestLinkService_Create_QuotaExhaustedRejectsNew(t *testing.T) {
	repo := &mockLinkRepo{
		getByOwnerAndURLFn: func(_ context.Context, _ *int64, _ string) (*repository.Link, error) {
			return nil, repository.ErrNotFound // no link to reuse
		},
		createFn: func(_ context.Context, l *repository.Link) (*repository.Link, error) { return l, nil },
	}
	svc := NewLinkService(repo, nil, 7, 0)

	_, _, err := svc.Create(context.Background(), CreateLinkInput{URL: "https://example.com", QuotaExhausted: true})
	wantStatus(t, err, http.StatusTooManyRequests)
	if repo.createCalls != 0 {
		t.Errorf("createCalls = %d, want 0 (must not create when quota exhausted)", repo.createCalls)
	}
}

func TestLinkService_Create_QuotaExhaustedStillServesDedup(t *testing.T) {
	existing := &repository.Link{ID: 1, ShortCode: "abc1234", OriginalURL: "https://example.com"}
	repo := &mockLinkRepo{
		getByOwnerAndURLFn: func(_ context.Context, _ *int64, _ string) (*repository.Link, error) {
			return existing, nil
		},
	}
	svc := NewLinkService(repo, nil, 7, 0)

	link, reused, err := svc.Create(context.Background(), CreateLinkInput{URL: "https://example.com", QuotaExhausted: true})
	if err != nil {
		t.Fatalf("reuse must succeed even when over quota, got %v", err)
	}
	if !reused || link.ShortCode != "abc1234" {
		t.Errorf("expected reused existing link, got reused=%v code=%q", reused, link.ShortCode)
	}
}

func TestLinkService_Create_CreatesNewWhenExistingExpired(t *testing.T) {
	past := time.Now().Add(-time.Hour)
	repo := &mockLinkRepo{
		getByOwnerAndURLFn: func(_ context.Context, _ *int64, _ string) (*repository.Link, error) {
			return &repository.Link{ID: 1, ShortCode: "old1234", OriginalURL: "https://example.com", ExpiresAt: &past}, nil
		},
		createFn: func(_ context.Context, link *repository.Link) (*repository.Link, error) {
			link.ID = 2
			return link, nil
		},
	}
	svc := NewLinkService(repo, nil, 7, 0)

	link, _, err := svc.Create(context.Background(), CreateLinkInput{URL: "https://example.com"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if link.ShortCode == "old1234" {
		t.Error("should have generated new code for expired link")
	}
	if repo.createCalls != 1 {
		t.Errorf("createCalls = %d, want 1", repo.createCalls)
	}
}

func TestLinkService_Resolve_NotFound(t *testing.T) {
	repo := &mockLinkRepo{
		getByCodeFn: func(_ context.Context, _ string) (*repository.Link, error) {
			return nil, repository.ErrNotFound
		},
	}
	svc := NewLinkService(repo, nil, 7, 0)

	_, err := svc.Resolve(context.Background(), "missing")
	wantStatus(t, err, http.StatusNotFound)
}

func TestLinkService_Resolve_Expired(t *testing.T) {
	past := time.Now().Add(-time.Minute)
	repo := &mockLinkRepo{
		getByCodeFn: func(_ context.Context, code string) (*repository.Link, error) {
			return &repository.Link{ID: 1, ShortCode: code, OriginalURL: "https://example.com", ExpiresAt: &past}, nil
		},
	}
	svc := NewLinkService(repo, nil, 7, 0)

	_, err := svc.Resolve(context.Background(), "expired")
	wantStatus(t, err, http.StatusGone)
}

func TestLinkService_Resolve_Valid(t *testing.T) {
	future := time.Now().Add(time.Hour)
	cases := map[string]*time.Time{"no-expiry": nil, "future-expiry": &future}
	for name, exp := range cases {
		t.Run(name, func(t *testing.T) {
			repo := &mockLinkRepo{
				getByCodeFn: func(_ context.Context, code string) (*repository.Link, error) {
					return &repository.Link{ID: 1, ShortCode: code, OriginalURL: "https://example.com", ExpiresAt: exp}, nil
				},
			}
			svc := NewLinkService(repo, nil, 7, 0)

			link, err := svc.Resolve(context.Background(), "abc")
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if link.OriginalURL != "https://example.com" {
				t.Errorf("original url = %q", link.OriginalURL)
			}
		})
	}
}

func TestLinkService_Resolve_CacheHitSkipsDB(t *testing.T) {
	repo := &mockLinkRepo{
		getByCodeFn: func(_ context.Context, _ string) (*repository.Link, error) {
			t.Error("DB should not be queried on cache hit")
			return nil, repository.ErrNotFound
		},
	}
	cache := &mockLinkCacheRepository{
		store: map[string]*repository.Link{
			"abc1234": {ID: 42, ShortCode: "abc1234", OriginalURL: "https://example.com"},
		},
	}
	svc := NewLinkService(repo, cache, 7, 24*time.Hour)

	link, err := svc.Resolve(context.Background(), "abc1234")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if link.OriginalURL != "https://example.com" {
		t.Errorf("original url = %q", link.OriginalURL)
	}
	if link.ID != 42 {
		t.Errorf("link.ID = %d, want 42 (click recording needs a real ID)", link.ID)
	}
}

func TestLinkService_Resolve_CacheMissBackfillsCache(t *testing.T) {
	repo := &mockLinkRepo{
		getByCodeFn: func(_ context.Context, code string) (*repository.Link, error) {
			return &repository.Link{ID: 1, ShortCode: code, OriginalURL: "https://example.com"}, nil
		},
	}
	cache := &mockLinkCacheRepository{}
	svc := NewLinkService(repo, cache, 7, 24*time.Hour)

	_, err := svc.Resolve(context.Background(), "abc1234")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cache.setCalls != 1 {
		t.Errorf("cache.setCalls = %d, want 1 (should backfill)", cache.setCalls)
	}
	if cached := cache.store["abc1234"]; cached == nil || cached.OriginalURL != "https://example.com" {
		t.Errorf("cached url = %v, want https://example.com", cached)
	}
}
