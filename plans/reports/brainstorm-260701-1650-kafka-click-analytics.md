# Brainstorm: Kafka-based Click Analytics (async, separate consumer)

- **Date:** 2026-07-01
- **Type:** Technical architecture (brainstorm)
- **Status:** Agreed — ready for /plan

## Problem

Redirect handler records clicks via **one unbounded `go func()` per request** doing a fire-and-forget `INSERT` into `clicks`. Issues: unbounded goroutines (pool exhaustion under spikes), per-click write amplification, at-most-once (already accepted). Want: move click recording off the request path into a **separate consumer service**, events via **Kafka**.

## Driver & constraints (decided)

- **Driver:** learning / portfolio — build a real event-driven microservice split. Kafka chosen eyes-open (over-engineered for the actual load, but that's the point).
- **Separate deployable consumer** is a hard requirement.
- **Kafka already running** elsewhere; brokers provided via env. No infra work.
- **Approximate counts OK** → no exactly-once, no dedup, no idempotency table.

## Approaches evaluated

| Option | Infra | Verdict |
|---|---|---|
| A. In-process worker + buffered channel + batch INSERT | none | Fixes goroutine/perf, no service split — rejected (want separate service) |
| B. Redis Streams consumer | reuse Redis | Lighter, ~90% of Kafka's value — rejected (learning goal is Kafka) |
| **C. Kafka + separate consumer service** | Kafka (exists) | **Chosen** — matches the learning/portfolio + separate-service driver |

## Final design

```
Redirect handler ──async, non-blocking, key=link_id──► Kafka "link-clicks" ──► cmd/click-consumer
   Publish(ClickEvent); DROP on failure                  (JSON events)          consumer group
   NEVER blocks/fails the 302                                                   │ batch (N or T)
                                                                                ▼
                                                                   Postgres `clicks` (batch INSERT)
                                                                                ▲
                                                        Stats endpoint reads (eventually-consistent)
```

### Decisions (locked)
- **Serialization:** JSON (v1). Protobuf/Schema-Registry deferred.
- **Code layout:** monorepo — new `cmd/click-consumer` binary, **reuses `internal/repository`** (DRY), separate deployable.
- **Storage:** consumer writes the **shared Postgres `clicks` table**; `Stats` endpoint unchanged (now eventually-consistent).
- **Delivery:** at-least-once (commit offsets *after* batch insert); approximate → occasional dup on rebalance acceptable.
- **Go client:** `github.com/twmb/franz-go` (pure Go, high-perf; async `Produce` with promise callback + consumer-group `PollFetches`/`CommitRecords`).

### Components

**Producer (main app):**
- New `internal/events` (or `pkg/kafka`) `ClickProducer`: kafka-go `Writer{Async:true}`, `Key=link_id`, `Completion` callback logs/meters errors.
- `Publish(ClickEvent)` is non-blocking; buffer bounded → **drop + metric on full/broker-down** (this replaces `go func()` and fixes the unbounded-goroutine bug).
- Redirect handler: swap the `go func(){ analytics.Record }()` for `producer.Publish(evt)`.
- **Fallback:** `KAFKA_BROKERS` empty → keep the current inline recording (local dev without Kafka). Set → publish.

**Topic:** `link-clicks`, key = `link_id` (per-link ordering + even spread), a few partitions; consumers ≤ partitions.

**ClickEvent (JSON):** `{ link_id, clicked_at (RFC3339), referrer, ip_address, user_agent }` (+ optional `event_id` UUID for tracing).

**Consumer (`cmd/click-consumer`):**
- kafka-go `Reader{GroupID}`; decode JSON → `repository.Click`; buffer up to **N (e.g. 500) or T (e.g. 1s)**; `ClickRepository.CreateBatch(batch)`; **then** commit offsets.
- Reuses `configs`, `pkg/database`, `internal/repository`. Graceful shutdown flushes the buffer. Poison message → log + skip (no DLQ v1).

**Repository:** add `ClickRepository.CreateBatch(ctx, []*Click) error` (GORM `CreateInBatches`).

**Config (`KAFKA_` prefix):** `BROKERS` ([]string csv), `CLICK_TOPIC` (default `link-clicks`), `CONSUMER_GROUP` (default `click-consumer`), batch `SIZE`/`INTERVAL`. Shared `configs.Config` used by both binaries.

## Risks / honest caveats

- **You now own a distributed system:** eventual consistency (stats lag = batch window + consumer lag), a new failure domain (consumer down → analytics gap), at-least-once **over-count** on crashes (accepted: approximate).
- **Redirect availability must never depend on Kafka** — enforced by async + drop-on-failure. #1 rule.
- **Local dev** without Kafka handled by the `KAFKA_BROKERS`-empty fallback.
- Ordering preserved per link (key=link_id), though counts don't require it.

## Success criteria

- Redirect latency unaffected by Kafka up/down; 302 never blocked/failed by analytics.
- Clicks flow Kafka → consumer → `clicks`; `Stats`/`total_clicks` reflect them within the batch window.
- Broker outage → clicks dropped (metric), redirect fine, consumer resumes on recovery.
- `cmd/click-consumer` is an independently deployable binary reusing the repo layer.
- `make build` + `make test` green; producer/consumer unit-testable behind interfaces.

## Open questions

- Partition count + consumer replicas (tune to load; start 3 partitions / 1–2 consumers).
- Metrics backend (slog only v1; Prometheus later) — deferred.
- Whether to keep `AnalyticsService.Record` (single insert) as a fallback path or remove once the consumer owns writes.

## Next steps

`/plan`: (1) config + `ClickProducer` + kafka-go dep + redirect swap (+ fallback); (2) `ClickRepository.CreateBatch`; (3) `cmd/click-consumer` (reader → batch → insert → commit, graceful shutdown); (4) tests (producer publish, consumer decode+batch, fallback path) + docs + Makefile/Dockerfile for the new binary.
