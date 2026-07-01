---
status: completed
created: 2026-07-01
slug: user-links-list
spec: ../reports/spec-260701-1613-user-links-list.md
---

# Plan: List a User's Links (API + UI)

Add `GET /api/links` (Keycloak-authenticated, owner-scoped, paginated, with per-link
`total_clicks`) and a "My links" list in the vanilla SPA. Reuses existing layering;
no migration.

**Spec:** [spec-260701-1613-user-links-list.md](../reports/spec-260701-1613-user-links-list.md)

## Principles

YAGNI / KISS / DRY. Mirror `handler → service → repository` layering, `apperror`,
uniform `response.Envelope`. Owner comes from the Keycloak middleware's context
`user_id`. `textContent`-only rendering on the frontend.

## Phases

| # | Phase | Status | Depends on |
|---|-------|--------|-----------|
| 1 | [Backend API + tests](phase-01-backend-list-endpoint.md) | pending | — |
| 2 | [Frontend "My links" UI](phase-02-frontend-links-list.md) | pending | 1 |
| 3 | [Docs (README + Swagger)](phase-03-docs.md) | pending | 1,2 |

## Key Facts

- Keycloak middleware already sets int64 `user_id` in context (`appmw.UserIDFrom`).
- `links.user_id` (owner), `clicks.link_id` (indexed) exist. No schema change.
- Route added beside the existing `POST /api/links` under the Keycloak group.

## Definition of Done

- Signed-in user sees their links newest-first with click counts; prev/next paginates;
  a newly created link appears after create; other users' / unowned links never shown.
- `limit` clamped 1–100 (default 20), `offset` ≥ 0.
- `make build` + `make test` green; README + Swagger updated.
