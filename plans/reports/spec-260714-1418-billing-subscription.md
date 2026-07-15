# Billing & Subscription — Design Spec

**Date**: 2026-07-14  
**Author**: brainstorm-features  
**Status**: Draft — pending user approval

---

## Problem Statement

App đã có `plans` + `subscriptions` tables và `QuotaService` đọc plan quota từ DB. Phần còn thiếu:

1. Không có cách để user **trả tiền** để upgrade plan
2. Không có webhook handler để nhận trạng thái subscription từ Paddle
3. Không có frontend pricing page và checkout flow

---

## User Stories

- US-1: Là guest, tôi muốn xem pricing page để biết các gói và giá trước khi đăng ký
- US-2: Là free user, tôi muốn upgrade lên Pro/Business để có quota cao hơn
- US-3: Là paid user, tôi muốn manage subscription (đổi thẻ, cancel, xem invoice) mà không cần liên hệ support
- US-4: Hệ thống phải tự động cập nhật quota khi subscription gia hạn/hủy mà không cần manual intervention

---

## Plan Tiers

| Code | Name | Daily quota | Monthly (USD) | Yearly (USD) |
|------|------|-------------|---------------|--------------|
| `basic` | Basic | 10 | Free | Free |
| `pro` | Pro | 500 | $9 | $90 (~17% off) |
| `business` | Business | unlimited (-1) | $29 | $290 (~17% off) |

> **DB**: Seed `pro` và `business` vào `plans` table. `daily_link_quota = -1` = unlimited (QuotaService cần handle).
> **Paddle**: Mỗi paid tier × 2 intervals = **4 price IDs** cần tạo trên Paddle Dashboard.
> **US-4 deferred**: Pricing page hiện cả 2 giá monthly/yearly để user chọn khi checkout lần đầu. Switch interval sau khi đã subscribe không support — user dùng Paddle Portal để cancel rồi re-subscribe với interval khác.

---

## Architecture

### Component Map

```
[Frontend Pricing Page]
        │ Paddle.js checkout overlay
        ▼
[Paddle Checkout] ── payment ──► [Paddle]
                                     │ webhook POST /webhook/paddle
                                     ▼
                          [WebhookHandler] (verify sig, enqueue)
                                     │ in-process channel (buffered)
                                     ▼
                          [WebhookWorker goroutine]
                                     │ upsert subscription
                                     ▼
                              [PostgreSQL subscriptions]
                                     │ read on next request
                                     ▼
                          [QuotaService.DailyLimit()]
```

### Không dùng Kafka

Billing events: vài chục/ngày — Kafka thêm operational cost không có ROI. Dùng in-process buffered channel (capacity 100) để decouple handler từ DB write.

> **Reliability note**: Khác với Kafka pipeline, đã trả 200 thì Paddle sẽ không retry. Worker **tự chịu trách nhiệm** retry khi DB write fails (xem WebhookWorker section). Mất event = mất thông tin subscription → cần alert ngay.

---

## Backend Design

### 1. Config additions (`configs/config.go`)

```go
type PaddleConfig struct {
    WebhookSecret string `env:"WEBHOOK_SECRET"` // pdl_ntf_...
    ClientToken   string `env:"CLIENT_TOKEN"`   // client-side token cho Paddle.js
    Enabled       bool   `env:"ENABLED" envDefault:"false"`
}
```

`PADDLE_ENABLED=false` → webhook route không register, frontend không show Paddle.js. Cho phép chạy local mà không cần Paddle credentials.

### 2. DB migration (000012_add_billing_fields.up.sql)

```sql
-- Add Paddle-specific columns to subscriptions
ALTER TABLE subscriptions
    ADD COLUMN IF NOT EXISTS paddle_subscription_id VARCHAR(255) UNIQUE,
    ADD COLUMN IF NOT EXISTS paddle_customer_id     VARCHAR(255),
    ADD COLUMN IF NOT EXISTS paddle_price_id        VARCHAR(255),
    ADD COLUMN IF NOT EXISTS billing_interval       VARCHAR(10),   -- 'month' | 'year'
    ADD COLUMN IF NOT EXISTS canceled_at            TIMESTAMPTZ;

-- Store customer ID on users for easy portal link generation
ALTER TABLE users
    ADD COLUMN IF NOT EXISTS paddle_customer_id VARCHAR(255) UNIQUE;

-- Seed paid plans
INSERT INTO plans (code, name, daily_link_quota, price_cents)
VALUES
    ('pro',      'Pro',      500,  900),
    ('business', 'Business', -1,  2900)
ON CONFLICT (code) DO NOTHING;
```

