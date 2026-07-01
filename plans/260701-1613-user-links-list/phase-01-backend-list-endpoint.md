# Phase 01 — Backend List Endpoint + Tests

**Context:** [plan.md](plan.md) · [spec](../reports/spec-260701-1613-user-links-list.md)

## Overview
- **Priority:** High
- **Status:** pending
- `GET /api/links` — owner-scoped, paginated, with per-link click counts.

## Related Code Files
- **Modify:** `internal/repository/link_repository.go`, `internal/service/link_service.go`,
  `internal/handler/link_handler.go`, `internal/router/router.go`,
  `internal/service/mocks_test.go`

## Implementation Steps

1. **`link_repository.go`** — add:
   ```go
   // OwnedLink is a link plus its click count, for the owner's list view.
   type OwnedLink struct {
       Link
       TotalClicks int64 `json:"total_clicks"`
   }
   ```
   Add to `LinkRepository` + impl:
   ```go
   ListByOwner(ctx context.Context, ownerID int64, limit, offset int) ([]*OwnedLink, error)
   CountByOwner(ctx context.Context, ownerID int64) (int64, error)
   ```
   `ListByOwner`:
   ```go
   var out []*OwnedLink
   err := r.db.WithContext(ctx).Model(&Link{}).
       Select("links.*, COUNT(clicks.id) AS total_clicks").
       Joins("LEFT JOIN clicks ON clicks.link_id = links.id").
       Where("links.user_id = ?", ownerID).
       Group("links.id").
       Order("links.created_at DESC").
       Limit(limit).Offset(offset).
       Scan(&out).Error
   ```
   `CountByOwner`: `r.db.WithContext(ctx).Model(&Link{}).Where("user_id = ?", ownerID).Count(&n)`.
   Verify the `total_clicks` column maps to `OwnedLink.TotalClicks` (Scan aliases by column name).

2. **`link_service.go`** — add to `LinkService`:
   ```go
   ListByOwner(ctx context.Context, ownerID int64, limit, offset int) ([]*repository.OwnedLink, int64, error)
   ```
   Impl: clamp `limit` (≤0 → 20; >100 → 100), `offset` (<0 → 0); call `repo.ListByOwner` + `repo.CountByOwner`; map errors to `apperror.Internal`; return `(items, total, nil)`.

3. **`link_handler.go`** — add `List`:
   ```go
   // @Summary List the authenticated user's links
   // @Tags links
   // @Produce json
   // @Security BearerAuth
   // @Param limit  query int false "Page size (default 20, max 100)"
   // @Param offset query int false "Offset (default 0)"
   // @Success 200 {object} response.Envelope
   // @Router /api/links [get]
   func (h *LinkHandler) List(c echo.Context) error {
       owner, ok := appmw.UserIDFrom(c)
       if !ok { return response.Error(c, apperror.New(401,"UNAUTHORIZED","not authenticated")) }
       limit := atoiDefault(c.QueryParam("limit"), 20)
       offset := atoiDefault(c.QueryParam("offset"), 0)
       items, total, err := h.links.ListByOwner(c.Request().Context(), owner, limit, offset)
       if err != nil { return response.Error(c, err) }
       // map to DTO: short_code, short_url=baseURL+"/"+code, original_url, created_at, expires_at, total_clicks
       return response.Success(c, 200, listResponse{Items: dtos, Limit: clampedLimit, Offset: clampedOffset, Total: total})
   }
   ```
   Add a `listResponse` struct + a small `linkListItem` DTO + an `atoiDefault` helper. Return the **clamped** limit/offset in the response (call the service, or clamp identically in the handler — prefer echoing the service's applied values; simplest: expose clamped values by re-clamping in the handler with shared consts, or have the service return them). Keep it DRY: put clamp consts in the service and have `ListByOwner` also return applied limit/offset, OR clamp in handler and pass through. Pick one; document.

4. **`router.go`** — add beside the existing POST:
   ```go
   links.GET("", h.Link.List)
   ```

5. **`mocks_test.go`** — add `ListByOwner`/`CountByOwner` to `mockLinkRepo` (return configurable slice + count).

6. **Tests:**
   - `link_service_test.go`: clamping (limit 0→20, 500→100, offset -5→0); returns items+total; repo error → 500.
   - `link_handler` test (new or existing file): `user_id` in context + query parse → correct scoping + response shape; missing user → 401.
   - Repository join test (DB-gated, follow existing pattern): owner's links only, newest-first, correct `total_clicks`, limit/offset respected.

7. `go build ./...` + `go test ./internal/...`.

## Todo
- [ ] `OwnedLink` + `ListByOwner` + `CountByOwner` (repo)
- [ ] `LinkService.ListByOwner` (clamp + total)
- [ ] `LinkHandler.List` (+ DTO, Swagger)
- [ ] Router `GET /api/links`
- [ ] mock + service/handler tests
- [ ] build + tests pass

## Success Criteria
- `GET /api/links` returns only the caller's links, newest-first, with counts + pagination; clamped params; build + tests green.

## Next
Phase 02 renders this in the SPA.
