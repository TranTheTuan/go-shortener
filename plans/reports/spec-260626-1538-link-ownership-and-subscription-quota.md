# Design Spec: Link Ownership + User Subscription & Daily Quota

- **Date:** 2026-06-26
- **Status:** Pending user review
- **Scope:** One feature in two coherent parts:
  - **Part A — Link ownership:** associate created links with the authenticated `user_id`; let JWT users create links (alongside the existing API key); per-owner dedup.
  - **Part B — Subscription & quota:** per-user plans + daily shorten-link quota enforced at creation. Billing-ready data model; billing itself NOT implemented now.
  - Part A is the foundation Part B gates; they ship together.

## Problem Statement

Links currently have no owner and are created only via a shared static `X-API-Key`. Now that auth exists, links should belong to the authenticated user (Part A), and each user should have a usage limit: every user defaults to a **basic** plan = **10 shorten links/day**, checked at link creation, with a foundation a future billing system can extend to sell higher-quota plans without redesign (Part B).

## User Stories

- As a logged-in user, links I create are owned by me (`user_id` stamped from my JWT).
- As an API-key client, I can still create links; they have no owner (`user_id` null).
- As a user, reusing the same URL I already shortened returns my existing link (per-owner dedup) and does NOT consume quota.
- As a basic user, I can create up to 10 links per UTC calendar day; the 11th returns 429.
- As a user, my quota resets at 00:00 UTC.
- As a (future) paid user, an active subscription to a higher plan raises my daily quota immediately.
- As an operator, I add/price plans via the `plans` table without code changes.

## Dependencies (build order)

1. **Auth** (user identity / JWT) — **implemented** (committed).
2. **Part A — Link ownership** (this spec): `links.user_id`, combined `Authn` middleware that accepts JWT *or* API key and puts `user_id` in context on `/api/links`, per-owner dedup.
3. **Part B — Subscription & quota** (this spec): gates the JWT-authenticated creation Part A introduces.

> Quota *counting* is Redis-based and does NOT read `links.user_id`; it only needs the JWT `user_id` set by the combined `Authn` middleware. Part A provides that middleware.

## Decisions (locked)

### Part A — Link ownership

| # | Decision | Choice |
|---|---|---|
| A1 | Auth on `/api/links` | **Keep both**: combined `Authn` middleware — JWT sets `user_id`; API key → no `user_id`. Fail-closed if neither. |
| A2 | `links.user_id` | **Nullable** (API-key links unowned); FK `ON DELETE SET NULL` (links keep redirecting if owner deleted) |
| A3 | Dedup | **Per-owner**: reuse only the same owner's existing link (`WHERE original_url=? AND user_id=?` / `IS NULL`) |
| A4 | New endpoints | **None** (no list/delete/owner-stats); redirect stays public |

### Part B — Subscription & quota

| # | Decision | Choice |
|---|---|---|
| 1 | Plan model | `plans` table **+** `subscriptions` table (full billing shape) |
| 2 | Quota counting | Redis daily counter (`INCR`/TTL) |
| 3 | Window | **Calendar-day UTC** — key holds the UTC date |
| 4 | Enforcement | **Middleware** before create handler, atomic `INCR`/`DECR` |
| 5 | Default plan | Absence of an active subscription → fall back to `basic` (registration untouched) |
| 6 | API-key (ownerless) creation | Not subject to per-user quota (skipped) |
| 7 | Redis failure | **Circuit breaker** (`sony/gobreaker`): 10 consecutive failures → Open 5m → fail-open (skip quota) |
| 8 | Dedup vs quota | `DuplicateURLCheck` middleware (Redis) **before** quota; reused link returns early, no quota consumed |

## Architecture

Middleware chain on `POST /api/links` (authenticated path):

```
Authn ─► DuplicateURLCheck ─► QuotaCheck ─► LinkHandler.Create ─► LinkService.Create
 (sets        (Redis fast-path:     (Redis INCR/limit/DECR        (DB dedup backstop;
  user_id)     hit → 302-style        via circuit breaker)          warms dedup cache)
               early return)
```

- `user_id` absent (API-key/ownerless) → both DuplicateURLCheck and QuotaCheck are **skipped**; request falls through to the service-layer dedup (null-owner group).

### Part A — Link ownership

**Schema** (`000006_add_link_owner`):
```sql
ALTER TABLE links ADD COLUMN user_id BIGINT REFERENCES users(id) ON DELETE SET NULL;
CREATE INDEX idx_links_user_id_original_url ON links (user_id, original_url); -- per-owner dedup lookups
```
`repository.Link` gains `UserID *int64 json:"user_id,omitempty"`.

**Combined `Authn(issuer, apiKeys)` middleware** (replaces `APIKey` on the `/api` group):
- Valid `Authorization: Bearer <jwt>` → parse, set `user_id` in context.
- Else valid `X-API-Key` → allow, no `user_id`.
- Neither → 401 (fail-closed). Stats endpoint inherits it (no owner check, per A4).

