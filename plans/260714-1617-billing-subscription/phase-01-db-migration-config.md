---
phase: 1
status: pending
priority: P1
---

# Phase 1: DB Migration + Config

## Context Links
- Spec: [spec-260714-1418-billing-subscription.md](../reports/spec-260714-1418-billing-subscription.md)
- Plan: [plan.md](./plan.md)

## Overview
Add Paddle-specific DB columns, seed paid plans, add `PaddleConfig` to config,
extend `Subscription` model and `SubscriptionRepository` interface,
add `UpdatePaddleCustomerID` to `UserRepository`.

## Key Insights
- Migration number is **000013** — 000012 is already taken by `add_link_clicks_count`
- `daily_link_quota = -1` signals unlimited; QuotaService handles in Phase 2
- `UpsertByPaddleID` uses `INSERT ... ON CONFLICT (paddle_subscription_id) DO UPDATE` — idempotent by design
- `paddle_customer_id` on `users` is used by `GeneratePortalURL` to avoid extra Paddle API lookup

## Requirements
- New migration: ALTER subscriptions + ALTER users + INSERT plans
- `PaddleConfig` struct with `envPrefix:"PADDLE_"` in `Config`
- Updated `Subscription` model with Paddle fields
- New interface methods on `SubscriptionRepository` and `UserRepository`

## Architecture

```
migrations/000013_add_billing_fields.{up,down}.sql
configs/config.go        ← add PaddleConfig
repository/subscription_repository.go  ← model + 2 new methods
repository/user_repository.go          ← 1 new method
```

## Related Code Files

Modify:
- `configs/config.go`
- `internal/repository/subscription_repository.go`
- `internal/repository/user_repository.go`

Create:
- `migrations/000013_add_billing_fields.up.sql`
- `migrations/000013_add_billing_fields.down.sql`

## Implementation Steps

### 1. Migration up (000013_add_billing_fields.up.sql)

```sql
ALTER TABLE subscriptions
    ADD COLUMN IF NOT EXISTS paddle_subscription_id VARCHAR(255) UNIQUE,
    ADD COLUMN IF NOT EXISTS paddle_customer_id     VARCHAR(255),
    ADD COLUMN IF NOT EXISTS paddle_price_id        VARCHAR(255),
    ADD COLUMN IF NOT EXISTS billing_interval       VARCHAR(10),
    ADD COLUMN IF NOT EXISTS canceled_at            TIMESTAMPTZ;

ALTER TABLE users
    ADD COLUMN IF NOT EXISTS paddle_customer_id VARCHAR(255) UNIQUE;

INSERT INTO plans (code, name, daily_link_quota, price_cents)
VALUES
    ('pro',      'Pro',      500,  900),
    ('business', 'Business', -1,  2900)
ON CONFLICT (code) DO NOTHING;
```

### 2. Migration down (000013_add_billing_fields.down.sql)

```sql
ALTER TABLE subscriptions
    DROP COLUMN IF EXISTS paddle_subscription_id,
    DROP COLUMN IF EXISTS paddle_customer_id,
    DROP COLUMN IF EXISTS paddle_price_id,
    DROP COLUMN IF EXISTS billing_interval,
    DROP COLUMN IF EXISTS canceled_at;

ALTER TABLE users DROP COLUMN IF EXISTS paddle_customer_id;

DELETE FROM plans WHERE code IN ('pro', 'business');
```

### 3. PaddleConfig in configs/config.go

Add field to `Config` struct:
```go
Paddle PaddleConfig `envPrefix:"PADDLE_"`
```

Add struct:
```go
type PaddleConfig struct {
    Enabled       bool   `env:"ENABLED" envDefault:"false"`
    WebhookSecret string `env:"WEBHOOK_SECRET"` // pdl_ntf_...
    APIKey        string `env:"API_KEY"`         // server-side API key for portal sessions
    ClientToken   string `env:"CLIENT_TOKEN"`    // client-side token for Paddle.js
}
```

### 4. Subscription model — add Paddle fields

```go
type Subscription struct {
    // ... existing fields ...
    PaddleSubscriptionID *string    `gorm:"size:255;uniqueIndex" json:"paddle_subscription_id,omitempty"`
    PaddleCustomerID     *string    `gorm:"size:255" json:"paddle_customer_id,omitempty"`
    PaddlePriceID        *string    `gorm:"size:255" json:"paddle_price_id,omitempty"`
    BillingInterval      *string    `gorm:"size:10" json:"billing_interval,omitempty"`
    CanceledAt           *time.Time `json:"canceled_at,omitempty"`
}
```

### 5. SubscriptionRepository interface — add 2 methods

```go
UpsertByPaddleID(ctx context.Context, sub *Subscription) (*Subscription, error)
GetByUserID(ctx context.Context, userID int64) ([]*Subscription, error)
```

Implementation for `UpsertByPaddleID`:
```go
func (r *subscriptionRepository) UpsertByPaddleID(ctx context.Context, sub *Subscription) (*Subscription, error) {
    result := r.db.WithContext(ctx).
        Where(Subscription{PaddleSubscriptionID: sub.PaddleSubscriptionID}).
        Assign(*sub).
        FirstOrCreate(sub)
    // ponytail: FirstOrCreate + Assign = upsert; GORM handles ON CONFLICT semantics
    return sub, result.Error
}
```

### 6. UserRepository interface — add 1 method

```go
UpdatePaddleCustomerID(ctx context.Context, userID int64, customerID string) error
```

## Todo List

- [ ] Create `migrations/000013_add_billing_fields.up.sql`
- [ ] Create `migrations/000013_add_billing_fields.down.sql`
- [ ] Add `PaddleConfig` struct + field in `configs/config.go`
- [ ] Add Paddle fields to `Subscription` model
- [ ] Add `UpsertByPaddleID` + `GetByUserID` to `SubscriptionRepository` interface + impl
- [ ] Add `UpdatePaddleCustomerID` to `UserRepository` interface + impl
- [ ] Run `go build ./...` — no compile errors

## Success Criteria
- Migration applies cleanly against existing DB
- `go build ./...` passes
- `SubscriptionRepository` and `UserRepository` compile with new methods

## Risk Assessment
- Migration 000012 conflict: already verified — next is 000013
- `FirstOrCreate + Assign` for upsert: GORM-idiomatic, covers idempotency requirement

## Security Considerations
- `WebhookSecret` and `ClientToken` are credentials — never log them; config struct exposes via env only

## Next Steps
- Phase 2 depends on `PaddleConfig`, updated `Subscription` model, and new repo interfaces
