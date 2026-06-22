# Phase 01 — Config, Dependencies & Migrations

**Context:** [plan.md](plan.md) · [spec](../reports/spec-260622-1539-username-password-auth.md)

## Overview
- **Priority:** High (foundation)
- **Status:** pending
- Add auth config, install deps, create DB migrations. No app logic yet.

## Requirements
- `AuthConfig` with `AUTH_` env prefix; fail-closed JWT secret in non-dev.
- Two migration pairs: user credentials + refresh_tokens table.
- New Go deps added & tidied.

## Related Code Files
- **Modify:** `configs/config.go`, `.env.example`, `go.mod`/`go.sum`
- **Create:** `migrations/000004_add_user_credentials.{up,down}.sql`,
  `migrations/000005_create_refresh_tokens_table.{up,down}.sql`

## Implementation Steps

1. **Add deps:**
   ```bash
   go get github.com/golang-jwt/jwt/v5
   go get golang.org/x/crypto/bcrypt
   go mod tidy
   ```

2. **`configs/config.go`** — add `Auth AuthConfig` to `Config` with `envPrefix:"AUTH_"`, and:
   ```go
   // AuthConfig holds authentication settings.
   type AuthConfig struct {
       // JWTSecret signs access tokens. Required; empty is rejected outside development.
       JWTSecret  string        `env:"JWT_SECRET" envDefault:"dev-insecure-change-me"`
       AccessTTL  time.Duration `env:"ACCESS_TTL" envDefault:"15m"`
       RefreshTTL time.Duration `env:"REFRESH_TTL" envDefault:"168h"`
       BcryptCost int           `env:"BCRYPT_COST" envDefault:"12"`
   }
   ```
   Add a `Config.Validate()` (or inline in `Load`) returning an error when
   `Env != "development"` and `JWTSecret == "" || == "dev-insecure-change-me"`.
   Call it at the end of `Load`. (main.go already returns on Load error.)

3. **`.env.example`** — append `AUTH_JWT_SECRET=`, `AUTH_ACCESS_TTL=15m`,
   `AUTH_REFRESH_TTL=168h`, `AUTH_BCRYPT_COST=12` with comments.

4. **Migration 000004** (`make migrate-create NAME=add_user_credentials`), up:
   ```sql
   ALTER TABLE users ADD COLUMN username      varchar(255);
   ALTER TABLE users ADD COLUMN password_hash varchar(255);
   ALTER TABLE users ALTER COLUMN name DROP NOT NULL;
   CREATE UNIQUE INDEX idx_users_username ON users (username);
   ALTER TABLE users ALTER COLUMN username      SET NOT NULL;
   ALTER TABLE users ALTER COLUMN password_hash SET NOT NULL;
   ```
   down: drop index, drop both columns, `ALTER COLUMN name SET NOT NULL`.

5. **Migration 000005** (`make migrate-create NAME=create_refresh_tokens_table`), up:
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
   down: `DROP TABLE refresh_tokens;`

6. `go build ./...` to confirm config compiles.

## Todo
- [ ] Add deps + `go mod tidy`
- [ ] `AuthConfig` + validation in `configs/config.go`
- [ ] Update `.env.example`
- [ ] Migration 000004 up/down
- [ ] Migration 000005 up/down
- [ ] `go build ./...` passes

## Success Criteria
- `go build ./...` compiles. `make migrate-up` then `make migrate-down NUM=2` round-trips cleanly on a dev DB.

## Risks
- On a populated DB, `username SET NOT NULL` needs backfill first. Template/dev DBs start empty — note in README only.

## Next
Phase 02 (repository) depends on the new columns + tables.
