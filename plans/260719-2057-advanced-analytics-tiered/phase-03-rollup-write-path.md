# Phase 03 — Rollup Write Path

## Context Links
- Overview: [plan.md](plan.md)
- Existing write path: `internal/repository/click_repository.go` (`CreateBatch` — READ THIS)
- Consumer: `internal/events/click_consumer.go`, `cmd/server/consumer.go`
- Depends on: phase 01 (tables), phase 02 (parsers)

## Overview
- **Priority:** P0 (core of the feature; blocks 05)
- **Status:** pending
- Extend the existing `CreateBatch` tx to parse each click and upsert 3 rollup tables — reuse the sort-ID anti-deadlock pattern already there.

## Key Insights
- `CreateBatch` ALREADY: inserts clicks + groups by link_id + sorts IDs before updating `links.clicks_count` (deadlock-safe). We extend the SAME tx — no new consumer, no new infra.
- Rollup aggregation happens **in Go memory** over the batch (like `linkClickMap`), then a bounded set of UPSERTs.
- Parse UA/referrer via phase-02 parsers — parse each click once here, never on read.
- Day bucket = `click.ClickedAt.UTC()` truncated to date.

## Requirements
- **FR-1:** for each click in batch, derive `(day, referrer_domain, device, browser, os)`.
- **FR-2:** aggregate counts per rollup grain in memory, then UPSERT with `ON CONFLICT ... DO UPDATE SET clicks = clicks + EXCLUDED.clicks`.
- **FR-3:** all upserts inside the existing `CreateBatch` transaction (atomic with click insert + count update).
- **FR-4:** deterministic lock ordering across ALL tables (sort keys) to preserve deadlock safety across pods.
- **NFR:** no extra round trips per click — batch the upserts (GORM `CreateInBatches` or `clause.OnConflict`).

## Architecture
New `ClickStatsRepository` owns rollup UPSERT + read queries (isolation from raw click CRUD). But the WRITE upserts execute inside `CreateBatch`'s tx, so the aggregation helper must accept the `*gorm.DB` tx handle. Two clean options — pick **A**:

- **A (chosen):** `CreateBatch` builds the aggregates and calls unexported helpers on `clickRepository` that run within `tx`. Keeps one tx, one owner of the write path. `ClickStatsRepository` handles READ only.
- B: inject `ClickStatsRepository` into `clickRepository` — more wiring, splits the tx owner. Rejected (KISS).

Aggregation model (in-memory maps keyed by struct):
```go
type dailyKey    struct{ LinkID int64; Day time.Time }
type refKey      struct{ LinkID int64; Day time.Time; Domain string }
type deviceKey   struct{ LinkID int64; Day time.Time; Device, Browser, OS string }
```
Sort each key slice deterministically (LinkID, then Day, then remaining dims) before UPSERT → consistent lock order.

## Related Code Files
**Create:**
- `internal/repository/click_stats_repository.go` — models (`ClickStatsDaily`, `ClickStatsReferrer`, `ClickStatsDevice`) + `ClickStatsRepository` interface (READ methods) + GORM impl.

**Modify:**
- `internal/repository/click_repository.go` — extend `CreateBatch` tx with rollup aggregation + upserts. Import `pkg/useragent`, `pkg/referrer`. **Watch 200-line limit** — extract rollup aggregation into a separate file `internal/repository/click_rollup_write.go` (same package) if `click_repository.go` grows past ~200 lines.

## Implementation Steps

1. Define rollup GORM models + table names in `click_stats_repository.go`:
   ```go
   type ClickStatsDaily struct {
       LinkID int64     `gorm:"primaryKey"`
       Day    time.Time `gorm:"primaryKey;type:date"`
       Clicks int64
   }
   func (ClickStatsDaily) TableName() string { return "click_stats_daily" }
   // ...Referrer, Device similarly with composite primaryKey tags.
   ```

2. In `CreateBatch`, AFTER the existing click insert + `clicks_count` update, build aggregates:
   ```go
   daily := map[dailyKey]int64{}
   refs  := map[refKey]int64{}
   devs  := map[deviceKey]int64{}
   for _, c := range clicks {
       day := c.ClickedAt.UTC().Truncate(24 * time.Hour) // date bucket
       daily[dailyKey{c.LinkID, day}]++
       refs[refKey{c.LinkID, day, referrer.Domain(c.Referrer)}]++
       r := useragent.Parse(c.UserAgent)
       devs[deviceKey{c.LinkID, day, r.Device, r.Browser, r.OS}]++
   }
   ```

3. Convert each map → sorted slice of rows, then UPSERT within `tx`:
   ```go
   tx.Clauses(clause.OnConflict{
       Columns:   []clause.Column{{Name:"link_id"},{Name:"day"}},
       DoUpdates: clause.Assignments(map[string]any{
           "clicks": gorm.Expr("click_stats_daily.clicks + EXCLUDED.clicks"),
       }),
   }).CreateInBatches(&dailyRows, 500)
   ```
   Repeat for referrer (conflict cols +domain) and device (conflict cols +device,browser,os). Return error → tx rolls back → Kafka redelivers (at-least-once already accepted; counts approximate — consistent with existing `clicks_count` behavior).

4. Sort each row slice by full key before `CreateInBatches` so lock acquisition order is stable across pods (same rationale as existing linkID sort).

5. `go build ./...`; run existing consumer tests + add a repo test that feeds a mixed batch and asserts the 3 rollup tables hold correct aggregated counts.

## Todo List
- [ ] Create `click_stats_repository.go` (models + READ interface + impl)
- [ ] Extend `CreateBatch`: in-memory aggregation over batch
- [ ] Sorted UPSERT for daily / referrer / device within tx
- [ ] Extract to `click_rollup_write.go` if file > 200 lines
- [ ] Repo test: mixed batch → assert rollup counts
- [ ] `go build ./...` + `go test ./internal/repository/...`

## Success Criteria
- One batch of clicks produces correct summed rows in all 3 rollup tables.
- Re-delivered (duplicate) batch increments counts (accepted approximate semantics) without deadlock.
- `click_repository.go` stays under 200 lines (or split done).

## Risk Assessment
- **Tx size:** many distinct keys in one batch → many upsert rows. Bounded by batch size (Kafka poll) × distinct dims per batch; `CreateInBatches(…,500)` chunks it. Acceptable.
- **Double-count on redelivery:** already the accepted model for `clicks_count`; rollups inherit same approximate guarantee. Documented, not fixed (YAGNI — exactly-once not required).
- **Lock order:** MUST sort every rollup slice; unsorted upserts across 3 tables from 2 pods can deadlock. Covered by step 4.

## Security Considerations
- Rollups store no raw IP → reduces PII footprint vs `clicks`.
- Parsers handle untrusted UA/referrer (phase 02).

## Next Steps
- Phase 05 reads these tables via `ClickStatsRepository` read methods.
