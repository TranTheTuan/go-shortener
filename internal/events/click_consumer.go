package events

import (
	"context"
	"encoding/json"
	"log/slog"
	"time"

	"github.com/twmb/franz-go/pkg/kgo"

	"github.com/TranTheTuan/go-shortener/configs"
	"github.com/TranTheTuan/go-shortener/internal/repository"
)

// ClickConsumer consumes the link-clicks topic and batch-inserts into Postgres.
// At-least-once: commits offsets only after a successful CreateBatch.
type ClickConsumer struct {
	cl        *kgo.Client
	clicks    repository.ClickRepository
	batchSize int
	interval  time.Duration
}

// NewClickConsumer creates a consumer connected to Kafka using cfg for TLS/SASL.
func NewClickConsumer(cfg configs.KafkaConfig, clicks repository.ClickRepository) (*ClickConsumer, error) {
	opts := append(
		buildKGOOpts(cfg),
		kgo.SeedBrokers(cfg.Brokers...),
		kgo.ConsumerGroup(cfg.ConsumerGroup),
		kgo.ConsumeTopics(cfg.ClickTopic),
		kgo.DisableAutoCommit(),
	)
	cl, err := kgo.NewClient(opts...)
	if err != nil {
		return nil, err
	}
	return &ClickConsumer{
		cl:        cl,
		clicks:    clicks,
		batchSize: cfg.BatchSize,
		interval:  cfg.BatchInterval,
	}, nil
}

// Run polls Kafka and batch-inserts clicks until ctx is cancelled.
func (c *ClickConsumer) Run(ctx context.Context) error {
	ticker := time.NewTicker(c.interval)
	defer ticker.Stop()

	var (
		buf  []*repository.Click
		recs []*kgo.Record
	)

	flush := func() {
		if len(buf) == 0 {
			return
		}
		if err := c.clicks.CreateBatch(ctx, buf); err != nil {
			slog.Error("click batch insert failed", "count", len(buf), "error", err)
			// keep buffer; retry on next flush to avoid data loss
			return
		}
		if err := c.cl.CommitRecords(ctx, recs...); err != nil {
			slog.Warn("kafka commit failed", "error", err)
		}
		buf = buf[:0]
		recs = recs[:0]
	}

	for {
		select {
		case <-ctx.Done():
			flush()
			c.cl.Close()
			return ctx.Err()
		case <-ticker.C:
			flush()
		default:
		}

		fetches := c.cl.PollFetches(ctx)
		if fetches.IsClientClosed() {
			flush()
			return nil
		}
		fetches.EachError(func(_ string, _ int32, err error) {
			slog.Error("kafka fetch error", "error", err)
		})

		fetches.EachRecord(func(r *kgo.Record) {
			var ev ClickEvent
			if err := json.Unmarshal(r.Value, &ev); err != nil {
				slog.Warn("poison click event skipped", "offset", r.Offset, "error", err)
				recs = append(recs, r) // commit the poison offset so it isn't re-delivered
				return
			}
			buf = append(buf, &repository.Click{
				LinkID:    ev.LinkID,
				ClickedAt: ev.ClickedAt,
				Referrer:  ev.Referrer,
				IPAddress: ev.IPAddress,
				UserAgent: ev.UserAgent,
			})
			recs = append(recs, r)
			if len(buf) >= c.batchSize {
				flush()
			}
		})
	}
}
