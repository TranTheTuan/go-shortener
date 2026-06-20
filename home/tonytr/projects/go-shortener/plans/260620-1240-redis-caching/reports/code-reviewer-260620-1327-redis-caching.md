---
date: 2026-06-20
branch: feat/caching
scope: Redis caching layer
score: 8.5/10
---

## Code Review Summary

### Scope
- Files: 8 changed, 1 new (`link_cache_repository.go`)
- Focus: Redis caching — config, repository, service, tests
- Build: passes (`go build ./...`)

### Overall Assessment
Solid, idiomatic implementation. Clean separation of concerns, nil-safe cache injection, consistent error abstraction. A few gaps around stale-cache correctness and Redis startup failure handling worth addressing.

---

### Critical Issues

None.

---

### High Priority

**1. Stale cache on expired-link path in `Resolve`**
`cacheGet` returns a hit for a code that has an expired link. The service never reaches the expiry check — callers get a redirect to a URL that should be "Gone".

Scenario: link expires *after* it was cached (TTL was computed from `ExpiresAt`). With the current `cacheTTLFor` logic this is unlikely (TTL = remaining duration), but a clock skew between the app server computing TTL and Redis expiring the key creates a narrow window. Low probability but high impact (expired links are served). Consider either:
- Storing `expiresAt` in the cached value and re-checking it on hit, or
- Relying entirely on TTL precision (acceptable if clock skew is <1s and tolerance is fine).

Document the known trade-off as a comment in `cacheGet` so the next maintainer understands it was a deliberate choice.

**2. Redis startup failure is hard-fatal**
`database.SetupRedis` returns an error that propagates to `run()` and kills the server. A Redis outage therefore kills the app even though all cache paths are nil-safe. Either:
- Log a warning and wire `linkCacheRepo = nil` when Redis is unavailable, or
- Document explicitly that Redis is a required runtime dependency (update `README`).

---

### Medium Priority

**3. `GetByOriginalURL` uses `First` without an index**
`WHERE original_url = ?` on a potentially large table with no index causes a sequential scan on every `Create` dedup call. A DB index on `links(original_url)` should accompany this feature; it belongs in a migration file.

**4. `.env.example` leaks real-looking credentials**
`DB_PASSWORD=hdp12345`, `DB_HOST=192.168.1.205`, `DB_USER=admin`, `DB_NAME=hdp` appear to be real values from a dev/staging environment. Replace with generic placeholders (`DB_HOST=localhost`, `DB_PASSWORD=changeme`) before this branch is merged or reviewed externally.

**5. Error message in `pkg/database/redis.go` is in Vietnamese**
`"không thể kết nối Redis tại %s: %v"` — inconsistent with every other error message in the codebase. Replace with English.

---

### Low Priority

**6. `cacheSet` silently swallows errors**
Fire-and-forget is documented and intentional. Consider a `slog.Debug` or metric counter on the discarded error to make Redis write failures observable in production without blocking the hot path.

**7. Partial `Link` struct returned on cache hit**
`Resolve` returns `&repository.Link{ShortCode: code, OriginalURL: originalURL}` with zero values for `ID`, `ExpiresAt`, `CreatedAt`. This is fine for the current redirect handler (which only needs `OriginalURL`), but callers that inspect `ID` or `ExpiresAt` will silently get wrong values. Add a comment on `Resolve` stating the cache-hit return is a partial struct, or define a lighter `ResolveResult` type to make the partial nature type-explicit.

---

### Positive Observations

- `LinkCacheRepository` interface in the repository layer — mockable, consistent with DB layer pattern.
- `cacheTTLFor` correctly computes per-link TTL from `ExpiresAt`, preventing cache-outlives-link on explicitly expiring URLs.
- `cacheGet` / `cacheSet` helper split is clean; nil-guard in both keeps `cache=nil` deployments safe.
- Dedup logic in `Create` correctly distinguishes "not found" from "found but expired".
- Test coverage is thorough: cache hit/miss, dedup, backfill, nil cache, all exercised with zero external dependencies.
- `ErrNotFound` abstraction over `redis.Nil` is the right call — keeps service layer decoupled from Redis.
- Build is clean and all tests pass.

---

### Plan TODO Status

All 4 phases marked in `plan.md` as Todo — plan was not updated to reflect completion. Recommend marking phases 01-04 as Done.

---

### Unresolved Questions

1. Is Redis a required dependency or optional? Determines whether startup failure should be fatal or degraded.
2. Is `original_url` index already present in an earlier migration, or does this PR need to add one?
3. Is the `DB_PASSWORD` / `DB_HOST` in `.env.example` from a real environment? If so, rotate credentials.
