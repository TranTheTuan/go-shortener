# Phase 04 — Entitlement Service (`plan_features`)

## Context Links
- Overview: [plan.md](plan.md)
- Pattern reference: `internal/service/quota_service.go` (`MonthlyLimit` resolves user → sub → plan → default)
- Repos: `internal/repository/plan_repository.go`, `subscription_repository.go`
- Depends on: phase 01 (plan_features table + seed)

## Overview
- **Priority:** P0 (blocks 05 gating)
- **Status:** pending
- Resolve "does this user's plan enable feature_key X?" reusing the exact user→subscription→plan resolution `QuotaService.MonthlyLimit` already uses.

## Key Insights
- Resolution chain is identical to quota: active sub → its plan; else default plan (`cfg.Quota.DefaultPlanCode`).
- basic plan has NO seeded feature rows → absence = disabled. Default plan users get no advanced analytics (correct).
- Feature keys are constants shared with phase 01 seed + phase 05 gating (DRY — one const block).

## Requirements
- **FR-1:** `EntitlementService.HasFeature(ctx, userID, featureKey) (bool, error)`.
- **FR-2:** resolve plan via active subscription, fallback to default plan (mirror quota logic).
- **FR-3:** feature enabled ⇔ a `plan_features` row exists for (plan_id, feature_key) with `enabled = true`.
- **FR-4:** central `FeatureKey` constants (no string literals scattered).
- **NFR:** read-only; small; no caching needed for MVP (per-request DB read is cheap; YAGNI on cache).

## Architecture
New `PlanFeatureRepository` (READ) + `EntitlementService`. Service depends on `PlanFeatureRepository` + `SubscriptionRepository` + `PlanRepository` + `defaultPlanCode` — same collaborators as quota. Handler layer calls `HasFeature` before serving advanced analytics.

```go
const (
    FeatureAnalyticsTimeseries = "analytics.timeseries"
    FeatureAnalyticsReferrers  = "analytics.referrers"
    FeatureAnalyticsDevices    = "analytics.devices"
)
```

## Related Code Files
**Create:**
- `internal/repository/plan_feature_repository.go` — `PlanFeature` model + `IsEnabled(ctx, planID, key) (bool, error)`.
- `internal/service/entitlement_service.go` — `EntitlementService` interface + impl + `FeatureKey` consts.
- `internal/service/entitlement_service_test.go`

**Modify:**
- `internal/service/mocks_test.go` / `internal/service/mocks/repository/mock_repositories.go` — add mocks for new repo (follow existing mock style).

## Implementation Steps

1. `plan_feature_repository.go`:
   ```go
   type PlanFeature struct {
       ID uint64; PlanID int64; FeatureKey string; Enabled bool
       CreatedAt, UpdatedAt time.Time
   }
   // IsEnabled: SELECT enabled WHERE plan_id=? AND feature_key=?; ErrNotFound -> (false,nil).
   func (r *planFeatureRepository) IsEnabled(ctx, planID int64, key string) (bool, error)
   ```
   Treat `gorm.ErrRecordNotFound` as `(false, nil)` — absence = disabled.

2. `entitlement_service.go` — resolve plan then check:
   ```go
   func (s *entitlementService) HasFeature(ctx context.Context, userID int64, key string) (bool, error) {
       planID, err := s.resolvePlanID(ctx, userID) // sub.PlanID else default plan.ID
       if err != nil { return false, err }
       return s.features.IsEnabled(ctx, planID, key)
   }
   ```
   `resolvePlanID` mirrors `quotaService.MonthlyLimit`'s chain (active sub → default plan by code). Extract carefully; do NOT import quota (avoid coupling) — duplicate the ~6-line resolution or, better, add a small shared helper if it fits cleanly. Prefer duplication over premature abstraction (KISS) unless a helper is obviously clean.

3. Unit tests: user with pro sub → true for all analytics keys; basic/default user → false; unknown feature → false; repo error → error propagated.

## Todo List
- [ ] Create `plan_feature_repository.go` + model
- [ ] Create `entitlement_service.go` + FeatureKey consts
- [ ] Add repo mocks
- [ ] Unit tests (pro true / basic false / error path)
- [ ] `go build ./...` + `go test ./internal/service/...`

## Success Criteria
- pro/business user → `HasFeature(..., FeatureAnalyticsTimeseries)` = true.
- basic/no-sub user → false (row absent).
- All new tests pass; no regression in existing service tests.

## Risk Assessment
- **Divergence from quota resolution:** if quota logic changes later, entitlement could drift. Mitigate with a short comment cross-referencing `quotaService.MonthlyLimit`. Acceptable for MVP.
- **No cache:** per-request 1 indexed SELECT. Fine at current scale; revisit only if profiling shows cost.

## Security Considerations
- Gating is server-side authoritative — frontend hints are cosmetic only. Handler MUST call `HasFeature` before returning advanced data (phase 05).

## Next Steps
- Phase 05 handler calls `HasFeature`; 403/feature-locked error when false.
