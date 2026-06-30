---
status: completed
created: 2026-06-30
completed: 2026-06-30
slug: keycloak-auth-refactor
spec: ../reports/spec-260630-0914-keycloak-auth-refactor.md
---

# Plan: Refactor Authentication to Keycloak

Replace in-code auth (register/login/refresh/logout + self-issued JWT) with validation
of Keycloak access tokens. Service becomes an OIDC resource server. Identity is
JIT-provisioned from token claims (`keycloak_sub` → local int64 `user_id`), so link
ownership + subscription/quota are unchanged downstream.

**Spec:** [spec-260630-0914-keycloak-auth-refactor.md](../reports/spec-260630-0914-keycloak-auth-refactor.md)

## Principles

YAGNI / KISS / DRY. Keep existing layering (handler→service→repository), `apperror`,
`response` envelope. Downstream `UserIDFrom`/ownership/quota stay on int64 — only the
source of the id changes. Files < 200 LOC.

## Phases

| # | Phase | Status | Depends on |
|---|-------|--------|-----------|
| 1 | [Config, migration & deps](phase-01-config-migration-deps.md) | ✅ done | — |
| 2 | [pkg/keycloak verifier](phase-02-keycloak-verifier.md) | ✅ done | 1 |
| 3 | [User repo/service JIT provisioning](phase-03-user-jit-provisioning.md) | ✅ done | 1 |
| 4 | [Middleware, router, wiring & removals](phase-04-middleware-router-removals.md) | ✅ done | 2,3 |
| 5 | [Tests & docs](phase-05-tests-and-docs.md) | ✅ done | 4 |

## Outcome (260630)

Implemented + tested (34 tests pass; build/vet/gofmt green; Swagger regenerated).
Code review: GO, 0 critical / 0 high. Post-review fixes applied:
- **Scout/empty-claims:** `SyncFromKeycloak` rejects empty `sub`/`email`/`preferred_username`
  (prevents NOT NULL empty-string unique-index collisions). +test.
- **M2:** removed stale `@Security ApiKeyAuth` + `X-API-Key` params from link handler Swagger; regenerated.
- **M1:** deleted dead `AuthConfig` struct + `ShortenerConfig.APIKeys`.
- **M3:** refreshed stale "API-key/unowned" comments.
Deferred to docs-manager: **M4** — `docs/*` still describe old auth (synced in finalize).
Ops note: migration 000009 is destructive (drops `password_hash` + `refresh_tokens`); down restores `password_hash` as nullable. Reports in `reports/`.

## Key Dependencies

- `github.com/coreos/go-oidc/v3` (add) — JWKS + token verification
- Drop direct `golang-jwt/jwt/v5` + `golang.org/x/crypto/bcrypt` after `pkg/token`/auth removed (`go mod tidy`)
- Migration `000009_keycloak_auth` (add `keycloak_sub`, drop `password_hash`, drop `refresh_tokens`)

## Definition of Done

- Valid Keycloak access token → links/quota/`/auth/me`/`/users` work; user JIT-provisioned once, reused after.
- Invalid/expired/foreign-aud token → 401; app starts even if Keycloak briefly down (lazy JWKS).
- No self-issued tokens, no `refresh_tokens`, no password storage, no `X-API-Key`.
- Link ownership + daily quota behave exactly as before (JIT-mapped local id).
- `make build` + `make test` green; README + Swagger reflect Keycloak-only auth.
