package events

import (
	"context"
	"encoding/json"
	"log/slog"

	"github.com/twmb/franz-go/pkg/kgo"

	"github.com/TranTheTuan/go-shortener/configs"
	"github.com/TranTheTuan/go-shortener/internal/repository"
)

// decodeClick decodes a record payload into a Click. ok=false marks a poison
// (undecodable) event.
func decodeClick(value []byte) (*repository.Click, bool) {
	var ev ClickEvent
	if err := json.Unmarshal(value, &ev); err != nil {
		return nil, false
	}
	return ev.toClick(), true
}

// ClickConsumer consumes the link-clicks topic and inserts clicks into Postgres.
// Each poll's fetch is treated as one batch (franz-go already batches records).
// At-least-once: offsets are committed only after a successful insert, so a
// failure redelivers — counts are approximate (an occasional duplicate is fine).
type ClickConsumer struct {
	cl     *kgo.Client
	clicks repository.ClickRepository
}

// NewClickConsumer creates a consumer connected to Kafka using cfg for TLS/SASL.
func NewClickConsumer(cfg configs.KafkaConfig, clicks repository.ClickRepository) (*ClickConsumer, error) {
	opts := append(
		buildKGOOpts(cfg),
		kgo.SeedBrokers(cfg.Brokers...),
		kgo.ConsumerGroup(cfg.ConsumerGroup),
		kgo.ConsumeTopics(cfg.ClickTopic),
		kgo.DisableAutoCommit(), // commit manually, after a successful insert
	)
	cl, err := kgo.NewClient(opts...)
	if err != nil {
		return nil, err
	}
	return &ClickConsumer{cl: cl, clicks: clicks}, nil
}

// Run polls Kafka and inserts each fetched batch until ctx is cancelled.
func (c *ClickConsumer) Run(ctx context.Context) error {
	for {
		fetches := c.cl.PollFetches(ctx)

		if ctx.Err() != nil {
			// Shutting down: drop this fetch uncommitted (it'll be redelivered).
			c.cl.Close()
			return ctx.Err()
		}
		if fetches.IsClientClosed() {
			return nil
		}
		fetches.EachError(func(_ string, _ int32, err error) {
			slog.Error("kafka fetch error", "error", err)
		})

		var (
			clicks []*repository.Click
			recs   []*kgo.Record
		)
		fetches.EachRecord(func(r *kgo.Record) {
			recs = append(recs, r)
			if click, ok := decodeClick(r.Value); ok {
				clicks = append(clicks, click)
			} else {
				slog.Warn("skipping malformed click event", "offset", r.Offset)
			}
		})
		if len(recs) == 0 {
			continue
		}

		if len(clicks) > 0 {
			if err := c.clicks.CreateBatch(ctx, clicks); err != nil {
				// Don't commit — Kafka will redeliver this batch.
				slog.Error("insert click batch failed; will retry", "count", len(clicks), "error", err)
				continue
			}
		}
		// Commit all fetched offsets (including skipped poison ones) so they are
		// not redelivered forever.
		if err := c.cl.CommitRecords(ctx, recs...); err != nil {
			slog.Error("commit offsets failed", "error", err)
		}
	}
}
