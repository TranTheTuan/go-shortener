# Advanced Analytics + Tiered Feature Gating

**Status:** pending
**Created:** 2026-07-19
**Source spec:** brainstorm session (this plan encodes locked decisions)

## Goal

Nâng analytics từ "count + list 20 clicks gần nhất" lên charts/insights advanced,
gate theo subscription plan qua bảng feature-flags. Basic = như hiện tại; Pro/Business
= time-series + referrers + device/browser/OS.

## Locked Decisions (from brainstorm)

- **Insight scope:** A (time-series) + B (referrers) + D (device/browser/OS). KHÔNG Geo (bỏ GeoIP).
- **Gating:** bảng `plan_features(plan_id, feature_key, enabled)` — feature-flag linh hoạt.
- **Compute:** pre-aggregate rollup trong Kafka consumer (`CreateBatch`), KHÔNG on-the-fly.
- **Rollup grain:** 3 bảng tách chiều (time-series / referrer / device), upsert cùng 1 tx.
- **Backfill:** KHÔNG. Rollup tính từ thời điểm ra mắt. Clicks cũ = test data, xóa làm lại.
- **Parse write-time:** UA + referrer parse lúc consumer ghi, lưu sẵn; read chỉ SELECT.

## Architecture (1 dòng)

Write path mở rộng `CreateBatch` (parse UA/referrer → upsert 3 rollup tbảng trong tx sẵn có).
Read path mới: `GET /api/links/:code/analytics` → entitlement check (plan_features) → SELECT rollup.

## Phases

| # | Phase | Status | Blocked by |
|---|-------|--------|-----------|
| 01 | [Migrations + seed](phase-01-migrations-and-seed.md) | pending | — |
| 02 | [Parsers (`pkg/useragent`, `pkg/referrer`)](phase-02-parsers.md) | pending | — |
| 03 | [Rollup write path (repo + CreateBatch)](phase-03-rollup-write-path.md) | pending | 01, 02 |
| 04 | [Entitlement service (plan_features)](phase-04-entitlement-service.md) | pending | 01 |
| 05 | [Analytics read API](phase-05-analytics-read-api.md) | pending | 03, 04 |
| 06 | [Wiring + tests + docs](phase-06-wiring-tests-docs.md) | pending | 05 |

## Key Dependencies

- New lib: `github.com/mileusna/useragent` (zero-dep, pure Go UA parser).
- Reuses: existing Kafka consumer, `CreateBatch` tx pattern (sort-ID anti-deadlock),
  `plan`/`subscription` repos, `UserIDFrom` middleware, uniform response envelope.

## Non-Goals (YAGNI)

- No GeoIP / country charts.
- No historical backfill.
- No lazy/background rollup job.
- No per-feature UI paywall redesign (frontend gets minimal charts only).
- No rollup for basic-tier-only data already covered by `links.clicks_count`.

## Unresolved Questions

- Frontend chart lib choice (Chart.js vs vanilla SVG) — deferred to phase 06, default Chart.js CDN.
- Time-series bucket granularity exposed to API (hourly vs daily) — plan assumes daily rows,
  API accepts `?range=` (7d/30d/90d). Hourly deferred.
