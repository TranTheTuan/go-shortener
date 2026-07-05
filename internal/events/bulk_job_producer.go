package events

import (
	"context"
	"encoding/json"
	"strconv"

	"github.com/twmb/franz-go/pkg/kgo"

	"github.com/TranTheTuan/go-shortener/configs"
)

// BulkJobEvent is the payload published to Kafka when a bulk job is confirmed.
// The worker re-fetches the job by ID so the message stays minimal.
type BulkJobEvent struct {
	JobID int64 `json:"job_id"`
}

// BulkJobProducer publishes bulk-job events synchronously.
// Unlike the click producer, Publish blocks and returns an error so the outbox
// relay only marks entries published after confirmed broker receipt.
type BulkJobProducer interface {
	Publish(ctx context.Context, ev BulkJobEvent) error
	Close()
}

type bulkJobProducer struct {
	cl    *kgo.Client
	topic string
}

func NewBulkJobProducer(cfg configs.KafkaConfig) (BulkJobProducer, error) {
	opts := append(buildKGOOpts(cfg), kgo.SeedBrokers(cfg.Brokers...))
	cl, err := kgo.NewClient(opts...)
	if err != nil {
		return nil, err
	}
	return &bulkJobProducer{cl: cl, topic: cfg.BulkJobTopic}, nil
}

func (p *bulkJobProducer) Publish(ctx context.Context, ev BulkJobEvent) error {
	payload, _ := json.Marshal(ev)
	key := strconv.AppendInt(nil, ev.JobID, 10)
	return p.cl.ProduceSync(ctx, &kgo.Record{Topic: p.topic, Key: key, Value: payload}).FirstErr()
}

func (p *bulkJobProducer) Close() { p.cl.Close() }
