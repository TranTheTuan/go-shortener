# Phase 07 — Server Wiring & Worker Runner

## Context Links
- Design spec: `../reports/design-260704-1708-bulk-url-upload.md` (cmd/server changes, Outbox Relay)
- Pattern refs: `cmd/server/server.go`, `cmd/server/consumer.go`, `cmd/server/main.go`

## Overview
- **Priority:** P1
- **Status:** pending
- **Description:** Wire `BulkJobHandler` + routes into `runServer`; start `outboxRelay` goroutine; add `runBulkWorker()` runner and `"bulk-worker"` mode in `main.go`.

## Key Insights
- `runServer` needs: R2 client, bulkRepo, bulkSvc, bulkHandler, bulkProducer (for relay), and the relay goroutine tied to the shutdown `ctx`.
- R2 optional in dev: if `!cfg.R2.Enabled()`, log a warning and skip bulk wiring (server still boots) — mirrors Kafka's conditional wiring. Bulk endpoints then 503/absent. **Decision:** wire handler always but relay+producer only when both R2 and Kafka enabled; if not, relay is skipped (jobs sit pending). Keep it simple: warn-and-skip relay.
- `outboxRelay` lives in a new `cmd/server/outbox_relay.go` (orchestrates repo+producer at server scope; avoids events↔repo coupling questions).
- `runBulkWorker` mirrors `runAnalyzeConsumer`: load cfg, require Kafka + R2, open PG, build repo/linkSvc/R2/worker/consumer, `consumer.Run(ctx)`.

## Requirements
- **Functional:** server serves bulk routes + relays outbox; `main bulk-worker` consumes and processes.
- **Non-functional:** relay respects `ctx.Done()`; graceful shutdown unaffected.

## Architecture / Design
```go
// cmd/server/outbox_relay.go
func outboxRelay(ctx context.Context, repo repository.BulkJobRepository, producer events.BulkJobProducer, interval time.Duration)
```

## Related Code Files
- **Create:** `cmd/server/bulk_worker.go`, `cmd/server/outbox_relay.go`
- **Modify:** `cmd/server/server.go`, `cmd/server/main.go`

## Implementation Steps — server.go
1. After existing wiring, build R2 (guarded):
   ```go
   var bulkHandler *handler.BulkJobHandler
   if cfg.R2.Enabled() {
       r2, err := storage.NewR2Client(cfg.R2)
       if err != nil { return fmt.Errorf("r2 client: %w", err) }
       bulkRepo := repository.NewBulkJobRepository(db)
       bulkSvc := service.NewBulkJobService(bulkRepo, r2, cfg.Shortener.BaseURL)
       bulkHandler = handler.NewBulkJobHandler(bulkSvc)
   } else {
       slog.Warn("R2 not configured; bulk-upload endpoints disabled")
   }
   ```
2. Add `BulkJob: bulkHandler` to `router.Handlers{...}`. Router must nil-guard registration (only register bulk routes if handler non-nil) — add that guard in phase 06 `registerRoutes`, or pass always and let it be nil. **Update phase-06 note:** guard `if h.BulkJob != nil { ... }`.
3. Start relay after `ctx` is created (needs shutdown ctx), only when R2+Kafka enabled:
   ```go
   if cfg.R2.Enabled() && cfg.Kafka.Enabled() {
       bulkRepo := repository.NewBulkJobRepository(db) // or reuse from above
       bp, err := events.NewBulkJobProducer(cfg.Kafka)
       if err != nil { return fmt.Errorf("bulk producer: %w", err) }
       defer bp.Close()
       go outboxRelay(ctx, bulkRepo, bp, 5*time.Second)
   }
   ```
   (Refactor: build `bulkRepo` once, share.)

## Implementation Steps — outbox_relay.go
4. Implement loop per design: `time.NewTicker`; on tick `repo.PendingOutbox`; for each → `producer.Publish(ctx, BulkJobEvent{JobID: entry.JobID})`; on success `repo.MarkOutboxPublished(entry.ID)`; on error `continue` (retry next tick). Return on `ctx.Done()`.

## Implementation Steps — bulk_worker.go + main.go
5. `runBulkWorker()`:
   ```go
   cfg := configs.Load()
   if !cfg.Kafka.Enabled() { return errors.New("bulk-worker requires KAFKA_BROKERS") }
   if !cfg.R2.Enabled()   { return errors.New("bulk-worker requires R2_* config") }
   db := openPostgres(cfg)
   r2 := storage.NewR2Client(cfg.R2)
   linkRepo := repository.NewLinkRepository(db)
   linkCache := repository.NewLinkCacheRepository(rdb)   // needs Redis; or pass nil cache
   linkSvc := service.NewLinkService(linkRepo, linkCache, cfg.Shortener.CodeLength, cfg.Shortener.CacheTTL)
   bulkRepo := repository.NewBulkJobRepository(db)
   w := worker.NewBulkJobWorker(bulkRepo, linkSvc, r2, cfg.Shortener.BaseURL)
   consumer := events.NewBulkJobConsumer(cfg.Kafka, w)
   ctx, stop := signal.NotifyContext(...); defer stop()
   return consumer.Run(ctx)
   ```
   - Redis: worker's `linkSvc` cache is optional; if setting up Redis is heavy, pass `nil` cache (LinkService accepts nil). **Decision:** pass nil cache in worker (writes go to DB; redirect path warms cache later). Simpler, no Redis dep for worker.
6. `main.go`: add `case "bulk-worker": err = runBulkWorker()`; update the `default` error message to mention it.
7. `go build ./...`.

## Todo List
- [ ] R2 client + bulk wiring in `runServer` (guarded)
- [ ] `router.Handlers.BulkJob` + nil-guard registration
- [ ] `outboxRelay` goroutine started with shutdown ctx
- [ ] `cmd/server/outbox_relay.go`
- [ ] `runBulkWorker()` (nil cache for linkSvc)
- [ ] `main.go` `"bulk-worker"` case
- [ ] `go build ./...` passes

## Success Criteria
- `main server` boots with/without R2; `main bulk-worker` connects and consumes.
- Confirm → within ~5s outbox published → worker processes → job completed.

## Risk Assessment
- **Duplicated `bulkRepo` construction** — refactor to build once and share.
- **Relay leak on shutdown:** relay returns on `ctx.Done()`; producer `Close` deferred — OK.
- **Worker without Redis:** nil cache path must be safe (LinkService guards `cache == nil` — verified in service).

## Unresolved Questions
- Should relay also run inside the worker process for resilience, or only in server? (Server-only chosen — single writer, avoids double-publish.)
