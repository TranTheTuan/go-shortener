# Code Review — Keycloak Auth Refactor (OIDC resource server)

Date: 2026-06-30 | Reviewer: code-reviewer | Branch: master (uncommitted working tree)
Verdict: **GO with required follow-ups** (no critical/high blockers; cluster of medium cleanup items).

## Scope
- NEW: pkg/keycloak/verifier.go(+test), internal/middleware/keycloak.go(+test), migration 000009, internal/service/user_service_test.go
- MOD: user_service.go, user_repository.go, auth_handler.go, router.go, main.go, config.go, README/swagger/.env.example
- DEL: auth_service.go(+test), pkg/token(+test), refresh_token_repository.go, middleware/{api_key,authn,jwt}(+tests)
- Build green, tests pass, gofmt clean (per brief; not re-run for code edits — read-only review).

## Overall Assessment
Design intent met. Token validation is correct and fail-closed. JIT get-or-create is race-safe and matches existing apperror/repository conventions. go-oidc usage is idiomatic. No auth-bypass found. Remaining issues are leftover dead code + heavy docs drift, plus a few medium correctness nits. Nothing blocks merge security-wise.

---

## Critical
None.

## High
None.

## Medium

### M1 — Dead/misleading config left behind: `AuthConfig` + `ShortenerConfig.APIKeys`
- configs/config.go:56-67 (`AuthConfig`/`JWTSecret`/TTLs/bcrypt), :73-75 (`APIKeys`/`API_KEYS`)
- Issue: auth is fully Keycloak now; `AuthConfig` is not referenced anywhere (it is not even a field of `Config`), and `APIKeys` is unused after api_key mw deletion. Dead surface invites accidental reuse and contradicts the new model. `JWTSecret` env default `dev-insecure-change-me` still parsed silently.
- Recommend: delete `AuthConfig` struct entirely; remove `APIKeys` field (and `API_KEYS` from .env.example/deploy docs). Keep only `KeycloakConfig`.

### M2 — Stale handler comments + Swagger `ApiKeyAuth` referencing removed auth
- internal/handler/link_handler.go:46 (`@Security ApiKeyAuth`), :49 (`@Param X-API-Key header ... true`), :64-65 comment ("API-key callers have no user and create unowned links")
- internal/handler/auth_handler.go: fine.
- router.go:36-37 Deps doc-comment still says "JWT issuer, API keys".
- Issue: design intent says every /api/links create is now owned + quota-applies (no unowned path). Comments and Swagger advertise an API-key path that no longer exists; `@Param X-API-Key ... true` makes the documented contract wrong (marks a removed header as required). Swagger `swagger.json/yaml/docs.go` will carry this through.
- Recommend: drop `@Security ApiKeyAuth` + the `X-API-Key` `@Param`; regenerate swagger; fix the "API-key callers" comment to "all callers are authenticated (Keycloak); links are always owned"; fix router Deps comment.

### M3 — Now-dead `!ok` guards on /api chain (always-true invariant undocumented)
- internal/middleware/quota_check.go:32-35, internal/middleware/duplicate_url_check.go:30, internal/handler/link_handler.go:67
- Issue: on /api the Keycloak mw runs first and 401s before these execute, so `UserIDFrom` always returns ok=true downstream. The `if !ok { return next(c) }` (pass-through-without-quota) and the `owner=nil` branch are unreachable on the only route they guard. Harmless today, but they encode the *old* "anonymous write allowed" model — a future route reusing QuotaCheck without auth would silently bypass quota.
- Recommend: keep guards (defensive) but update comments — they currently justify the branch with "API-key/unowned" semantics that no longer exist. Either document "defensive only; all current routes are authenticated" or make link Create treat missing user as 500 (invariant violation) rather than silently creating an unowned link.