> `canceled_at` lưu khi user cancel — subscription vẫn `active` cho đến `current_period_end`, sau đó webhook `subscription.updated` (status=canceled) sẽ deactivate.

### 3. SubscriptionRepository — thêm methods

```go
type SubscriptionRepository interface {
    // existing
    Create(ctx, sub) (*Subscription, error)
    GetActiveByUserID(ctx, userID) (*Subscription, error)

    // new
    // UpsertByPaddleID: insert-or-update keyed on paddle_subscription_id. Idempotent.
    UpsertByPaddleID(ctx context.Context, sub *Subscription) (*Subscription, error)
    // GetByUserID: tất cả subscriptions của user.
    GetByUserID(ctx context.Context, userID int64) ([]*Subscription, error)
}
```

### 4. BillingService (`internal/service/billing_service.go`)

```go
type BillingService interface {
    // HandleEvent processes a verified Paddle webhook event. Idempotent.
    HandleEvent(ctx context.Context, event PaddleEvent) error
    // CurrentPlan trả plan hiện tại của user (active sub hoặc basic).
    CurrentPlan(ctx context.Context, userID int64) (*repository.Plan, *repository.Subscription, error)
    // GeneratePortalURL tạo Paddle Customer Portal session URL cho user.
    GeneratePortalURL(ctx context.Context, customerID string) (string, error)
}
```

**HandleEvent logic** (xử lý trong worker goroutine, không trong HTTP handler):

| Paddle event | Điều kiện | Action |
|---|---|---|
| `subscription.created` | — | UpsertByPaddleID: status=active, plan_id, paddle_price_id, period_end; lưu `paddle_customer_id` vào `users` |
| `subscription.updated` | price thay đổi, `planOrder[new] > planOrder[current]` (upgrade) | UpsertByPaddleID: update plan_id + paddle_price_id; **reset Redis quota counter** |
| `subscription.updated` | status=canceled (sau period_end Paddle deactivate) | UpsertByPaddleID: status=canceled — QuotaService fallback về basic |
| `subscription.canceled` | — | Set `canceled_at`; status vẫn `active` đến `current_period_end`; user dùng quota hiện tại đến hết kỳ |
| `transaction.completed` | renewal | UpsertByPaddleID: extend `current_period_end` |
| Khác | — | Log + ignore |

> **Detect upgrade**: Dùng plan order constant thay vì so quota. Định nghĩa thứ tự cứng: `basic=0 < pro=1 < business=2`. Lấy plan hiện tại từ DB, so order của plan mới (tra theo `price_id` mới trong `items[]`) với plan hiện tại. Nếu `order(new) > order(current)` → upgrade, ngược lại → ignore.

```go
var planOrder = map[string]int{
    "basic":    0,
    "pro":      1,
    "business": 2,
}
```

> **Reset on upgrade**: `QuotaService.Reset(ctx, userID)` — counter về 0, TTL còn lại của ngày giữ nguyên. Proration charge do Paddle xử lý qua `transaction.completed` riêng.

> **Downgrade workaround (document trong UI)**: User cancel subscription → vẫn dùng quota hiện tại đến `current_period_end` → sau đó subscribe lại gói thấp hơn.

**Unlimited quota (-1)**: QuotaService.DailyLimit() cần check `daily_link_quota == -1` → trả `math.MaxInt`. `Allow()` hiện dùng `int(n) > limit` — nếu limit = MaxInt, không bao giờ vượt.

### 5. WebhookHandler (`internal/handler/webhook_handler.go`)

