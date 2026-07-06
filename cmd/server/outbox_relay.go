package main

import (
	"context"
	"log/slog"
	"time"

	"github.com/TranTheTuan/go-shortener/internal/events"
	"github.com/TranTheTuan/go-shortener/internal/repository"
)

// outboxRelay polls the bulk_job_outbox table at the given interval and
// publishes pending entries to Kafka. FOR UPDATE SKIP LOCKED inside RelayOutbox
// makes it safe to run from multiple server replicas simultaneously.
func outboxRelay(ctx context.Context, repo repository.BulkJobRepository, producer events.BulkJobProducer, interval time.Duration) {
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			relayOnce(ctx, repo, producer)
		}
	}
}

// relayOnce is extracted for testability — runs one relay cycle without sleeping.
func relayOnce(ctx context.Context, repo repository.BulkJobRepository, producer events.BulkJobProducer) {
	err := repo.RelayOutbox(ctx, func(jobID int64) error {
		return producer.Publish(ctx, events.BulkJobEvent{JobID: jobID})
	})
	if err != nil && ctx.Err() == nil {
		slog.Warn("outbox relay error", "error", err)
	}
}
