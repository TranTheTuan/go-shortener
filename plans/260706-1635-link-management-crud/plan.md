---
title: "Link Management (CRUD)"
description: "Owner-only Delete, Enable/Disable, and Edit-expiry for short links."
status: complete
priority: P1
effort: ~6h
branch: master
tags: [backend, links, crud, cache, frontend]
created: 2026-07-06
completed: 2026-07-06
---

# Link Management (CRUD) — Implementation Plan

Fill the biggest UX gap: users can create + list links but can't remove or
manage them. Add owner-only **Delete** (hard, cascade clicks), **Enable/Disable**
(reversible, keeps analytics), and **Edit expiry**. REST via the 4 basic methods
only (GET/POST/PUT/DELETE).

## Architecture (one line)
`PUT/DELETE /api/links/:code → owner check → mutate row → evict Redis link-cache (+ dedup on delete) → redirect Resolve honors is_active (410 when off)`

## Key decisions (agreed)
- **Delete = hard** (row removed, `clicks` cascade via existing FK, code freed). Disable is the "keep data but off" path.
- **Disable** → new `links.is_active` column; redirect returns **410 Gone** (same as expired); inactive links are **not cached**.
- **PUT** replaces the full mutable state: body `{expires_at: RFC3339|null, is_active: bool}` — both fields sent (no null-vs-absent ambiguity).
- Identify by **`:code`** (short_code, already unique + exposed in list).
- Non-owner / unowned (`user_id=nil`) / missing → **404** (hide existence).
- **Cache invalidation is mandatory**: `Resolve` returns a cache hit without re-checking expiry/active, so every mutation MUST evict the cache. Cache repo needs a new `Delete`.
- **Status filter on list**: since `is_active` exists, `GET /api/links` gains `?status=active|disabled|expired` (empty = all). Filter applies to rows AND total count. Buckets mutually exclusive, matching the badge precedence (disabled > expired > active).

## Context Links
- Design discussion: this session (brainstorm-features → A: Link management).
- Touch points: `internal/repository/link_repository.go`, `link_cache_repository.go`,
  `internal/service/link_service.go` (`Resolve`, `Create`, dedup), `internal/handler/link_handler.go`,
  `internal/router/router.go` (links group), `web/static/app.js` (`wireLinks`), `web/index.html` (links table).

## Phases
| # | Phase | Depends on | Status |
|---|-------|-----------|--------|
| 01 | [Schema & model (is_active)](phase-01-schema-and-model.md) | – | ✅ done |
| 02 | [Repository + cache Delete](phase-02-repository-and-cache.md) | 01 | ✅ done |
| 03 | [Service: Delete/Update + Resolve active + evictions](phase-03-service-layer.md) | 02 | ✅ done |
| 04 | [Handlers & routes (PUT, DELETE)](phase-04-handlers-and-routes.md) | 03 | ✅ done |
| 05 | [Frontend: status + row actions](phase-05-frontend.md) | 04 | ✅ done |
| 06 | [Tests](phase-06-tests.md) | 01–05 | ✅ done |

## Cross-cutting principles
- YAGNI: no soft-delete, no `updated_at`, no custom codes, no admin override (owner-only; RBAC is a later roadmap item).
- KISS: one `PUT` covers expiry + active; reuse existing envelope/apperror/ownership patterns.
- DRY: reuse `GetByCode`, `response.Success/Error`, `appmw.UserIDFrom`.

## Global risks
- **Stale cache** serving a deleted/disabled link → mitigated by mandatory cache `Delete` on every mutation + `Resolve` skipping cache for inactive.
- **Dedup lock** after delete: `DuplicateURLCheck` keys on `(user,url)`; deleting a link must evict that key so the user can re-create the same URL.
- Expired vs disabled precedence: expiry check stays first; a re-enabled but expired link is still Gone.

## Unresolved questions
- None. (PUT-full-state chosen; disable→410; hard delete confirmed.)
