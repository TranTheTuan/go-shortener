# Phase 05 — Tests & Docs

**Context:** [plan.md](plan.md) · [spec](../reports/spec-260622-1539-username-password-auth.md)

## Overview
- **Priority:** High (DoD gate)
- **Status:** pending
- Unit tests (service/middleware/token) + README + Swagger. No fake/skipped tests.

## Related Code Files
- **Create:** `internal/service/auth_service_test.go`, `internal/middleware/jwt_test.go`,
  `pkg/token/token_test.go`
- **Modify:** `internal/service/mocks_test.go` (add mock repos), `README.md`,
  Swagger (`make swag` / regen under `docs/swagger/`)

## Implementation Steps

1. **`mocks_test.go`** — add in-memory/mock `UserRepository` (with `GetByEmail`,
   `GetByUsername`) and `RefreshTokenRepository`. Follow existing mock style in the file.

2. **`auth_service_test.go`** — inject mocks + fixed `now` + `token.NewIssuer("test", ttl)`:
   - Register success → user persisted, password hashed (not plaintext), `password_hash` set.
   - Register duplicate username → 409; duplicate email → 409.
   - Login success (email) → non-empty access + refresh; refresh row stored hashed.
   - Login unknown email → 401; wrong password → 401 (same message).
   - Refresh success → new pair, old token row revoked (rotation).
   - Refresh with revoked / expired / unknown token → 401.
   - Logout → token revoked; logout unknown token → nil (idempotent).

3. **`jwt_test.go`** — build issuer, table tests: valid token → `user_id` set, next called;
   missing header → 401; `Bearer ` empty → 401; malformed → 401; expired → 401.
   Use `httptest` + Echo context like existing `api_key_test.go`.

4. **`token_test.go`** — Issue→Parse roundtrip returns same `UserID`; tampered token → err;
   expired (negative TTL) → err; token signed with different secret → err.

5. **README.md** — update API table: add `/auth/*` rows; note `POST /users` removed;
   add `AUTH_*` env vars to the config table; short auth usage curl block (register/login/refresh).

6. **Swagger** — regenerate (`make swag` or `swag init`); confirm `/auth/*` documented.
   Add a Bearer `securityDefinitions` (`@securityDefinitions.apikey BearerAuth` / `Authorization` header) in `main.go` annotations.

7. **Gate:** `make build` && `make test` (all green) && lint. Fix failures per recommendations — do NOT weaken assertions.

## Todo
- [ ] Mocks for user + refresh repos
- [ ] `auth_service_test.go` (all cases above)
- [ ] `jwt_test.go`
- [ ] `token_test.go`
- [ ] README updates (API table, env vars, usage)
- [ ] Swagger regen + Bearer security def
- [ ] `make build` + `make test` green

## Success Criteria
- All tests pass; coverage of auth happy + error paths.
- Docs reflect new endpoints, removed `POST /users`, new env vars.

## Next
Plan complete → mark phases done, optional `/plan archive`.
