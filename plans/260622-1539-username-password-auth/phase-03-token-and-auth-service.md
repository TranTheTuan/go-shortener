# Phase 03 — pkg/token + AuthService

**Context:** [plan.md](plan.md) · [spec](../reports/spec-260622-1539-username-password-auth.md)

## Overview
- **Priority:** High (core logic)
- **Status:** pending
- JWT issuer/parser + `AuthService` (register/login/refresh/logout).

## Related Code Files
- **Create:** `pkg/token/token.go`, `internal/service/auth_service.go`

## Implementation Steps

1. **`pkg/token/token.go`** — HS256 via `golang-jwt/jwt/v5`:
   ```go
   type Claims struct {
       UserID int64 `json:"uid"`
       jwt.RegisteredClaims
   }
   type Issuer struct {
       secret    []byte
       accessTTL time.Duration
       now       func() time.Time
   }
   func NewIssuer(secret string, accessTTL time.Duration) *Issuer
   func (i *Issuer) Issue(userID int64) (string, error)        // sets sub/uid, iat, exp
   func (i *Issuer) Parse(tokenStr string) (*Claims, error)    // validates sig + exp; rejects non-HS256
   ```
   `Parse` MUST check signing method is HMAC (reject `alg` confusion). Return a
   sentinel `ErrInvalidToken` for any parse/validation failure.

2. **`internal/service/auth_service.go`**:
   ```go
   type RegisterInput struct{ Username, Email, Password string; Name *string }
   type LoginInput struct{ Email, Password string }
   type TokenPair struct {
       AccessToken  string `json:"access_token"`
       RefreshToken string `json:"refresh_token"`
       TokenType    string `json:"token_type"`  // "Bearer"
       ExpiresIn    int    `json:"expires_in"`   // access TTL seconds
   }
   type AuthService interface {
       Register(ctx, RegisterInput) (*repository.User, error)
       Login(ctx, LoginInput) (*TokenPair, error)
       Refresh(ctx, refreshToken string) (*TokenPair, error)
       Logout(ctx, refreshToken string) error
   }
   ```
   `authService` deps: `users repository.UserRepository`, `refresh repository.RefreshTokenRepository`,
   `issuer *token.Issuer`, `refreshTTL time.Duration`, `bcryptCost int`, `now func() time.Time`.

3. **Register:** trim/validate (username 3–50 `[a-zA-Z0-9_]`, email contains `@`,
   password ≥ 8). `bcrypt.GenerateFromPassword([]byte(pw), cost)`. Build `User`,
   `users.Create`. Map `ErrConflict` → `apperror.Conflict("username or email already taken")`.
   Pre-check `GetByUsername`/`GetByEmail` optional; rely on unique index + ErrConflict (DRY).

4. **Login:** `users.GetByEmail`; on `ErrNotFound` → return generic
   `apperror.New(401,"UNAUTHORIZED","invalid email or password")`. `bcrypt.CompareHashAndPassword`;
   mismatch → same generic 401. Success → `issueTokenPair(userID)`.

5. **issueTokenPair (private helper):** `issuer.Issue(userID)` for access;
   generate 32 random bytes (`crypto/rand`) → base64url raw string = refresh token;
   `sha256` hex → store via `refresh.Create` with `ExpiresAt = now+refreshTTL`.
   Return `TokenPair`.

6. **Refresh:** sha256 the incoming token → `refresh.GetByHash`. On `ErrNotFound`,
   `RevokedAt != nil`, or `ExpiresAt < now` → generic 401. Else **rotate**:
   `refresh.Revoke(old.ID)` then `issueTokenPair(old.UserID)`.

7. **Logout:** sha256 → `GetByHash` → if found & not revoked, `Revoke`. Idempotent;
   return nil even if not found (no enumeration / no error).

8. `go build ./...`.

## Key Insights
- Generic 401 message everywhere (no user enumeration).
- Refresh rotation: every successful refresh invalidates the presented token.
- `now` injected for deterministic tests.

## Todo
- [ ] `pkg/token` Issuer/Parse with HMAC-only check + `ErrInvalidToken`
- [ ] AuthService types + constructor
- [ ] Register (bcrypt, conflict mapping)
- [ ] Login (email lookup, generic 401)
- [ ] issueTokenPair helper (crypto/rand + sha256)
- [ ] Refresh (validate + rotate)
- [ ] Logout (idempotent revoke)
- [ ] `go build ./...` passes

## Success Criteria
- Compiles. Logic matches spec contracts. Ready for handler wiring.

## Security
bcrypt cost from config; refresh raw never stored; HMAC alg pinned in Parse.

## Next
Phase 04 wires middleware + handlers.
