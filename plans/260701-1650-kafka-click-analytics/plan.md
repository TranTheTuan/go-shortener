---
status: pending
created: 2026-07-01
slug: kafka-click-analytics
spec: ../reports/brainstorm-260701-1650-kafka-click-analytics.md
---

# Plan: Kafka-based Click Analytics (async producer + separate consumer)

Move click recording off the redirect request path. Redirect becomes an **async,
non-blocking Kafka producer** (drop-on-failure — the 302 never depends on Kafka);
a new **`cmd/click-consumer`** binary consumes the topic and **batch-inserts** into
the shared Postgres `clicks` table. At-least-once, **approximate** counts (no dedup).
Client: **franz-go**. Local dev without Kafka keeps working via a fallback.

**Brainstorm:** [brainstorm-260701-1650-kafka-click-analytics.md](../reports/brainstorm-260701-1650-kafka-click-analytics.md)

## Principles

YAGNI / KISS / DRY. Reuse `configs`, `pkg/database`, `internal/repository` across
both binaries (monorepo). Producer/consumer behind small interfaces for testing.
**#1 rule:** redirect availability must never depend on Kafka (async + drop-on-failure).

## Phases

| # | Phase | Status | Depends on |
|---|-------|--------|-----------|
| 1 | [Config, producer & redirect swap](phase-01-producer-and-redirect.md) | pending | — |
| 2 | [Batch insert + consumer service](phase-02-consumer-service.md) | pending | 1 |
| 3 | [Tests & docs](phase-03-tests-and-docs.md) | pending | 1,2 |

## Key Dependencies

- `github.com/twmb/franz-go` (kgo) — producer + consumer group
- Existing: GORM/Postgres (`clicks` table), Echo redirect handler, `configs`
- Kafka brokers provided via `KAFKA_BROKERS` env (cluster already running)

## Definition of Done

- Redirect latency/availability unaffected by Kafka up/down; 302 never blocked/failed.
- Click → Kafka → consumer → `clicks`; `Stats`/`total_clicks` reflect it within the batch window.
- Broker outage → clicks dropped (metric/log), redirect fine, consumer resumes on recovery.
- `cmd/click-consumer` is an independently deployable binary reusing the repo layer.
- `KAFKA_BROKERS` empty → inline fallback (local dev). `make build` + `make test` green.
