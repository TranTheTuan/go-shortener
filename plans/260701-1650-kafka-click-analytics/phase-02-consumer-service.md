# Phase 02 — Batch Insert + Consumer Service

**Context:** [plan.md](plan.md) · [brainstorm](../reports/brainstorm-260701-1650-kafka-click-analytics.md)

## Overview
- **Priority:** High
- **Status:** pending
- New `cmd/click-consumer` binary: consume `link-clicks`, batch-insert into Postgres `clicks`, commit after write (at-least-once).

## Related Code Files
- **Modify:** `internal/repository/click_repository.go` (add `CreateBatch`)
- **Create:** `internal/events/click_consumer.go` (consume loop, decode, batch, insert, commit),
  `cmd/click-consumer/main.go`

## Implementation Steps

1. **`click_repository.go`** — add to `ClickRepository`:
   ```go
   CreateBatch(ctx context.Context, clicks []*Click) error
   ```
   Impl: `r.db.WithContext(ctx).CreateInBatches(clicks, 500).Error` (no-op on empty slice).

2. **`internal/events/click_consumer.go`** — a reusable consumer runner (testable independent of Kafka via a small `fetcher` seam, or keep the kgo client here and test the batch/decode logic separately):
   ```go
   type ClickConsumer struct {
       cl        *kgo.Client
       clicks    repository.ClickRepository
       batchSize int
       interval  time.Duration
   }
   func NewClickConsumer(brokers []string, topic, group string, clicks repository.ClickRepository, batchSize int, interval time.Duration) (*ClickConsumer, error) {
       cl, err := kgo.NewClient(
           kgo.SeedBrokers(brokers...),
           kgo.ConsumerGroup(group),
           kgo.ConsumeTopics(topic),
           kgo.DisableAutoCommit(), // manual commit AFTER insert = at-least-once
       )
       ...
   }
   // Run polls fetches, accumulates decoded clicks, flushes on batchSize or interval,
   // inserts via CreateBatch, then CommitRecords for the flushed records. Poison
   // messages (bad JSON) are logged + skipped. Returns on ctx cancel after a final flush.
   func (c *ClickConsumer) Run(ctx context.Context) error { ... }
   ```
   Flush logic: keep a buffer of `(*Click, *kgo.Record)`; on `len>=batchSize` or ticker tick, `CreateBatch(clicks)` then `cl.CommitRecords(ctx, recs...)`; clear buffer. On decode error: log + skip (don't add to buffer, but still commit its offset with the batch so it isn't reprocessed forever).

3. **`cmd/click-consumer/main.go`** — mirror `cmd/server/main.go`'s bootstrap:
   - `configs.Load()`; require `cfg.Kafka.Enabled()` (else fatal "consumer needs KAFKA_BROKERS").
   - `database.NewPostgres(cfg.Database...)` → `repository.NewClickRepository(db)`.
   - `NewClickConsumer(cfg.Kafka.Brokers, cfg.Kafka.ClickTopic, cfg.Kafka.ConsumerGroup, clickRepo, cfg.Kafka.BatchSize, cfg.Kafka.BatchInterval)`.
   - `signal.NotifyContext(SIGINT,SIGTERM)` → `consumer.Run(ctx)`; graceful shutdown flushes the buffer + closes the client.

4. `go build ./...`.

## Key Insights
- **At-least-once:** commit only after a successful batch insert. A crash between insert and commit re-delivers → duplicate rows → over-count. Acceptable (approximate).
- Consumers in the group ≤ topic partitions for parallelism.
- The consumer reuses `configs`, `pkg/database`, `internal/repository` (same module) — no duplication.

## Todo
- [ ] `ClickRepository.CreateBatch`
- [ ] `ClickConsumer` (poll → decode → batch(N/T) → CreateBatch → CommitRecords; skip poison)
- [ ] `cmd/click-consumer/main.go` (bootstrap + graceful shutdown)
- [ ] `go build ./...`

## Success Criteria
- Consumer drains `link-clicks` and batch-inserts into `clicks`; offsets commit after write; graceful shutdown flushes; builds as a standalone binary.

## Next
Phase 03 tests + docs.
