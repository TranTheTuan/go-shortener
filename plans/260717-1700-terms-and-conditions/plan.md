---
slug: terms-and-conditions-gate
status: pending
created: 2026-07-17
---

# Plan: Terms & Conditions Acceptance Gate

Implement versioned T&C gate blocking app access until user accepts. Required due to complex billing rules (no downgrade, no refunds, credit-based interval changes) that need disclosure.

## Phases

| # | Phase | Status | File |
|---|-------|--------|------|
| 1 | DB + Config | pending | [phase-01-db-config.md](phase-01-db-config.md) |
| 2 | Backend (Repo + Service + Handler) | pending | [phase-02-backend.md](phase-02-backend.md) |
| 3 | Terms HTML Content | pending | [phase-03-terms-content.md](phase-03-terms-content.md) |
| 4 | Frontend Gate + Modal | pending | [phase-04-frontend-gate.md](phase-04-frontend-gate.md) |

## Critical Files

- `migrations/000014_add_terms_fields.{up,down}.sql` — new columns on `users`
- `configs/config.go` — `TERMS_VERSION` env var
- `internal/repository/user_repository.go` — `UpdateTermsAccepted` method
- `internal/service/user_service.go` — `AcceptTerms` method
- `internal/handler/auth_handler.go` — `AcceptTerms` handler
- `internal/router/router.go` — route wiring + terms serve
- `web/terms/v1.html` — T&C content
- `web/static/app.js` — gate modal logic
- `web/embed.go` — ensure `terms/` is embedded

## Key Design

- **Storage**: DB (`users.terms_accepted_at`, `users.terms_version`) + localStorage cache
- **Versioning**: Env var `TERMS_VERSION` (default `"1.0"`); bump to force re-accept
- **Gate**: Modal between Keycloak auth and app shell; checkbox + Accept button
- **Validation**: Server checks version matches `Config.Terms.CurrentVersion`
- **Content**: Static HTML `/web/terms/v1.html` embedded in binary

## Dependencies

1 → 2 → 3 → 4 (sequential; each depends on prior)
