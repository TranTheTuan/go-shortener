# Redis Caching — Implementation Plan

**Branch:** `feat/caching`
**Date:** 2026-06-20
**Status:** ✅ Complete

## Goal

Add Redis caching to 2 APIs:
1. **POST /api/links** (Create short link) — dedup check + cache warm-up on write
2. **GET /:code** (Redirect) — cache-first lookup, DB fallback, cache backfill

## Context

- Redis client infrastructure (`pkg/database/redis.go`, `configs/redis.go`) already exists
- `linkService` struct already has `redisClient *database.RedisClient` field
- But: **caching is not implemented** — `Create` and `Resolve` bypass Redis entirely
- **Compile errors exist on this branch** (see Phase 1)

## Phases

| Phase | Description | Status |
|-------|-------------|--------|
| [01](./phase-01-fix-compile-and-config.md) | Fix compile errors + config | ✅ Done |
| [02](./phase-02-repository-getbyurl.md) | Add `GetByOriginalURL` to repository | ✅ Done |
| [03](./phase-03-service-caching.md) | Implement caching in `linkService` | ✅ Done |
| [04](./phase-04-tests.md) | Fix broken tests + add cache tests | ✅ Done |

## Key Decisions

- Redis key: `link:{shortCode}` → plain `originalURL` string
- TTL: If link has `ExpiresAt` → TTL = `expiresAt - now`. Else → configurable default (24h)
- Dedup on Create: DB lookup via `GetByOriginalURL`, then warm cache
- **Cache follows repository pattern**: `LinkCacheRepository` interface in `internal/repository/` → mockable in tests, consistent with DB layer
- `linkService` depends on `LinkCacheRepository` interface (not Redis directly) → nil-safe, testable without Redis

## Files Changed

```
configs/config.go                                   — add CacheTTL to ShortenerConfig
configs/redis.go                                    — add Addr() helper
pkg/database/redis.go                               — fix host:port bug
internal/repository/link_repository.go              — add GetByOriginalURL
internal/repository/link_cache_repository.go        — NEW: LinkCacheRepository interface + Redis impl
internal/service/link_service.go                    — depend on LinkCacheRepository, implement cache logic
internal/service/mocks_test.go                      — add mockLinkCacheRepository + GetByOriginalURL mock
internal/service/link_service_test.go               — fix NewLinkService call (4 args), add cache tests
cmd/server/main.go                                  — remove bad import, wire LinkCacheRepository + CacheTTL
.env.example                                        — add REDIS_* + SHORTENER_CACHE_TTL
```
