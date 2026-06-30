# Phase 01 — Config, Migration & Dependencies

**Context:** [plan.md](plan.md) · [spec](../reports/spec-260630-0914-keycloak-auth-refactor.md)

## Overview
- **Priority:** High (foundation)
- **Status:** pending
- Add Keycloak config + go-oidc dep + schema migration. No app logic yet.

## Related Code Files
- **Modify:** `configs/config.go`, `.env.example`, `go.mod`
- **Create:** `migrations/000009_keycloak_auth.{up,down}.sql`

## Implementation Steps

1. **Add dep:** `go get github.com/coreos/go-oidc/v3` && `go mod tidy`.

2. **`configs/config.go`** — replace `AuthConfig` (and `Auth` field) with:
   ```go
   type KeycloakConfig struct {
       Issuer   string `env:"ISSUER"`    // public, e.g. https://auth.cd.me/realms/<realm>
       JWKSURL  string `env:"JWKS_URL"`  // in-cluster certs endpoint
       ClientID string `env:"CLIENT_ID"` // audience; empty → SkipClientIDCheck
   }
   ```
   Add `Keycloak KeycloakConfig `envPrefix:"KEYCLOAK_"`` to `Config`; remove the `Auth` field + `AuthConfig` + `defaultDevJWTSecret` + the JWT-secret branch in `validate()`. Add validation: non-development requires `Issuer` and `JWKSURL` non-empty (fail-closed).

3. **`.env.example`** — remove `AUTH_*`; add:
   ```
   KEYCLOAK_ISSUER=https://auth.cd.me/realms/<realm>
   KEYCLOAK_JWKS_URL=http://keycloak-keycloakx-http.keycloak.svc.cluster.local/realms/<realm>/protocol/openid-connect/certs
   KEYCLOAK_CLIENT_ID=url-shortener-backend
   ```

4. **Migration 000009** (`make migrate-create NAME=keycloak_auth`), up:
   ```sql
   ALTER TABLE users ADD COLUMN keycloak_sub VARCHAR(36);
   CREATE UNIQUE INDEX idx_users_keycloak_sub ON users (keycloak_sub);
   ALTER TABLE users DROP COLUMN password_hash;
   DROP TABLE IF EXISTS refresh_tokens;
   ```
   down: recreate `refresh_tokens` (copy 000005 up body), `ALTER TABLE users ADD COLUMN password_hash VARCHAR(255);`, `DROP INDEX IF EXISTS idx_users_keycloak_sub;`, `ALTER TABLE users DROP COLUMN keycloak_sub;`.

5. `go build ./...` — config compiles. (Auth wiring in main.go still references removed types → expect build break here; it is fixed in Phase 04. To keep this phase self-contained, only assert `configs` + `migrations` are correct; full build is green at Phase 04.)

## Todo
- [ ] add go-oidc, `go mod tidy`
- [ ] `KeycloakConfig` replaces `AuthConfig` + validation
- [ ] `.env.example` KEYCLOAK_* (remove AUTH_*)
- [ ] migration 000009 up/down
- [ ] `go build ./configs/...` compiles

## Success Criteria
- `configs` package compiles; `make migrate-up` then `migrate-down NUM=1` round-trips on a dev DB.

## Risks
- Existing rows: `keycloak_sub` nullable (Postgres allows multiple NULLs) — no backfill needed. Dropping `password_hash` is destructive but intended.

## Next
Phase 02 (verifier) + Phase 03 (user JIT) depend only on the dep + migration.
