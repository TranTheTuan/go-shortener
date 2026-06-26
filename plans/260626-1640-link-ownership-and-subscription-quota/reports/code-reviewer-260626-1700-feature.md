# Code Review — Link Ownership + Subscription & Daily Quota

- Date: 2026-06-26
- Reviewer: code-reviewer
- Scope: Part A (link ownership) + Part B (subscription & daily quota)
- Build/tests: green (`go build ./...`, `go test ./... -count=1`, gofmt clean) — verified `internal/...`, `pkg/redisbreaker/...`
- Verdict: **GO with fixes** — no critical/data-loss/security bugs; 2 high-priority correctness gaps worth fixing before/just-after merge.

## Files reviewed
authn.go, quota_check.go, duplicate_url_check.go, quota_service.go, dedup_cache.go, redisbreaker.go, link_service.go, link_handler.go, link_repository.go, router.go, plan_repository.go, subscription_repository.go, main.go, config.go, apperror.go, jwt.go, response.go, migrations 000006/7/8 + tests.

---

## Critical
None.

## High

### H1 — Dedup-reuse wrongly rejected with 429 when at quota limit (Redis fast-path missed)
`quota_check.go:30-33` + `link_service.go:70-76`.
Spec: "reusing the same URL I already shortened ... does NOT consume quota." The refund-on-reused path only runs *after* `next(c)`. But if the DedupCache fast-path missed (TTL expired while link still alive — explicitly an expected edge case per spec) and the user is at their limit, `Allow()` returns false and QuotaCheck returns 429 **before** `next` ever runs. The DB-dedup backstop in the handler never executes, so a user at quota who re-submits an already-shortened URL gets 429 instead of their existing link. Counter integrity is fine (rejected INCR is refunded), but the user-facing behavior violates the spec story.
Impact: medium-frequency once cache TTL < link lifetime; user blocked from a no-op reuse.
Recommendation: on quota rejection, do not 429 blindly — either (a) consult DB dedup before enforcing quota (move the per-owner DB dedup ahead of QuotaCheck, e.g. a second cheap dedup middleware or have QuotaCheck call a dedup-lookup hook), or (b) accept as known limitation and document it in the spec's Edge Cases (currently spec claims this path "still correct, still refunds" — it does not refund because the handler is skipped). Minimum: fix the spec/edge-case note; ideally close the gap.

### H2 — Quota counter can drift negative and silently grant extra daily quota
`quota_service.go:104-118`, `quota_check.go:38-39`.
`Release`/`decr` issue an unconditional `DECR` with no floor. Refunds fire from two independent places (QuotaCheck on `status>=400` OR `CtxLinkReused`; plus the in-`Allow` refund on over-limit). Scenarios that over-refund:
- Handler both reuses (`CtxLinkReused=true`) **and** the fast-path already returned 200 earlier — not concurrent, but `status>=400 || reused` is an OR; a reused create returns 201 so only one refund — OK normally.
- Real risk: a downstream write that sets status >=400 AFTER a dedup-reuse increments could double count; more importantly any future caller of `Release` plus a Redis key that was reset (Redis flush, TTL expiry mid-day) can DECR a key whose value is 0/absent → key becomes -1, -2..., effectively granting N extra slots before the user is throttled again.
Impact: quota under-counts after refund races / key resets; soft-limit so non-billing-critical (spec accepts Redis-as-source-of-truth caveat), but worth hardening.
Recommendation: clamp refunds — use a tiny Lua script `if redis.call('GET',KEYS[1]) and tonumber(...)>0 then return redis.call('DECR',KEYS[1]) end`, or `DECR` then `if n<0 then SET key 0`. Keeps counter >=0.

## Medium