```go
// POST /webhook/paddle
func (h *WebhookHandler) PaddleWebhook(c echo.Context) error {
    body, _ := io.ReadAll(io.LimitReader(c.Request().Body, 1<<20)) // 1MB limit

    // 1. Verify Paddle signature (HMAC-SHA256, header: Paddle-Signature)
    if !paddle.VerifySignature(body, c.Request().Header.Get("Paddle-Signature"), h.secret) {
        return c.NoContent(http.StatusUnauthorized)
    }

    // 2. Parse event type
    var evt PaddleEvent
    if err := json.Unmarshal(body, &evt); err != nil {
        return c.NoContent(http.StatusBadRequest)
    }

    // 3. Enqueue non-blocking — trả 200 ngay
    select {
    case h.queue <- evt:
        return c.NoContent(http.StatusOK)
    default:
        // Queue full = worker stuck (likely DB down). Return 503 so Paddle retries.
        // Do NOT return 200 here — that would silently drop a billing event.
        slog.Warn("paddle webhook queue full", "type", evt.EventType)
        return c.NoContent(http.StatusServiceUnavailable)
    }
}
```

**Signature verification** (`pkg/paddle/signature.go`):
```go
// Paddle Billing dùng TS:t=<timestamp>;h1=<hmac>
// HMAC-SHA256 của: timestamp + ":" + raw body
func VerifySignature(body []byte, header, secret string) bool { ... }
```

### 6. WebhookWorker goroutine

Khởi động cùng server trong `cmd/server/main.go`, shutdown gracefully qua context:

```go
func RunWebhookWorker(ctx context.Context, queue <-chan PaddleEvent, billing BillingService) {
    for {
        select {
        case evt := <-queue:
            // Retry với exponential backoff — đã trả 200, Paddle sẽ không retry.
            // Mất event billing = mất thông tin subscription, phải tự xử lý.
            var err error
            for attempt := range 3 {
                if err = billing.HandleEvent(ctx, evt); err == nil {
                    break
                }
                wait := time.Duration(1<<attempt) * time.Second // 1s, 2s, 4s
                slog.Warn("paddle event failed, retrying", "type", evt.EventType, "attempt", attempt+1, "wait", wait)
                time.Sleep(wait)
            }
            if err != nil {
                // Alert: mất event billing, cần investigate ngay
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

## Bulk Upload Quota Integration

Bulk upload phải tính chung quota với link tạo đơn — tổng links tạo trong ngày (đơn + bulk) không vượt plan limit.

### QuotaService — thêm Remaining()

```go
type QuotaService interface {
    Allow(ctx, userID) (bool, error)   // existing
    Release(ctx, userID)               // existing
    Reset(ctx, userID)                 // existing (added for billing)
    // Remaining trả số slot còn lại trong ngày. Fails open (MaxInt) nếu Redis unavailable.
    Remaining(ctx context.Context, userID int64) int
}
```

`Remaining()` = `DailyLimit(userID) - currentCount`. Nếu Redis unavailable → trả `math.MaxInt` (fail open, consistent với `Allow()`).

### Check points

**1. `BulkJobService.ConfirmUpload()` (primary gate)**

```go
func (s *bulkJobService) ConfirmUpload(ctx, ownerID, fileKey, filename string, rowCount int) (*repository.BulkJob, error) {
    remaining := s.quota.Remaining(ctx, ownerID)
    if rowCount > remaining {
        return nil, apperror.TooManyRequests(
            fmt.Sprintf("quota insufficient: need %d slots, only %d remaining today", rowCount, remaining),
        )
    }
    // proceed to create job...
}
```

Response `429 QUOTA_EXCEEDED` với message rõ: user biết cần xóa bao nhiêu row hoặc upgrade.

**2. `BulkJobWorker.Process()` (safety net + Kafka commit logic)**

```go
// process() — inner logic, trả lỗi nếu quota hết
func (w *BulkJobWorker) process(ctx, job) error {
    // Safety net: quota có thể đã giảm giữa ConfirmUpload và khi worker chạy
    // (user tạo link đơn trong khoảng đó, hoặc quota reset by subscription change).
    remaining := w.quota.Remaining(ctx, job.OwnerID)
    if job.TotalRows > remaining {
        return apperror.TooManyRequests("quota exceeded at processing time")
    }
    // BatchCreate tiêu thụ quota qua Allow() per-URL (existing behavior)...
}

