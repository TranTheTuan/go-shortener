---
status: pending
created: 2026-06-22
slug: username-password-auth
spec: ../reports/spec-260622-1539-username-password-auth.md
---

# Plan: Username/Password Authentication (JWT access + refresh)

Add register/login auth with username + email + password. JWT access (15m) +
refresh (7d, DB-stored, hashed, rotated). Login by **email only**. JWT middleware
added alongside the existing static API key (which stays unchanged on `/api/links`).

**Spec:** [spec-260622-1539-username-password-auth.md](../reports/spec-260622-1539-username-password-auth.md)

## Principles

YAGNI / KISS / DRY. Mirror existing `handler → service → repository` layering,
uniform `response.Envelope`, `apperror` typed errors. Files < 200 LOC.
Do NOT build user-owned API keys / rate-limiting now (future).

## Phases

| # | Phase | Status | Depends on |
|---|-------|--------|-----------|
| 1 | [Config, deps & migrations](phase-01-config-deps-migrations.md) | pending | — |
| 2 | [Repository layer](phase-02-repository-layer.md) | pending | 1 |
| 3 | [pkg/token + AuthService](phase-03-token-and-auth-service.md) | pending | 2 |
| 4 | [Middleware, handlers & wiring](phase-04-middleware-handlers-wiring.md) | pending | 3 |
| 5 | [Tests & docs](phase-05-tests-and-docs.md) | pending | 4 |

## Key Dependencies

- `github.com/golang-jwt/jwt/v5` — JWT sign/verify
- `golang.org/x/crypto/bcrypt` — password hashing
- Existing: Echo v4, GORM, golang-migrate

## Definition of Done

- Register → login (email) → `GET /auth/me` (Bearer) → refresh → logout works E2E.
- `make build` + `make test` green; no compile/lint errors.
- Static API key on `/api/links` unchanged & still enforced.
- `password_hash` never serialized in any response.
- README + Swagger updated.