### M4 — Docs not updated (large drift)
- docs/codebase-summary.md, system-architecture.md, code-standards.md, project-overview-pdr.md, project-roadmap.md, deployment-guide.md
- Issue: still describe AuthService, RefreshTokenRepository, X-API-Key middleware, AUTH_JWT_SECRET, password_hash. README/.env.example updated but `docs/` (the canonical set per documentation-management.md) is stale. deployment-guide.md:207-208,263-268 still instruct setting `SHORTENER_API_KEYS` + `AUTH_JWT_SECRET` in prod.
- Recommend: delegate docs-manager to sync docs/* to the resource-server model (Keycloak verifier, JIT provisioning, removed components). Out of code scope but required by project rules before "done".

### M5 — `repo.Update` uses GORM `Save` (full-row write); UpdatedAt not stamped, name/zero risk
- internal/repository/user_repository.go:117-125 (Save), internal/service/user_service.go:50-51
- Issue: claim-refresh updates only Email/Username on the struct read from DB, then `Save` writes the whole row. `UpdatedAt` is not set by the service (relies on GORM autoupdate via `Save` — works, but `CreatedAt` on create is set manually while update timestamps are implicit; inconsistent). Low data risk because the struct is the freshly-fetched row. Mostly a consistency nit.
- Recommend: confirm GORM auto-sets UpdatedAt on Save (it does for the `UpdatedAt` field); optionally use `Select("email","username","updated_at")` to scope the write and avoid clobbering columns added later. Not blocking.

## Low

### L1 — Verifier validates access token via `oidc.IDTokenVerifier`
- pkg/keycloak/verifier.go:44,50
- go-oidc's IDTokenVerifier is fine for Keycloak access tokens (they are JWTs with iss/exp). It does NOT enforce `at_hash`/`nonce` here (only relevant to ID tokens / it skips when absent). Confirmed v3.19.0 defaults `SupportedSigningAlgs`→[RS256] only (verify.go:309-310), so alg-confusion (none/HS256) is blocked. No action; documented for the record.

### L2 — No DB timeout/deadline on per-request sync
- internal/middleware/keycloak.go:44, user_service.go
- Every authenticated request does a DB round-trip (GetByKeycloakSub). If Postgres is slow/down, requests block up to the Echo ReadTimeout / driver default; sync errors map to 500 (correct fail-closed). Acceptable, but consider a short context timeout around the sync to avoid pile-ups when DB is degraded. Caching sub→id (e.g. small TTL LRU) is a future optimization, not needed now (YAGNI).

### L3 — Down-migration not a true mirror (acceptable, flag for awareness)
- migrations/000009_keycloak_auth.down.sql:2 vs 000004 up
- 000004 created `password_hash VARCHAR(255) NOT NULL`; the down restores it as **nullable**. This is intentional and safer (can't backfill dropped hashes), but it means down≠original schema. Data in password_hash and the entire refresh_tokens table are permanently destroyed by the up migration — irreversible. This matches design intent; just ensure ops are aware before running in prod (take a backup).
- Up migration correctness: OK. Nullable unique index on keycloak_sub is correct — Postgres permits multiple NULLs, so legacy rows (nil sub) don't collide; new Keycloak users get a unique non-null sub.

### L4 — `context.Background()` for RemoteKeySet — correct
- main.go:88, verifier.go:38-39
- Background context is the right lifetime: it backs lazy JWKS fetch + background key rotation for the process lifetime; it is never cancelled (intended). No leak — the keyset's goroutine lives as long as the server. Per-request `Verify` correctly uses `c.Request().Context()` (verifier.go:49, keycloak.go:39). Good.

---

## Edge cases scouted

- Partial claims (missing email/username): User.Email/Username are `not null` (repo tags + 000004 NOT NULL). If a Keycloak token lacks `email` or `preferred_username`, SyncFromKeycloak Create inserts empty strings — **not** a NULL violation (Go zero value is "", which satisfies NOT NULL), but empty username/email could collide on the `uniqueIndex` for a *second* such user → ErrConflict → re-fetch returns the *wrong* user (first empty-claim user). Low likelihood (Keycloak normally issues email+preferred_username; scopes must be configured), but worth a guard: reject sync when Sub present but username/email empty, or relax uniqueness. **Severity: medium-if-scopes-misconfigured.** Recommend validating non-empty Sub at minimum in SyncFromKeycloak (currently a token with empty sub would create/lookup a user keyed on "").
- Empty `sub`: verifier returns Identity{Sub:""}; SyncFromKeycloak would GetByKeycloakSub("") → create with KeycloakSub=&"". A token with no sub should never validate in practice, but add an explicit empty-sub rejection in Verify or middleware for defense-in-depth.
- JIT race: correctly handled (unique index + ErrConflict→re-fetch). GORM TranslateError:true confirmed (pkg/database/postgres.go:25), so ErrDuplicatedKey→ErrConflict mapping is live. Good.
- aud as array vs string: go-oidc parses `aud` to []string and `contains` checks membership — handles both Keycloak shapes. OK.
- DB down during auth: sync error → apperror.Internal → 500, handler never runs. Fail-closed. Good.

## Positive observations
- Clean split-horizon: in-cluster JWKS fetch + public issuer validation, lazy fetch so app boots if Keycloak down. Matches intent exactly.
- Interface-based TokenVerifier + userSyncer → middleware fully unit-tested without Keycloak/DB. Good test design (verifier_test mints real RS256 via go-jose; covers wrong-iss/expired/garbage/aud).
- Race-safe get-or-create with correct ErrNotFound vs ErrConflict vs real-error mapping.
- Fail-closed config.validate() requires issuer+JWKS outside development.
- Consistent apperror/response envelope usage; layering preserved (middleware→service, no OIDC import leak into service via local SyncInput).

## Recommended actions (priority order)
1. (M2) Remove `@Security ApiKeyAuth` + `X-API-Key @Param` from link_handler; regenerate swagger.
2. (M1) Delete `AuthConfig` and `ShortenerConfig.APIKeys` + purge API_KEYS/AUTH_JWT_SECRET from .env.example & deploy docs.
3. (M4) Sync docs/* to Keycloak model (docs-manager).
4. (scout) Add empty-Sub / empty-claim guard in SyncFromKeycloak or Verify.
5. (M3) Update now-misleading "API-key/unowned" comments in quota_check/duplicate_url_check/link_handler.
6. (L2) Consider a context timeout around per-request sync.

## Metrics
- New code type-safe (interfaces, no `interface{}` abuse). Type coverage: full.
- Test coverage: verifier (sig/iss/exp/aud/garbage), middleware (valid+4 reject paths), service (create/existing/refresh/distinct). Missing: explicit test for ErrConflict re-fetch race path and empty-claim handling.
- Linting: none re-run; gofmt clean per brief. Leftover dead config = the main "lint" debt.

## Unresolved questions
1. Are Keycloak client scopes guaranteed to emit `email` + `preferred_username` on the access token (not just ID token)? If not, M-scout empty-claim guard becomes higher priority.
2. Is `KEYCLOAK_CLIENT_ID` intended to be set in prod (aud enforced) or intentionally empty (SkipClientIDCheck)? .env.example sets it; main wiring passes it through — confirm prod posture (enforcing aud is recommended).
3. Should leftover `AuthConfig`/`APIKeys` removal be in THIS PR or a follow-up cleanup PR? (Recommend this PR — they are part of the auth surface being replaced.)
4. Will docs/* sync happen before merge or as a tracked follow-up? Project rules require docs update after feature implementation.
