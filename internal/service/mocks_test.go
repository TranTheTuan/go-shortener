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
	getByOwnerAndURLFn func(ctx context.Context, ownerID *int64, url string) (*repository.Link, error)
	createCalls        int
}

func (m *mockLinkRepo) Create(ctx context.Context, link *repository.Link) (*repository.Link, error) {
	m.createCalls++
	return m.createFn(ctx, link)
}

func (m *mockLinkRepo) GetByCode(ctx context.Context, code string) (*repository.Link, error) {
	return m.getByCodeFn(ctx, code)
}

func (m *mockLinkRepo) GetByOwnerAndURL(ctx context.Context, ownerID *int64, url string) (*repository.Link, error) {
	if m.getByOwnerAndURLFn != nil {
		return m.getByOwnerAndURLFn(ctx, ownerID, url)
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

// mockUserRepo is an in-memory test double for repository.UserRepository. It
// enforces unique username/email so Create can surface repository.ErrConflict.
type mockUserRepo struct {
	users  map[int64]*repository.User
	nextID int64
}

func newMockUserRepo() *mockUserRepo {
	return &mockUserRepo{users: make(map[int64]*repository.User)}
}

func (m *mockUserRepo) Create(_ context.Context, user *repository.User) (*repository.User, error) {
	for _, u := range m.users {
		if u.Username == user.Username || u.Email == user.Email {
			return nil, repository.ErrConflict
		}
	}
	m.nextID++
	user.ID = m.nextID
	m.users[user.ID] = user
	return user, nil
}

func (m *mockUserRepo) GetByID(_ context.Context, id int64) (*repository.User, error) {
	if u, ok := m.users[id]; ok {
		return u, nil
	}
	return nil, repository.ErrNotFound
}

func (m *mockUserRepo) GetByEmail(_ context.Context, email string) (*repository.User, error) {
	for _, u := range m.users {
		if u.Email == email {
			return u, nil
		}
	}
	return nil, repository.ErrNotFound
}

func (m *mockUserRepo) GetByUsername(_ context.Context, username string) (*repository.User, error) {
	for _, u := range m.users {
		if u.Username == username {
			return u, nil
		}
	}
	return nil, repository.ErrNotFound
}

func (m *mockUserRepo) List(_ context.Context) ([]*repository.User, error) {
	out := make([]*repository.User, 0, len(m.users))
	for _, u := range m.users {
		out = append(out, u)
	}
	return out, nil
}

// mockRefreshRepo is an in-memory test double for repository.RefreshTokenRepository.
type mockRefreshRepo struct {
	byID   map[int64]*repository.RefreshToken
	byHash map[string]*repository.RefreshToken
	nextID int64
}

func newMockRefreshRepo() *mockRefreshRepo {
	return &mockRefreshRepo{
		byID:   make(map[int64]*repository.RefreshToken),
		byHash: make(map[string]*repository.RefreshToken),
	}
}

func (m *mockRefreshRepo) Create(_ context.Context, rt *repository.RefreshToken) (*repository.RefreshToken, error) {
	m.nextID++
	rt.ID = m.nextID
	m.byID[rt.ID] = rt
	m.byHash[rt.TokenHash] = rt
	return rt, nil
}

func (m *mockRefreshRepo) GetByHash(_ context.Context, hash string) (*repository.RefreshToken, error) {
	if rt, ok := m.byHash[hash]; ok {
		return rt, nil
	}
	return nil, repository.ErrNotFound
}

func (m *mockRefreshRepo) Revoke(_ context.Context, id int64) (bool, error) {
	rt, ok := m.byID[id]
	if !ok || rt.RevokedAt != nil {
		return false, nil
	}
	now := time.Now().UTC()
	rt.RevokedAt = &now
	return true, nil
}

// mockPlanRepo is a configurable test double for repository.PlanRepository.
type mockPlanRepo struct {
	byCode map[string]*repository.Plan
	byID   map[int64]*repository.Plan
}

func (m *mockPlanRepo) GetByCode(_ context.Context, code string) (*repository.Plan, error) {
	if p, ok := m.byCode[code]; ok {
		return p, nil
	}
	return nil, repository.ErrNotFound
}

func (m *mockPlanRepo) GetByID(_ context.Context, id int64) (*repository.Plan, error) {
	if p, ok := m.byID[id]; ok {
		return p, nil
	}
	return nil, repository.ErrNotFound
}

// mockSubRepo is a configurable test double for repository.SubscriptionRepository.
type mockSubRepo struct {
	active map[int64]*repository.Subscription
}

func (m *mockSubRepo) Create(_ context.Context, sub *repository.Subscription) (*repository.Subscription, error) {
	if m.active == nil {
		m.active = make(map[int64]*repository.Subscription)
	}
	m.active[sub.UserID] = sub
	return sub, nil
}

func (m *mockSubRepo) GetActiveByUserID(_ context.Context, userID int64) (*repository.Subscription, error) {
	if s, ok := m.active[userID]; ok {
		return s, nil
	}
	return nil, repository.ErrNotFound
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
