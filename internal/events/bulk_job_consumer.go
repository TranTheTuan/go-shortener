package events

import (
	"context"
	"encoding/json"
	"log/slog"

	"github.com/twmb/franz-go/pkg/kgo"

	"github.com/TranTheTuan/go-shortener/configs"
	"github.com/TranTheTuan/go-shortener/internal/worker"
)

// BulkJobConsumer consumes the bulk-link-jobs topic and dispatches each job to
// the worker. Offsets are committed manually after successful processing so
// failed jobs are redelivered (at-least-once). The worker is idempotent via the
// status guard (only processes pending jobs).
type BulkJobConsumer struct {
	cl     *kgo.Client
	worker *worker.BulkJobWorker
}

func NewBulkJobConsumer(cfg configs.KafkaConfig, w *worker.BulkJobWorker) (*BulkJobConsumer, error) {
	opts := append(
		buildKGOOpts(cfg),
		kgo.SeedBrokers(cfg.Brokers...),
		kgo.ConsumerGroup(cfg.BulkConsumerGroup),
		kgo.ConsumeTopics(cfg.BulkJobTopic),
		kgo.DisableAutoCommit(),
	)
	cl, err := kgo.NewClient(opts...)
	if err != nil {
		return nil, err
	}
	return &BulkJobConsumer{cl: cl, worker: w}, nil
}

// Run polls Kafka and dispatches jobs until ctx is cancelled.
func (c *BulkJobConsumer) Run(ctx context.Context) error {
	for {
		fetches := c.cl.PollFetches(ctx)

		if ctx.Err() != nil {
			c.cl.Close()
			return ctx.Err()
		}
		if fetches.IsClientClosed() {
			return nil
		}

		var toCommit []*kgo.Record

		fetches.EachRecord(func(rec *kgo.Record) {
			var ev BulkJobEvent
			if err := json.Unmarshal(rec.Value, &ev); err != nil {
				// Poison message — skip forever (matches click consumer pattern).
				slog.Warn("bulk consumer: decode failed, skipping", "error", err)
				toCommit = append(toCommit, rec)
				return
			}

			if err := c.worker.Process(ctx, ev.JobID); err != nil {
				// Do not commit — record will be redelivered.
				slog.Error("bulk consumer: process failed", "job_id", ev.JobID, "error", err)
				return
			}

			toCommit = append(toCommit, rec)
		})

		if len(toCommit) > 0 {
			if err := c.cl.CommitRecords(ctx, toCommit...); err != nil {
				slog.Warn("bulk consumer: commit failed", "error", err)
			}
		}
	}
}
