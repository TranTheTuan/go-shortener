# Phase 04 — Handlers & Routes (PUT, DELETE)

## Overview
- **Priority:** P1 · **Status:** ✅ done · Depends on: 03
- Expose the two owner-only mutations. Basic methods only: `PUT` (edit expiry + toggle), `DELETE` (remove).

## Related files
- Modify: `internal/handler/link_handler.go` (add `Update`, `Delete`)
- Modify: `internal/router/router.go` (links group)

## API contract
- `DELETE /api/links/:code` → **204 No Content**. Owner-only. Hard delete.
- `PUT /api/links/:code` — full mutable state:
  ```json
  { "expires_at": "2026-12-31T00:00:00Z" | null, "is_active": true }
  ```
  → **200** `{ data: <link> }`. `expires_at: null` clears expiry.
- Errors: 401 (no token), 404 (missing/non-owner/unowned), 400 (bad body).

## Steps
1. **Delete handler**:
   ```go
   func (h *LinkHandler) Delete(c echo.Context) error {
     owner, ok := appmw.UserIDFrom(c)
     if !ok { return response.Error(c, apperror.New(401, "UNAUTHORIZED", "not authenticated")) }
     if err := h.svc.Delete(c.Request().Context(), c.Param("code"), owner); err != nil {
       return response.Error(c, err)
     }
     return c.NoContent(http.StatusNoContent)
   }
   ```
2. **Update handler** — bind `{expires_at *time.Time, is_active *bool}`:
   - `is_active` required (PUT = full state) → nil ⇒ `apperror.BadRequest("is_active is required")`.
   - `expires_at` optional pointer; `nil` ⇒ clear (permanent). Time zero-value guard via pointer.
   - Reject a past `expires_at`? **No** — allow (lets a user expire-now on purpose). Document it.
   - Call `svc.Update(ctx, code, owner, req.ExpiresAt, *req.IsActive)`, return `response.Success(c, 200, link)`.
   - Reuse the link→response shape used by `Create`/`List` (short_url built from `cfg.Shortener.BaseURL`).
3. **Routes** (in the authenticated `links` group, alongside existing Create/List/Stats):
   ```go
   links.PUT("/:code", h.Link.Update)
   links.DELETE("/:code", h.Link.Delete)
   ```
   No dedup/quota middleware on these (only `Create` has those).
4. **List — add `?status` filter** (existing `List` handler). Parse `c.QueryParam("status")`; validate against `{"", "all", "active", "disabled", "expired"}` → unknown ⇒ `apperror.BadRequest("invalid status")`. Forward to `svc.ListByOwner(ctx, owner, status, limit, offset)`. `total` in the response already reflects the filtered count (repo applies the same WHERE). Keep `limit`/`offset` parsing unchanged.
5. **Swagger** — add annotations matching the existing handler style (BearerAuth, params, responses). Add the `status` query param to `List`'s doc with an `Enums(active, disabled, expired)`.

## Todo
- [x] `Delete` handler → 204
- [x] `Update` handler (bind, validate is_active, PUT semantics)
- [x] `List` handler `?status` filter (validate, forward)
- [x] routes PUT + DELETE
- [x] swagger comments (incl. status enum)
- [x] compiles

## Success criteria
- `DELETE` a foreign/nonexistent code → 404; own code → 204 and it's gone from `GET /api/links`.
- `PUT` toggles `is_active` and sets/clears `expires_at`; response reflects new state.
- Malformed body / missing `is_active` → 400.

## Notes
- `:code` collides with nothing else in the group (`""`, `/:code/stats` are distinct). Echo routing handles it.