### M1 — Shared breaker couples dedup and quota failure budgets (and `IsUnavailable` over-trips on non-Redis errors)
`main.go:94-98`, `redisbreaker.go:23-26,39-41`.
One `*Breaker` instance is shared by QuotaService + DedupCache. Consecutive-failure count is pooled, so dedup Redis hiccups can trip the breaker that also governs quota (acceptable: same Redis). But `ReadyToTrip` uses `ConsecutiveFailures` and `IsUnavailable` treats **any** non-nil error as "Redis unavailable / fail-open." A context-cancellation/deadline (client disconnect) returned by go-redis counts as a breaker failure AND triggers fail-open — i.e. transient client-side cancels erode the failure budget and quietly skip quota. `redis.Nil` is correctly excluded in DedupCache (`dedup_cache.go:45`) but the quota `Incr` path has no Nil case (INCR never returns Nil, so fine).
Recommendation: ignore `context.Canceled`/`context.DeadlineExceeded` from the *caller's* ctx when deciding `IsUnavailable` (don't fail-open silently on client cancel; let the request die naturally). Optional: separate breakers per subsystem if you want isolation.

### M2 — Dedup fast-path response shape & status diverge from the create contract
`duplicate_url_check.go:17-20,49-51` vs `link_handler.go:95-100`.
Fast-path returns `200 {short_url, reused}`; a normal create returns `201 {short_code, short_url, original_url, expires_at}`; the DB-backstop reuse also returns `201` (full body). So the same logical "reused" outcome yields different status (200 vs 201) and different JSON depending on whether the Redis cache was warm. Clients can't rely on a stable contract.
Recommendation: make the fast-path mirror the handler's reused response (same 201 + same fields, or standardize both reuse outcomes on 200). At minimum return `short_code`/`original_url` so the payload is consistent.

### M3 — `DailyLimit` swallows DB errors, can't distinguish "no sub" from "DB down"
`quota_service.go:67-78`.
`GetActiveByUserID` returning a real DB error (not ErrNotFound) is treated identically to "no active subscription" → falls to default plan. A paid user briefly gets basic limits during a DB blip. Spec says fail-open to basic on plan-lookup failure (intended), so this is by-design, but the silent `err == nil` check also hides genuine errors with no log on the sub-lookup branch (only the final fallback logs). Low-ish, but a paid user being silently downgraded mid-incident is surprising.
Recommendation: log at warn when `GetActiveByUserID` returns a non-ErrNotFound error before falling through.

### M4 — `int(n) > limit` conversion / no upper bound
`quota_service.go:103-104`. `n` is int64 from INCR; `int(n)` on a 64-bit platform is fine, but a corrupted/huge counter is silently truncated. Cosmetic given soft-limit. Recommendation: compare as int64 (`n > int64(limit)`).

## Low

### L1 — `Remember` TTL from handler ignores remaining-expiry edge
`link_handler.go:84-89`. Handler computes `ttl = time.Until(*link.ExpiresAt)`; if the link is reused and near expiry, ttl may be tiny/negative → `Remember` falls back to defaultTTL (dedup_cache.go:63-67), which can outlive the link. Cache-then-DB backstop still corrects it. Acceptable; note only.

### L2 — Body fully buffered in `DuplicateURLCheck` with no size guard
`duplicate_url_check.go:36`. `io.ReadAll` on the request body before the handler's bind; no `MaxBytesReader`. Create bodies are tiny, and Echo's bind would also read fully, so risk is low, but the read happens for every owned create regardless. Recommendation: rely on a global body-limit middleware (echo `BodyLimit`) so this read is bounded.

### L3 — `Authn` doesn't trim/secure-compare API key; minor parity with existing
`authn.go:34-37` uses a plain map lookup (constant-time-ish via map, fine) but doesn't trim the incoming key while `keySet` trims stored keys — a key with trailing space won't match. Matches existing `api_key.go` behavior, so consistent; non-issue functionally.

### L4 — Migration 000006 lacks index on `user_id` alone for FK `ON DELETE SET NULL`
`000006_add_link_owner.up.sql`. The composite `(user_id, original_url)` index covers the FK's left-most column, so cascade-nulling on user delete can use it. Fine. No action.

---

