package service

import (
	"context"
	"time"

	"github.com/TranTheTuan/go-shortener/internal/repository"
)

// mockLinkRepo is a configurable test double for repository.LinkRepository.
type mockLinkRepo struct {
	createFn           func(ctx context.Context, link *repository.Link) (*repository.Link, error)
	getByCodeFn        func(ctx context.Context, code string) (*repository.Link, error)
	getByOriginalURLFn func(ctx context.Context, url string) (*repository.Link, error)
	createCalls        int
}

func (m *mockLinkRepo) Create(ctx context.Context, link *repository.Link) (*repository.Link, error) {
	m.createCalls++
	return m.createFn(ctx, link)
}

func (m *mockLinkRepo) GetByCode(ctx context.Context, code string) (*repository.Link, error) {
	return m.getByCodeFn(ctx, code)
}

func (m *mockLinkRepo) GetByOriginalURL(ctx context.Context, url string) (*repository.Link, error) {
	if m.getByOriginalURLFn != nil {
		return m.getByOriginalURLFn(ctx, url)
	}
	return nil, repository.ErrNotFound
}

// mockLinkCacheRepository is an in-memory test double for repository.LinkCacheRepository.
type mockLinkCacheRepository struct {
	store    map[string]*repository.Link
	setCalls int
}

func (m *mockLinkCacheRepository) Set(_ context.Context, link *repository.Link, _ time.Duration) error {
	if m.store == nil {
		m.store = make(map[string]*repository.Link)
	}
	m.store[link.ShortCode] = link
	m.setCalls++
	return nil
}

func (m *mockLinkCacheRepository) Get(_ context.Context, code string) (*repository.Link, error) {
	if m.store == nil {
		return nil, repository.ErrNotFound
	}
	link, ok := m.store[code]
	if !ok {
		return nil, repository.ErrNotFound
	}
	return link, nil
}

// mockClickRepo is a configurable test double for repository.ClickRepository.
type mockClickRepo struct {
	createFn        func(ctx context.Context, click *repository.Click) error
	countFn         func(ctx context.Context, linkID int64) (int64, error)
	listFn          func(ctx context.Context, linkID int64, limit int) ([]*repository.Click, error)
	createCalls     int
	lastClickStored *repository.Click
}

func (m *mockClickRepo) Create(ctx context.Context, click *repository.Click) error {
	m.createCalls++
	m.lastClickStored = click
	if m.createFn != nil {
		return m.createFn(ctx, click)
	}
	return nil
}

func (m *mockClickRepo) CountByLinkID(ctx context.Context, linkID int64) (int64, error) {
	return m.countFn(ctx, linkID)
}

func (m *mockClickRepo) ListByLinkID(ctx context.Context, linkID int64, limit int) ([]*repository.Click, error) {
	return m.listFn(ctx, linkID, limit)
}
