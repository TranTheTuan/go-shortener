# Design Spec: Provision Basic Subscription on User Creation

- **Date:** 2026-06-30
- **Status:** Pending user review
- **Scope:** When `SyncFromKeycloak` JIT-creates a new local user, atomically create a matching **basic** subscription row in the same transaction. Existing users untouched.

## Problem Statement

`UserService.SyncFromKeycloak` inserts only into `users`. The `subscriptions` table is never populated on provisioning — quota only works because `QuotaService` falls back to the basic plan when no active subscription exists. The subscriptions table should reflect reality: every user has an explicit active subscription row (billing-ready, queryable state).

## Decisions (locked)

| # | Decision | Choice |
|---|----------|--------|
| 1 | Atomicity | **Transaction** — user + subscription inserted both-or-nothing (no user without a subscription). |
| 2 | Existing users | **No backfill** — keep `QuotaService`'s "no active sub → basic" fallback as a safety net. |
| 3 | Plan | The configured default plan (`QUOTA_DEFAULT_PLAN_CODE`, = `basic`), resolved via `PlanRepository.GetByCode`. |
| 4 | Period | `current_period_start = now (UTC)`, `current_period_end = NULL` (open-ended free tier). |

## Architecture

Only the **create branch** of `SyncFromKeycloak` changes; the get-existing and update-claims branches are unchanged (no subscription writes there).

```
SyncFromKeycloak (new user)
  ├─ plan := PlanRepository.GetByCode(defaultPlanCode)        // read, outside tx
  └─ UserRepository.CreateWithSubscription(user, sub)         // ONE transaction:
         tx.Create(user)  →  sub.UserID = user.ID  →  tx.Create(sub)
```

### Components

**repository — new transactional method** (`internal/repository/user_repository.go`):
```go
// CreateWithSubscription inserts a user and its initial subscription in one
// transaction (both-or-nothing). A uniqueness violation on either table maps to
// ErrConflict so the caller can resolve a provisioning race.
CreateWithSubscription(ctx context.Context, user *User, sub *Subscription) (*User, error)
```
Impl uses `r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error { tx.Create(user); sub.UserID = user.ID; tx.Create(sub) })`; `gorm.ErrDuplicatedKey` → `ErrConflict`. The tx lives in the repository (it owns the DB handle) — the service stays storage-agnostic. `Subscription` is in the same package, so no new import.

**service** (`internal/service/user_service.go`):
- `NewUserService(userRepo, planRepo, defaultPlanCode)` — gains `PlanRepository` + the default plan code (passed from `cfg.Quota.DefaultPlanCode`).
- Create branch of `SyncFromKeycloak`:
  1. `plan, err := planRepo.GetByCode(defaultPlanCode)`; `ErrNotFound`/error → `apperror.Internal` (misconfiguration — basic is seeded by migration 000007).
  2. Build `user` (as today) + `sub := &repository.Subscription{PlanID: plan.ID, Status: "active", CurrentPeriodStart: now}`.
  3. `created, err := userRepo.CreateWithSubscription(ctx, user, sub)`.
  4. On `ErrConflict` (concurrent first request): re-fetch via `GetByKeycloakSub` and return the winner (who already has a subscription) — no duplicate.

**main.go wiring:** reuse the existing `planRepo`; `service.NewUserService(userRepo, planRepo, cfg.Quota.DefaultPlanCode)`.

**QuotaService:** unchanged — keep the basic fallback as a safety net for pre-existing users and any provisioning gap.

## Error Handling

- Default plan missing/unreadable → `500` (should never happen; seeded). Surfaces a real misconfiguration rather than silently degrading.
- Transaction failure (either insert) → rolls back, no partial state; mapped error returned (`ErrConflict` → race re-fetch; other → `500`).
- The `users` unique indexes (`keycloak_sub`, `username`, `email`) and the partial-unique active-subscription index keep the race correct: the loser's whole tx aborts.

## Testing Strategy

- `SyncFromKeycloak` new user → user **and** an active basic subscription created (assert both); subscription has `plan_id` = basic, `status=active`, `current_period_end` nil.
- Existing user (get/update branches) → **no** subscription created.
- Conflict/race path → re-fetch returns existing user, no duplicate subscription.
- Default-plan-missing → 500.
- Update test setup: `NewUserService` now takes `planRepo` + plan code; add `CreateWithSubscription` to `mockUserRepo` (records the created subscription for assertions); seed `mockPlanRepo` with `basic`.
- Gate: `make build` + `make test` green.

## Files

- **Modify:** `internal/repository/user_repository.go` (+`CreateWithSubscription`), `internal/service/user_service.go` (deps + create branch), `cmd/server/main.go` (wiring), `internal/service/mocks_test.go` + `internal/service/user_service_test.go` (mock + tests).
- **No new migration** (000007/000008 already define plans/subscriptions; basic seeded).

## Risks

- `userService` now depends on `PlanRepository` + config plan code (slightly more wiring). Acceptable; cohesive with provisioning.
- Tiny added latency on **first** login per user (the tx); negligible.
- `CreateWithSubscription` writes two tables from one repo method — a mild departure from strict repo-per-aggregate; justified because the atomic guarantee requires a shared transaction. Alternative (a dedicated `ProvisioningRepository`) noted but heavier for one method.

## Success Criteria

- A new Keycloak user's first authenticated request creates exactly one `users` row **and** one active basic `subscriptions` row, atomically.
- No user can exist without a subscription (barring pre-existing rows, covered by fallback).
- Re-login / concurrent first requests never create duplicates.
- `make build` + `make test` green.

## Open Questions

- None blocking. (If billing later needs period/renewal semantics on the free tier, revisit `current_period_end`.)

## Next Steps

`/plan` (small, single-phase) or implement directly via `/cook`.