// Process() — phân biệt permanent vs transient failure để Kafka biết có retry không
func (w *BulkJobWorker) Process(ctx, jobID) error {
    // ...
    if err := w.process(ctx, job); err != nil {
        _ = w.jobs.UpdateStatus(ctx, jobID, Failed, job.TotalRows)
        // Quota exceeded = permanent. Return nil → Kafka commit offset, không retry.
        // Transient errors (DB down, storage fail) return err → Kafka redelivers.
        if apperror.IsStatus(err, http.StatusTooManyRequests) {
            return nil
        }
        return err
    }
}
```

Job status = `failed`, result file không được tạo. User thấy lỗi khi poll job status.

> **Tại sao phân biệt?** Kafka consumer chỉ commit offset khi `Process()` trả `nil`. Nếu quota exceeded trả `error`, consumer sẽ không commit → Kafka redeliver → lần sau worker thấy `status=failed` → skip → commit → một round trip Kafka thừa. Return `nil` ngay khi biết là permanent failure để tránh redelivery vô nghĩa.

### Frontend check (UX layer)

Khi user chọn file để upload, frontend đọc row count và so với `remaining` từ `GET /api/subscription` response:

```
if (rowCount > subscription.quota_remaining) {
    showError(`File has ${rowCount} URLs but you only have ${subscription.quota_remaining} slots left today.
               Remove ${rowCount - subscription.quota_remaining} rows or upgrade your plan.`)
    disableUploadButton()
}
```

`GET /api/subscription` response thêm field `quota_remaining`:

```json
{
  "data": {
    "plan": { "code": "pro", "daily_link_quota": 500 },
    "quota_remaining": 347,
    "subscription": { ... }
  }
}
```

### Không consume quota trước khi BatchCreate

`BulkJobWorker` vẫn dùng `BatchCreate` → `Allow()` per-URL (existing flow). Không pre-reserve quota lúc `ConfirmUpload` — nếu job fail sau đó, không cần refund logic.

---

## API Endpoints

| Method | Path | Auth | Description |
|--------|------|------|-------------|
| `GET` | `/api/subscription` | Keycloak | Trả plan + sub hiện tại của user |
| `GET` | `/api/subscription/portal` | Keycloak | Redirect tới Paddle Customer Portal |
| `POST` | `/webhook/paddle` | None (sig verify) | Nhận Paddle webhook events |

`/api/subscription` response:
```json
{
  "data": {
    "plan": { "code": "pro", "name": "Pro", "daily_link_quota": 500, "price_cents": 900 },
    "subscription": {
      "status": "active",
      "billing_interval": "month",
      "current_period_end": "2026-08-14T00:00:00Z",
      "paddle_customer_id": "ctm_xxx"
    }
  }
}
```

---

## Frontend Design

### Pricing Page (mới)

Route: `/pricing` (hoặc section trong landing page)

**Layout**: 3-column card (Basic / Pro / Business)
- Toggle monthly/yearly ở trên — khi chuyển yearly, giá update và hiện "Save 17%"
- Card của plan đang active có badge "Current plan"
- Nút "Get started" / "Upgrade" / "Contact us" tùy plan
- Nút gọi `Paddle.Checkout.open()` với `priceId` tương ứng + `customData: { user_id }`

**Paddle.js integration**:
```javascript
// Khởi tạo 1 lần
Paddle.Initialize({ token: APP_CONFIG.paddleClientToken });

