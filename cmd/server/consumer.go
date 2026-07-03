package main

import (
	"context"
	"errors"
	"log/slog"
	"os/signal"
	"syscall"

	"github.com/TranTheTuan/go-shortener/configs"
	"github.com/TranTheTuan/go-shortener/internal/events"
	"github.com/TranTheTuan/go-shortener/internal/repository"
)

// runAnalyzeConsumer runs the Kafka click-consumer worker: it batch-inserts click
// events into Postgres until interrupted.
func runAnalyzeConsumer() error {
	cfg, err := configs.Load()
	if err != nil {
		return err
	}
	if !cfg.Kafka.Enabled() {
		return errors.New("consumer requires KAFKA_BROKERS to be set")
	}

	db, err := openPostgres(cfg)
	if err != nil {
		return err
	}

	clickRepo := repository.NewClickRepository(db)
	consumer, err := events.NewClickConsumer(cfg.Kafka, clickRepo)
	if err != nil {
		return err
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	slog.Info("click-consumer starting", "brokers", cfg.Kafka.Brokers, "topic", cfg.Kafka.ClickTopic)
	return consumer.Run(ctx)
}
