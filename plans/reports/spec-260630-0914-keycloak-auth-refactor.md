# Design Spec: Refactor Authentication to Keycloak (Resource Server)

- **Date:** 2026-06-30
- **Status:** Pending user review
- **Scope:** Replace in-code auth (register/login/refresh/logout + self-issued JWT) with validation of **Keycloak-issued** access tokens. The service becomes an OIDC resource server. Link ownership + subscription/quota behavior is preserved.

## Problem Statement

Keycloak now owns authentication (users, credentials, sessions, refresh). The app should stop issuing/managing its own tokens and instead **validate Keycloak access tokens** on protected routes, mapping the Keycloak identity to the existing local ownership/quota model. Keep changes minimal and follow existing layering.

- Keycloak public domain: `auth.cd.me`
- Keycloak in-cluster service: `keycloak-keycloakx-http.keycloak.svc.cluster.local`

## User Stories

- As a user, I obtain a token from Keycloak and call the API with `Authorization: Bearer <access_token>`; the API validates it locally (cached JWKS) without a round-trip per request.
- As a first-time caller, my local user record is auto-provisioned from token claims (JIT) so my links/quota work immediately.
- As a machine client, I use a Keycloak service account (client-credentials) — the static `X-API-Key` is removed.
- As an operator, I configure issuer/JWKS/client via env; no secrets baked in.

## Decisions (locked)

| # | Decision | Choice |
|---|----------|--------|
| 1 | Identity model | Keep local `users`; **JIT-provision** from claims. Add `keycloak_sub` (unique); ownership/quota stay int64 FKs (unchanged downstream). |
| 2 | Token validation | **go-oidc** with explicit `NewRemoteKeySet(internal JWKS URL)` + `NewVerifier(public issuer, …)` — JWKS fetched in-cluster, `iss` validated against the public domain. No OIDC discovery (Keycloak discovery returns the *public* `jwks_uri`). |
| 3 | API key | **Removed** — Keycloak only. Machine clients use Keycloak service accounts. |
| 4 | Endpoints | Remove `register/login/refresh/logout`. Keep `GET /auth/me` (from Keycloak identity) + `/users` (behind Keycloak JWT). |
| 5 | Audience | Configure a Keycloak **audience mapper** so access tokens include the backend client in `aud`; app validates `KEYCLOAK_CLIENT_ID`. |
| 6 | Users schema | Keep `username` + `email` (filled from claims), add `keycloak_sub` (unique), **drop `password_hash`**. Existing demo rows: `keycloak_sub` NULL. |

## Architecture

Protected request flow:

```
Bearer token ─► Keycloak middleware ─► verifier.Verify (sig via cached JWKS, iss, exp, aud)
                      │                       │
                      │                       ▼
                      │              UserService.SyncFromKeycloak(sub,email,username)  ── get-or-create ─► users (int64 id)
                      │                       │
                      ▼                       ▼
              c.Set(ctxUserID, localUser.ID)  ───►  handler / DuplicateURLCheck / QuotaCheck (unchanged: read int64 user_id)
```

Downstream (`UserIDFrom`, link ownership, quota Redis keys) is **unchanged** — it still sees a local int64 `user_id`. Only the *source* of that id changes (Keycloak sub → JIT-mapped local id).

### Components

**pkg/keycloak** (new) — go-oidc wrapper behind a small interface for testability:
```go
type Identity struct{ Sub, Email, Username string }
type TokenVerifier interface { Verify(ctx context.Context, rawToken string) (*Identity, error) }
```
Production impl:
```go
keySet := oidc.NewRemoteKeySet(ctx, cfg.JWKSURL)          // internal svc DNS; lazy fetch + auto-refresh + cache
verifier := oidc.NewVerifier(cfg.Issuer, keySet, &oidc.Config{ClientID: cfg.ClientID}) // ClientID "" → SkipClientIDCheck
```
`Verify` parses the token, runs `verifier.Verify`, extracts `sub`/`email`/`preferred_username`. `NewRemoteKeySet` is **lazy** → app startup does not block on Keycloak (improvement over discovery-at-startup).

**internal/middleware/keycloak.go** (new, replaces `authn.go`/`api_key.go`/`jwt.go`) — holds `ctxUserID` + `UserIDFrom` (moved here, same `"user_id"` key). Flow: extract Bearer → `verifier.Verify` (401 on failure) → `users.SyncFromKeycloak` (500 on DB error) → `c.Set(ctxUserID, user.ID)` → next.

**UserService.SyncFromKeycloak(ctx, Identity) (*repository.User, error)** — `GetByKeycloakSub`; if found, update email/username if changed; else `Create`. **UserRepository** gains `GetByKeycloakSub`; `User` gains `KeycloakSub *string`, drops `PasswordHash`.

**Identity handler** — `auth_handler.go` stripped to `Me` only (returns the synced local user). `/users` list/get unchanged (now behind Keycloak mw).

### Data model — migration `000009_keycloak_auth`

up:
```sql
ALTER TABLE users ADD COLUMN keycloak_sub VARCHAR(36);
CREATE UNIQUE INDEX idx_users_keycloak_sub ON users (keycloak_sub);
ALTER TABLE users DROP COLUMN password_hash;
DROP TABLE IF EXISTS refresh_tokens;
```
down: recreate `refresh_tokens` (mirror 000005), re-add `password_hash`, drop index + `keycloak_sub`.

