---
slug: change-subscription-interval
status: completed
spec: plans/reports/brainstorm-260717-1604-change-subscription-interval.md
---

# Plan: ChangeSubscription — interval + tier change

Refactor `UpgradeSubscription` to allow changing billing interval (monthly ↔ yearly)
alongside plan tier, while keeping the no-downgrade rule on tier rank.

## Phases

| # | Phase | Status | File |
|---|-------|--------|------|
| 1 | Service layer | completed | [phase-01-service.md](phase-01-service.md) |
| 2 | Handler layer | completed | [phase-02-handler.md](phase-02-handler.md) |
| 3 | Frontend (app.js) | completed | [phase-03-frontend.md](phase-03-frontend.md) |

## Key files

- `internal/service/billing_service.go` — interface + implementation
- `internal/handler/subscription_handler.go` — request struct + handler
- `web/static/app.js` — button condition + API call body

## Dependencies

Phase 2 depends on Phase 1 (handler calls the renamed service method).
