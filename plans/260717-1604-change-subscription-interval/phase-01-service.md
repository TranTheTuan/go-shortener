# Phase 01 — Service layer

**Status:** pending  
**Priority:** high  
**Effort:** small  

## Context

- Spec: [brainstorm-260717-1604-change-subscription-interval.md](../reports/brainstorm-260717-1604-change-subscription-interval.md)
- Interface: `internal/service/billing_service.go:25-34`
- Implementation: `internal/service/billing_service.go:261-311`

## Files to modify

- `internal/service/billing_service.go`

## Implementation steps

1. In the `BillingService` interface, replace:
   ```go
   UpgradeSubscription(ctx context.Context, userID int64, planID int64) error
   ```
   with:
   ```go
   // ChangeSubscription switches plan tier and/or billing interval.
   // Downgrades (lower tier) are rejected; interval changes are unrestricted.
   ChangeSubscription(ctx context.Context, userID int64, planID int64, interval string) error
   ```

2. Rename the method receiver from `UpgradeSubscription` → `ChangeSubscription`, add `interval string` param.

3. Replace the rank guard:
   ```go
   // before
   if !currentOk || !targetOk || targetRank <= currentRank {
       return apperror.New(400, "INVALID_UPGRADE", "must upgrade to a higher tier plan")
   }
   ```
   with:
   ```go
   if !currentOk || !targetOk || targetRank < currentRank {
       return apperror.New(400, "INVALID_UPGRADE", "cannot downgrade plan tier")
   }
   ```

4. After the rank guard, validate interval and add no-op guard:
   ```go
   if interval != "month" && interval != "year" {
       return apperror.New(400, "INVALID_INTERVAL", fmt.Sprintf("interval must be month or year, got %q", interval))
   }
   if sub.BillingInterval == nil {
       return apperror.New(400, "INVALID_SUBSCRIPTION", "current subscription has no billing interval")
   }
   if targetPlan.ID == currentPlan.ID && interval == *sub.BillingInterval {
       return apperror.New(400, "NO_CHANGE", "plan and interval are already set to the requested values")
   }
   ```

5. Replace the `switch *sub.BillingInterval` block — it currently reads from `sub.BillingInterval`; change it to switch on `interval` (the requested value):
   ```go
   var priceID string
   switch interval {
   case "month":
       if targetPlan.PaddlePriceIDMonthly == nil {
           return apperror.New(400, "UNAVAILABLE", "target plan is not available with monthly billing")
       }
       priceID = *targetPlan.PaddlePriceIDMonthly
   case "year":
       if targetPlan.PaddlePriceIDYearly == nil {
           return apperror.New(400, "UNAVAILABLE", "target plan is not available with yearly billing")
       }
       priceID = *targetPlan.PaddlePriceIDYearly
   }
   ```
   Remove the old `if sub.BillingInterval == nil` nil-guard above (now handled in step 4).

6. Remove the now-redundant `default` case in the switch (interval already validated in step 4).

## Todo

- [ ] Rename interface method
- [ ] Rename receiver + add param
- [ ] Update rank guard (`<` not `<=`)
- [ ] Add interval validation + no-op guard
- [ ] Switch on `interval` not `*sub.BillingInterval`
- [ ] Compile: `go build ./...`

## Success criteria

`go build ./...` passes with no errors referencing the old `UpgradeSubscription` signature.
