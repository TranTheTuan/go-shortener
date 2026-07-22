package main

import (
	"context"
	"errors"
	"log/slog"
	"os/signal"
	"syscall"
	"time"

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

	tpShutdown, err := setupTracing(context.Background(), cfg, "go-shortener-consumer")
	if err != nil {
		return err
	}
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = tpShutdown(ctx) // bounded so an unreachable Alloy can't stall drain
	}()

	db, err := openPostgres(cfg)
	if err != nil {
		return err
	}

	clickRepo := repository.NewClickRepository(db)
	// clickRepo.CreateBatch upserts analytics rollups (click_stats_daily/referrer/device) inside the same tx.
	consumer, err := events.NewClickConsumer(cfg.Kafka, clickRepo)
	if err != nil {
		return err
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	slog.Info("click-consumer starting", "brokers", cfg.Kafka.Brokers, "topic", cfg.Kafka.ClickTopic)
	return consumer.Run(ctx)
}
