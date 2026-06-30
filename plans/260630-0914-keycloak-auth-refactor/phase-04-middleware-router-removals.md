# Phase 04 — Middleware, Router, Wiring & Removals

**Context:** [plan.md](plan.md) · [spec](../reports/spec-260630-0914-keycloak-auth-refactor.md)

## Overview
- **Priority:** High
- **Status:** pending
- New Keycloak middleware; rewire routes; delete the old in-code auth. After this phase the full build is green again.

## Related Code Files
- **Create:** `internal/middleware/keycloak.go`
- **Modify:** `internal/router/router.go`, `cmd/server/main.go`, `internal/handler/auth_handler.go` (strip to `Me`)
- **Delete:** `internal/service/auth_service.go` (+`auth_service_test.go`), `internal/repository/refresh_token_repository.go`, `pkg/token/` (token.go + token_test.go), `internal/middleware/{api_key,api_key_test,authn,authn_test,jwt,jwt_test}.go`

## Implementation Steps

1. **`internal/middleware/keycloak.go`** — move `ctxUserID` + `UserIDFrom` here (same `"user_id"` key) so downstream is unchanged. Define a minimal user-sync seam to avoid importing the whole service:
   ```go
   const ctxUserID = "user_id"

   type userSyncer interface { // satisfied by service.UserService
       SyncFromKeycloak(ctx context.Context, in service.SyncInput) (*repository.User, error)
   }

   func Keycloak(v keycloak.TokenVerifier, users userSyncer) echo.MiddlewareFunc {
       return func(next echo.HandlerFunc) echo.HandlerFunc {
           return func(c echo.Context) error {
               raw, ok := strings.CutPrefix(c.Request().Header.Get("Authorization"), "Bearer ")
               if !ok || strings.TrimSpace(raw) == "" {
                   return response.Error(c, apperror.New(401, "UNAUTHORIZED", "missing bearer token"))
               }
               id, err := v.Verify(c.Request().Context(), raw)
               if err != nil {
                   return response.Error(c, apperror.New(401, "UNAUTHORIZED", "invalid or expired token"))
               }
               u, err := users.SyncFromKeycloak(c.Request().Context(), service.SyncInput{Sub: id.Sub, Email: id.Email, Username: id.Username})
               if err != nil { return response.Error(c, err) }
               c.Set(ctxUserID, u.ID)
               return next(c)
           }
       }
   }

   func UserIDFrom(c echo.Context) (int64, bool) { id, ok := c.Get(ctxUserID).(int64); return id, ok }
   ```

2. **`auth_handler.go`** — delete `Register/Login/Refresh/Logout` + their request structs; keep `Me` (and a constructor taking only `service.UserService`). Update Swagger annotation on `Me` to `@Security BearerAuth`.

3. **`router.go`** — drop the `/auth` write routes + the API-key `/api` middleware. Replace `Deps.Issuer`/`APIKeys` with a single `Auth echo.MiddlewareFunc` (the built Keycloak mw) or pass `verifier` + `userSvc`:
   ```go
   km := appmw.Keycloak(deps.Verifier, deps.Users)
   auth := e.Group("/auth"); auth.GET("/me", h.Auth.Me, km)
   users := e.Group("/users", km); users.GET("", h.User.List); users.GET("/:id", h.User.Get)
   api := e.Group("/api", km)
   links := api.Group("/links")
   links.POST("", h.Link.Create, appmw.DuplicateURLCheck(deps.Dedup), appmw.QuotaCheck(deps.Quota))
   links.GET("/:code/stats", h.Link.Stats)
   e.GET("/:code", h.Redirect.Redirect) // public, last
   ```
   Update `router.Deps` (remove `Issuer`/`APIKeys`; add `Verifier keycloak.TokenVerifier`, `Users service.UserService`).

4. **`main.go`** — remove issuer/auth-service/refresh-repo wiring. Add:
   ```go
   verifier := keycloak.NewVerifier(ctx, cfg.Keycloak.Issuer, cfg.Keycloak.JWKSURL, cfg.Keycloak.ClientID) // ctx = long-lived app context
   ```
   Pass `Verifier`/`Users` into `router.Deps`. `AuthHandler` now `handler.NewAuthHandler(userSvc)`.

5. **Delete** the files listed above; `go mod tidy` (drops `golang-jwt/jwt/v5`, `golang.org/x/crypto/bcrypt` if now unused).

6. `go build ./...` — full build green.

## Edge Cases / Notes
- `DuplicateURLCheck`/`QuotaCheck` keep their `if !ok { next }` guards; with Keycloak-only `/api`, `ok` is always true (defensive, harmless).
- The Keycloak mw on `/api` runs before the create-route middleware chain, so `user_id` is set before dedup/quota — order preserved.

## Todo
- [ ] `keycloak.go` middleware (+ moved `ctxUserID`/`UserIDFrom`)
- [ ] strip `auth_handler.go` to `Me`
- [ ] router rewire (Keycloak mw on /api, /users, /auth/me; drop API key + auth-write routes)
- [ ] main wiring (verifier; drop issuer/auth/refresh)
- [ ] delete auth_service / refresh repo / pkg/token / api_key+authn+jwt mw
- [ ] `go mod tidy`; `go build ./...` green

## Success Criteria
- Full build green. Manual E2E (curl with a real Keycloak token): create link → owned + quota; `/auth/me` → synced user; no `/auth/register` etc.; redirect public.

## Next
Phase 05: tests + docs.