**Repository** — replace `GetByOriginalURL(ctx, url)` with `GetByOwnerAndURL(ctx, ownerID *int64, url)` (`user_id = ?` or `user_id IS NULL`).

**LinkService.Create** — gains `ownerID *int64` (read from context via `UserIDFrom`); dedups per owner; stamps `UserID` on new links. Handler passes the context owner; signals "reused" for the quota refund path (Part B).

### Part B — Subscription & quota

**`plans`** (`000007_create_plans_table`):
```sql
CREATE TABLE plans (
    id               BIGSERIAL PRIMARY KEY,
    code             VARCHAR(50)  NOT NULL,        -- "basic", "pro", ...
    name             VARCHAR(255) NOT NULL,
    daily_link_quota INT          NOT NULL,
    price_cents      INT          NOT NULL DEFAULT 0,
    is_active        BOOLEAN      NOT NULL DEFAULT TRUE,
    created_at       TIMESTAMPTZ  NOT NULL DEFAULT now(),
    updated_at       TIMESTAMPTZ  NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX idx_plans_code ON plans (code);
INSERT INTO plans (code, name, daily_link_quota, price_cents)
VALUES ('basic', 'Basic', 10, 0);
```

**`subscriptions`** (`000008_create_subscriptions_table`):
```sql
CREATE TABLE subscriptions (
    id                   BIGSERIAL PRIMARY KEY,
    user_id              BIGINT      NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    plan_id              BIGINT      NOT NULL REFERENCES plans(id),
    status               VARCHAR(20) NOT NULL DEFAULT 'active', -- active|canceled|expired
    current_period_start TIMESTAMPTZ NOT NULL DEFAULT now(),
    current_period_end   TIMESTAMPTZ,                            -- NULL = open-ended
    created_at           TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at           TIMESTAMPTZ NOT NULL DEFAULT now()
);
-- At most one active subscription per user.
CREATE UNIQUE INDEX idx_subscriptions_active_user
    ON subscriptions (user_id) WHERE status = 'active';
CREATE INDEX idx_subscriptions_user_id ON subscriptions (user_id);
```

GORM entities: `repository.Plan`, `repository.Subscription` (matching tags).

### Components

**PlanRepository** — `GetByCode(ctx, code) (*Plan, error)`, `GetByID(ctx, id) (*Plan, error)`.

**SubscriptionRepository** — `Create(ctx, *Subscription)`, `GetActiveByUserID(ctx, userID) (*Subscription, error)` (joins plan or returns sub with PlanID; service resolves plan).

**redisbreaker** (new small wrapper, `pkg/redisbreaker` or inside service) — wraps a `*redis.Client` op with a `gobreaker.CircuitBreaker`. Settings: `MaxConsecutiveFailures=10`, `OpenTimeout=5m`, `HalfOpen` single probe. When Open or op errors → caller treats Redis as unavailable.

**DedupCache** (encapsulates the `user:links:{uid}:{urlHash}` namespace, breaker-wrapped):
- `Lookup(ctx, userID, url) (shortURL string, found bool)` — used by DuplicateURLCheck mw.
- `Remember(ctx, userID, url, shortURL string, ttl)` — used by LinkService after a new create.
- `urlHash = sha256_hex(TrimSpace(url))`. TTL = link expiry remaining, else default (reuse `SHORTENER_CACHE_TTL`).

**QuotaService** (breaker-wrapped Redis):
- `DailyLimit(ctx, userID) (int, error)` — active subscription → its plan `daily_link_quota`; no active sub → `basic` plan quota. (Plan lookups cacheable later.)
- `Allow(ctx, userID) (allowed bool, err error)` — resolve limit; `key = user:quota:{userID}:{UTCdate}`; `INCR`; if result `==1` → `EXPIRE 48h` (cleanup; correctness from the date in key); if result `> limit` → `DECR` + return `false`; else `true`.
- `Release(ctx, userID)` — `DECR key` (refund).
- `now func() time.Time` injected (UTC date + tests).
- Breaker Open / Redis error → **fail-open**: `Allow` returns `true` (log warn), `Release` no-op.

**Middleware `DuplicateURLCheck(dedup DedupCache)`** (after Authn, `POST /api/links`):
1. `user_id` absent → `next()`.
2. `Lookup` hit → write `200` envelope with existing `short_url` (+ `"reused": true`), return (no quota, no DB).
3. miss / Redis-unavailable → `next()`.

**Middleware `QuotaCheck(quota QuotaService)`** (after DuplicateURLCheck):
1. `user_id` absent → `next()`.
2. `Allow()` → `false` → **429 `QUOTA_EXCEEDED`**.
3. `next()`.
4. After: if `c.Response().Status >= 400` **or** create signalled "reused" (ctx flag set by handler on DB-dedup backstop) → `Release()` (refund).

