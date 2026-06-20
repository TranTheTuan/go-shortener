# Phase 04 — Fix & Extend Tests

**Status:** ⬜ Todo
**Depends on:** Phase 01, 02, 03

## Fix Broken `NewLinkService` Calls

All `NewLinkService(repo, 7)` calls fail to compile — signature is now 4 args.
Pass `nil, 0` for cache args (nil-safe path, no caching side-effects):

```go
// Before
svc := NewLinkService(repo, 7)

// After
svc := NewLinkService(repo, nil, 7, 0)
```

All existing test assertions remain unchanged.

## Add `mockLinkCacheRepository` to `mocks_test.go`

```go
type mockLinkCacheRepository struct {
    store map[string]string
    setCalls int
}

func (m *mockLinkCacheRepository) Set(_ context.Context, code, url string, _ time.Duration) error {
    if m.store == nil {
        m.store = make(map[string]string)
    }
    m.store[code] = url
    m.setCalls++
    return nil
}

func (m *mockLinkCacheRepository) Get(_ context.Context, code string) (string, error) {
    if m.store == nil {
        return "", repository.ErrNotFound
    }
    val, ok := m.store[code]
    if !ok {
        return "", repository.ErrNotFound
    }
    return val, nil
}
```

## Add `getByOriginalURLFn` to `mockLinkRepo` (from Phase 02)

```go
type mockLinkRepo struct {
    createFn           func(ctx context.Context, link *repository.Link) (*repository.Link, error)
    getByCodeFn        func(ctx context.Context, code string) (*repository.Link, error)
    getByOriginalURLFn func(ctx context.Context, url string) (*repository.Link, error)
    createCalls        int
}

func (m *mockLinkRepo) GetByOriginalURL(ctx context.Context, url string) (*repository.Link, error) {
    if m.getByOriginalURLFn != nil {
        return m.getByOriginalURLFn(ctx, url)
    }
    return nil, repository.ErrNotFound
}
```

## New Tests in `link_service_test.go`

### Create — dedup returns existing non-expired link
```go
func TestLinkService_Create_DeduplicatesExistingURL(t *testing.T) {
    existing := &repository.Link{ID: 1, ShortCode: "abc1234", OriginalURL: "https://example.com"}
    repo := &mockLinkRepo{
        getByOriginalURLFn: func(_ context.Context, _ string) (*repository.Link, error) {
            return existing, nil
        },
    }
    cache := &mockLinkCacheRepository{}
    svc := NewLinkService(repo, cache, 7, 24*time.Hour)

    link, err := svc.Create(context.Background(), CreateLinkInput{URL: "https://example.com"})
    if err != nil {
        t.Fatalf("unexpected error: %v", err)
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
```

### Create — expired duplicate creates new link
```go
func TestLinkService_Create_CreatesNewWhenExistingExpired(t *testing.T) {
    past := time.Now().Add(-time.Hour)
    repo := &mockLinkRepo{
        getByOriginalURLFn: func(_ context.Context, _ string) (*repository.Link, error) {
            return &repository.Link{ID: 1, ShortCode: "old1234", OriginalURL: "https://example.com", ExpiresAt: &past}, nil
        },
        createFn: func(_ context.Context, link *repository.Link) (*repository.Link, error) {
            link.ID = 2
            return link, nil
        },
    }
    svc := NewLinkService(repo, nil, 7, 0)

    link, err := svc.Create(context.Background(), CreateLinkInput{URL: "https://example.com"})
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
```

### Resolve — cache hit skips DB
```go
func TestLinkService_Resolve_CacheHitSkipsDB(t *testing.T) {
    repo := &mockLinkRepo{
        getByCodeFn: func(_ context.Context, _ string) (*repository.Link, error) {
            t.Error("DB should not be queried on cache hit")
            return nil, repository.ErrNotFound
        },
    }
    cache := &mockLinkCacheRepository{
        store: map[string]string{"abc1234": "https://example.com"},
    }
    svc := NewLinkService(repo, cache, 7, 24*time.Hour)

    link, err := svc.Resolve(context.Background(), "abc1234")
    if err != nil {
        t.Fatalf("unexpected error: %v", err)
    }
    if link.OriginalURL != "https://example.com" {
        t.Errorf("original url = %q", link.OriginalURL)
    }
}
```

### Resolve — cache miss backfills cache
```go
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
    if url := cache.store["abc1234"]; url != "https://example.com" {
        t.Errorf("cached url = %q, want https://example.com", url)
    }
}
```

## Todo

- [ ] Fix all `NewLinkService(repo, 7)` → `NewLinkService(repo, nil, 7, 0)` in `link_service_test.go`
- [ ] Add `mockLinkCacheRepository` to `mocks_test.go`
- [ ] Add `getByOriginalURLFn` + `GetByOriginalURL` to `mockLinkRepo`
- [ ] Add `TestLinkService_Create_DeduplicatesExistingURL`
- [ ] Add `TestLinkService_Create_CreatesNewWhenExistingExpired`
- [ ] Add `TestLinkService_Resolve_CacheHitSkipsDB`
- [ ] Add `TestLinkService_Resolve_CacheMissBackfillsCache`
- [ ] Run `go test ./...` — all tests pass
