---
status: pending
created: 2026-06-17
slug: url-shortener
---

# URL Shortener — Implementation Plan

Build a URL shortener on top of the existing Go (Echo + GORM + PostgreSQL) layered template.

## Spec Decisions (locked)

- **Scope:** Shorten + Redirect (core) · Expiration/TTL · Click analytics
- **Code gen:** random base62, 7 chars, retry on collision
- **Auth:** API key, static list from env (`X-API-Key` header)
- **Analytics:** detailed `clicks` table (time, referrer, IP, user-agent)
- **Redirect:** HTTP 302; expired link → 410 Gone; unknown code → 404
- No custom alias (deferred, YAGNI)

## Architecture

```
POST /api/links   →[api_key mw]→ link_handler  → link_service.Create   → shortcode.Generate + link_repo
GET  /api/links/:code/stats →[api_key mw]→ link_handler → analytics_service.Stats → click_repo + link_repo
GET  /:code        (public)  → redirect_handler → link_service.Resolve → link_repo
                                                   └─ go analytics_service.Record → click_repo (async)
GET  /healthz      (public)  → existing
```

Reuses existing `apperror`, `response`, `database`, config patterns. Adds `apperror.Gone` (410).

## Phases

| # | Phase | Status | Depends |
|---|-------|--------|---------|
| 01 | [Config + DB migrations](phase-01-config-and-migrations.md) | pending | — |
| 02 | [Repository layer](phase-02-repository-layer.md) | pending | 01 |
| 03 | [Service layer + shortcode pkg](phase-03-service-and-shortcode.md) | pending | 02 |
| 04 | [Handlers + API-key middleware + wiring](phase-04-handlers-middleware-wiring.md) | pending | 03 |
| 05 | [Tests](phase-05-tests.md) | pending | 04 |

## Key Files

**Create:** `pkg/shortcode/shortcode.go`, `internal/middleware/api_key.go`,
`internal/repository/link_repository.go`, `internal/repository/click_repository.go`,
`internal/service/link_service.go`, `internal/service/analytics_service.go`,
`internal/handler/link_handler.go`, `internal/handler/redirect_handler.go`,
`migrations/000002_*`, `migrations/000003_*`

**Modify:** `configs/config.go`, `pkg/apperror/apperror.go`,
`internal/router/router.go`, `cmd/server/main.go`, `.env.example`, `README.md`

## Success Criteria

- `make build` + `make test` pass
- create → redirect → stats flow works end-to-end (manual curl)
- expired link returns 410; missing returns 404; missing/invalid API key returns 401
- each file ≤ 200 LOC

## Conventions

- Module path stays `github.com/TranTheTuan/YOUR-REPO-NAME` (no rename unless asked)
- Run `go build ./...` after each phase to catch compile errors
