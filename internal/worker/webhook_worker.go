package worker

import (
	"context"
	"log/slog"
	"time"

	"github.com/TranTheTuan/go-shortener/internal/service"
)

// RunWebhookWorker drains the Paddle event queue and delegates each event to
// BillingService. It runs until ctx is canceled (server shutdown). Because we
// already returned 200 to Paddle, we retry transiently-failing events with
// exponential backoff before giving up and logging an alert.
func RunWebhookWorker(ctx context.Context, queue <-chan service.PaddleEvent, billing service.BillingService) {
	for {
		select {
		case evt := <-queue:
			processWithRetry(ctx, evt, billing)
		case <-ctx.Done():
			// drain remaining events
			for {
				select {
				case evt := <-queue:
					processWithRetry(context.Background(), evt, billing)
				default:
					return
				}
			}
		}
	}
}

func processWithRetry(ctx context.Context, evt service.PaddleEvent, billing service.BillingService) {
	var err error
	for attempt := range 3 {
		if err = billing.HandleEvent(ctx, evt); err == nil {
			return
		}
		wait := time.Duration(1<<attempt) * time.Second // 1s, 2s, 4s
		slog.Warn("paddle event failed, retrying",
			"type", evt.EventType, "id", evt.EventID, "attempt", attempt+1, "wait", wait)
		select {
		case <-time.After(wait):
		case <-ctx.Done():
			return
		}
	}
	// Alert: subscription state may be stale — needs manual investigation.
	slog.Error("paddle event permanently failed after retries",
		"type", evt.EventType, "id", evt.EventID, "error", err)
}
