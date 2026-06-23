# Design Spec: Username/Password Authentication (JWT access + refresh)

- **Date:** 2026-06-22
- **Status:** Approved (ready for /plan)
- **Scope:** Register + login with username & password. JWT access + refresh tokens. Adds JWT middleware alongside existing static API key.

## Problem Statement

Template has no user authentication. `users` table is a demo resource (`id/name/email`, no password). Write endpoints (`/api/links`) are protected only by a shared static `X-API-Key`. Need real per-user auth (register/login) so users have identities — foundation for a later feature: **user-owned API keys + per-key rate limiting** (NOT built now).

## User Stories

- As a new user, I register with `username + email + password` (both username and email unique) → get an account.
- As a user, I log in with **email** + password → receive an access token + refresh token. (Email-only login keeps the door open for email verification later.)
- As a user, I call protected endpoints with `Authorization: Bearer <access_token>`.
- As a user, I refresh an expired access token using my refresh token (old refresh token rotated/invalidated).
- As a user, I log out → my refresh token is revoked.

## Decisions (locked)

| # | Decision | Choice |
|---|---|---|
| 1 | Session mechanism | JWT stateless access token (HS256) |
| 2 | User model | Extend existing `users` table |
| 3 | Integration | Keep static API key on `/api/links`; **add** JWT middleware alongside |
| 4 | Token lifetime | Access token (15m) + refresh token (7d) |
| 5 | Refresh storage | DB table, sha256-hashed, rotation on use, revocable |
| 6 | Token transport | JSON response body |
| 7 | Legacy `POST /users` | **Removed** — `/auth/register` is the only user-creation path; `username`+`password_hash` are `NOT NULL` |
| 8 | Login input | **Email only** (`email` + `password`) — keeps path open for future email verification |

Future (out of scope, design leaves door open): user-owned API keys reusing the hashed-token-in-DB pattern; per-key rate limiting.

## Approaches Considered

