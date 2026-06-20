# Phase 01 — Fix Compile Errors + Config

**Status:** ⬜ Todo
**Priority:** Critical (blocks all other phases)

## Bugs to Fix

### 1. `cmd/server/main.go` — broken import
```go
// REMOVE this import (package doesn't exist):
"github.com/TranTheTuan/go-shortener/pkg/redis_client"
```
The import is unused and the package doesn't exist → compile error.

### 2. `pkg/database/redis.go` — wrong Redis address
```go
// CURRENT (broken):
Addr: cfg.Host,  // missing port!

// FIX:
Addr: cfg.Addr(),  // "host:port"
```

### 3. `configs/redis.go` — add `Addr()` helper
```go
func (r RedisConfig) Addr() string {
    return fmt.Sprintf("%s:%d", r.Host, r.Port)
}
```

### 4. `configs/config.go` — add `CacheTTL` to `ShortenerConfig`
```go
CacheTTL time.Duration `env:"CACHE_TTL" envDefault:"24h"`
```

### 5. `.env.example` — add Redis + CacheTTL vars
```env
# Redis
REDIS_HOST=localhost
REDIS_PORT=6379
REDIS_PASSWORD=
REDIS_DB=0
REDIS_POOL_SIZE=10
REDIS_MIN_IDLE_CONNS=5

# Shortener (add)
SHORTENER_CACHE_TTL=24h
```

### 6. `cmd/server/main.go` — wire `LinkCacheRepository` + `CacheTTL`
```go
linkCacheRepo := repository.NewLinkCacheRepository(rdb)
linkSvc := service.NewLinkService(linkRepo, linkCacheRepo, cfg.Shortener.CodeLength, cfg.Shortener.CacheTTL)
```

## Implementation Steps

1. Edit `configs/redis.go` — add `Addr()` with `fmt` import
2. Edit `pkg/database/redis.go` — replace `cfg.Host` with `cfg.Addr()`
3. Edit `configs/config.go` — add `CacheTTL time.Duration` field
4. Edit `cmd/server/main.go` — remove bad import, add `CacheTTL` arg
5. Edit `.env.example` — add Redis + CacheTTL entries
6. Run `go build ./...` to verify compile

## Todo

- [ ] Add `Addr()` to `configs/redis.go`
- [ ] Fix `pkg/database/redis.go` address
- [ ] Add `CacheTTL` to `configs/config.go`
- [ ] Fix `cmd/server/main.go` import + wire CacheTTL
- [ ] Update `.env.example`
- [ ] `go build ./...` passes
