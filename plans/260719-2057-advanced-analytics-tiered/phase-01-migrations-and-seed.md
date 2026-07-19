# Phase 01 — Migrations + Seed

## Context Links
- Overview: [plan.md](plan.md)
- Existing plans table: `migrations/000007_create_plans_table.up.sql`, `000013_add_billing_fields.up.sql`
- Existing clicks table: `migrations/000003_create_clicks_table.up.sql`

## Overview
- **Priority:** P0 (blocks 03, 04)
- **Status:** pending
- Create 4 new tables (3 rollup + 1 feature-flag) + seed feature flags per plan.

## Key Insights
- `make migrate-create NAME=xxx` generates the pair; migrations run manually (not on startup).
- Existing plans: `basic` (free), `pro` ($9), `business` ($29). Seeded across 000007 + 000013.
- Rollup grain locked to **B (tách chiều)** → 3 tables, low cardinality each.
- No backfill → rollup tables start empty; safe to `migrate-up` then discard old clicks test data.

## Requirements
- **FR-1:** `plan_features(plan_id, feature_key, enabled)` — many features per plan.
- **FR-2:** `click_stats_daily(link_id, day, clicks)` — time-series (chart A).
- **FR-3:** `click_stats_referrer(link_id, day, referrer_domain, clicks)` — sources (chart B).
- **FR-4:** `click_stats_device(link_id, day, device, browser, os, clicks)` — device (chart D).
- **FR-5:** seed feature flags: pro+business → analytics.* enabled; basic → not present (= disabled).
- **NFR:** every rollup table has a UNIQUE constraint on its full grain (for UPSERT `ON CONFLICT`).

## Architecture
Each rollup keyed by `(link_id, day, <dims>)` with UNIQUE index → `INSERT ... ON CONFLICT DO UPDATE SET clicks = clicks + EXCLUDED.clicks`. FK `link_id → links(id) ON DELETE CASCADE` (matches clicks table).

## Related Code Files
**Create (via `make migrate-create`):**
- `migrations/000016_create_plan_features.up.sql` / `.down.sql`
- `migrations/000017_create_click_stats_daily.up.sql` / `.down.sql`
- `migrations/000018_create_click_stats_referrer.up.sql` / `.down.sql`
- `migrations/000019_create_click_stats_device.up.sql` / `.down.sql`

(Number = next after 000015; confirm with `ls migrations/` before creating.)

## Implementation Steps

1. `make migrate-create NAME=create_plan_features`
   ```sql
   -- up
   CREATE TABLE IF NOT EXISTS plan_features (
       id          BIGSERIAL   PRIMARY KEY,
       plan_id     BIGINT      NOT NULL REFERENCES plans(id) ON DELETE CASCADE,
       feature_key VARCHAR(64) NOT NULL,
       enabled     BOOLEAN     NOT NULL DEFAULT TRUE,
       created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
       updated_at  TIMESTAMPTZ NOT NULL DEFAULT now()
   );
   CREATE UNIQUE INDEX idx_plan_features_plan_key ON plan_features (plan_id, feature_key);

   -- Seed: pro + business get advanced analytics.
   INSERT INTO plan_features (plan_id, feature_key, enabled)
   SELECT p.id, f.key, TRUE
   FROM plans p
   CROSS JOIN (VALUES
       ('analytics.timeseries'),
       ('analytics.referrers'),
       ('analytics.devices')
   ) AS f(key)
   WHERE p.code IN ('pro', 'business')
   ON CONFLICT (plan_id, feature_key) DO NOTHING;
   ```
   ```sql
   -- down
   DROP TABLE IF EXISTS plan_features;
   ```

2. `make migrate-create NAME=create_click_stats_daily`
   ```sql
   -- up
   CREATE TABLE IF NOT EXISTS click_stats_daily (
       link_id BIGINT NOT NULL REFERENCES links(id) ON DELETE CASCADE,
       day     DATE   NOT NULL,
       clicks  BIGINT NOT NULL DEFAULT 0,
       PRIMARY KEY (link_id, day)
   );
   -- down: DROP TABLE IF EXISTS click_stats_daily;
   ```

3. `make migrate-create NAME=create_click_stats_referrer`
   ```sql
   -- up
   CREATE TABLE IF NOT EXISTS click_stats_referrer (
       link_id         BIGINT       NOT NULL REFERENCES links(id) ON DELETE CASCADE,
       day             DATE         NOT NULL,
       referrer_domain VARCHAR(255) NOT NULL,  -- '' stored as 'direct'
       clicks          BIGINT       NOT NULL DEFAULT 0,
       PRIMARY KEY (link_id, day, referrer_domain)
   );
   -- down: DROP TABLE IF EXISTS click_stats_referrer;
   ```

4. `make migrate-create NAME=create_click_stats_device`
   ```sql
   -- up
   CREATE TABLE IF NOT EXISTS click_stats_device (
       link_id BIGINT      NOT NULL REFERENCES links(id) ON DELETE CASCADE,
       day     DATE        NOT NULL,
       device  VARCHAR(20) NOT NULL,  -- desktop|mobile|tablet|bot|unknown
       browser VARCHAR(40) NOT NULL,
       os      VARCHAR(40) NOT NULL,
       clicks  BIGINT      NOT NULL DEFAULT 0,
       PRIMARY KEY (link_id, day, device, browser, os)
   );
   -- down: DROP TABLE IF EXISTS click_stats_device;
   ```

5. `make migrate-up` then verify with `make migrate-version`.

## Todo List
- [ ] Confirm next migration number (`ls migrations/`)
- [ ] Create plan_features migration + seed
- [ ] Create click_stats_daily migration
- [ ] Create click_stats_referrer migration
- [ ] Create click_stats_device migration
- [ ] `make migrate-up` + verify version
- [ ] Truncate old test clicks (`TRUNCATE clicks RESTART IDENTITY; UPDATE links SET clicks_count = 0;`)

## Success Criteria
- All 4 tables exist with correct PKs/UNIQUE constraints.
- Seed: `SELECT p.code, f.feature_key FROM plan_features f JOIN plans p ON p.id=f.plan_id` shows 3 rows each for pro + business, 0 for basic.
- `migrate-down NUM=4` cleanly reverses.

## Risk Assessment
- **PK vs UNIQUE for UPSERT:** using composite PRIMARY KEY doubles as the conflict target — simpler than separate UNIQUE index. OK.
- **referrer_domain length:** 255 covers any host; over-long referrers truncated by parser (phase 02).

## Security Considerations
- No PII beyond what clicks already stores. Rollup drops raw IP entirely (privacy win).

## Next Steps
- Phase 02 parsers produce the dim values these tables store.
- Phase 03 writes to these tables.
