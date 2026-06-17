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
	svc := NewLinkService(repo, 7)

	link, err := svc.Create(context.Background(), CreateLinkInput{URL: "https://example.com/path"})
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
	svc := NewLinkService(repo, 7)

	for _, raw := range []string{"", "   ", "not-a-url", "ftp://files.example.com", "http://"} {
		_, err := svc.Create(context.Background(), CreateLinkInput{URL: raw})
		wantStatus(t, err, http.StatusBadRequest)
	}
}

func TestLinkService_Create_PastExpiry(t *testing.T) {
	repo := &mockLinkRepo{createFn: func(_ context.Context, l *repository.Link) (*repository.Link, error) { return l, nil }}
	svc := NewLinkService(repo, 7)

	past := time.Now().Add(-time.Hour)
	_, err := svc.Create(context.Background(), CreateLinkInput{URL: "https://example.com", ExpiresAt: &past})
	wantStatus(t, err, http.StatusBadRequest)
}

func TestLinkService_Create_RetriesOnCollision(t *testing.T) {
	repo := &mockLinkRepo{}
	// First attempt collides, second succeeds.
	repo.createFn = func(_ context.Context, link *repository.Link) (*repository.Link, error) {
		if repo.createCalls == 1 {
			return nil, repository.ErrConflict
		}
		return link, nil
	}
	svc := NewLinkService(repo, 7)

	if _, err := svc.Create(context.Background(), CreateLinkInput{URL: "https://example.com"}); err != nil {
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
	svc := NewLinkService(repo, 7)

	_, err := svc.Create(context.Background(), CreateLinkInput{URL: "https://example.com"})
	wantStatus(t, err, http.StatusInternalServerError)
	if repo.createCalls != maxCodeGenAttempts {
		t.Errorf("createCalls = %d, want %d", repo.createCalls, maxCodeGenAttempts)
	}
}

func TestLinkService_Resolve_NotFound(t *testing.T) {
	repo := &mockLinkRepo{
		getByCodeFn: func(_ context.Context, _ string) (*repository.Link, error) {
			return nil, repository.ErrNotFound
		},
	}
	svc := NewLinkService(repo, 7)

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
	svc := NewLinkService(repo, 7)

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
			svc := NewLinkService(repo, 7)

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
