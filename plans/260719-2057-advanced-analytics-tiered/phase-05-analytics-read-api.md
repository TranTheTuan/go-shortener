# Phase 05 — Analytics Read API

## Context Links
- Overview: [plan.md](plan.md)
- Existing analytics: `internal/service/analytics_service.go`, `internal/handler/link_handler.go` (`Stats`)
- Router: `internal/router/router.go` (link routes group)
- Ownership check pattern: `appmw.UserIDFrom(c)`; existing `Stats` handler
- Depends on: phase 03 (rollup read repo), phase 04 (entitlement)

## Overview
- **Priority:** P0
- **Status:** pending
- Add `GET /api/links/:code/analytics` returning time-series + referrers + devices, gated by entitlement. Keep existing `/stats` (basic tier) untouched.

## Key Insights
- Basic `/stats` endpoint stays as-is → basic users unaffected.
- New endpoint is owner-scoped (like `/stats`) AND feature-gated. Both checks server-side.
- Read = pure SELECT over rollup tables filtered by date range. Fast.

## Requirements
- **FR-1:** `GET /api/links/:code/analytics?range=7d|30d|90d` (default 30d).
- **FR-2:** verify caller owns the link (reuse ownership check; 404 if not owner — don't leak existence).
- **FR-3:** feature gate: `EntitlementService.HasFeature(userID, FeatureAnalyticsTimeseries)`; if false → `403 FEATURE_LOCKED` (apperror).
- **FR-4:** response payload:
  ```jsonc
  { "data": {
      "short_code": "Ab3xY7q",
      "range": "30d",
      "timeseries": [ {"day":"2026-07-01","clicks":12}, ... ],
      "referrers":  [ {"domain":"google.com","clicks":40}, {"domain":"direct","clicks":5} ],
      "devices":    [ {"device":"mobile","browser":"Chrome","os":"Android","clicks":30}, ... ]
  } }
  ```
- **FR-5:** referrers + devices ordered by clicks DESC, capped (e.g. top 50) to bound payload.
- **NFR:** single service method aggregates all three (one owner + entitlement check, three SELECTs).

## Architecture
Extend `AnalyticsService` with `Advanced(ctx, code, userID, range) (*AdvancedStats, error)`. It: (1) loads link + checks owner, (2) checks entitlement, (3) queries `ClickStatsRepository` read methods. `ClickStatsRepository` (created phase 03) gains READ methods:
```go
TimeseriesByLink(ctx, linkID int64, from, to time.Time) ([]DailyPoint, error)
ReferrersByLink(ctx, linkID int64, from, to time.Time, limit int) ([]ReferrerPoint, error)
DevicesByLink(ctx, linkID int64, from, to time.Time, limit int) ([]DevicePoint, error)
```
Range string → `from = now - N days`, `to = now` (UTC dates).

## Related Code Files
**Modify:**
- `internal/repository/click_stats_repository.go` — add 3 READ methods + point structs.
- `internal/service/analytics_service.go` — add `Advanced(...)`; inject `ClickStatsRepository` + `EntitlementService` into `analyticsService` (update `NewAnalyticsService` signature). **Watch 200-line limit** — split advanced logic into `internal/service/analytics_advanced.go` if needed.
- `internal/handler/link_handler.go` — add `Analytics(c echo.Context)` handler + register route. **Watch 200-line limit** — link_handler.go is already large; put the new handler in `internal/handler/link_analytics_handler.go` (same package) instead.
- `internal/router/router.go` — `links.GET("/:code/analytics", h.Link.Analytics)`.
- `pkg/apperror/` — add `FeatureLocked(msg)` helper mapping to HTTP 403 + code `FEATURE_LOCKED` (if no equivalent exists; check first).

## Implementation Steps

1. Add read structs + methods to `ClickStatsRepository`:
   ```go
   type DailyPoint    struct{ Day time.Time; Clicks int64 }
   type ReferrerPoint struct{ Domain string; Clicks int64 }
   type DevicePoint   struct{ Device, Browser, OS string; Clicks int64 }
   // Timeseries: WHERE link_id=? AND day BETWEEN ? AND ? ORDER BY day ASC
   // Referrers/Devices: same range, GROUP-less (rows already aggregated), ORDER BY clicks DESC LIMIT ?
   ```

2. Add `apperror.FeatureLocked` (403) if missing — mirror existing `NotFound`/`Internal` constructors.

3. `AnalyticsService.Advanced`:
   ```go
   link, err := s.links.GetByCode(ctx, code)      // ErrNotFound -> apperror.NotFound
   if link.UserID != userID { return nil, apperror.NotFound(...) } // don't leak
   ok, err := s.entitle.HasFeature(ctx, userID, FeatureAnalyticsTimeseries)
   if !ok { return nil, apperror.FeatureLocked("advanced analytics requires Pro or Business") }
   from, to := rangeWindow(rangeStr)
   // three repo reads -> assemble AdvancedStats
   ```
   Verify the link ownership field name via `repository.Link` (confirm `UserID`/`OwnerID`).

4. Handler `Analytics`:
   ```go
   owner, ok := appmw.UserIDFrom(c); if !ok { 401 }
   code := c.Param("code"); rng := c.QueryParam("range")
   stats, err := h.analytics.Advanced(ctx, code, owner, rng)
   // apperror -> uniform envelope (existing response helper)
   return response.OK(c, stats)
   ```

5. Register route AFTER `/:code/stats` in the links group. Update `NewAnalyticsService` call site (phase 06 wiring).

6. Add Swagger annotations matching existing handler style (`docs/swagger`).

## Todo List
- [ ] Add 3 READ methods + point structs to click_stats repo
- [ ] Add `apperror.FeatureLocked` (if missing)
- [ ] Add `Advanced` to AnalyticsService (+ new deps in constructor)
- [ ] Add `Analytics` handler (new file to respect 200-line limit)
- [ ] Register route
- [ ] Swagger annotations
- [ ] `go build ./...`

## Success Criteria
- pro/business owner → 200 with populated timeseries/referrers/devices for the range.
- basic owner → 403 FEATURE_LOCKED.
- non-owner (any tier) → 404 (no existence leak).
- Range param honored (7d/30d/90d); default 30d.

## Risk Assessment
- **Constructor signature change** ripples to wiring + tests → covered in phase 06.
- **Empty rollups (fresh launch):** returns empty arrays, not error. Frontend renders "no data yet".
- **Ownership field name** must be confirmed against `repository.Link` before coding.

## Security Considerations
- Server-side gate is authoritative. Owner check prevents cross-user data access; 404 (not 403) on non-owner avoids leaking link existence.
- No raw IP in response (privacy).

## Next Steps
- Phase 06 wires new deps, adds handler/service tests, updates docs + minimal frontend.