`username` + `email` stay `NOT NULL UNIQUE` (filled from `preferred_username` + `email` claims). Existing demo users keep their values with `keycloak_sub` NULL until they re-auth (they won't match a Keycloak sub, so effectively orphaned — acceptable for a template/dev DB).

### Config — `KeycloakConfig` (`KEYCLOAK_` prefix), replaces `AuthConfig`

| Var | Example | Purpose |
|-----|---------|---------|
| `KEYCLOAK_ISSUER` | `https://auth.cd.me/realms/<realm>` | Expected `iss` (public) |
| `KEYCLOAK_JWKS_URL` | `http://keycloak-keycloakx-http.keycloak.svc.cluster.local/realms/<realm>/protocol/openid-connect/certs` | In-cluster JWKS fetch |
| `KEYCLOAK_CLIENT_ID` | `url-shortener-backend` | Audience to validate (empty → skip aud check) |

## Removed vs Added/Changed

**Remove:** `internal/service/auth_service.go` (+test); `internal/handler/auth_handler.go` register/login/refresh/logout (strip to `Me`); `internal/repository/refresh_token_repository.go`; `pkg/token/*`; `internal/middleware/{api_key,authn,jwt}.go` (+ tests); bcrypt + golang-jwt direct usage; `AuthConfig`.

**Add:** `pkg/keycloak/verifier.go` (+test); `internal/middleware/keycloak.go` (+test); migration `000009`; `UserService.SyncFromKeycloak`; `UserRepository.GetByKeycloakSub`.

**Modify:** `user_repository.go` (User: +KeycloakSub, −PasswordHash; +GetByKeycloakSub); `user_service.go`; `auth_handler.go` (Me only); `router.go` (drop auth-write routes + API key; Keycloak mw on `/api`, `/users`, `/auth/me`); `cmd/server/main.go` (build verifier + mw; drop issuer/auth/refresh wiring); `configs/config.go`; `.env.example`; README + Swagger; `go.mod`.

> Since every `/api/links` create now carries a Keycloak user, the old "no user_id → skip" branches in `DuplicateURLCheck`/`QuotaCheck` are always-true; keep them as defensive guards (harmless) — no behavior change.

## Error Handling

- Missing/!Bearer → 401. Invalid/expired/wrong-iss/wrong-aud token → 401 (generic). JIT DB error → 500. JWKS unreachable → first verify fails 401 (cached keys keep working after first success); app still starts.
- Reuse `apperror` + `response` envelope. New errors map to existing 401/500 helpers.

## Testing Strategy

- **keycloak middleware** (mock `TokenVerifier` + mock `UserService`): valid → `user_id` set + next; verify-fail → 401; missing/!Bearer → 401; sync DB error → 500.
- **UserService.SyncFromKeycloak** (mock repo): new sub → Create; existing sub → returns existing (+updates changed email/username).
- **pkg/keycloak verifier** (focused): httptest JWKS server + RSA-signed token → Verify ok; wrong iss → fail; expired → fail; bad signature → fail.
- Link/quota/dedup tests **unchanged** (they already inject `user_id`); update any that referenced removed auth/api-key middleware.
- Gate: `make build` + `make test` green; `make swag`.

## New Dependencies

- `github.com/coreos/go-oidc/v3`
- (drop direct `golang-jwt/jwt/v5`, `golang.org/x/crypto/bcrypt` once `pkg/token`/auth removed — `go mod tidy`)

## Risks

- **Missing claims:** `email`/`preferred_username` must be present (NOT NULL columns). Require `email`+`profile` scopes on the backend client; a user without a verified email fails provisioning. Mitigation: ensure scopes, or relax columns to nullable (follow-up).
- **Audience mapper not configured:** access tokens won't carry the client in `aud`; either add the mapper (chosen) or set `CLIENT_ID=""`. Document clearly.
- **Per-request DB lookup** for sub→local id. Indexed unique; fine at current scale. Cache (in-mem/Redis) later if hot.
- **Access vs ID token:** clients must send the **access** token; go-oidc verifies any realm-signed JWT — aud mapper makes audience validation meaningful.
- **Orphaned demo users:** pre-existing `users` rows (null `keycloak_sub`) won't map to Keycloak; acceptable for dev/template.

## Success Criteria

- Valid Keycloak access token → create/list links, `/auth/me`, `/users` work; user JIT-provisioned once, reused after.
- Invalid/expired/foreign-aud token → 401. App starts even if Keycloak briefly unavailable.
- Link ownership + daily quota behave exactly as before (keyed on the JIT-mapped local id).
- No self-issued tokens, no `refresh_tokens` table, no password storage, no `X-API-Key` remaining.
- `make build` + `make test` green; README/Swagger reflect Keycloak-only auth.

## Open Questions

- Real **realm name** + **backend client id** — supplied at deploy via env (placeholders in `.env.example`).
- Should roles/authorization (Keycloak `realm_access.roles`) gate any endpoint now, or later? (Assumed later — authentication only this pass.)
- Keep orphaned demo users, or add a cleanup migration? (Assumed keep; dev-only.)

## Next Steps

`/plan` this spec. Suggested phases: (1) config + migration 000009 + go.mod; (2) `pkg/keycloak` verifier; (3) user repo/service JIT (`keycloak_sub`, `GetByKeycloakSub`, `SyncFromKeycloak`); (4) keycloak middleware + router/main rewire + remove old auth; (5) tests + README/Swagger.