## Security review
- Quota cannot be bypassed by an authenticated user: key is server-derived from JWT `user_id` (`quota_service.go:61-62`); client cannot influence it. ✓
- Cross-user isolation: dedup key and quota key both namespace by `userID`; no way to read/affect another user's cache/counter. ✓
- Ownership stamping: owner read from validated JWT claims only (`link_handler.go:66-69`), never from body. API-key path leaves owner nil → unowned. ✓ Pointer aliasing: `owner = &id` takes address of a local loop-free copy each request — no aliasing bug.
- Fail-open is the intended posture (spec decision #7); it means a Redis outage lets users exceed quota. Acceptable for non-billing soft limit, but note: an attacker who can DoS Redis disables quota entirely. Documented risk, fine for now.
- `unauthorized`/401 messages don't leak which credential failed. ✓
- Stats endpoint inherits `Authn` (any JWT or API key) with no per-owner check — matches spec A4 (no ownership on stats). ✓

## Atomicity / concurrency
- INCR is atomic; TOCTOU avoided as designed. ✓
- Over-limit refund (DECR) is a separate non-atomic op, so two concurrent requests at the boundary can transiently both see `n<=limit` is not the risk — both INCR, the higher one refunds. Worst case a request at the exact boundary refunds and a concurrent one keeps the slot; net counter correct. No double-spend. ✓ (negative-drift is H2.)
- `Expire` only on `n==1` is fine; date-in-key governs correctness, TTL is cleanup. ✓

## Per-owner dedup correctness
- `GetByOwnerAndURL` builds `user_id IS NULL` vs `user_id = ?` correctly (`link_repository.go:67-73`). Null-owner (API-key) group dedups independently. ✓
- Service trims URL before both lookup and store; DedupCache `key()` also trims — Lookup(raw)/Remember(trimmed) hash-match. ✓
- Expired existing link → falls through to create (`link_service.go:72-77`). ✓

## Migrations
- 000006: nullable FK `ON DELETE SET NULL` matches A2; composite dedup index present. ✓ Down drops index then column. ✓
- 000007: unique index on `code`, seeds basic(10,0). ✓ Note: seed `INSERT` is not idempotent — re-running up after a partial/`IF NOT EXISTS` table-create-but-failed-seed could duplicate; golang-migrate version tracking makes this moot in practice. Low.
- 000008: partial unique `WHERE status='active'` enforces ≤1 active sub/user. ✓ FK to plans has no `ON DELETE` (RESTRICT default) — deleting a referenced plan is blocked; sensible. ✓ `idx_subscriptions_user_id` partly redundant with the partial unique index for active lookups but covers non-active scans. Minor.

## Consistency with existing patterns
- apperror/response envelope/repository Err mapping all followed. ✓
- Middleware→service direction: middleware imports `service` (DedupCache concrete type, QuotaService interface) — same direction as existing handlers; acceptable. DuplicateURLCheck depends on `*service.DedupCache` concrete (not interface) — minor; QuotaCheck uses the interface. Consider an interface for DedupCache for test symmetry (it's already test-backed via miniredis, so low priority).
- Files all < 200 LOC. ✓

## Positive observations
- Clean fail-open story, breaker isolates Redis stalls as intended.
- Atomic INCR + refund design is sound; rejected attempts refund so mid-day upgrades aren't blocked.
- `redis.Nil` correctly excluded from breaker failure accounting in DedupCache.
- Ownership derived strictly from server-side JWT; no client-trust surface.
- Good test coverage on middleware + quota service (miniredis), incl. fail-open-on-outage.

## Metrics
- New/changed files reviewed: ~17 + 3 migrations.
- Test files: 6 (~578 LOC); middleware + service packages pass.
- Repository/handler/router: no unit tests (integration-gated, consistent with existing repo).
- Lint/format: gofmt clean per task; no syntax/build issues.

## Unresolved questions
1. H1: is the "at-limit user re-submitting an existing URL gets 429" outcome acceptable, or must reuse always bypass quota even on cache miss? Spec wording implies the latter; current code does the former.
2. Should the dedup fast-path (200) and create/backstop-reuse (201) be unified into one contract? (M2)
3. Is a single shared breaker across dedup+quota the intended blast radius, or should they be isolated? (M1)
4. Should fail-open quota emit a metric/alert so a silent Redis outage (quota disabled) is observable in prod?