**LinkService.Create** (extended by link-ownership to take `ownerID *int64`):
- DB dedup backstop via `GetByOwnerAndURL`; on reuse set a context/return signal so QuotaCheck refunds.
- On new create: `DedupCache.Remember(ownerID, url, shortURL, ttl)`.

### Error Handling

- Add `apperror.TooManyRequests(msg)` → `429 / "QUOTA_EXCEEDED"`.
- Redis/breaker errors: never surface to client on the quota path (fail-open); log at warn.
- Plan/subscription DB error in `DailyLimit`: fail-open with basic-quota fallback (never block creation on plan-lookup failure); log.

## Config (new `QUOTA_` / breaker knobs)

| Var | Default | Purpose |
|-----|---------|---------|
| `QUOTA_DEFAULT_PLAN_CODE` | `basic` | Fallback plan when no active subscription |
| `QUOTA_BASIC_FALLBACK_LIMIT` | `10` | Last-resort limit if `plans` lookup fails |
| `QUOTA_BREAKER_MAX_FAILURES` | `10` | Consecutive Redis failures to trip breaker |
| `QUOTA_BREAKER_OPEN_TIMEOUT` | `5m` | Open duration before half-open probe |

## Edge Cases

- **TOCTOU:** atomic `INCR` (no read-then-write race).
- **Plan upgrade mid-day:** limit re-resolved per request → takes effect immediately; `DECR`-on-reject keeps counter truthful.
- **Failed DB insert:** QuotaCheck refunds on response `>= 400`.
- **Dedup:** Redis fast-path avoids quota entirely; DB backstop path refunds via handler signal; ownerless dedup handled by service (no quota involved).
- **Dedup cache TTL expiry while link alive:** falls through to DB dedup backstop (still correct, still refunds).
- **Redis down:** breaker Open → quota skipped (fail-open) + dedup fast-path skipped (DB dedup still works).

## Testing Strategy

- **Part A — combined `Authn` mw:** JWT sets `user_id` + passes; valid API key passes with no `user_id`; neither → 401. **Per-owner dedup:** same owner + URL reuses; different owner → separate link; null-owner group dedups independently. Update existing link service tests/mocks for `GetByOwnerAndURL`.
- **QuotaService** (Redis via `alicebob/miniredis` or store interface mock): `DailyLimit` basic-fallback vs active-pro sub; `Allow` under limit; reject at limit (+`DECR`); `Release` decrements; breaker-open → fail-open allow.
- **DedupCache:** `Remember` then `Lookup` hit; miss; hash stability.
- **DuplicateURLCheck mw:** hit returns early (no next); miss calls next; no `user_id` → next.
- **QuotaCheck mw:** skip when no `user_id`; 429 over limit; refund on downstream `>=400`; refund on reused signal; pass under limit.
- **Repos:** plan/subscription lookups (integration, gated like existing suite).
- `make build` + `make test` green; no weakened assertions.

## New Dependencies

- `github.com/sony/gobreaker` (circuit breaker)
- `github.com/alicebob/miniredis/v2` (test-only Redis)

## Risks

- **Complexity creep:** circuit breaker + dedup middleware + two-layer dedup add moving parts. Mitigated by isolating each behind a small interface; each unit independently testable.
- **Redis as quota source of truth:** a Redis flush resets counts (users get fresh quota). Acceptable for non-billing daily quota; revisit when billing introduces hard caps.
- **Dedup-cache/DB divergence:** TTL-bounded; DB backstop guarantees correctness.

## Success Criteria

- 11th link in a UTC day → 429 `QUOTA_EXCEEDED`; counter resets next UTC day.
- Active higher-plan subscription raises limit immediately.
- Reused URL returns existing short link without consuming quota.
- Redis outage → links still creatable (fail-open) with no per-request timeout stalls after breaker trips.
- API-key (ownerless) creation unaffected by quota.

## Open Questions

- Should a future `GET /me/quota` endpoint expose remaining quota to clients? (Deferred; not in this scope.)
- When billing lands: does canceling a paid plan downgrade to basic immediately or at period end? (Billing-spec concern.)
- Existing API-key link tests/mocks must update for the `GetByOwnerAndURL` signature change (Part A) — touch point, not a blocker.

## Next Steps

`/plan` this spec → phased plan. Suggested phases:
1. **Part A** — migration 000006 + `links.user_id` + combined `Authn` middleware + per-owner dedup (repo/service/handler) + tests.
2. **Part B data** — migrations 000007/000008 + plan/subscription repos.
3. **Part B logic** — QuotaService + DedupCache + circuit breaker.
4. **Part B wiring** — `DuplicateURLCheck` + `QuotaCheck` middlewares, route wiring, `apperror.TooManyRequests`.
5. **Tests & docs** — service/middleware tests (miniredis), README + Swagger.
