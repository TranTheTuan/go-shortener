package handler

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"

	"github.com/labstack/echo/v4"

	"github.com/TranTheTuan/go-shortener/internal/service"
)

// WebhookHandler handles inbound Paddle webhook events.
// Signature verification is done upstream by the PaddleSignature middleware.
type WebhookHandler struct {
	queue chan<- service.PaddleEvent
}

func NewWebhookHandler(queue chan<- service.PaddleEvent) *WebhookHandler {
	return &WebhookHandler{queue: queue}
}

// PaddleWebhook receives a verified Paddle event, parses minimal fields for
// routing, and enqueues the raw body for the worker goroutine. Returns 503 if
// the queue is full so Paddle retries rather than silently dropping billing events.
//
// @Summary      Receive Paddle webhook event
// @Tags         billing
// @Accept       json
// @Param        X-Paddle-Signature  header  string  true  "Paddle webhook signature"
// @Success      200  "event accepted"
// @Failure      400  "invalid payload"
// @Failure      503  "queue full — Paddle should retry"
// @Router       /webhooks/paddle [post]
func (h *WebhookHandler) PaddleWebhook(c echo.Context) error {
	body, err := io.ReadAll(io.LimitReader(c.Request().Body, 1<<20))
	if err != nil {
		return c.NoContent(http.StatusBadRequest)
	}

	var peek struct {
		EventType string `json:"event_type"`
		EventID   string `json:"event_id"`
	}
	if err := json.Unmarshal(body, &peek); err != nil {
		return c.NoContent(http.StatusBadRequest)
	}

	evt := service.PaddleEvent{
		EventType: peek.EventType,
		EventID:   peek.EventID,
		Raw:       body,
	}

	select {
	case h.queue <- evt:
		return c.NoContent(http.StatusOK)
	default:
		slog.Warn("paddle webhook queue full", "type", evt.EventType, "id", evt.EventID)
		return c.NoContent(http.StatusServiceUnavailable)
	}
}
