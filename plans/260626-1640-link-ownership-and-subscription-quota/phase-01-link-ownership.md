# Phase 01 — Part A: Link Ownership

**Context:** [plan.md](plan.md) · [spec](../reports/spec-260626-1538-link-ownership-and-subscription-quota.md)

## Overview
- **Priority:** High (foundation for quota)
- **Status:** pending
- Stamp `user_id` on links; let JWT users create links alongside API key; per-owner dedup.

## Related Code Files
- **Create:** `migrations/000006_add_link_owner.{up,down}.sql`, `internal/middleware/authn.go`
- **Modify:** `internal/repository/link_repository.go`, `internal/service/link_service.go`,
  `internal/handler/link_handler.go`, `internal/router/router.go`, `cmd/server/main.go`,
  `internal/service/mocks_test.go`, `internal/service/link_service_test.go`

## Implementation Steps

1. **Migration 000006** (`make migrate-create NAME=add_link_owner`), up:
   ```sql
   ALTER TABLE links ADD COLUMN user_id BIGINT REFERENCES users(id) ON DELETE SET NULL;
   CREATE INDEX idx_links_user_id_original_url ON links (user_id, original_url);
   ```
   down: drop index, drop column.

2. **`repository.Link`** — add `UserID *int64 `gorm:"index" json:"user_id,omitempty"``.
   Replace `GetByOriginalURL(ctx, url)` with:
   ```go
   GetByOwnerAndURL(ctx context.Context, ownerID *int64, url string) (*Link, error)
   ```
   Impl: `q := r.db.WithContext(ctx); if ownerID == nil { q = q.Where("user_id IS NULL") } else { q = q.Where("user_id = ?", *ownerID) }`
   then `q.Where("original_url = ?", url).First(&link)`; map `ErrRecordNotFound`→`ErrNotFound`.

3. **`internal/middleware/authn.go`** — combined auth (replaces APIKey on `/api`):
   ```go
   func Authn(issuer *token.Issuer, apiKeys []string) echo.MiddlewareFunc {
       set := keySet(apiKeys) // reuse APIKey's trimming logic (extract a helper)
       return func(next echo.HandlerFunc) echo.HandlerFunc {
           return func(c echo.Context) error {
               // 1. JWT?
               if raw, ok := strings.CutPrefix(c.Request().Header.Get("Authorization"), "Bearer "); ok && strings.TrimSpace(raw) != "" {
                   if claims, err := issuer.Parse(raw); err == nil {
                       c.Set(ctxUserID, claims.UserID)
                       return next(c)
                   }
                   return unauthorized(c, "invalid or expired token")
               }
               // 2. API key?
               if k := c.Request().Header.Get(apiKeyHeader); k != "" {
                   if _, ok := set[k]; ok { return next(c) }
               }
               return unauthorized(c, "missing or invalid credentials")
           }
       }
   }
   ```
   Keep `JWT()` (used by `/auth/me`, `/users`). Factor shared `ctxUserID`, `apiKeyHeader`, key-set builder so `APIKey` + `Authn` stay DRY.

4. **`LinkService.Create`** — add `ownerID *int64` to `CreateLinkInput`; dedup via
   `GetByOwnerAndURL(ctx, in.OwnerID, target)`; set `UserID: in.OwnerID` on new `Link`.

5. **`link_handler.go` Create** — read owner from context: `var owner *int64; if id, ok := appmw.UserIDFrom(c); ok { owner = &id }`; pass into `CreateLinkInput`. Add `user_id` to nothing client-facing unless desired (keep response shape).

6. **`router.go`** — swap `api.Use(appmw.APIKey(apiKeys))` → `api.Use(appmw.Authn(issuer, apiKeys))`. Pass `issuer` (already in `registerRoutes`).

7. **Update existing tests/mocks**: `mockLinkRepo.GetByOriginalURL` → `GetByOwnerAndURL`; fix `link_service_test.go` call sites (pass `ownerID`).

8. `go build ./...` + `go test ./internal/service/`.

## Key Insight
Ownerless (API-key) requests have `ownerID == nil` → dedup within the NULL group;
JWT requests dedup within their own `user_id`. One code path, two scopes.

## Todo
- [ ] Migration 000006 up/down
- [ ] `Link.UserID` + `GetByOwnerAndURL`
- [ ] `Authn` middleware (DRY with `APIKey`)
- [ ] `LinkService.Create` ownerID + per-owner dedup
- [ ] Handler reads owner from context
- [ ] Router swaps APIKey→Authn on `/api`
- [ ] Fix link mocks/tests
- [ ] build + service tests pass

## Success Criteria
- JWT create → link row has `user_id`; API-key create → `user_id` NULL; both still 201.
- `X-API-Key` flow unchanged; redirect public.

## Next
Phase 02 adds plans/subscriptions; quota (Phase 3-4) gates the JWT path established here.
