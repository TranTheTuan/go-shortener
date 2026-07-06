# Phase 02 — Repository + Cache Delete

## Overview
- **Priority:** P1 · **Status:** ✅ done · Depends on: 01
- Add persistence for delete + update, and the missing cache eviction primitive.

## Related files
- Modify: `internal/repository/link_repository.go` (interface + impl)
- Modify: `internal/repository/link_cache_repository.go` (interface + impl)

## Steps
1. **LinkRepository interface** — add:
   ```go
   Delete(ctx context.Context, id int64) error
   Update(ctx context.Context, id int64, fields map[string]any) error
   ```
   - `Delete`: `db.WithContext(ctx).Delete(&Link{}, id)` — `clicks` cascade via FK. Returning `RowsAffected == 0` → return `ErrNotFound` (idempotent-friendly; service maps to 404).
   - `Update`: `db.WithContext(ctx).Model(&Link{}).Where("id = ?", id).Updates(fields)`. Use an explicit column map so `expires_at = nil` (clear) is written (GORM skips zero-value struct fields; a map does not). `RowsAffected == 0` → `ErrNotFound`.
   - Keep `GetByCode` for the owner lookup done in the service.
2. **ListByOwner — add status filter.** Extend signature to accept a status string:
   ```go
   ListByOwner(ctx context.Context, ownerID int64, status string, limit, offset int) ([]*OwnedLink, int64, error)
   ```
   Build a shared `WHERE` from `(ownerID, status, now)` and apply it to BOTH the rows query and the total count so paging stays correct:
   - `"active"`   → `is_active AND (expires_at IS NULL OR expires_at > ?now)`
   - `"disabled"` → `NOT is_active`
   - `"expired"`  → `is_active AND expires_at IS NOT NULL AND expires_at <= ?now`
   - `""`/`"all"` → owner only (no extra predicate)
   Pass `now` in from the service (`s.now()`), don't call `time.Now()` in the repo. Unknown status is rejected upstream (handler), so the repo can treat anything unexpected as "all".

3. **LinkCacheRepository interface** — add:
   ```go
   Delete(ctx context.Context, code string) error
   ```
   Impl: `r.rdb.Client.Del(ctx, linkCacheKeyPrefix+code).Err()`. Treat redis errors as non-fatal at the call site (log, don't fail the mutation) — matches the breaker-tolerant style elsewhere.

## Todo
- [x] `Delete(id)` (cascade, ErrNotFound on 0 rows)
- [x] `Update(id, fields map)` (map so nil expiry clears)
- [x] `ListByOwner` status filter (rows + count share the WHERE; `now` passed in)
- [x] cache `Delete(code)`
- [x] compiles

## Success criteria
- Deleting a link removes its `clicks` rows (FK cascade).
- `Update` can set `expires_at` to NULL and toggle `is_active` in one call.
- Cache key for a code can be evicted.

## Notes
- **Why a field map for Update, not a struct:** GORM `.Updates(struct)` omits zero values → can't clear `expires_at` or set `is_active=false`. A `map[string]any` writes exactly the given columns.
