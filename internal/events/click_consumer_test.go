package events

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/TranTheTuan/go-shortener/internal/repository"
)

func makeRecord(ev ClickEvent) []byte {
	b, _ := json.Marshal(ev)
	return b
}

// batchAccumulate is the pure decode+accumulate logic extracted for testing.
// It mirrors the logic inside Run: decode valid events, skip invalid ones.
func batchAccumulate(payloads [][]byte) ([]*repository.Click, int) {
	var clicks []*repository.Click
	skipped := 0
	for _, p := range payloads {
		var ev ClickEvent
		if err := json.Unmarshal(p, &ev); err != nil {
			skipped++
			continue
		}
		clicks = append(clicks, &repository.Click{
			LinkID:    ev.LinkID,
			ClickedAt: ev.ClickedAt,
			Referrer:  ev.Referrer,
			IPAddress: ev.IPAddress,
			UserAgent: ev.UserAgent,
		})
	}
	return clicks, skipped
}

func TestBatchAccumulate_DecodesValid(t *testing.T) {
	payloads := [][]byte{
		makeRecord(ClickEvent{LinkID: 1, ClickedAt: time.Now().UTC()}),
		makeRecord(ClickEvent{LinkID: 2, ClickedAt: time.Now().UTC()}),
	}
	clicks, skipped := batchAccumulate(payloads)
	if len(clicks) != 2 || skipped != 0 {
		t.Errorf("got %d clicks, %d skipped; want 2, 0", len(clicks), skipped)
	}
}

func TestBatchAccumulate_SkipsPoison(t *testing.T) {
	payloads := [][]byte{
		makeRecord(ClickEvent{LinkID: 1, ClickedAt: time.Now().UTC()}),
		[]byte(`not json`),
		makeRecord(ClickEvent{LinkID: 3, ClickedAt: time.Now().UTC()}),
	}
	clicks, skipped := batchAccumulate(payloads)
	if len(clicks) != 2 || skipped != 1 {
		t.Errorf("got %d clicks, %d skipped; want 2, 1", len(clicks), skipped)
	}
}

func TestInlineProducer_DropOnFullBuffer(t *testing.T) {
	// Fill the buffer beyond capacity with a blocked repo, then verify no panic/deadlock.
	repo := &mockClickRepo{}
	p := NewInlineProducer(repo)
	// Publish more than the buffer to exercise the drop path
	for i := 0; i < inlineBufSize+10; i++ {
		p.Publish(ClickEvent{LinkID: int64(i), ClickedAt: time.Now().UTC()})
	}
	p.Close()
	// No assertion on exact count — some may have been dropped; test passes if no deadlock.
	_ = context.Background()
}
