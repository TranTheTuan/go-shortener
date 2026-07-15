---
status: pending
priority: P1
effort: ~20h
branch: test/update-read-heavy-test
tags: [backend, billing, api]
---

# Paddle Billing & Subscription Integration

## Spec
[spec-260714-1418-billing-subscription.md](../reports/spec-260714-1418-billing-subscription.md)

## Phases

| # | Phase | Status | Est |
|---|-------|--------|-----|
| 1 | [DB Migration + Config](./phase-01-db-migration-config.md) | pending | 3h |
| 2 | [QuotaService + BillingService](./phase-02-quota-billing-service.md) | pending | 6h |
| 3 | [Webhook Handler + Worker + Routes](./phase-03-webhook-handler-worker.md) | pending | 5h |
| 4 | [Bulk Upload Quota Integration](./phase-04-bulk-upload-quota.md) | pending | 3h |
| 5 | [Tests](./phase-05-tests.md) | pending | 3h |

## Key Dependencies

- Phase 1 must complete before any other phase (model changes, migration)
- Phase 2 depends on Phase 1 (Subscription model + PaddleConfig)
- Phase 3 depends on Phase 2 (BillingService + QuotaService interfaces)
- Phase 4 depends on Phase 2 (QuotaService.Remaining())
- Phase 5 tests all phases; run last

## Critical Notes

- Migration 000013 (existing 000012 taken by link_clicks_count)
- `PADDLE_ENABLED=false` → webhook route not registered, portal 404
- No downgrade: ignore `subscription.updated` when `planOrder[new] <= planOrder[current]`
- `IsStatus` helper missing from apperror — add in Phase 4
- `billing_event_failed` metric — add in Phase 3
