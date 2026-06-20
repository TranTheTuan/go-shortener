# Phase 03 — Service: Implement Redis Caching via Repository Pattern

**Status:** ⬜ Todo
**Depends on:** Phase 01, Phase 02

## New File: `internal/repository/link_cache_repository.go`

```go
package repository

import (
    "context"
    "time"

    "github.com/TranTheTuan/go-shortener/pkg/database"
    "github.com/redis/go-redis/v9"
)

const linkCacheKeyPrefix = "link:"

// LinkCacheRepository defines cache operations for short links.
type LinkCacheRepository interface {
    Set(ctx context.Context, code, url string, ttl time.Duration) error
    Get(ctx context.Context, code string) (string, error)
}

type linkCacheRepository struct {
    rdb *database.RedisClient
}

func NewLinkCacheRepository(rdb *database.RedisClient) LinkCacheRepository {
    return &linkCacheRepository{rdb: rdb}
}

func (r *linkCacheRepository) Set(ctx context.Context, code, url string, ttl time.Duration) error {
    return r.rdb.Client.Set(ctx, linkCacheKeyPrefix+code, url, ttl).Err()
}

func (r *linkCacheRepository) Get(ctx context.Context, code string) (string, error) {
    val, err := r.rdb.Client.Get(ctx, linkCacheKeyPrefix+code).Result()
    if err == redis.Nil {
        return "", ErrNotFound
    }
    return val, err
}
```

> Returns `ErrNotFound` (already defined in `repository` package) on cache miss — keeps error handling consistent across DB and cache layers.

## Cache Design

| Key | Value | TTL |
|-----|-------|-----|
| `link:{shortCode}` | `originalURL` (plain string) | `expiresAt - now` if link has expiry, else `CacheTTL` (default 24h) |

## Updated `linkService` Struct

```go
type linkService struct {
    repo      repository.LinkRepository
    cache     repository.LinkCacheRepository // nil-safe: nil = no caching
    codeLen   int
    cacheTTL  time.Duration
    now       func() time.Time
}

func NewLinkService(
    repo     repository.LinkRepository,
    cache    repository.LinkCacheRepository,
    codeLen  int,
    cacheTTL time.Duration,
) LinkService {
    if codeLen <= 0 {
        codeLen = defaultCodeLength
    }
    return &linkService{repo: repo, cache: cache, codeLen: codeLen, cacheTTL: cacheTTL, now: time.Now}
}
```

## Cache Helpers (private methods on `linkService`)

```go
func (s *linkService) cacheTTLFor(link *repository.Link) time.Duration {
    if link.ExpiresAt != nil {
        ttl := time.Until(*link.ExpiresAt)
        if ttl <= 0 {
            return 0
        }
        return ttl
    }
    return s.cacheTTL
}

// cacheSet is fire-and-forget: errors never break the main flow.
func (s *linkService) cacheSet(ctx context.Context, link *repository.Link) {
    if s.cache == nil {
        return
    }
    ttl := s.cacheTTLFor(link)
    if ttl <= 0 {
        return
    }
    _ = s.cache.Set(ctx, link.ShortCode, link.OriginalURL, ttl)
}

func (s *linkService) cacheGet(ctx context.Context, code string) (string, bool) {
    if s.cache == nil {
        return "", false
    }
    url, err := s.cache.Get(ctx, code)
    if err != nil {
        return "", false
    }
    return url, true
}
```

## Create Flow (Write-heavy)

```
Receive LongURL
  └─ Validate URL
  └─ GetByOriginalURL(url) from DB
       ├─ Found & not expired → cacheSet (warm-up) → return existing link
       └─ Not found / expired → generate ShortCode → Create in DB
                                   └─ cacheSet → return new link
```

```go
func (s *linkService) Create(ctx context.Context, in CreateLinkInput) (*repository.Link, error) {
    target := strings.TrimSpace(in.URL)
    if err := validateURL(target); err != nil {
        return nil, err
    }

    now := s.now().UTC()
    if in.ExpiresAt != nil && !in.ExpiresAt.After(now) {
        return nil, apperror.BadRequest("expires_at must be in the future")
    }

    // Dedup: reuse existing non-expired link for the same URL
    existing, err := s.repo.GetByOriginalURL(ctx, target)
    if err == nil {
        notExpired := existing.ExpiresAt == nil || existing.ExpiresAt.After(now)
        if notExpired {
            s.cacheSet(ctx, existing)
            return existing, nil
        }
    }
    // ErrNotFound or expired → create new

    for attempt := 0; attempt < maxCodeGenAttempts; attempt++ {
        code, err := shortcode.Generate(s.codeLen)
        if err != nil {
            return nil, apperror.Internal(err)
        }
        link := &repository.Link{
            ShortCode:   code,
            OriginalURL: target,
            ExpiresAt:   in.ExpiresAt,
            CreatedAt:   now,
        }
        created, err := s.repo.Create(ctx, link)
        if errors.Is(err, repository.ErrConflict) {
            continue
        }
        if err != nil {
            return nil, apperror.Internal(err)
        }
        s.cacheSet(ctx, created)
        return created, nil
    }

    return nil, apperror.Internal(errors.New("could not generate a unique short code"))
}
```

## Resolve Flow (Read-heavy / Critical Path)

```
Receive ShortCode
  └─ cacheGet(code)
       ├─ HIT  → redirect immediately (< 5ms, zero DB)
       └─ MISS → GetByCode from DB
            ├─ Not found → 404
            ├─ Expired   → 410
            └─ Found     → cacheSet (backfill) → redirect
```

```go
func (s *linkService) Resolve(ctx context.Context, code string) (*repository.Link, error) {
    if url, ok := s.cacheGet(ctx, code); ok {
        return &repository.Link{ShortCode: code, OriginalURL: url}, nil
    }

    link, err := s.repo.GetByCode(ctx, code)
    if errors.Is(err, repository.ErrNotFound) {
        return nil, apperror.NotFound("short link not found")
    }
    if err != nil {
        return nil, apperror.Internal(err)
    }
    if link.ExpiresAt != nil && link.ExpiresAt.Before(s.now().UTC()) {
        return nil, apperror.Gone("short link has expired")
    }

    s.cacheSet(ctx, link)
    return link, nil
}
```

## Todo

- [ ] Create `internal/repository/link_cache_repository.go` with interface + Redis impl
- [ ] Remove `redisClient` field from `linkService`, add `cache LinkCacheRepository` + `cacheTTL`
- [ ] Update `NewLinkService` to 4-arg signature
- [ ] Implement `cacheTTLFor`, `cacheSet`, `cacheGet` helpers
- [ ] Implement dedup + cache warm-up in `Create`
- [ ] Implement cache-first + DB fallback + backfill in `Resolve`
- [ ] `go build ./...` passes
