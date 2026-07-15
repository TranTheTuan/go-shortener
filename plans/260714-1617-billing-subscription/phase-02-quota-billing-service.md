---
phase: 2
status: pending
priority: P1
depends_on: phase-01
---

# Phase 2: QuotaService + BillingService

## Context Links
- Spec: [spec-260714-1418-billing-subscription.md](../reports/spec-260714-1418-billing-subscription.md)
- Phase 1: [phase-01-db-migration-config.md](./phase-01-db-migration-config.md)

## Overview
Extend `QuotaService` with `Reset()` and `Remaining()`, handle unlimited quota (`-1`),
create `pkg/paddle/` wrapper around `github.com/PaddleHQ/paddle-go-sdk/v5`,
and implement `BillingService` with full event-handling logic.

## Key Insights
- `DailyLimit()` must return `math.MaxInt` when `plan.DailyLinkQuota == -1`
- `Remaining()` fails open (`math.MaxInt`) on Redis unavailable — consistent with `Allow()`
- `Reset()` uses `SET key 0 KEEPTTL` — preserves day boundary TTL, counter → 0
- Upgrade detection uses `planOrder` constant map, not quota comparison
- `HandleEvent` is idempotent via `UpsertByPaddleID`; safe to replay Paddle deliveries
- `GeneratePortalURL` calls Paddle Customer Portal API — wrap in `pkg/paddle` client

## Requirements

### QuotaService additions
- `Reset(ctx, userID)` — counter → 0, TTL preserved
- `Remaining(ctx, userID) int` — `DailyLimit - current count`; fails open to `math.MaxInt`
- `DailyLimit` fix: `if plan.DailyLinkQuota == -1 { return math.MaxInt }`

### New files
- `pkg/paddle/client.go` — wrapper around `paddle-go-sdk/v5`: verifier + portal client
- `internal/service/billing_service.go` — `BillingService` interface + impl

## Architecture

```
pkg/paddle/client.go
    ├── NewVerifier(secret string) *paddle.WebhookVerifier
    └── NewPortalClient(apiKey string) *paddle.SDK  (for portal session)

internal/service/quota_service.go    (modify)
    ├── DailyLimit: handle -1 → math.MaxInt
    ├── Reset(ctx, userID)
    └── Remaining(ctx, userID) int

internal/service/billing_service.go  (new)
    ├── HandleEvent(ctx, PaddleEvent) error
    ├── CurrentPlan(ctx, userID) (*Plan, *Subscription, error)
    └── GeneratePortalURL(ctx, customerID) (string, error)
```

## Related Code Files

Modify:
- `internal/service/quota_service.go`

Create:
- `pkg/paddle/client.go`
- `internal/service/billing_service.go`

### 0. Add SDK dependency (run first)

```bash
go get github.com/PaddleHQ/paddle-go-sdk/v5
```

### 1. pkg/paddle/client.go

Thin wrapper exposing what the app needs:

```go
package paddle

import (
    paddle "github.com/PaddleHQ/paddle-go-sdk/v5"
)

// NewVerifier returns a Paddle webhook verifier using the given secret.
// Usage in handler: ok, err := NewVerifier(secret).Verify(r)
func NewVerifier(secret string) *paddle.WebhookVerifier {
    return paddle.NewWebhookVerifier(secret)
}

// NewSDK returns a Paddle API client for server-side calls (portal sessions, etc).
func NewSDK(apiKey string) (*paddle.SDK, error) {
    return paddle.New(apiKey)
}
```

### 2. QuotaService — DailyLimit fix

In `quota_service.go`, `DailyLimit()`:
```go
// After fetching plan.DailyLinkQuota:
if plan.DailyLinkQuota == -1 {
    return math.MaxInt
}
return plan.DailyLinkQuota
```

### 3. QuotaService — add Reset() and Remaining() to interface

```go
type QuotaService interface {
    Allow(ctx context.Context, userID int64) (bool, error)
    Release(ctx context.Context, userID int64)
    Reset(ctx context.Context, userID int64)
    Remaining(ctx context.Context, userID int64) int
}
```

### 4. QuotaService — Reset() implementation

```go
func (s *quotaService) Reset(ctx context.Context, userID int64) {
    key := s.key(userID)
    _, _ = s.breaker.Do(func() (any, error) {
        return s.rdb.Client.Set(ctx, key, 0, redis.KeepTTL).Result()
        // ponytail: KEEPTTL preserves day-boundary TTL; counter resets to 0
    })
}
```

