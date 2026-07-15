---
phase: 3
status: pending
priority: P1
depends_on: phase-02
---

# Phase 3: Webhook Handler + Worker + Routes

## Context Links
- Spec: [spec-260714-1418-billing-subscription.md](../reports/spec-260714-1418-billing-subscription.md)
- Phase 2: [phase-02-quota-billing-service.md](./phase-02-quota-billing-service.md)

## Overview
Create `WebhookHandler` (POST /webhook/paddle), `SubscriptionHandler` (GET /api/subscription,
GET /api/subscription/portal), `WebhookWorker` goroutine, register routes in router.go,
wire worker in main.go, and add `billing_event_failed` metric.

## Key Insights
- Queue full → **503**, not 200 — Paddle must retry. Returning 200 silently drops a billing event.
- Worker retries 3× with exponential backoff (1s/2s/4s) because Paddle won't retry after 200
- `PADDLE_ENABLED=false` → webhook route not registered at all; portal returns 404
- `billing_event_failed` metric needs to be added to `pkg/metrics/metrics.go`
- `WebhookWorker` is a plain goroutine (not Kafka) — billing volume ~tens/day, no broker needed
- Body read limited to 1MB; signature verified before any JSON parsing

## Requirements
- `internal/middleware/paddle.go` — `PaddleSignature(verifier)` middleware
- `internal/handler/webhook_handler.go` — POST /webhook/paddle (no sig logic)
- `internal/handler/subscription_handler.go` — GET /api/subscription, GET /api/subscription/portal
- `internal/worker/webhook_worker.go` — `RunWebhookWorker(ctx, queue, billing)`
- Update `pkg/metrics/metrics.go` — add `billingEventFailed` counter + `RecordBillingEventFailed`
- Update `internal/router/router.go` — register new routes conditionally on `Paddle.Enabled`
- Update `cmd/server/main.go` — wire `WebhookWorker` goroutine + buffered channel

## Architecture

```
POST /webhook/paddle
    └── PaddleSignature middleware     ← verifier.Verify(r); 401 if fail
        └── WebhookHandler.PaddleWebhook
            ├── parse event
            └── enqueue → chan PaddleEvent (cap 100)
                               │
                        WebhookWorker goroutine
                               ├── billing.HandleEvent (retry 3×)
                               └── on permanent fail → RecordBillingEventFailed

GET /api/subscription       → SubscriptionHandler.Get
GET /api/subscription/portal → SubscriptionHandler.Portal (redirect)
```

## Related Code Files

Create:
- `internal/middleware/paddle.go`
- `internal/handler/webhook_handler.go`
- `internal/handler/subscription_handler.go`
- `internal/worker/webhook_worker.go`

Modify:
- `pkg/metrics/metrics.go`
- `internal/router/router.go`
- `cmd/server/main.go`

## Implementation Steps

### 1. pkg/metrics — add billing_event_failed counter

Add to `instruments` struct:
```go
billingEventFailed metric.Int64Counter
```

Initialize in `Setup()`:
```go
inst.billingEventFailed, _ = meter.Int64Counter("billing_event_failed",
    metric.WithDescription("Paddle webhook events that failed permanently after retries"))
```

Add helper:
```go
func RecordBillingEventFailed(ctx context.Context, eventType string) {
    if inst == nil { return }
    inst.billingEventFailed.Add(ctx, 1, metric.WithAttributes(
        attribute.String("event_type", eventType),
    ))
}
```

### 2. internal/middleware/paddle.go

```go
// PaddleSignature returns middleware that verifies Paddle webhook signatures.
// Rejects with 401 before the handler sees the request.
func PaddleSignature(verifier *paddle.WebhookVerifier) echo.MiddlewareFunc {
    return func(next echo.HandlerFunc) echo.HandlerFunc {
        return func(c echo.Context) error {
            ok, err := verifier.Verify(c.Request())
            if err != nil || !ok {
                return c.NoContent(http.StatusUnauthorized)
            }
            return next(c)
        }
    }
}
```

### 3. internal/handler/webhook_handler.go

Handler has no sig logic — middleware already handled it:

```go
type WebhookHandler struct {
    queue chan<- service.PaddleEvent
}

// POST /webhook/paddle — sig already verified by PaddleSignature middleware
func (h *WebhookHandler) PaddleWebhook(c echo.Context) error {
    var evt service.PaddleEvent
    if err := json.NewDecoder(c.Request().Body).Decode(&evt); err != nil {
        return c.NoContent(http.StatusBadRequest)
    }
    select {
    case h.queue <- evt:
        return c.NoContent(http.StatusOK)
    default:
        slog.Warn("paddle webhook queue full", "type", evt.EventType)
        return c.NoContent(http.StatusServiceUnavailable)
    }
}
```

> Note: Paddle SDK `Verify()` reads and resets request body internally — body is available after `Verify()`. Confirm with SDK source or test before assuming.

### 3. internal/handler/subscription_handler.go

