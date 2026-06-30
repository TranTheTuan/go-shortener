# Phase 05 — Tests & Docs

**Context:** [plan.md](plan.md) · [spec](../reports/spec-260630-0914-keycloak-auth-refactor.md)

## Overview
- **Priority:** High (DoD gate)
- **Status:** pending
- New tests for verifier + middleware + JIT; prune obsolete auth tests; README + Swagger.

## Related Code Files
- **Create:** `internal/middleware/keycloak_test.go`, `internal/service/user_service_test.go` (or extend), `pkg/keycloak/verifier_test.go`
- **Modify:** `internal/service/mocks_test.go` (drop refresh mock; add `GetByKeycloakSub`/`Update` to user mock), `README.md`, Swagger
- **Delete (with their code in Phase 04):** `auth_service_test.go`, `pkg/token/token_test.go`, `internal/middleware/{api_key_test,authn_test,jwt_test}.go`

## Implementation Steps

1. **`keycloak_test.go`** — mock `keycloak.TokenVerifier` + mock `userSyncer`:
   - valid token → `Verify` returns Identity → `SyncFromKeycloak` → `user_id` set + next runs.
   - missing/!Bearer → 401; verify error → 401; sync DB error → 500.

2. **`user_service_test.go`** — `SyncFromKeycloak` with mock `UserRepository`:
   - unknown sub → `Create` called, returns new user.
   - known sub → returns existing; email/username change → `Update` called.
   - create race (`ErrConflict`) → re-fetch returns existing.

3. **`pkg/keycloak/verifier_test.go`** — generate RSA key; serve JWKS via `httptest`; mint RS256 tokens:
   - valid (iss+aud+exp ok) → Identity with sub/email/username.
   - wrong iss → error; expired → error; tampered sig → error.
   (Use `github.com/golang-jwt/jwt/v5` in the *test* to mint tokens even though prod no longer signs — or `gopkg.in/square/go-jose`. Keep token-minting confined to the test.)

4. **mocks_test.go** — remove `mockRefreshRepo`; add `GetByKeycloakSub` + `Update` to `mockUserRepo`. Ensure quota/link/dedup tests still compile (they inject `user_id` directly — unaffected).

5. **README** — replace the Authentication section: obtain token from Keycloak, send `Authorization: Bearer`, JIT provisioning, no register/login, no API key. Update API table (remove `/auth/register|login|refresh|logout`; `/api/links` now "Keycloak JWT"). Update env table (`KEYCLOAK_*`, remove `AUTH_*` and `SHORTENER_API_KEYS` note). Add a short note on the required Keycloak audience mapper + `email`/`profile` scopes.

6. **Swagger** — `make swag`. Remove `ApiKeyAuth` security def + the deleted auth routes; ensure `BearerAuth` (Keycloak) documents protected routes incl. 401.

7. **Gate:** `make build` && `make test` (all green) && `gofmt`/`make lint`. Fix per recommendations — no weakened assertions.

## Todo
- [ ] keycloak middleware tests
- [ ] `SyncFromKeycloak` tests (create/existing/race/refresh)
- [ ] verifier test (httptest JWKS + RS256)
- [ ] prune obsolete auth tests; fix `mocks_test.go`
- [ ] README + Swagger updates
- [ ] `make build` + `make test` green

## Success Criteria
- All tests pass; coverage of verify/sync/middleware paths.
- Docs reflect Keycloak-only auth (no register/login/refresh/logout, no API key); env documents `KEYCLOAK_*`.

## Next
Plan complete → mark phases done; optional `/plan archive`. Roles/authorization remain future work.
