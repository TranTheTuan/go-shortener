# Phase 03 — Service: Delete/Update, Resolve active, evictions

## Overview
- **Priority:** P1 · **Status:** ✅ done · Depends on: 02
- Core business logic: ownership enforcement, mutations, cache/dedup eviction, and honoring `is_active` on redirect.

## Related files
- Modify: `internal/service/link_service.go` (interface + impl, `Resolve`)
- Modify: `internal/service/dedup_cache.go` (add `Forget`) — verify exact filename/type first
- Wiring already present: `linkSvc` has `repo` (LinkRepository) + `cache` (LinkCacheRepository).

## Steps
1. **Ownership helper** — fetch by code, verify owner, uniform 404:
   ```go
   func (s *linkService) ownedByCode(ctx, code string, ownerID int64) (*repository.Link, error) {
     link, err := s.repo.GetByCode(ctx, code)
     if errors.Is(err, repository.ErrNotFound) { return nil, apperror.NotFound("short link not found") }
     if err != nil { return nil, apperror.Internal(err) }
     if link.UserID == nil || *link.UserID != ownerID { return nil, apperror.NotFound("short link not found") }
     return link, nil
   }
   ```
2. **Delete** `Delete(ctx, code string, ownerID int64) error`:
   - `link := ownedByCode(...)`; `s.repo.Delete(ctx, link.ID)`.
   - Evict link cache: `s.cache.Delete(ctx, code)` (non-fatal).
   - Evict dedup so the URL can be re-created: `s.dedup.Forget(ctx, ownerID, link.OriginalURL)`.
3. **Update** `Update(ctx, code string, ownerID int64, expiresAt *time.Time, isActive bool) (*repository.Link, error)`:
   - `link := ownedByCode(...)`.
   - `fields := map[string]any{"is_active": isActive, "expires_at": expiresAt}` (nil clears expiry).
   - `s.repo.Update(ctx, link.ID, fields)`; evict cache `s.cache.Delete(ctx, code)`.
   - Return the merged/updated link (apply fields to `link` in memory, or re-`GetByCode`) for the 200 body.
4. **Resolve — honor is_active** (redirect path). Current code returns a cache hit without re-checking; and only checks expiry on the DB path. Add after the expiry check, before `cacheSet`:
   ```go
   if !link.IsActive { return nil, apperror.Gone("short link is disabled") }
   ```
   Because inactive links hit this branch **before** `cacheSet`, they are never cached → re-enabling works on next resolve, and a disabled link is only reachable via a pre-existing cache entry, which step 3 evicts.
5. **ListByOwner — thread status.** Update the signature to accept `status string` and forward `(ownerID, status, s.now().UTC(), limit, offset)` to the repo. Clamp paging as today. No other logic.
6. **Interface** — add `Delete` + `Update` to `LinkService`; update `ListByOwner` signature.
7. **DedupCache.Forget** — add `Forget(ctx, ownerID int64, url string) error` that `Del`s the same key `DuplicateURLCheck`/`Remember` builds. Reuse the existing key-builder; wrap in the breaker like the other dedup ops. Non-fatal on error.

## Todo
- [x] `ownedByCode` helper (404 on missing/non-owner/unowned)
- [x] `Delete` (repo delete + cache evict + dedup forget)
- [x] `Update` (map update + cache evict, returns updated link)
- [x] `Resolve` returns Gone for `!is_active`, never caches inactive
- [x] `ListByOwner` accepts + forwards `status` (+ `now`)
- [x] `DedupCache.Forget`
- [x] interface + compiles

## Success criteria
- Non-owner delete/update → 404 (not 403 — don't leak existence).
- Disabling then hitting the code → 410 Gone even if it was cached (evicted).
- Re-enabling → resolves again (200/redirect) unless expired.
- After delete, creating the same URL again is allowed (dedup forgotten).

## Risks
- If `DedupCache` key builder isn't exported/reusable, replicate the exact key format — a mismatch silently no-ops the forget. Verify against `DuplicateURLCheck`.