- **JWT stateless (chosen)** — no per-request DB lookup for access tokens; simple; fits API template. Refresh tokens kept stateful in DB for revocation.
- Redis-backed opaque sessions — revocable but per-request Redis hit; rejected (access tokens don't need it).
- JWT access-only (no refresh) — simpler but worse UX/security tradeoff; rejected per decision #4.

## Architecture

Mirrors existing `handler → service → repository` layering and uniform `response.Envelope` + `apperror` conventions.

```
POST /auth/register ─┐
POST /auth/login     ├─► AuthHandler ─► AuthService ─┬─► UserRepository (users)
POST /auth/refresh   │                               ├─► RefreshTokenRepository (refresh_tokens)
POST /auth/logout    │                               └─► pkg/token (JWT sign/verify), bcrypt
GET  /auth/me  ──[JWT middleware]─► AuthHandler.Me ─► UserService.GetUser
```

### Components

**pkg/token** (new) — JWT sign/verify, HS256 via `github.com/golang-jwt/jwt/v5`.
- `Issuer` struct holding secret + access TTL.
- `Issue(userID int64) (string, error)` → signed access token; claims: `sub`(user_id), `exp`, `iat`.
- `Parse(tokenStr string) (Claims, error)` → validates signature + expiry, returns `user_id`.
- Keep < 200 LOC; split `token.go` / `claims.go` only if needed.

**repository** (modify + new)
- `User` struct gains: `Username string` (`gorm:"size:255;uniqueIndex;not null"`), `PasswordHash string` (`gorm:"size:255;not null" json:"-"`). `Name` becomes nullable (`*string`) — optional display name. `Email` stays `not null;uniqueIndex`.
- `UserRepository` gains `GetByUsername(ctx, username) (*User, error)` and `GetByEmail(ctx, email) (*User, error)`. Existing `Create` reused for registration.
- New `RefreshTokenRepository` + `RefreshToken` entity:
  - Fields: `ID int64`, `UserID int64` (FK→users, indexed), `TokenHash string` (sha256 hex, `uniqueIndex;not null`), `ExpiresAt time.Time`, `RevokedAt *time.Time`, `CreatedAt time.Time`.
  - Methods: `Create(ctx, *RefreshToken)`, `GetByHash(ctx, hash) (*RefreshToken, error)`, `Revoke(ctx, id) error`.

**service** (new) — `AuthService` interface:
- `Register(ctx, RegisterInput) (*repository.User, error)` — validate, bcrypt-hash password, persist. Maps `ErrConflict` → 409 (username or email taken).
- `Login(ctx, LoginInput) (*TokenPair, error)` — resolve user by email, bcrypt compare, issue access + refresh. Bad creds → generic 401 `invalid email or password` (no enumeration).
- `Refresh(ctx, refreshToken string) (*TokenPair, error)` — hash → lookup → check not revoked/expired → **rotate** (revoke old, create new) → issue new access + refresh.
- `Logout(ctx, refreshToken string) error` — hash → lookup → revoke (idempotent, no error if already gone/revoked).
- `TokenPair{AccessToken, RefreshToken string; ExpiresIn int; TokenType "Bearer"}`.
- Injected deps: `UserRepository`, `RefreshTokenRepository`, `token.Issuer`, `now func() time.Time`, refresh TTL, bcrypt cost. Refresh token value = crypto-random (32 bytes, base64url); only its sha256 stored.

**middleware** (new) — `JWT(issuer)`:
- Read `Authorization: Bearer <token>`; missing/malformed/expired → 401 via `apperror`/`response.Error` (same shape as `APIKey`).
- On success set `user_id` in `echo.Context` (e.g. `c.Set("user_id", claims.UserID)`).

**handler** (new) — `AuthHandler` (thin): `Register`, `Login`, `Refresh`, `Logout`, `Me`. Bind → call service → uniform envelope. Swagger annotations matching existing style.

**config** (modify) — new `AuthConfig` (`envPrefix:"AUTH_"`):
- `JWTSecret string` (`env:"JWT_SECRET"`) — required; if empty and `ENV != development` → fail at startup (fail-closed). Dev default provided with a startup warning.
- `AccessTTL time.Duration` (`env:"ACCESS_TTL" envDefault:"15m"`).
- `RefreshTTL time.Duration` (`env:"REFRESH_TTL" envDefault:"168h"`).
- `BcryptCost int` (`env:"BCRYPT_COST" envDefault:"12"`).

**router** (modify) — add public `auth := e.Group("/auth")`: `POST /register`, `/login`, `/refresh`, `/logout`. Protected `auth.GET("/me", h.Auth.Me, appmw.JWT(issuer))`. Remove `users.POST("")`; keep `GET /users`, `GET /users/:id`.

**main** (modify) — build `token.Issuer`, `RefreshTokenRepository`, `AuthService`, `AuthHandler`; pass JWT middleware into router.

## Data Model / Migrations

`migrations/000004_add_user_credentials.up.sql`:
```sql
ALTER TABLE users ADD COLUMN username      varchar(255);
ALTER TABLE users ADD COLUMN password_hash varchar(255);
ALTER TABLE users ALTER COLUMN name DROP NOT NULL;
-- backfill not needed (template/dev); for existing rows set username before NOT NULL in real deploys
CREATE UNIQUE INDEX idx_users_username ON users (username);
ALTER TABLE users ALTER COLUMN username      SET NOT NULL;
ALTER TABLE users ALTER COLUMN password_hash SET NOT NULL;
```
`.down.sql`: drop index, drop columns, restore `name NOT NULL`.

`migrations/000005_create_refresh_tokens_table.up.sql`:
```sql
CREATE TABLE refresh_tokens (
    id         BIGSERIAL PRIMARY KEY,
    user_id    BIGINT      NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    token_hash varchar(64) NOT NULL,
    expires_at TIMESTAMPTZ NOT NULL,
    revoked_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX idx_refresh_tokens_token_hash ON refresh_tokens (token_hash);
CREATE INDEX idx_refresh_tokens_user_id ON refresh_tokens (user_id);
```
`.down.sql`: `DROP TABLE refresh_tokens;`

## Interface Contracts

`POST /auth/register` → `201`
```jsonc
// req
{ "username": "alice", "email": "alice@example.com", "password": "s3cretpw!", "name": "Alice" } // name optional
// res
{ "data": { "id": 1, "username": "alice", "email": "alice@example.com", "name": "Alice", "created_at": "..." } }
```
`POST /auth/login` → `200` (req: `{ "email": "alice@example.com", "password": "..." }`)
```jsonc
{ "data": { "access_token": "<jwt>", "refresh_token": "<opaque>", "token_type": "Bearer", "expires_in": 900 } }
```
`POST /auth/refresh` → `200` (req `{ "refresh_token": "..." }`) → same shape as login.
`POST /auth/logout` → `204` (req `{ "refresh_token": "..." }`).
`GET /auth/me` (Bearer) → `200 { "data": { user } }`.

Errors (uniform envelope): `400` validation, `401` bad creds / invalid-expired token, `409` username or email already exists.

## Validation Rules

- `username`: required, 3–50 chars, `[a-zA-Z0-9_]`.
- `email`: required, contains `@` (matches existing user-service check), unique.
- `password`: required, min 8 chars.
- Login: `email` + `password` required; user resolved by email only (username is not a login credential).

## Security

- bcrypt cost 12 (configurable). Never serialize `password_hash` (`json:"-"`).
- Refresh tokens: 32-byte crypto-random, stored as sha256 hex only; rotated on every refresh; revoked on logout; `ON DELETE CASCADE` with user.
- Generic auth-failure message (no user enumeration).
- JWT secret from env; fail-closed (non-dev) if empty.
- Access TTL short (15m) limits stolen-token window.

## Testing Strategy

Unit tests follow existing mock pattern ([internal/service/mocks_test.go](../../internal/service/mocks_test.go)):
- **AuthService**: register success; duplicate username; duplicate email; login by email success; login unknown email → 401; login bad password → 401; refresh success + rotation (old revoked); refresh with revoked/expired token; logout revokes.
- **JWT middleware**: valid token sets `user_id`; missing header → 401; malformed → 401; expired → 401.
- **pkg/token**: sign→parse roundtrip; tampered signature rejected; expired token rejected.
- **Repository**: optional integration tests gated like existing suite.
Run `make test`; do not weaken assertions to pass.

## Files

**Create:**
- `migrations/000004_add_user_credentials.{up,down}.sql`
- `migrations/000005_create_refresh_tokens_table.{up,down}.sql`
- `pkg/token/token.go` (+ `token_test.go`)
- `internal/repository/refresh_token_repository.go`
- `internal/service/auth_service.go` (+ `auth_service_test.go`)
- `internal/middleware/jwt.go` (+ `jwt_test.go`)
- `internal/handler/auth_handler.go`

**Modify:**
- `internal/repository/user_repository.go` (fields + `GetByUsername`/`GetByEmail`)
- `internal/service/user_service.go` (drop unused `CreateUser` or keep for GET-only flows — decide in plan)
- `configs/config.go` (`AuthConfig`)
- `internal/router/router.go` (auth routes, JWT mw, remove `POST /users`)
- `cmd/server/main.go` (wiring)
- `.env.example` (`AUTH_*` vars)
- `README.md` + Swagger (docs-manager) — API table, security definitions
- `go.mod`/`go.sum` (`golang-jwt/jwt/v5`, `golang.org/x/crypto`)

## Risks

- **Migration on populated DB** — adding `username NOT NULL` to existing rows needs backfill before `SET NOT NULL`. Fine for template/dev; note for real deployments.
- **Removing `POST /users`** breaks existing API consumers/tests — acceptable (template demo); update tests + README.
- **JWT secret management** — must be set in production; startup guard mitigates.

## Success Criteria

- Register → login (by email) → access protected `/auth/me` → refresh → logout flow works end-to-end.
- `make test` green; no lint/compile errors (`make build`).
- Static API key on `/api/links` unchanged and still enforced.
- `password_hash` never appears in any JSON response.

## Open Questions

- None blocking. (Backfill strategy for existing `users` rows deferred to deployment time — template/dev start empty.)

## Next Steps

Proceed to `/plan` to produce phased implementation plan (suggested phases: 1. config + migrations, 2. repository, 3. pkg/token + service, 4. middleware + handlers + wiring, 5. tests + docs).
