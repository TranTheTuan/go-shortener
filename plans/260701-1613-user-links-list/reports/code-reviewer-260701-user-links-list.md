# Code Review: List a User's Links (GET /api/links + SPA)

- **Date:** 2026-07-01
- **Reviewer:** code-reviewer
- **Scope:** repository/link_repository.go, service/link_service.go, handler/link_handler.go, router/router.go, service tests, handler test, web/{index.html,app.js,styles.css}
- **Build:** `go build ./...` green, `go vet` clean. Repo join NOT covered by any test (no `internal/repository/*_test.go` exists).

## Verdict summary
- Critical: 0
- High: 0
- Medium: 2
- Low: 4
- **Go / No-go: GO** (mergeable). No security or correctness blocker found. See untested-join note below.

---

## Focus-area findings

### 1. GORM correctness (highest risk) — PASS, but unverified by test
- `Model(&Link{})` + `Select("links.*, COUNT(clicks.id) AS total_clicks")` + `Group("links.id")` + `Scan(&[]*OwnedLink)` is correct GORM usage. `Scan` (not `Find`) maps result columns by name onto struct fields: `links.*` → embedded `Link` fields; alias `total_clicks` → `OwnedLink.TotalClicks` (snake→camel default mapping). This works.
- Postgres GROUP BY: `SELECT links.*` with `GROUP BY links.id` IS valid in Postgres because `links.id` is the PK — Postgres functionally-dependent-column rule lets non-aggregated columns of the same table be selected when grouping by the PK. Confirmed valid.
- LIMIT/OFFSET apply after aggregation (correct — pages of links, not of click rows). LEFT JOIN → click-less links COUNT = 0 (correct).
- **RISK (informational, not a defect):** The join is exercised by ZERO tests. Spec Testing Strategy line 71 called for a DB-gated repo test (owner's links / counts / newest-first / excludes other+unowned); it was not written. Everything above is verified by code reading + Postgres semantics, not execution.
  - **Merge risk assessment:** LOW. The query is textbook GORM and the PK-grouping is standard Postgres. The only realistic failure mode is a column-mapping surprise on `Scan` into an embedded struct — unlikely with gorm v1.31.1. Recommend one manual `make test` run against real Postgres (or add the gated repo test) before or right after merge; do not treat as a hard blocker.

### 2. Security / owner-scoping — PASS
- `ownerID` is `int64` sourced only from `appmw.UserIDFrom(c)` (server-set from verified Keycloak token in ctx `user_id`); never from a query/path param. No user-controlled owner. (M-2 below: 401 path.)
- `WHERE links.user_id = ?` is parameterized; unowned links have `user_id IS NULL` and never match `= <int>`, so API-key/unowned links are excluded. Other users' links excluded. Matches spec decision #3.
- limit/offset are ints passed to GORM `Limit()/Offset()` (parameterized) — no injection.

### 3. Pagination edge cases — PASS (one UI nit, see L-1)
- `ClampPaging`: limit ≤0→20, >100→100, offset<0→0. Correct, covered by service test.
- offset beyond total: GORM returns empty slice → `items:[]`, `total` still real. API correct.
- `total` from `CountByOwner` (unfiltered by paging) is consistent with the paged `items`. Correct.

### 4. Handler — PASS
- `atoiDefault("abc",0)`→0, `atoiDefault("-5",0)`→-5 (then clamped to 0 by ClampPaging), `atoiDefault("",0)`→0. Garbage/negative handled. Correct.
- `short_url = baseURL + "/" + short_code` — matches Create handler pattern.
- 401 path returns `apperror.New(401,...)` when `UserIDFrom` false. Correct.

### 5. Frontend — PASS (XSS-safe), minor nits below
- All API data rendered via `textContent` / `createElement`; no `innerHTML` of API data anywhere. `linkAnchor` sets `.href`+`.textContent`+`rel="noopener"`. XSS-safe. (See M-1 re: `a.href`.)
- reload-after-create wired: `wireCreateForm(api, links.reload)` → `onCreated?.()` resets offset=0 and reloads. Correct.
- Pager: prev disabled at offset 0; next disabled when `offset+PAGE>=total`. Correct.
- `td.textContent = it.total_clicks` (number) — DOM coerces number→string fine.

---

## Issues

### MEDIUM

**M-1 — `linkAnchor` sets `a.href` to server-built short_url without scheme guard**
- File: web/static/app.js:118-124 (and :190, :143)
- Issue: `a.href = shortURL`. `short_url` is `baseURL + "/" + short_code`. baseURL is server config and short_code is `[A-Za-z0-9]` generated, so today this is safe. But `.href` (unlike `.textContent`) is a navigation sink: if baseURL were ever mis-set to a `javascript:`-style value, clicking navigates. Defense-in-depth only — not exploitable given current baseURL source.
- Fix: none required now; if baseURL ever becomes user-influenced, validate scheme is http/https before assigning `.href`.

**M-2 — 401 error code inconsistency between List and rest of link API**
- File: internal/handler/link_handler.go:136
- Issue: List hand-rolls `apperror.New(http.StatusUnauthorized, "UNAUTHORIZED", "not authenticated")`. In practice the route is behind `keycloakMW`, so an unauthenticated caller is rejected by the middleware before reaching the handler — this branch is effectively dead in production (only hit in unit test). The bespoke code/message may differ from the middleware's 401 envelope, giving two different 401 shapes for the same endpoint.
- Fix: acceptable as a defensive guard; for consistency consider reusing `apperror.Unauthorized(...)` if such a constructor exists (grep shows `New/NotFound/Internal`; no `Unauthorized` helper — so current approach is the available option). Leave as-is or add a helper.

### LOW

**L-1 — Pager "Next" can present an empty page when total changes between loads**
- File: web/static/app.js:238,248
- Issue: `next` uses cached `total` from the last load; if links were deleted elsewhere (no delete endpoint today) or count shifts, `offset` could exceed real total → empty table but pager shown. Benign now (no delete feature). 
- Fix: on load, if `items.length===0 && offset>0`, step back a page. Not needed until a delete endpoint exists.

**L-2 — `expiryLabel` uses client clock; API includes expired links intentionally**
- File: web/static/app.js:171-175
- Issue: "expired" is computed from `Date.now()` (client tz/clock), not server. Cosmetic only; matches spec decision #4 (UI marks expired). Fine.

**L-3 — Repo integration test missing (spec line 71 deliverable not delivered)**
- File: internal/repository/ (no test file)
- Issue: Spec explicitly listed a DB-gated repo test for the join (counts, ordering, exclusion of other/unowned). Absent. See section 1 merge-risk note.
- Fix: add `link_repository_test.go` gated like other DB-integration tests; assert owner-scoping + total_clicks + newest-first + limit/offset.

**L-4 — Duplicated `short_url` construction**
- File: internal/handler/link_handler.go:80 and :150
- Issue: `h.baseURL + "/" + code` appears in Create and List (minor DRY). 
- Fix: optional tiny helper `h.shortURL(code)`.

---

## Positive observations
- Clean layering preserved (repo→service→handler→router); apperror + response envelope used consistently.
- Owner scoping is airtight: owner never user-controllable.
- Frontend is genuinely XSS-safe by construction (textContent/createElement discipline, shared `linkAnchor`/`copyButton` helpers).
- Service clamping + total behavior well tested; handler owner-scoping/shape/401 tested.
- Good comments explaining non-obvious choices (LEFT JOIN→0, dedup scope, cache fire-and-forget).

## Metrics
- Type safety: full (Go, no `interface{}` misuse in changed code).
- Test coverage: service (clamp/total/error) + handler (scope/shape/401) covered; **repo join = 0%** (DB-gated, not written).
- Lint/vet: clean; gofmt clean (per task).

## Unresolved questions
1. Is a real-Postgres `make test` run planned pre-merge to exercise the join, or is the DB-gated repo test (L-3) going to be added? This is the only residual risk.
2. Does an `apperror.Unauthorized` helper exist elsewhere, or is `apperror.New(401,...)` the intended convention for handler-level 401s? (M-2)
