# Phase 01 — Config, Click Producer & Redirect Swap

**Context:** [plan.md](plan.md) · [brainstorm](../reports/brainstorm-260701-1650-kafka-click-analytics.md)

## Overview
- **Priority:** High (foundation + the hot-path change)
- **Status:** pending
- Add Kafka config + franz-go async producer; publish click events from the redirect handler; keep an inline fallback when Kafka isn't configured.

## Related Code Files
- **Create:** `internal/events/click_producer.go` (+ its `ClickEvent` type)
- **Modify:** `configs/config.go`, `.env.example`, `cmd/server/main.go`,
  `internal/handler/redirect_handler.go`, `go.mod`

## Implementation Steps

1. **Add dep:** `go get github.com/twmb/franz-go/pkg/kgo` && `go mod tidy`.

2. **`configs/config.go`** — add `Kafka KafkaConfig` (`envPrefix:"KAFKA_"`):
   ```go
   type KafkaConfig struct {
       Brokers       []string      `env:"BROKERS" envSeparator:","` // empty = disabled (fallback)
       ClickTopic    string        `env:"CLICK_TOPIC" envDefault:"link-clicks"`
       ConsumerGroup string        `env:"CONSUMER_GROUP" envDefault:"click-consumer"`
       BatchSize     int           `env:"BATCH_SIZE" envDefault:"500"`
       BatchInterval time.Duration `env:"BATCH_INTERVAL" envDefault:"1s"`
   }
   func (k KafkaConfig) Enabled() bool { return len(k.Brokers) > 0 }
   ```
   Add to `Config`; update `.env.example` (`KAFKA_BROKERS=` empty by default so local dev uses the fallback).

3. **`internal/events/click_producer.go`**:
   ```go
   type ClickEvent struct {
       LinkID    int64     `json:"link_id"`
       ClickedAt time.Time `json:"clicked_at"`
       Referrer  string    `json:"referrer,omitempty"`
       IPAddress string    `json:"ip_address,omitempty"`
       UserAgent string    `json:"user_agent,omitempty"`
   }

   // ClickProducer publishes click events. Publish is non-blocking and never
   // returns an error to the caller — a failure drops the event (approximate
   // analytics) and is logged/metered, so the redirect path never depends on Kafka.
   type ClickProducer interface {
       Publish(ev ClickEvent)
       Close()
   }
   ```
   franz-go impl:
   ```go
   type kafkaProducer struct{ cl *kgo.Client; topic string }
   func NewKafkaProducer(brokers []string, topic string) (ClickProducer, error) {
       cl, err := kgo.NewClient(kgo.SeedBrokers(brokers...), kgo.ProducerLinger(50*time.Millisecond))
       if err != nil { return nil, err }
       return &kafkaProducer{cl: cl, topic: topic}, nil
   }
   func (p *kafkaProducer) Publish(ev ClickEvent) {
       payload, err := json.Marshal(ev)
       if err != nil { slog.Warn("click event marshal failed", "error", err); return }
       key := strconv.AppendInt(nil, ev.LinkID, 10) // partition by link_id
       p.cl.Produce(context.Background(), &kgo.Record{Topic: p.topic, Key: key, Value: payload},
           func(_ *kgo.Record, err error) {
               if err != nil { slog.Warn("click produce failed (dropped)", "link_id", ev.LinkID, "error", err) }
           }) // async; buffers internally, never blocks the caller
   }
   func (p *kafkaProducer) Close() { p.cl.Close() }
   ```

4. **Fallback producer** (`internal/events`) — when `KAFKA_BROKERS` is empty, keep today's behavior so local dev works without Kafka:
   ```go
   // inlineProducer writes each event straight to the DB in a bounded worker,
   // preserving pre-Kafka behavior. Implements ClickProducer.
   func NewInlineProducer(clicks repository.ClickRepository) ClickProducer { ... }
   ```
   Simplest inline impl: a single background goroutine draining a buffered channel → `clicks.Create` (bounded — also fixes the old unbounded-goroutine bug). Drop on full channel.

5. **`redirect_handler.go`** — replace the per-request `go func(){ analytics.Record }()` with `h.clicks.Publish(events.ClickEvent{...})` (inject a `ClickProducer`). Capture request fields before publishing (as today). Redirect no longer touches the DB directly.

6. **`main.go`** — build the producer: `if cfg.Kafka.Enabled() { events.NewKafkaProducer(...) } else { events.NewInlineProducer(clickRepo) }`; inject into `RedirectHandler`; `defer producer.Close()`.

7. `go build ./...`.

## Todo
- [ ] franz-go dep + `KafkaConfig` + `.env.example`
- [ ] `ClickEvent` + `ClickProducer` (franz-go async, drop-on-failure)
- [ ] inline fallback producer (bounded worker)
- [ ] redirect handler publishes instead of `go func()`
- [ ] main wiring (kafka vs inline by config) + Close
- [ ] `go build ./...`

## Success Criteria
- With `KAFKA_BROKERS` set, redirects publish to Kafka and never block/fail on producer errors. Empty → inline fallback works. Build green.

## Security / Reliability
Producer errors are logged + dropped, never surfaced to the visitor or blocking the 302 (**#1 rule**).

## Next
Phase 02 adds the consumer + batch insert.
