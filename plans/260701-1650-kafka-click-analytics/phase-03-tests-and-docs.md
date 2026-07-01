# Phase 03 — Tests & Docs

**Context:** [plan.md](plan.md) · [brainstorm](../reports/brainstorm-260701-1650-kafka-click-analytics.md)

## Overview
- **Priority:** High (DoD gate)
- **Status:** pending
- Unit-test the batching/decoding + fallback; document the new binary + env; wire build/run.

## Related Code Files
- **Create:** `internal/events/click_consumer_test.go`, `internal/events/click_producer_test.go`
- **Modify:** `Makefile`, `README.md`, `.env.example`, `docker`/`Dockerfile` (optional consumer image)

## Implementation Steps

1. **Producer test** (no real Kafka):
   - Inline fallback: `NewInlineProducer` → `Publish` N events → the mock `ClickRepository` receives them (bounded worker drains); channel-full drop is tolerated.
   - `ClickEvent` JSON round-trips (marshal → the expected shape/keys).

2. **Consumer test** (no real Kafka) — isolate the pure logic:
   - Extract the **decode + batch-accumulate + flush** decision into a small pure function/struct (`batcher`) so it's testable without kgo. Test: decodes valid JSON → `Click`; skips/records poison (bad JSON) without aborting; flushes at `batchSize`; flushes on interval; final flush on shutdown; `CreateBatch` receives the accumulated rows.
   - (The kgo client wiring in `ClickConsumer.Run` stays a thin adapter; integration with a real broker is manual/out of scope.)
   - Add `createBatchFn` to the service-layer `mockClickRepo` (or a local mock) for assertions.

3. **Makefile** — add:
   ```
   run-consumer:   ## Run the click-consumer service.
       go run ./cmd/click-consumer
   build-consumer: ## Build the click-consumer binary.
       go build -o ./build/click-consumer ./cmd/click-consumer
   ```
   (Optional) a `Dockerfile.consumer` mirroring the server multi-stage build.

4. **README** — new "Click analytics (Kafka)" section: redirect publishes async (drop-on-failure, never blocks the 302); `cmd/click-consumer` batch-inserts into `clicks`; `Stats` is eventually-consistent; `KAFKA_*` env table; **empty `KAFKA_BROKERS` = inline fallback** for local dev. Note at-least-once / approximate counts.

5. **Gate:** `make build` (both binaries) && `make test` (all green) && `gofmt`/`make lint`.

## Todo
- [ ] producer test (inline fallback + JSON shape)
- [ ] consumer batcher test (decode/skip/flush-by-size/flush-by-time/final flush)
- [ ] Makefile `run-consumer`/`build-consumer` (+ optional Dockerfile.consumer)
- [ ] README Kafka section + `KAFKA_*` env docs
- [ ] `make build` + `make test` green

## Success Criteria
- Batching/decoding/fallback covered by unit tests (no broker needed); both binaries build; docs explain the async pipeline, the fallback, and the eventual-consistency/approximate trade-offs.

## Next
Plan complete → mark phases done. Manual E2E against the real Kafka cluster (brokers via env) validates the end-to-end flow. Follow-ups: metrics/Prometheus, partition/replica tuning, remove `AnalyticsService.Record` once the consumer fully owns writes.
