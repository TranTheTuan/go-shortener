package events

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"log/slog"
	"strconv"
	"time"

	"github.com/twmb/franz-go/pkg/kgo"
	"github.com/twmb/franz-go/pkg/sasl/scram"

	"github.com/TranTheTuan/go-shortener/configs"
	"github.com/TranTheTuan/go-shortener/internal/repository"
	"github.com/TranTheTuan/go-shortener/pkg/metrics"
)

// ClickEvent is the payload published to Kafka for every redirect.
type ClickEvent struct {
	LinkID    int64     `json:"link_id"`
	ClickedAt time.Time `json:"clicked_at"`
	Referrer  string    `json:"referrer,omitempty"`
	IPAddress string    `json:"ip_address,omitempty"`
	UserAgent string    `json:"user_agent,omitempty"`
}

// toClick maps the event to a persisted click row (shared by the inline
// producer and the consumer).
func (ev ClickEvent) toClick() *repository.Click {
	return &repository.Click{
		LinkID:    ev.LinkID,
		ClickedAt: ev.ClickedAt,
		Referrer:  ev.Referrer,
		IPAddress: ev.IPAddress,
		UserAgent: ev.UserAgent,
	}
}

// ClickProducer publishes click events. Publish is non-blocking and never
// returns an error — failures drop the event and log, so the redirect path
// never depends on Kafka.
type ClickProducer interface {
	Publish(ev ClickEvent)
	Close()
}

// buildKGOOpts assembles TLS+SASL options from config. Returns nil for plaintext.
func buildKGOOpts(cfg configs.KafkaConfig) []kgo.Opt {
	var opts []kgo.Opt
	if cfg.TLSEnabled {
		opts = append(opts, kgo.DialTLSConfig(new(tls.Config)))
	}
	if cfg.SASLEnabled() {
		opts = append(opts, kgo.SASL(scram.Auth{
			User: cfg.SASLUsername,
			Pass: cfg.SASLPassword,
		}.AsSha256Mechanism()))
	}
	return opts
}

// --- Kafka producer ---

// produceFunc is the record-send seam (satisfied by (*kgo.Client).TryProduce),
// so Publish can be unit-tested without a broker.
type produceFunc func(ctx context.Context, rec *kgo.Record, promise func(*kgo.Record, error))

type kafkaProducer struct {
	produce produceFunc
	topic   string
	closeFn func()
}

// NewKafkaProducer creates a franz-go async producer with SASL/TLS from cfg.
func NewKafkaProducer(cfg configs.KafkaConfig) (ClickProducer, error) {
	opts := append(
		buildKGOOpts(cfg),
		kgo.SeedBrokers(cfg.Brokers...),
		kgo.ProducerLinger(50*time.Millisecond),
	)
	cl, err := kgo.NewClient(opts...)
	if err != nil {
		return nil, err
	}
	// TryProduce (not Produce): fail fast with ErrMaxBuffered when the client's
	// buffer is full instead of blocking the caller — the redirect path must never
	// block on Kafka.
	return &kafkaProducer{produce: cl.TryProduce, topic: cfg.ClickTopic, closeFn: cl.Close}, nil
}

func (p *kafkaProducer) Publish(ev ClickEvent) {
	payload, err := json.Marshal(ev)
	if err != nil {
		metrics.RecordClickEvent(context.Background(), "dropped")
		slog.Warn("click event marshal failed", "error", err)
		return
	}
	key := strconv.AppendInt(nil, ev.LinkID, 10) // partition by link_id
	p.produce(context.Background(), &kgo.Record{Topic: p.topic, Key: key, Value: payload},
		func(_ *kgo.Record, err error) {
			if err != nil {
				metrics.RecordClickEvent(context.Background(), "dropped")
				slog.Warn("click produce failed (dropped)", "link_id", ev.LinkID, "error", err)
				return
			}
			metrics.RecordClickEvent(context.Background(), "produced")
		})
}

func (p *kafkaProducer) Close() { p.closeFn() }

// --- Inline fallback producer ---

const inlineBufSize = 256

// inlineProducer writes each event to the DB via a bounded background worker,
// preserving pre-Kafka behavior for local dev.
type inlineProducer struct {
	ch     chan ClickEvent
	clicks repository.ClickRepository
	done   chan struct{}
}

// NewInlineProducer returns a ClickProducer that inserts directly into Postgres.
func NewInlineProducer(clicks repository.ClickRepository) ClickProducer {
	p := &inlineProducer{
		ch:     make(chan ClickEvent, inlineBufSize),
		clicks: clicks,
		done:   make(chan struct{}),
	}
	go p.run()
	return p
}

func (p *inlineProducer) run() {
	defer close(p.done)
	for ev := range p.ch {
		if err := p.clicks.Create(context.Background(), ev.toClick()); err != nil {
			slog.Warn("inline click insert failed", "link_id", ev.LinkID, "error", err)
		}
	}
}

func (p *inlineProducer) Publish(ev ClickEvent) {
	select {
	case p.ch <- ev:
	default:
		slog.Warn("inline click buffer full, event dropped", "link_id", ev.LinkID)
	}
}

func (p *inlineProducer) Close() {
	close(p.ch)
	<-p.done
}
