# Design Spec: ChangeSubscription — interval + tier change

**Date:** 2026-07-17  
**Status:** draft

---

## Problem

`UpgradeSubscription` only moves users to a higher plan tier; it silently locks
the billing interval to whatever is already on the subscription. Users cannot
switch from monthly to yearly (or yearly to monthly) without going through the
Paddle portal — even when staying on the same plan.

---

## Rules (agreed)

| Rule | Detail |
|------|--------|
| No plan-tier downgrade | `targetRank < currentRank` → rejected |
| Same tier allowed | `targetRank == currentRank` is valid (interval-only change) |
| Interval is free | monthly → yearly and yearly → monthly both allowed |
| No-op rejected | if plan AND interval are both unchanged → rejected |
| Both fields required | caller must always supply `plan_id` + `interval` |

---

## Design

### Service layer

Rename `UpgradeSubscription` → `ChangeSubscription`, add `interval string` param.

```go
// Before
UpgradeSubscription(ctx context.Context, userID int64, planID int64) error

// After
ChangeSubscription(ctx context.Context, userID int64, planID int64, interval string) error
```

Validation order inside `ChangeSubscription`:

1. Fetch `currentPlan`, `sub` via `CurrentPlan` (unchanged)
2. Guard: `sub == nil || sub.PaddleSubscriptionID == nil` → 404
3. Fetch `targetPlan` by `planID` → 400 if not found
4. Guard: `targetRank < currentRank` → 400 `INVALID_UPGRADE` "cannot downgrade plan tier"
5. Guard: `targetRank == currentRank && interval == *sub.BillingInterval` → 400 `NO_CHANGE` "no change requested"
6. Guard: `interval` not in `{"month","year"}` → 400 `INVALID_INTERVAL`
7. Resolve Paddle price ID from `targetPlan` + `interval` (same switch as today)
8. Call `sdk.UpdateSubscription` (unchanged)

Step 4 replaces the current `targetRank <= currentRank` check.  
`planRank` map is unchanged.

### Handler layer

`upgradeRequest` gains `Interval`:

```go
type upgradeRequest struct {
    PlanID   int64  `json:"plan_id"`
    Interval string `json:"interval"` // "month" | "year"
}
```

Binding guard:

```go
if err := c.Bind(&req); err != nil || req.PlanID == 0 || req.Interval == "" {
    return response.Error(c, apperror.New(400, "BAD_REQUEST", "plan_id and interval are required"))
}
```

Call site:

```go
h.billing.ChangeSubscription(c.Request().Context(), userID, req.PlanID, req.Interval)
```

### Route / URL

`POST /api/subscription/upgrade` is a breaking change if renamed. Two options:

| Option | Trade-off |
|--------|-----------|
| Keep `/upgrade` | No client breakage; misleading name |
| Rename to `/change` | Clean name; clients must update |

**Recommendation: keep `/upgrade` for now.** Update the Swagger summary to reflect the broader semantics. Rename when a v2 API is introduced.

### Interface update

`BillingService` interface in `service/billing_service.go`:

```go
// rename UpgradeSubscription → ChangeSubscription, add interval
ChangeSubscription(ctx context.Context, userID int64, planID int64, interval string) error
```

---

## Files touched

| File | Change |
|------|--------|
| `internal/service/billing_service.go` | rename method, add `interval` param, update validation |
| `internal/handler/subscription_handler.go` | add `Interval` to request struct, update bind guard, call new signature |

No migration needed — `BillingInterval` column already exists on `subscriptions`.

---

## Edge cases

- `PaddlePriceIDYearly == nil` on target plan while requesting `"year"` → already handled by existing nil-guard in the price-ID switch; returns 400 `UNAVAILABLE`
- `BillingInterval` nil on current sub → still guarded (step 2's `sub` nil check covers missing paddle sub; add explicit interval nil check before step 5 no-op guard)

---

## Testing

- `targetRank < currentRank` → 400
- Same tier, same interval → 400 `NO_CHANGE`
- Same tier, different interval → Paddle called with new price ID
- Higher tier, same interval → Paddle called (existing upgrade path)
- Higher tier, different interval → Paddle called with new tier + new interval price ID
- Unknown interval string → 400
- Target plan missing price for requested interval → 400

---

## Out of scope

- Downgrade path (still Paddle portal only)
- Proation mode change (stays `ProratedImmediately`)
- Route rename (deferred to v2)
