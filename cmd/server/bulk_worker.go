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
	"github.com/TranTheTuan/go-shortener/internal/service"
	"github.com/TranTheTuan/go-shortener/internal/worker"
	"github.com/TranTheTuan/go-shortener/pkg/storage"
)

// runBulkWorker runs the Kafka bulk-job consumer: downloads files from R2,
// shortens each URL via linkSvc, and writes result files back to R2.
func runBulkWorker() error {
	cfg, err := configs.Load()
	if err != nil {
		return err
	}
	if !cfg.Kafka.Enabled() {
		return errors.New("bulk-worker requires KAFKA_BROKERS to be set")
	}
	if !cfg.R2.Enabled() {
		return errors.New("bulk-worker requires R2_ACCOUNT_ID and R2_ACCESS_KEY_ID to be set")
	}

	tpShutdown, err := setupTracing(context.Background(), cfg, "go-shortener-bulk-worker")
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

	r2, err := storage.NewR2Client(cfg.R2)
	if err != nil {
		return err
	}

	// Link cache is optional; pass nil so the worker has no Redis dependency.
	linkRepo := repository.NewLinkRepository(db)
	linkSvc := service.NewLinkService(linkRepo, nil, cfg.Shortener.CodeLength, cfg.Shortener.CacheTTL)

	bulkRepo := repository.NewBulkJobRepository(db)
	w := worker.NewBulkJobWorker(bulkRepo, linkSvc, r2, cfg.Shortener.BaseURL)

	consumer, err := events.NewBulkJobConsumer(cfg.Kafka, w)
	if err != nil {
		return err
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	slog.Info("bulk-worker starting", "brokers", cfg.Kafka.Brokers, "topic", cfg.Kafka.BulkJobTopic)
	return consumer.Run(ctx)
}