function startCheckout(priceId, userId) {
    Paddle.Checkout.open({
        items: [{ priceId, quantity: 1 }],
        customData: { user_id: String(userId) }
    });
}
```

`APP_CONFIG.paddleClientToken` được serve qua endpoint `/app-config.json` hiện có (thêm field `paddleClientToken` khi `PADDLE_ENABLED=true`, empty string khi disabled).

### Subscription status trong Dashboard

- Show plan badge ("Basic" / "Pro" / "Business") ở header/profile
- Nút "Manage subscription" → gọi `GET /api/subscription/portal` → redirect tới Paddle portal

---

## Idempotency

Paddle có thể deliver cùng 1 webhook nhiều lần. `UpsertByPaddleID` dùng `INSERT ... ON CONFLICT (paddle_subscription_id) DO UPDATE SET ...` → safe to replay. Không cần separate idempotency key table.

---

## Error Handling

| Scenario | Behavior |
|---|---|
| Paddle signature invalid | 401, log, Paddle không retry |
| Queue full (capacity 100) | 503 → Paddle retry theo schedule (72h window); xảy ra khi worker stuck, thường do DB down |
| DB write fails in worker | Retry 3× exponential backoff (1s/2s/4s); fail permanent → log critical + metric `billing_event_failed` |
| Upgrade mid-cycle | Reset counter → quota mới ngay; Paddle tính phí proration qua `transaction.completed` riêng |
| Downgrade attempt via Paddle API | `subscription.updated` với quota thấp hơn → bỏ qua event; plan/quota không đổi |
| Cancel → re-subscribe (downgrade workaround) | `subscription.canceled` → quota hiện tại đến period_end; `subscription.created` mới → plan mới active |
| Renewal | Extend `current_period_end`; quota reset (counter về 0 cho kỳ mới) |
| Paddle API down (portal URL) | 503, user retry manually |
| `PADDLE_ENABLED=false` | Webhook route không register; pricing page ẩn upgrade buttons; portal endpoint trả 404 |

---

## Testing Strategy

- **Unit**: `BillingService.HandleEvent()` — từng event type, idempotency, upgrade reset counter, downgrade event bị bỏ qua, renewal reset counter, unlimited quota (-1)
- **Unit**: `paddle.VerifySignature()` — valid sig, tampered body, expired timestamp
- **Unit**: `WebhookHandler` — enqueue → 200, invalid sig → 401, full queue → **503** (not 200)
- **Unit**: `QuotaService.Reset()` — counter về 0, TTL preserved
- **Integration**: Paddle Sandbox — upgrade flow; cancel + re-subscribe flow

---

## Implementation Risks

| Risk | Mitigation |
|---|---|
| DB write fails permanently after retries | `billing_event_failed` metric → alert; manual fix via admin |
| User cố downgrade qua Paddle API trực tiếp | Event bị bỏ qua, plan không đổi — document rõ trong UI "downgrade via cancel + re-subscribe" |
| Queue full during spike | Capacity 100 đủ cho billing volume |
| Paddle price ID hardcode ở frontend | Serve via `/app-config.json` (`paddlePriceIds` map) |
| `paddle_customer_id` không sync | Lưu vào `users` khi `subscription.created` — 1 customer per user |

---

## Environment Variables Mới

| Variable | Required | Description |
|---|---|---|
| `PADDLE_ENABLED` | No (default false) | Enable Paddle integration |
| `PADDLE_WEBHOOK_SECRET` | If enabled | `pdl_ntf_...` từ Paddle Dashboard |
| `PADDLE_CLIENT_TOKEN` | If enabled | Client-side token cho Paddle.js |

---

## File Changes Summary

**New files**:
- `pkg/paddle/signature.go` — HMAC-SHA256 sig verification
- `internal/service/billing_service.go` — HandleEvent, CurrentPlan, GeneratePortalURL
- `internal/handler/webhook_handler.go` — POST /webhook/paddle
- `internal/handler/subscription_handler.go` — GET /api/subscription, GET /api/subscription/portal
- `migrations/000012_add_billing_fields.up.sql` + `.down.sql`

**Modified files**:
- `configs/config.go` — thêm PaddleConfig
- `internal/repository/subscription_repository.go` — thêm UpsertByPaddleID, GetByUserID
- `internal/repository/user_repository.go` — thêm UpdatePaddleCustomerID
- `internal/service/quota_service.go` — handle unlimited (-1), thêm Reset()
- `internal/router/router.go` — register webhook + subscription routes
- `web/` — pricing page, plan badge, manage subscription button
- `cmd/server/main.go` — khởi động WebhookWorker goroutine

---

## Success Criteria

- Paddle Sandbox end-to-end: "Upgrade" → checkout → webhook → quota tăng ngay, counter reset
- Cancel flow: cancel → quota hiện tại đến period_end → hết kỳ → fallback basic
- Downgrade attempt qua Paddle API → `subscription.updated` bị bỏ qua → plan không đổi
- Replay cùng webhook 2 lần → DB state không đổi (idempotency)
- DB write fail → retry 3×, fail permanent → metric `billing_event_failed` tăng
- `PADDLE_ENABLED=false` → app chạy bình thường không cần credentials
