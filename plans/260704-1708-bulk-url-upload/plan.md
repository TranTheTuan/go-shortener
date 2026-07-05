---
title: "Bulk URL Upload"
description: "Upload CSV/XLSX (≤10k URLs), async-shorten via worker, download result file."
status: pending
priority: P2
effort: ~20h
branch: feat/handle-sheet-upload
tags: [backend, kafka, r2, storage, worker, outbox]
created: 2026-07-04
---

# Bulk URL Upload — Implementation Plan

Async batch URL shortening. User uploads a fixed 2-column (`url`,`result`) CSV/XLSX
straight to R2 via presigned PUT (Content-MD5 integrity), confirms the job, an
outbox relay publishes to Kafka, and a separate `bulk-worker` process fills in
short codes and writes a result file back to R2.

## Context Links
- Design spec: `../reports/design-260704-1708-bulk-url-upload.md`
- Existing patterns: `cmd/server/{server,consumer,main}.go`, `internal/events/click_{producer,consumer}.go`,
  `internal/repository/link_repository.go`, `internal/service/link_service.go`,
  `internal/handler/link_handler.go`, `internal/router/router.go`, `configs/config.go`

## Architecture (one line)
`Frontend → presign PUT → R2 | confirm → INSERT job+outbox (tx) → relay(5s) → Kafka → bulk-worker → parse+shorten → result to R2 → job.completed`

## Phases

| # | Phase | Depends on | Status |
|---|-------|-----------|--------|
| 01 | [Config & migrations](phase-01-config-and-migrations.md) | – | pending |
| 02 | [R2 storage client](phase-02-r2-storage-client.md) | 01 | pending |
| 03 | [Repository layer (job + outbox tx)](phase-03-repository-layer.md) | 01 | pending |
| 04 | [Service & worker](phase-04-service-and-worker.md) | 02, 03 | pending |
| 05 | [Kafka events (producer/consumer)](phase-05-kafka-events.md) | 03, 04 | pending |
| 06 | [HTTP handlers & routes](phase-06-http-handlers-and-routes.md) | 04 | pending |
| 07 | [Server wiring & worker runner](phase-07-server-wiring.md) | 05, 06 | pending |
| 08 | [Tests](phase-08-tests.md) | 01–07 | pending |

## Key Dependencies (new)
- `github.com/minio/minio-go/v7` — R2 S3-compatible client (`PresignHeader`, `PutObject` w/ `SendContentMd5`)
- `github.com/xuri/excelize/v2` — XLSX read/write
- `encoding/csv` (stdlib) — CSV read/write

## Cross-cutting Principles
- YAGNI: no quota deduction, no SSE progress, no admin reset (out of scope).
- KISS: fixed 2-column template — no column detection.
- DRY: reuse `linkSvc.Create` (handles dedup + validation), reuse `buildKGOOpts`, follow existing repo/service/handler shapes.

## Global Risks
- Worker crash mid-job → job stuck `processing`. Idempotency guard: worker only processes `status=pending`.
- 422 not in `apperror` — use `apperror.BadRequest` (400) for validation, or add `UnprocessableEntity` helper (decided in phase 06).
- At-least-once Kafka delivery → dedup via status guard.

## Unresolved Questions
See end of phase-01 and phase-06.
