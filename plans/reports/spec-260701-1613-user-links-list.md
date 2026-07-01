# Design Spec: List a User's Links (API + UI)

- **Date:** 2026-07-01
- **Status:** Approved (ready for /plan)
- **Scope:** `GET /api/links` returning the authenticated user's own links (paginated, with per-link click counts) + a "My links" list in the vanilla SPA.

## Problem Statement

Users can create links but have no way to see the links they've created. Add a Keycloak-authenticated, owner-scoped list endpoint and render it in the frontend.

## Decisions (locked)

| # | Decision | Choice |
|---|----------|--------|
| 1 | Pagination | **limit + offset** (`?limit=&offset=`); `total` returned for prev/next |
| 2 | Click counts | **Include `total_clicks`** per link via `LEFT JOIN clicks` + `COUNT` |
| 3 | Scope | Owner only — `WHERE links.user_id = <ctx user>`; never other users' or unowned (API-key) links |
| 4 | Sort | Newest first (`created_at DESC`); expired links included (UI marks them) |
| 5 | Defaults | `limit` default 20, clamped 1–100; `offset` default 0, min 0 |

## API

`GET /api/links?limit=&offset=` — under the Keycloak middleware (owner = context `user_id`).

Response (uniform envelope):
```jsonc
{ "data": {
  "items": [
    { "short_code": "Ab3xY7q", "short_url": "http://localhost:8080/Ab3xY7q",
      "original_url": "https://…", "created_at": "…", "expires_at": "…|null", "total_clicks": 12 }
  ],
  "limit": 20, "offset": 0, "total": 57
}}
```
- `401` if unauthenticated (enforced by middleware). No `429` (read).

## Architecture (reuse existing layering)

**Repository** (`internal/repository/link_repository.go`):
- `OwnedLink` struct: `Link` embedded + `TotalClicks int64`.
- `ListByOwner(ctx, ownerID int64, limit, offset int) ([]*OwnedLink, error)`:
  ```sql
  SELECT links.*, COUNT(clicks.id) AS total_clicks
  FROM links LEFT JOIN clicks ON clicks.link_id = links.id
  WHERE links.user_id = ?
  GROUP BY links.id
  ORDER BY links.created_at DESC
  LIMIT ? OFFSET ?
  ```
  (GORM: `Model(&Link{}).Select("links.*, COUNT(clicks.id) as total_clicks").Joins(...).Where("links.user_id = ?", ownerID).Group("links.id").Order("links.created_at DESC").Limit(limit).Offset(offset).Scan(&out)`.)
- `CountByOwner(ctx, ownerID int64) (int64, error)` — total for pagination.

**Service** (`internal/service/link_service.go`):
- `LinkService.ListByOwner(ctx, ownerID int64, limit, offset int) ([]*repository.OwnedLink, int64, error)` — clamps `limit`∈[1,100] (default 20 when ≤0), `offset`≥0; returns `(items, total, err)`; maps repo errors to `apperror.Internal`.

**Handler** (`internal/handler/link_handler.go`):
- `LinkHandler.List(c)`: `owner, _ := appmw.UserIDFrom(c)`; parse `limit`/`offset` query (ignore parse errors → defaults); `items, total, err := links.ListByOwner(...)`; map each item to a response DTO with `short_url = baseURL + "/" + short_code`; return `{data:{items, limit, offset, total}}`. Swagger-annotated (`@Security BearerAuth`).

**Router** (`internal/router/router.go`): add `links.GET("", h.Link.List)` beside the existing `links.POST("", …)` (same `/api/links`, method-distinguished, under the Keycloak group).

**Frontend** (`web/index.html`, `web/static/app.js`, `web/static/styles.css`): a "My links" section rendered on sign-in — a table (short URL + Copy, original URL truncated with `title`, created, expiry/"expired", clicks) + **Prev/Next** buttons driving `offset` (fixed page size 20) + "showing X–Y of Z". Reload page 0 after a successful create. `textContent`/`createElement` only (XSS-safe).

## Error Handling
- Empty list → `items:[]`, `total:0`; UI shows "No links yet."
- Click-less link → `total_clicks:0` (LEFT JOIN).
- `limit`/`offset` out of range → clamped (parameterized query; no injection).
- Repo/DB error → `500` envelope; UI shows a retry message.

## Testing Strategy
- **Service:** clamping (limit >100→100, ≤0→20, offset <0→0); returns items+total; error mapping. Extend `mockLinkRepo` with `ListByOwner`/`CountByOwner`.
- **Repository:** join returns owner's links with correct counts, newest-first, respects limit/offset; excludes other/unowned links. Gated like existing DB-integration tests.
- **Handler:** query parse + owner scoping + response shape (httptest; set `user_id` in context).
- **Frontend:** manual.
- Gate: `make build` + `make test` green.

## Files
- **Modify:** `internal/repository/link_repository.go`, `internal/service/link_service.go`, `internal/handler/link_handler.go`, `internal/router/router.go`, `internal/service/mocks_test.go`, `web/index.html`, `web/static/app.js`, `web/static/styles.css`, `README.md` (API table), Swagger (`make swag`).
- **No migration** (uses existing `links` + `clicks`).

## Risks
- **Unbounded-ish growth:** mitigated by pagination + the daily quota cap.
- **Join/GROUP BY cost:** fine at this scale; `clicks(link_id)` index already exists (analytics). Note if a hot path later.
- **Scan into `OwnedLink`:** ensure GORM maps `total_clicks` → `TotalClicks` (tag/column alias). Verify in the repo test.

## Success Criteria
- Signed-in user sees their links (newest-first) with click counts; prev/next paginates; new link appears after create; other users' / unowned links never shown.
- `make build` + `make test` green; README + Swagger updated.

## Open Questions
- None blocking. (Per-row delete deferred — no delete endpoint exists; out of scope.)

## Next Steps
`/plan` → phased plan (backend API → frontend UI → tests/docs).