### 5. QuotaService — Remaining() implementation

```go
func (s *quotaService) Remaining(ctx context.Context, userID int64) int {
    limit := s.DailyLimit(ctx, userID)
    if limit == math.MaxInt {
        return math.MaxInt // unlimited plan — no need to check Redis
    }
    res, err := s.breaker.Do(func() (any, error) {
        return s.rdb.Client.Get(ctx, s.key(userID)).Int64()
    })
    if redisbreaker.IsUnavailable(err) {
        return math.MaxInt // fail open
    }
    used, _ := res.(int64)
    if remaining := limit - int(used); remaining > 0 {
        return remaining
    }
    return 0
}
```

### 6. BillingService — types and interface

```go
// PaddleEvent is the minimal webhook payload shape.
type PaddleEvent struct {
    EventID   string          `json:"event_id"`
    EventType string          `json:"event_type"`
    Data      json.RawMessage `json:"data"`
}

var planOrder = map[string]int{
    "basic":    0,
    "pro":      1,
    "business": 2,
}

type BillingService interface {
    HandleEvent(ctx context.Context, event PaddleEvent) error
    CurrentPlan(ctx context.Context, userID int64) (*repository.Plan, *repository.Subscription, error)
    GeneratePortalURL(ctx context.Context, customerID string) (string, error)
}
```

### 7. BillingService — HandleEvent logic

Event routing table:

| Event | Condition | Action |
|-------|-----------|--------|
| `subscription.created` | — | UpsertByPaddleID (active) + UpdatePaddleCustomerID on users |
| `subscription.updated` | `planOrder[new] > planOrder[current]` | UpsertByPaddleID + quota.Reset() |
| `subscription.updated` | status=canceled | UpsertByPaddleID (status=canceled) |
| `subscription.updated` | downgrade attempt | log + return nil (ignore) |
| `subscription.canceled` | — | set canceled_at; status stays active until period_end |
| `transaction.completed` | — | UpsertByPaddleID: extend current_period_end |
| other | — | log + return nil |

### 8. BillingService — GeneratePortalURL

Uses Paddle SDK to create a Customer Portal session:
```go
func (s *billingService) GeneratePortalURL(ctx context.Context, customerID string) (string, error) {
    res, err := s.paddle.CreateCustomerPortalSession(ctx, &paddle.CreateCustomerPortalSessionRequest{
        CustomerID: customerID,
    })
    if err != nil {
        return "", apperror.Internal(err)
    }
    return res.Data.Urls.General.Overview, nil
}
```

## Todo List

- [ ] `go get github.com/PaddleHQ/paddle-go-sdk/v5`
- [ ] Create `pkg/paddle/client.go` (NewVerifier, NewSDK wrappers)
- [ ] Fix `DailyLimit()` for `daily_link_quota == -1` → `math.MaxInt`
- [ ] Add `Reset()` + `Remaining()` to `QuotaService` interface
- [ ] Implement `Reset()` using `SET key 0 KEEPTTL`
- [ ] Implement `Remaining()` with fail-open on Redis unavailable
- [ ] Create `internal/service/billing_service.go` with `PaddleEvent`, `planOrder`, interface
- [ ] Implement `HandleEvent` with full event routing table
- [ ] Implement `CurrentPlan` (active sub or default basic plan)
- [ ] Implement `GeneratePortalURL` via Paddle SDK portal session
- [ ] Run `go build ./...` — no compile errors

## Success Criteria
- `QuotaService` interface satisfied with all 4 methods
- `Reset()` test: counter goes to 0, TTL untouched (verifiable in Phase 5)
- `Remaining()` returns `math.MaxInt` when unlimited or Redis down
- `BillingService` compiles; `HandleEvent` routes all spec'd event types

## Risk Assessment
- `KEEPTTL` requires Redis ≥ 6.0 — already used in project (`redis.KeepTTL` in `decr()`)
- Paddle Customer Portal API call in `GeneratePortalURL` needs `PADDLE_WEBHOOK_SECRET` for auth header

## Security Considerations
- `VerifySignature` uses `hmac.Equal` (constant-time comparison) — no timing leak
- Paddle API calls must use `Authorization: Bearer <PADDLE_WEBHOOK_SECRET>` header

## Next Steps
- Phase 3 depends on `BillingService` interface and `pkg/paddle` package
