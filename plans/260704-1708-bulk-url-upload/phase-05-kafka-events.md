# Phase 05 — Kafka Events (producer + consumer)

## Context Links
- Design spec: `../reports/design-260704-1708-bulk-url-upload.md` (bulk_job_producer.go, bulk_job_consumer.go, Outbox Relay)
- Pattern refs: `internal/events/click_producer.go` (`buildKGOOpts`), `internal/events/click_consumer.go`

## Overview
- **Priority:** P1
- **Status:** pending
- **Description:** `BulkJobProducer` (synchronous `Publish` returning error — outbox relay needs success/fail), `BulkJobConsumer` (mirrors ClickConsumer, one job per message → `worker.Process`), and the `outboxRelay` loop.

## Key Insights
- **Difference from ClickProducer:** click producer is fire-and-forget (`TryProduce`). Bulk producer MUST return error so the relay only marks published on success → use `ProduceSync` (blocking, returns error).
- Reuse `buildKGOOpts(cfg)` (unexported, same package `events`) — DRY.
- Consumer follows click pattern: `DisableAutoCommit`, `PollFetches`, `CommitRecords` after successful `Process`. At-least-once → worker idempotency (phase 04) handles dupes.
- Event payload minimal: `{JobID}`. Worker re-fetches job by ID (has FileKey/OwnerID) → smaller message, single source of truth.

## Requirements
- **Functional:** publish job event; consume and dispatch to worker; manual commit on success.
- **Non-functional:** publish blocks ≤ produce timeout; consumer commits per-record batch.

## Architecture / Design
```go
// bulk_job_producer.go
type BulkJobEvent struct { JobID int64 `json:"job_id"` }

type BulkJobProducer interface {
    Publish(ctx context.Context, ev BulkJobEvent) error
    Close()
}
func NewBulkJobProducer(cfg configs.KafkaConfig) (BulkJobProducer, error)

// bulk_job_consumer.go
type BulkJobConsumer struct {
    cl     *kgo.Client
    worker *worker.BulkJobWorker
}
func NewBulkJobConsumer(cfg configs.KafkaConfig, w *worker.BulkJobWorker) (*BulkJobConsumer, error)
func (c *BulkJobConsumer) Run(ctx context.Context) error
```

## Related Code Files
- **Create:** `internal/events/bulk_job_producer.go`, `internal/events/bulk_job_consumer.go`
- **Note:** `outboxRelay` lives in `cmd/server/` (phase 07) since it wires repo+producer at server scope. Keep it there, not in `events` (avoids import cycle events→repository is fine, but relay orchestrates both).

## Implementation Steps
1. **Producer:**
   ```go
   func NewBulkJobProducer(cfg configs.KafkaConfig) (BulkJobProducer, error) {
       opts := append(buildKGOOpts(cfg), kgo.SeedBrokers(cfg.Brokers...))
       cl, err := kgo.NewClient(opts...)
       if err != nil { return nil, err }
       return &bulkJobProducer{cl: cl, topic: cfg.BulkJobTopic}, nil
   }
   func (p *bulkJobProducer) Publish(ctx context.Context, ev BulkJobEvent) error {
       payload, _ := json.Marshal(ev)
       key := strconv.AppendInt(nil, ev.JobID, 10)
       return p.cl.ProduceSync(ctx, &kgo.Record{Topic: p.topic, Key: key, Value: payload}).FirstErr()
   }
   func (p *bulkJobProducer) Close() { p.cl.Close() }
   ```
2. **Consumer:** copy ClickConsumer skeleton; opts add `kgo.ConsumerGroup(cfg.BulkConsumerGroup)`, `kgo.ConsumeTopics(cfg.BulkJobTopic)`, `kgo.DisableAutoCommit()`.
3. `Run`: `PollFetches`; for each record decode `BulkJobEvent`; call `c.worker.Process(ctx, ev.JobID)`; on success append to commit list; commit records after loop. On `Process` error → log, DO NOT commit that record (redelivery). (Simplest: process sequentially, commit only successfully-processed records.)
   - Poison (undecodable) message → log + commit (skip forever), matching click pattern.
4. `go build ./...`.

## Todo List
- [ ] `BulkJobEvent` + `BulkJobProducer` interface
- [ ] `NewBulkJobProducer` with `ProduceSync`
- [ ] `BulkJobConsumer` + `Run` (manual commit, per-record)
- [ ] Idempotent dispatch to `worker.Process`
- [ ] `go build ./...` passes

## Success Criteria
- Producer returns error on broker failure (relay retries).
- Consumer dispatches exactly to worker; failed jobs redeliver; poison messages skipped.

## Risk Assessment
- **Import cycle:** `events` importing `worker` and `worker` importing `service` — verify no cycle back to `events`. Worker does not import events → safe.
- **Commit granularity:** committing a whole batch when one job fails would lose redelivery. Commit per successfully-processed record only.
- **ProduceSync latency:** relay is a background 5s loop, blocking is fine.

## Unresolved Questions
- One job per Kafka message assumed (10k URLs handled inside one worker invocation). Confirmed by design (message = jobID).
