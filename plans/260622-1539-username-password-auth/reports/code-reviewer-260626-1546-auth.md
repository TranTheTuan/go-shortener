# Code Review — Username/Password Auth

Date: 2026-06-26
Reviewer: code-reviewer
Scope: JWT auth feature (register/login/refresh/logout + middleware + migrations)
Build: green. Tests: pass (`go build ./...`, `go test ./...`).

## Verdict: GO (conditional) — no critical merge blockers in the auth code itself.
Two HIGH items below should be fixed before this is exposed to untrusted traffic;
neither breaks the build or the existing API-key link flow.

---

## Critical
None.

## High

### H1 — `/users` and `/users/:id` are PUBLIC and leak all usernames + emails
File: internal/router/router.go:57-59; internal/handler/user_handler.go:55-62
The user list/get endpoints have NO auth and `repository.User` serializes
`username`, `email`, `name`, timestamps (id field json-exposed). `GET /users`
returns the full account roster. This directly defeats the "no user
enumeration" design intent — login is careful to return a generic 401, but any
client can `GET /users` and dump every username/email.
Recommendation: put `/users` behind JWT (or remove the list endpoint entirely;
YAGNI — `/auth/me` covers self-lookup). At minimum drop `List`. If kept, scope
to admin. This is the single most impactful gap.

### H2 — Login timing side-channel enables email enumeration
File: internal/service/auth_service.go:128-139
On unknown email, `Login` returns `errInvalidCredentials` immediately without
running bcrypt; on known email it runs a ~cost-12 bcrypt compare (tens of ms).
The response-time delta lets an attacker enumerate registered emails despite the
generic message. Combined with H1 the value is reduced, but should be fixed
independently.
Recommendation: always perform a bcrypt comparison against a fixed dummy hash
when the user is not found (constant-time-ish), e.g. compare against a
package-level precomputed `bcrypt` hash before returning. Keep the generic 401.

## Medium

### M1 — Refresh rotation has no token-reuse detection (replay window)
File: internal/service/auth_service.go:145-163; refresh_token_repository.go:65-71
`Revoke` is an unconditional `UPDATE ... WHERE id=?`. Two concurrent `Refresh`
calls with the same (valid) token both pass the `RevokedAt==nil && !expired`
check, both "revoke" (idempotent), and BOTH receive new token pairs — classic
refresh-token race. Also, replaying an already-rotated token just yields 401
with no breach signal (standard practice is to revoke the whole token family on
reuse).
Recommendation (pick per threat model):
- Make rotation atomic + single-winner: `UPDATE refresh_tokens SET revoked_at=now()
  WHERE id=? AND revoked_at IS NULL` and check `RowsAffected==1`; if 0, treat as
  reuse → return 401 (and optionally revoke all of the user's tokens).
- This closes the concurrency race and adds reuse detection in one change.
Severity medium (single-device flows rarely hit it), but it is the only real
correctness/concurrency gap.

### M2 — Migration 000004 adds NOT NULL columns without defaults
File: migrations/000004_add_user_credentials.up.sql:4-6
`ADD COLUMN username VARCHAR(255) NOT NULL` (and password_hash) fails on any
non-empty `users` table. Comment says "assumed empty" — fine for greenfield, but
fragile and the migration is not wrapped in an explicit transaction, so a
partial failure can leave `username` added but the unique index missing.
Recommendation: document the empty-table precondition in the plan/changelog, or
guard with `... USING`/backfill if prod data may exist. Low effort: add an
explicit `BEGIN;`/`COMMIT;` so the three statements are atomic.

### M3 — No length cap on password / refresh-token inputs
File: internal/service/auth_service.go:100-104; handler bind
bcrypt silently truncates at 72 bytes (accepted tradeoff) but there is no upper
bound on `Password`, allowing a client to POST a multi-MB password that the
server bcrypts (CPU DoS). Refresh/logout bodies are likewise unbounded.
Recommendation: reject passwords > 72 bytes with 400 (also avoids the silent
bcrypt-truncation surprise), and/or set an Echo body-limit middleware.

## Low

### L1 — Empty JWT secret accepted in development
File: configs/config.go:111-113
`validate` rejects the default/empty secret only when `Env != "development"`. An
empty `AUTH_JWT_SECRET` in dev produces a zero-byte HMAC key (tokens signable by
anyone). Harmless in practice, but consider rejecting empty unconditionally.

### L2 — `Logout` requires no proof of ownership
File: internal/handler/auth_handler.go:133-143
Anyone holding a raw refresh token can revoke it (no access token required).
This is acceptable (the token is the credential) and idempotent, but worth a
note; a stolen-but-unused token could be force-revoked by an attacker to cause a
DoS on one session. Low.

### L3 — `expires_in` int seconds via `Seconds()` truncation
File: internal/service/auth_service.go:210
`int(AccessTTL().Seconds())` truncates sub-second TTLs to 0; only matters for
exotic configs. Fine as-is.

---

## Positive observations
- HS256 alg pinned two ways: method type assertion + `jwt.WithValidMethods`
  (token.go:69-73). Algorithm-confusion (alg=none / RS->HS) correctly blocked.
- Refresh tokens: 32 bytes crypto/rand, only sha256 hex persisted; raw never
  written. Verified by test (auth_service_test.go:116-121) and schema.
- `password_hash` `json:"-"` — never serialized. Confirmed on the struct.
- Generic `errInvalidCredentials` reused for unknown-email / wrong-password /
  unknown-refresh — consistent, no detail leak in messages.
- Rotation revokes presented token before issuing new pair; expiry checked in
  UTC; revoked/expired both rejected. Covered by tests.
- Error mapping clean: ErrConflict->409, ErrNotFound->401 (auth) / 404 (user),
  validation->400, internal cause wrapped + logged but not exposed.
- GORM `TranslateError: true` is set, so `ErrDuplicatedKey` -> `ErrConflict`
  mapping in user repo actually fires (verified pkg/database/postgres.go:25).
- Existing X-API-Key flow on `/api/links` is UNCHANGED and still enforced
  (api_key.go untouched; router still wraps `/api` with `APIKey`). No regression.
- Fail-closed JWT secret outside development; bcrypt cost 12 default; sensible.
- Layering respected: handler->service->repo, apperror + response envelope used
  uniformly.

## Metrics
- Files reviewed: 18 (6 new code, 6 modified, 4 migrations, 4 tests + mocks)
- Auth code LOC: ~600
- Lint/build: clean. Tests: pass.
- Test coverage: token issue/parse/tamper/expiry/wrong-secret; service
  register/login/refresh-rotate/revoke/expire/logout; middleware accept/reject.
  Solid unit coverage. Gaps: no test for concurrent refresh (M1), no test for
  password length cap (M3).

## Unresolved questions
1. Are `/users` list/get intended to be public? If yes, is exposing all
   emails/usernames an accepted product decision? (drives H1 severity)
2. Is single-use refresh-token reuse detection in scope, or is silent 401 on a
   rotated token acceptable for this release? (drives M1)
3. Is the `users` table guaranteed empty at 000004 deploy time in every
   environment (staging/prod), or only locally? (drives M2)