```go
type SubscriptionHandler struct {
    billing service.BillingService
    quota   service.QuotaService
}

// GET /api/subscription
func (h *SubscriptionHandler) Get(c echo.Context) error {
    userID := appmw.UserIDFrom(c)
    plan, sub, err := h.billing.CurrentPlan(c.Request().Context(), userID)
    if err != nil {
        return err
    }
    remaining := h.quota.Remaining(c.Request().Context(), userID)
    return response.Success(c, map[string]any{
        "plan":            plan,
        "subscription":    sub,
        "quota_remaining": remaining,
    })
}

// GET /api/subscription/portal
func (h *SubscriptionHandler) Portal(c echo.Context) error {
    userID := appmw.UserIDFrom(c)
    _, sub, err := h.billing.CurrentPlan(c.Request().Context(), userID)
    if err != nil || sub == nil || sub.PaddleCustomerID == nil {
        return apperror.NotFound("no active paid subscription")
    }
    url, err := h.billing.GeneratePortalURL(c.Request().Context(), *sub.PaddleCustomerID)
    if err != nil {
        return err
    }
    return c.Redirect(http.StatusFound, url)
}
```

### 4. internal/worker/webhook_worker.go

```go
func RunWebhookWorker(ctx context.Context, queue <-chan service.PaddleEvent, billing service.BillingService) {
    for {
        select {
        case evt := <-queue:
            var err error
            for attempt := range 3 {
                if err = billing.HandleEvent(ctx, evt); err == nil {
                    break
                }
                wait := time.Duration(1<<attempt) * time.Second // 1s, 2s, 4s
                slog.Warn("paddle event failed, retrying",
                    "type", evt.EventType, "attempt", attempt+1, "wait", wait)
                time.Sleep(wait)
            }
            if err != nil {
                slog.Error("paddle event permanently failed after retries — subscription state may be stale",
                    "type", evt.EventType, "event_id", evt.EventID, "error", err)
                metrics.RecordBillingEventFailed(ctx, evt.EventType)
            }
        case <-ctx.Done():
            return
        }
    }
}
```

### 5. router.go — conditional webhook route

```go
if h.Webhook != nil {
    verifier := paddlepkg.NewVerifier(paddleSecret) // passed in from main.go
    e.POST("/webhook/paddle",
        h.Webhook.PaddleWebhook,
        appmw.PaddleSignature(verifier),
    )
}
```

### 6. cmd/server/main.go — wire channel + worker goroutine

```go
var (
    webhookHandler      *handler.WebhookHandler
    subscriptionHandler *handler.SubscriptionHandler
)
if cfg.Paddle.Enabled {
    // ponytail: cap 100 — billing volume ~tens/day, spike headroom fine
    queue := make(chan service.PaddleEvent, 100)
    paddleSDK, _ := paddlepkg.NewSDK(cfg.Paddle.APIKey)
    billingSvc := service.NewBillingService(subRepo, userRepo, planRepo, quotaSvc, paddleSDK)
    verifier := paddlepkg.NewVerifier(cfg.Paddle.WebhookSecret)
    webhookHandler = handler.NewWebhookHandler(verifier, queue)
    subscriptionHandler = handler.NewSubscriptionHandler(billingSvc, quotaSvc)
    go worker.RunWebhookWorker(ctx, queue, billingSvc)
}
```

## Todo List

- [ ] Add `billingEventFailed` counter + `RecordBillingEventFailed` to `pkg/metrics/metrics.go`
- [ ] Create `internal/middleware/paddle.go` — `PaddleSignature(verifier)` middleware
- [ ] Create `internal/handler/webhook_handler.go` (no sig logic — just parse + enqueue)
- [ ] Create `internal/handler/subscription_handler.go`
- [ ] Create `internal/worker/webhook_worker.go`
- [ ] Add `Webhook` + `Subscription` handler fields to `router.Handlers`
- [ ] Register webhook route with `PaddleSignature` middleware in `router.New()`
- [ ] Register subscription routes under existing `/api` auth group
- [ ] Wire buffered channel + worker goroutine in `cmd/server/main.go`
- [ ] Run `go build ./...` — no compile errors

## Success Criteria
- `POST /webhook/paddle`: valid sig → 200, invalid sig → 401, queue full → 503
- `GET /api/subscription`: returns plan + sub + quota_remaining JSON
- `GET /api/subscription/portal`: redirects to Paddle portal URL
- Worker goroutine shuts down cleanly when context cancelled
- `PADDLE_ENABLED=false`: routes not registered, app starts without credentials

## Risk Assessment
- Worker goroutine leak: bounded by `ctx.Done()` — same pattern as Kafka workers in project
- Queue channel leak on shutdown: unbuffered sends after ctx cancel are safe (select + default)

## Security Considerations
- Signature verified before JSON parse — malformed bodies never reach business logic
- 1MB body limit prevents memory exhaustion from oversized payloads
- Auth middleware on /api/* routes — webhook route intentionally has no auth (sig verifies instead)

## Next Steps
- Phase 4: `BulkJobService.ConfirmUpload` + `BulkJobWorker.Process` quota integration
