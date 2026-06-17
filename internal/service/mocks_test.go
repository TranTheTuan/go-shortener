package service

import (
	"context"

	"github.com/TranTheTuan/go-shortener/internal/repository"
)

// mockLinkRepo is a configurable test double for repository.LinkRepository.
type mockLinkRepo struct {
	createFn    func(ctx context.Context, link *repository.Link) (*repository.Link, error)
	getByCodeFn func(ctx context.Context, code string) (*repository.Link, error)
	createCalls int
}

func (m *mockLinkRepo) Create(ctx context.Context, link *repository.Link) (*repository.Link, error) {
	m.createCalls++
	return m.createFn(ctx, link)
}

func (m *mockLinkRepo) GetByCode(ctx context.Context, code string) (*repository.Link, error) {
	return m.getByCodeFn(ctx, code)
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
