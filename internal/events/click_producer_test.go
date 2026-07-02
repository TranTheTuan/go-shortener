package events

import (
	"context"
	"encoding/json"
	"sync"
	"testing"
	"time"

	"github.com/TranTheTuan/go-shortener/internal/repository"
)

// mockClickRepo records calls to Create/CreateBatch for assertions.
type mockClickRepo struct {
	mu     sync.Mutex
	clicks []*repository.Click
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
	m.clicks = append(m.clicks, cs...)
	return nil
}

func (m *mockClickRepo) CountByLinkID(_ context.Context, _ int64) (int64, error) { return 0, nil }

func (m *mockClickRepo) ListByLinkID(_ context.Context, _ int64, _ int) ([]*repository.Click, error) {
	return nil, nil
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
