# Phase 06 — Wiring + Tests + Docs + Minimal Frontend

## Context Links
- Overview: [plan.md](plan.md)
- Wiring: `cmd/server/server.go` (~L80-210), `cmd/server/consumer.go`
- Frontend: `web/static/app.js`, `web/index.html`, `web/static/styles.css`
- Docs: `docs/system-architecture.md`, `docs/project-changelog.md`, `docs/project-roadmap.md`, `README.md`
- Depends on: phases 03, 04, 05 complete

## Overview
- **Priority:** P0 (integration + release)
- **Status:** pending
- Wire new repos/services into both binaries, run full test suite, minimal frontend charts, update docs.

## Key Insights
- TWO binaries touch clicks: `server` (read + inline producer fallback) and `consumer` (write path). Both must construct the new repos so `CreateBatch` rollup works in the consumer AND inline fallback.
- `NewAnalyticsService` signature changed (phase 05) → update `server.go` call site + tests.
- Frontend is vanilla no-build; add charts via a CDN lib (Chart.js) — keep minimal.

## Requirements
- **FR-1:** consumer binary wires parsers into `CreateBatch` (already via repo — just confirm no new inject needed; parsers imported inside repo).
- **FR-2:** `server.go` constructs `PlanFeatureRepository`, `EntitlementService`, `ClickStatsRepository`; passes into `NewAnalyticsService`.
- **FR-3:** all tests pass (`make test`); new unit tests for parsers, entitlement, rollup repo, advanced analytics service/handler.
- **FR-4:** minimal frontend: on link stats view, if `/analytics` returns 200 render 3 charts; if 403 show "Upgrade to Pro for advanced analytics".
- **FR-5:** docs updated (architecture, changelog, roadmap, README API table).

## Architecture
`ClickStatsRepository` is a plain GORM repo → construct in both `server.go` and (for reads) inject into analytics service. Consumer's `CreateBatch` rollup needs NO new injection (parsers are package imports inside the repo). Confirm `clickRepo` in `consumer.go` already flows to `CreateBatch` (it does).

## Related Code Files
**Modify:**
- `cmd/server/server.go` — build `clickStatsRepo`, `planFeatureRepo`, `entitlementSvc`; update `NewAnalyticsService(linkRepo, clickRepo, clickStatsRepo, entitlementSvc)` call.
- `cmd/server/consumer.go` — verify no change needed (rollup lives inside `CreateBatch`); add comment noting rollup happens here.
- `internal/service/analytics_service_test.go` — update constructor calls + add Advanced tests (mock entitlement + stats repo).
- `internal/handler/link_handler_test.go` (or new `link_analytics_handler_test.go`) — 200 / 403 / 404 cases.
- `web/index.html`, `web/static/app.js`, `web/static/styles.css` — charts + upgrade prompt.
- `docs/system-architecture.md`, `docs/project-changelog.md`, `docs/project-roadmap.md`, `README.md`.

## Implementation Steps

1. **Wiring (`server.go`):**
   ```go
   clickStatsRepo := repository.NewClickStatsRepository(db)
   planFeatureRepo := repository.NewPlanFeatureRepository(db)
   entitlementSvc := service.NewEntitlementService(planFeatureRepo, subRepo, planRepo, cfg.Quota.DefaultPlanCode)
   analyticsSvc := service.NewAnalyticsService(linkRepo, clickRepo, clickStatsRepo, entitlementSvc)
   ```

2. **Consumer:** confirm `clickRepo` → `NewClickConsumer` → `CreateBatch` path unchanged; rollup is internal. Add a one-line comment in `consumer.go` documenting that rollups are produced here.

3. **Tests** (delegate to `tester` agent per workflow):
   - parsers (phase 02 tests)
   - entitlement service (phase 04 tests)
   - rollup write: mixed batch → assert 3 tables
   - advanced analytics service: owner+entitled → data; basic → FeatureLocked; non-owner → NotFound
   - handler: 200/403/404
   - Fix failures; re-run until green. DO NOT skip failing tests.

4. **Frontend (minimal):**
   - Add Chart.js via CDN `<script>` in `index.html`.
   - In `app.js`, after fetching a link's stats, call `GET /api/links/:code/analytics`:
     - 200 → render line chart (timeseries), bar/pie (devices), table/bar (referrers).
     - 403 → show "Upgrade to Pro for advanced analytics" linking to the existing plans/subscription UI.
   - Keep styles minimal in `styles.css`.

5. **Docs:**
   - `README.md` API table: add `GET /api/links/:code/analytics` row + note tier gating.
   - `system-architecture.md`: document rollup write path + entitlement gate + new tables.
   - `project-changelog.md`: entry (feat: advanced analytics + tiered gating).
   - `project-roadmap.md`: mark milestone status.
   - Delegate to `docs-manager` agent.

6. **Final:** `make test` green, `make build` clean, `code-reviewer` agent pass.

## Todo List
- [ ] Wire repos/services in `server.go`
- [ ] Confirm consumer path + add comment
- [ ] Update analytics service test constructor + add Advanced tests
- [ ] Handler tests (200/403/404)
- [ ] Run full suite via `tester`; fix until green
- [ ] Frontend charts + upgrade prompt
- [ ] Update README + docs via `docs-manager`
- [ ] `make build` + `code-reviewer`

## Success Criteria
- `make test` all pass; `make build` clean.
- End-to-end: pro user sees charts; basic user sees upgrade prompt; non-owner gets 404.
- Docs reflect new endpoint, tables, gating.
- No file exceeds 200 lines (split where noted).

## Risk Assessment
- **Constructor ripple:** changing `NewAnalyticsService` breaks existing test call sites → update all in this phase.
- **Frontend scope creep:** keep charts minimal (YAGNI); no dashboard redesign.
- **CDN dependency:** Chart.js via CDN adds an external asset; acceptable for MVP (vanilla frontend already vendors keycloak.js — could vendor Chart.js too if offline needed).

## Security Considerations
- Frontend gating is cosmetic; server 403 is authoritative (verified in phase 05).
- No secrets in frontend; analytics endpoint requires Keycloak JWT (existing middleware).

## Next Steps
- Feature complete. Optional future: GeoIP (chart C), rollup for hourly granularity, exactly-once rollup, entitlement caching — all explicitly out of scope now.
