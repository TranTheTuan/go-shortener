---
phase: 5
status: pending
priority: P1
depends_on: phase-01, phase-02, phase-03, phase-04
---

# Phase 5: Tests

## Context Links
- Spec: [spec-260714-1418-billing-subscription.md](../reports/spec-260714-1418-billing-subscription.md)
- Phase 2: [phase-02-quota-billing-service.md](./phase-02-quota-billing-service.md)
- Phase 3: [phase-03-webhook-handler-worker.md](./phase-03-webhook-handler-worker.md)
- Phase 4: [phase-04-bulk-upload-quota.md](./phase-04-bulk-upload-quota.md)

## Overview
Unit tests covering all new logic paths: `QuotaService` additions,
`BillingService.HandleEvent`, `WebhookHandler`, and `BulkJobWorker` quota failure handling.
Follow existing test patterns: table-driven, hand-written mocks in `*_test.go`.

## Key Insights
- Existing `mocks_test.go` in `internal/service/` has mock patterns to follow
- `QuotaService` tests need a mock Redis via miniredis (check if already used) or spy on `rdb.Client`
- `BillingService` tests mock `SubscriptionRepository`, `UserRepository`, `PlanRepository`, `QuotaService`
- `WebhookHandler` tests use `httptest.NewRecorder` + Echo's test helpers
- `BulkJobWorker` quota test: mock `QuotaService.Remaining()` → 0, verify `Process()` returns nil

## Test Files

| File | Tests |
|------|-------|
| `internal/service/quota_service_test.go` | `Reset`, `Remaining`, unlimited quota |
| `internal/service/billing_service_test.go` | `HandleEvent` all event types |
| `internal/handler/webhook_handler_test.go` | sig valid/invalid, queue full |
| `pkg/paddle/signature_test.go` | valid sig, tampered body |
| `internal/worker/bulk_job_worker_test.go` | quota exceeded → nil return |

## Implementation Steps

### 1. pkg/paddle/signature_test.go

```go
func TestVerifySignature(t *testing.T) {
    secret := "test_secret"
    body := []byte(`{"event_type":"subscription.created"}`)
    ts := "1700000000"

    mac := hmac.New(sha256.New, []byte(secret))
    mac.Write([]byte(ts + ":" + string(body)))
    validSig := hex.EncodeToString(mac.Sum(nil))
    header := fmt.Sprintf("ts=%s;h1=%s", ts, validSig)

    cases := []struct {
        name   string
        body   []byte
        header string
        want   bool
    }{
        {"valid", body, header, true},
        {"tampered body", []byte(`{"event_type":"tampered"}`), header, false},
        {"missing header", body, "", false},
        {"wrong sig", body, "ts=1700000000;h1=deadbeef", false},
    }
    for _, tc := range cases {
        t.Run(tc.name, func(t *testing.T) {
            got := VerifySignature(tc.body, tc.header, secret)
            if got != tc.want {
                t.Errorf("got %v, want %v", got, tc.want)
            }
        })
    }
}
```

### 2. QuotaService — Reset and Remaining tests

Add to `internal/service/quota_service_test.go`:

**Reset**: set counter to 5, call Reset(), assert counter is 0, TTL still set.

**Remaining**:
- plan limit=10, used=3 → remaining=7
- plan limit=-1 (unlimited) → math.MaxInt
- Redis unavailable → math.MaxInt (fail open)

### 3. BillingService.HandleEvent tests

Table-driven in `internal/service/billing_service_test.go`:

| Case | Input | Expected |
|------|-------|----------|
| subscription.created | new sub | UpsertByPaddleID called, UpdatePaddleCustomerID called |
| subscription.updated (upgrade pro→business) | planOrder[new]>planOrder[current] | Upsert + quota.Reset called |
| subscription.updated (downgrade) | planOrder[new]<=planOrder[current] | no upsert, no Reset |
| subscription.updated (status=canceled) | status=canceled in data | Upsert with status=canceled |
| subscription.canceled | — | canceled_at set, status=active |
| transaction.completed | renewal | current_period_end extended |
| unknown event type | — | returns nil, no DB calls |
| idempotency | same event replayed | UpsertByPaddleID called (idempotent by design) |

### 4. WebhookHandler tests

In `internal/handler/webhook_handler_test.go`:

```go
func TestPaddleWebhook(t *testing.T) {
    // helper: build valid header for body
    makeHeader := func(body []byte, secret, ts string) string { ... }

    cases := []struct {
        name       string
        body       []byte
        header     string
        queueFull  bool
        wantStatus int
    }{
        {"valid sig enqueued", validBody, validHeader, false, 200},
        {"invalid sig", validBody, "ts=x;h1=bad", false, 401},
        {"malformed json", []byte("not-json"), validHeader, false, 400},
        {"queue full", validBody, validHeader, true, 503},
    }
    // For queue full: create chan of cap 0 (or pre-fill cap 1)
}
```

### 5. BulkJobWorker — quota exceeded returns nil

In `internal/worker/bulk_job_worker_test.go`, add case:

```go
{
    name:          "quota exceeded at processing time → nil (no kafka retry)",
    quotaRemaining: 0,
    jobTotalRows:   100,
    wantErr:        nil,        // nil = Kafka commits offset
    wantJobStatus:  "failed",
},
```

Verify `Process()` returns `nil` and job status is set to `failed`.

## Todo List

- [ ] Create `pkg/paddle/signature_test.go` — 4 cases (valid, tampered, missing header, wrong sig)
- [ ] Add `Reset` tests to `internal/service/quota_service_test.go`
- [ ] Add `Remaining` tests (normal, unlimited, Redis down) to quota_service_test.go
- [ ] Create `internal/service/billing_service_test.go` — 8 HandleEvent cases
- [ ] Create `internal/handler/webhook_handler_test.go` — 4 cases
- [ ] Add quota-exceeded-nil-return case to `internal/worker/bulk_job_worker_test.go`
- [ ] Run `go test ./...` — all pass

## Success Criteria
- All new tests pass
- No regressions in existing test suite
- `go test ./...` exits 0

## Risk Assessment
- BillingService mock complexity: 4 dependencies (subRepo, userRepo, planRepo, quotaSvc) — follow existing mock pattern in `mocks_test.go`
- miniredis availability: if not already in go.mod, use interface-based mock for Redis in quota tests

## Security Considerations
- Signature test covers tampered body → ensures HMAC check is over actual bytes

## Next Steps
- All phases complete → feature ready for Paddle Sandbox end-to-end validation
