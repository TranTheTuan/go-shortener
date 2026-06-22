# Phase 04 — Middleware, Handlers & Wiring

**Context:** [plan.md](plan.md) · [spec](../reports/spec-260622-1539-username-password-auth.md)

## Overview
- **Priority:** High
- **Status:** pending
- JWT middleware, `AuthHandler`, router routes, main wiring. Remove `POST /users`.

## Related Code Files
- **Create:** `internal/middleware/jwt.go`, `internal/handler/auth_handler.go`
- **Modify:** `internal/router/router.go`, `cmd/server/main.go`,
  `internal/service/user_service.go` (drop now-unused `CreateUser`)

## Implementation Steps

1. **`internal/middleware/jwt.go`** — mirror `api_key.go` style:
   ```go
   const ctxUserID = "user_id"
   func JWT(issuer *token.Issuer) echo.MiddlewareFunc {
       return func(next echo.HandlerFunc) echo.HandlerFunc {
           return func(c echo.Context) error {
               h := c.Request().Header.Get("Authorization")
               raw, ok := strings.CutPrefix(h, "Bearer ")
               if !ok || raw == "" {
                   return response.Error(c, apperror.New(401,"UNAUTHORIZED","missing bearer token"))
               }
               claims, err := issuer.Parse(raw)
               if err != nil {
                   return response.Error(c, apperror.New(401,"UNAUTHORIZED","invalid or expired token"))
               }
               c.Set(ctxUserID, claims.UserID)
               return next(c)
           }
       }
   }
   ```
   Export a helper `UserIDFrom(c echo.Context) (int64, bool)` for handlers.

2. **`internal/handler/auth_handler.go`** — `AuthHandler{auth service.AuthService; users service.UserService}`.
   Methods + request structs + Swagger annotations (match `user_handler.go` style):
   - `Register` POST → bind `{username,email,password,name?}` → `auth.Register` → `201 {user}`.
   - `Login` POST → bind `{email,password}` → `auth.Login` → `200 {TokenPair}`.
   - `Refresh` POST → bind `{refresh_token}` → `auth.Refresh` → `200 {TokenPair}`.
   - `Logout` POST → bind `{refresh_token}` → `auth.Logout` → `204` (`c.NoContent`).
   - `Me` GET → `UserIDFrom(c)` → `users.GetUser` → `200 {user}`.
   Bind failure → `apperror.BadRequest("invalid request body")`.

3. **`internal/router/router.go`:**
   - Add `Auth *handler.AuthHandler` to `Handlers`; add `issuer *token.Issuer` param to `New`/`registerRoutes`.
   - Remove `users.POST("")`. Keep `GET ""` and `GET "/:id"`.
   - Add group:
     ```go
     auth := e.Group("/auth")
     auth.POST("/register", h.Auth.Register)
     auth.POST("/login", h.Auth.Login)
     auth.POST("/refresh", h.Auth.Refresh)
     auth.POST("/logout", h.Auth.Logout)
     auth.GET("/me", h.Auth.Me, appmw.JWT(issuer))
     ```

4. **`cmd/server/main.go`** — wire:
   ```go
   issuer := token.NewIssuer(cfg.Auth.JWTSecret, cfg.Auth.AccessTTL)
   refreshRepo := repository.NewRefreshTokenRepository(db)
   authSvc := service.NewAuthService(userRepo, refreshRepo, issuer, cfg.Auth.RefreshTTL, cfg.Auth.BcryptCost)
   ```
   Add `Auth: handler.NewAuthHandler(authSvc, userSvc)` to `router.Handlers`, pass `issuer` to `router.New`.

5. **`user_service.go`** — remove `CreateUser` + `CreateUserInput` (no longer routed).
   Keep `GetUser`, `ListUsers`. Update interface. (Registration owns user creation now.)

6. `go build ./...`.

## Todo
- [ ] `jwt.go` middleware + `UserIDFrom` helper
- [ ] `auth_handler.go` (5 methods + Swagger)
- [ ] Router: add `/auth` group, JWT on `/me`, remove `POST /users`
- [ ] `main.go` wiring (issuer, refresh repo, auth service/handler)
- [ ] Trim `user_service.go` (drop CreateUser)
- [ ] `go build ./...` passes

## Success Criteria
- Server builds & boots. Manual E2E (curl): register → login → `GET /auth/me` with Bearer → refresh → logout.
- `/api/links` still requires `X-API-Key` (unchanged).

## Security
JWT middleware fail-closed; generic 401s; no token logged.

## Next
Phase 05 adds tests + docs.
