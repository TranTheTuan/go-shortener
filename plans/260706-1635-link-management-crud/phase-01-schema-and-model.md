# Phase 01 — Schema & Model (is_active)

## Overview
- **Priority:** P1 · **Status:** ✅ done · Depends on: none
- Add a reversible on/off flag to links. Delete needs no schema change (hard delete uses the existing `clicks.link_id ... ON DELETE CASCADE`).

## Related files
- Create: `migrations/000011_add_link_is_active.up.sql`, `migrations/000011_add_link_is_active.down.sql`
- Modify: `internal/repository/link_repository.go` (`Link` struct)

## Steps
1. **Up migration** — add the column with a safe default so existing rows stay live:
   ```sql
   ALTER TABLE links ADD COLUMN is_active BOOLEAN NOT NULL DEFAULT true;
   ```
2. **Down migration**:
   ```sql
   ALTER TABLE links DROP COLUMN is_active;
   ```
3. **Model** — add field to `Link` (keep JSON so the list/stats responses expose it):
   ```go
   IsActive bool `gorm:"not null;default:true" json:"is_active"`
   ```
   Place after `OriginalURL`; `OwnedLink` embeds `Link` so the list API gets it for free.

## Todo
- [x] 000011 up/down SQL
- [x] `Link.IsActive` field
- [x] `make migrate-up` applies cleanly; `go build ./...` (or `go vet ./...`) passes

## Success criteria
- New column defaults `true` for all existing links (no redirect behavior change yet).
- `Link` (and `OwnedLink`) serialize `is_active`.

## Notes
- No `updated_at`, no `deleted_at` (YAGNI — hard delete + no audit requirement).
