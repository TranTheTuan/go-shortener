# Phase 02 — Part B: Plans & Subscriptions Data

**Context:** [plan.md](plan.md) · [spec](../reports/spec-260626-1538-link-ownership-and-subscription-quota.md)

## Overview
- **Priority:** High
- **Status:** pending
- DB tables + repositories for plans and subscriptions. No quota logic yet.

## Related Code Files
- **Create:** `migrations/000007_create_plans_table.{up,down}.sql`,
  `migrations/000008_create_subscriptions_table.{up,down}.sql`,
  `internal/repository/plan_repository.go`, `internal/repository/subscription_repository.go`

## Implementation Steps

1. **Migration 000007** (plans), up — schema + seed basic:
   ```sql
   CREATE TABLE plans (
       id BIGSERIAL PRIMARY KEY,
       code VARCHAR(50) NOT NULL, name VARCHAR(255) NOT NULL,
       daily_link_quota INT NOT NULL, price_cents INT NOT NULL DEFAULT 0,
       is_active BOOLEAN NOT NULL DEFAULT TRUE,
       created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
       updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
   );
   CREATE UNIQUE INDEX idx_plans_code ON plans (code);
   INSERT INTO plans (code, name, daily_link_quota, price_cents) VALUES ('basic','Basic',10,0);
   ```
   down: `DROP TABLE plans;`

2. **Migration 000008** (subscriptions), up:
   ```sql
   CREATE TABLE subscriptions (
       id BIGSERIAL PRIMARY KEY,
       user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
       plan_id BIGINT NOT NULL REFERENCES plans(id),
       status VARCHAR(20) NOT NULL DEFAULT 'active',
       current_period_start TIMESTAMPTZ NOT NULL DEFAULT now(),
       current_period_end TIMESTAMPTZ,
       created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
       updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
   );
   CREATE UNIQUE INDEX idx_subscriptions_active_user ON subscriptions (user_id) WHERE status = 'active';
   CREATE INDEX idx_subscriptions_user_id ON subscriptions (user_id);
   ```
   down: `DROP TABLE subscriptions;`

3. **`plan_repository.go`** — `Plan` entity (tags match schema) +
   ```go
   type PlanRepository interface {
       GetByCode(ctx, code string) (*Plan, error)
       GetByID(ctx, id int64) (*Plan, error)
   }
   ```
   GORM impl; `ErrRecordNotFound`→`ErrNotFound`.

4. **`subscription_repository.go`** — `Subscription` entity +
   ```go
   type SubscriptionRepository interface {
       Create(ctx, *Subscription) (*Subscription, error)
       GetActiveByUserID(ctx, userID int64) (*Subscription, error) // status='active'
   }
   ```
   `GetActiveByUserID`: `Where("user_id = ? AND status = ?", userID, "active").First(...)`.

5. `go build ./...`.

## Todo
- [ ] Migration 000007 (plans + seed basic) up/down
- [ ] Migration 000008 (subscriptions) up/down
- [ ] PlanRepository + entity
- [ ] SubscriptionRepository + entity
- [ ] build passes

## Success Criteria
- `make migrate-up` seeds basic (quota 10). Round-trip `migrate-down NUM=2` clean.
- Repos compile; interfaces match what QuotaService (Phase 3) needs.

## Next
Phase 03 consumes these repos for limit resolution.
