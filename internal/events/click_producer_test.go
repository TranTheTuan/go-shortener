package events

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/twmb/franz-go/pkg/kgo"

	"github.com/TranTheTuan/go-shortener/internal/repository"
)

// mockClickRepo records Create/CreateBatch calls for assertions. When batchErr
// is set, CreateBatch fails (to exercise flush-failure / backpressure paths).
type mockClickRepo struct {
	mu       sync.Mutex
	clicks   []*repository.Click
	batchErr error
}

func (m *mockClickRepo) Create(_ context.Context, c *repository.Click) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.clicks = append(m.clicks, c)
	return nil
}

func (m *mockClickRepo) CreateBatch(_ context.Context, cs []*repository.Click) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.batchErr != nil {
		return m.batchErr
	}
	m.clicks = append(m.clicks, cs...)
	return nil
}

func (m *mockClickRepo) CountByLinkID(_ context.Context, _ int64) (int64, error) { return 0, nil }

func (m *mockClickRepo) ListByLinkID(_ context.Context, _ int64, _ int) ([]*repository.Click, error) {
	return nil, nil
}

func TestKafkaProducer_PublishBuildsRecord(t *testing.T) {
	var got *kgo.Record
	p := &kafkaProducer{
		topic:   "link-clicks",
		closeFn: func() {},
		produce: func(_ context.Context, rec *kgo.Record, promise func(*kgo.Record, error)) {
			got = rec
			promise(rec, nil) // simulate a successful ack
		},
	}

	p.Publish(ClickEvent{LinkID: 42, ClickedAt: time.Now().UTC(), Referrer: "https://example.com"})

	if got == nil {
		t.Fatal("produce was not called")
	}
	if got.Topic != "link-clicks" {
		t.Errorf("topic = %q, want link-clicks", got.Topic)
	}
	if string(got.Key) != "42" {
		t.Errorf("key = %q, want 42 (partition by link_id)", got.Key)
	}
	var ev ClickEvent
	if err := json.Unmarshal(got.Value, &ev); err != nil {
		t.Fatalf("record value is not valid JSON: %v", err)
	}
	if ev.LinkID != 42 || ev.Referrer != "https://example.com" {
		t.Errorf("decoded event = %+v", ev)
	}
}

func TestKafkaProducer_DropsOnProduceError(t *testing.T) {
	// A produce error must be swallowed (drop-and-log), never panic or block.
	p := &kafkaProducer{
		topic:   "link-clicks",
		closeFn: func() {},
		produce: func(_ context.Context, rec *kgo.Record, promise func(*kgo.Record, error)) {
			promise(rec, errors.New("buffer full"))
		},
	}
	p.Publish(ClickEvent{LinkID: 1, ClickedAt: time.Now().UTC()}) // must not panic
}

func TestInlineProducer_PublishAndReceive(t *testing.T) {
	repo := &mockClickRepo{}
	p := NewInlineProducer(repo)

	ev := ClickEvent{LinkID: 42, ClickedAt: time.Now().UTC(), IPAddress: "1.2.3.4"}
	p.Publish(ev)
	p.Close() // waits for worker to drain

	repo.mu.Lock()
	defer repo.mu.Unlock()
	if len(repo.clicks) != 1 {
		t.Fatalf("expected 1 click, got %d", len(repo.clicks))
	}
	if repo.clicks[0].LinkID != 42 {
		t.Errorf("link_id = %d, want 42", repo.clicks[0].LinkID)
	}
}

func TestClickEvent_JSONShape(t *testing.T) {
	ev := ClickEvent{LinkID: 1, ClickedAt: time.Now().UTC(), Referrer: "https://example.com"}
	b, err := json.Marshal(ev)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	for _, key := range []string{"link_id", "clicked_at", "referrer"} {
		if _, ok := m[key]; !ok {
			t.Errorf("missing key %q in JSON", key)
		}
	}
}
