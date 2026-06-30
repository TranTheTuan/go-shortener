---
status: completed
created: 2026-06-26
completed: 2026-06-26
slug: link-ownership-and-subscription-quota
spec: ../reports/spec-260626-1538-link-ownership-and-subscription-quota.md
---

# Plan: Link Ownership + Subscription & Daily Quota

One feature, two parts. **Part A** stamps `user_id` on links (combined JWT-or-API-key
auth on `/api/links`, per-owner dedup). **Part B** adds plans + subscriptions and a
daily shorten-link quota (Redis counter, calendar-day UTC, circuit breaker), enforced
by middleware before link creation. Billing-ready data model; billing NOT built.

**Spec:** [spec-260626-1538-link-ownership-and-subscription-quota.md](../reports/spec-260626-1538-link-ownership-and-subscription-quota.md)

## Principles

YAGNI / KISS / DRY. Mirror existing `handler → service → repository` layering,
uniform `response.Envelope`, `apperror` typed errors. Files < 200 LOC. Existing
static `X-API-Key` flow must keep working; redirect stays public.

## Phases

| # | Phase | Status | Depends on |
|---|-------|--------|-----------|
| 1 | [Part A — link ownership](phase-01-link-ownership.md) | ✅ done | — (auth done) |
| 2 | [Part B — plans & subscriptions data](phase-02-plans-subscriptions-data.md) | ✅ done | 1 |
| 3 | [Part B — quota service, dedup cache, breaker](phase-03-quota-service-and-breaker.md) | ✅ done | 2 |
| 4 | [Part B — middlewares & wiring](phase-04-middlewares-and-wiring.md) | ✅ done | 3 |
| 5 | [Tests & docs](phase-05-tests-and-docs.md) | ✅ done | 4 |

## Outcome (260626)

Implemented + tested (56 tests pass; build/vet/gofmt green). Code review: GO, 0 critical.
Post-review fixes applied:
- **H1** at-limit reuse no longer 429s — quota decision deferred to after dedup
  (`CtxQuotaExhausted` flag; service enforces 429 only for a genuinely new link).
- **H2** quota counter floored at 0 on refund (no negative drift).
- **M3** log when an active subscription's plan lookup fails (no silent paid downgrade).
Deferred (acceptable): M1 breaker fail-open on client-cancel, M2 dedup fast-path 200 vs 201 contract, M4 int64/int compare. Reports in `reports/`.

## Key Dependencies

- `github.com/sony/gobreaker` — circuit breaker around Redis
- `github.com/alicebob/miniredis/v2` — test-only in-memory Redis
- Existing: Echo v4, GORM, go-redis, golang-jwt, golang-migrate
- Migrations: `000006` (links.user_id), `000007` (plans + seed basic), `000008` (subscriptions)

## Definition of Done

- JWT user → links stamped with their `user_id`; API-key links unowned; redirect public.
- Per-owner dedup (same owner+URL reuses; different owners isolated).
- 11th link in a UTC day → `429 QUOTA_EXCEEDED`; resets next UTC day; reused URL doesn't consume quota.
- Active higher-plan subscription raises the limit immediately; no sub → basic (10/day).
- Redis outage → breaker opens, link creation continues (fail-open), no per-request stalls.
- `make build` + `make test` green; static `X-API-Key` flow unchanged; README + Swagger updated.
