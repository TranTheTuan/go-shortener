# Phase 02 — Repository: Add GetByOriginalURL

**Status:** ⬜ Todo
**Depends on:** Phase 01

## Why

Create flow Step 2 requires checking if a LongURL was already shortened to avoid duplicates.
Current `LinkRepository` only has `GetByCode` — no reverse lookup exists.

## Changes to `internal/repository/link_repository.go`

### Interface addition
```go
type LinkRepository interface {
    Create(ctx context.Context, link *Link) (*Link, error)
    GetByCode(ctx context.Context, code string) (*Link, error)
    GetByOriginalURL(ctx context.Context, url string) (*Link, error) // NEW
}
```

### Implementation
```go
func (r *linkRepository) GetByOriginalURL(ctx context.Context, url string) (*Link, error) {
    var link Link
    err := r.db.WithContext(ctx).Where("original_url = ?", url).First(&link).Error
    if errors.Is(err, gorm.ErrRecordNotFound) {
        return nil, ErrNotFound
    }
    if err != nil {
        return nil, err
    }
    return &link, nil
}
```

## Changes to `internal/service/mocks_test.go`

Add `getByOriginalURLFn` to `mockLinkRepo`:
```go
type mockLinkRepo struct {
    createFn            func(ctx context.Context, link *repository.Link) (*repository.Link, error)
    getByCodeFn         func(ctx context.Context, code string) (*repository.Link, error)
    getByOriginalURLFn  func(ctx context.Context, url string) (*repository.Link, error) // NEW
    createCalls         int
}

func (m *mockLinkRepo) GetByOriginalURL(ctx context.Context, url string) (*repository.Link, error) {
    if m.getByOriginalURLFn != nil {
        return m.getByOriginalURLFn(ctx, url)
    }
    return nil, repository.ErrNotFound // default: not found
}
```

## Todo

- [ ] Add `GetByOriginalURL` to `LinkRepository` interface
- [ ] Implement `GetByOriginalURL` in `linkRepository`
- [ ] Update `mockLinkRepo` in `mocks_test.go`
- [ ] `go build ./...` passes
