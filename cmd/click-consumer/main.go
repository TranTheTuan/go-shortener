package main

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/TranTheTuan/go-shortener/configs"
	"github.com/TranTheTuan/go-shortener/internal/events"
	"github.com/TranTheTuan/go-shortener/internal/repository"
	"github.com/TranTheTuan/go-shortener/pkg/database"
)

func main() {
	if err := run(); err != nil && !errors.Is(err, context.Canceled) {
		slog.Error("consumer exited with error", "error", err)
		os.Exit(1)
	}
}

func run() error {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	slog.SetDefault(logger)

	cfg, err := configs.Load()
	if err != nil {
		return err
	}
	if !cfg.Kafka.Enabled() {
		return errors.New("consumer requires KAFKA_BROKERS to be set")
	}

	db, err := database.NewPostgres(cfg.Database.DSN(), database.PostgresOptions{
		MaxOpenConns:    cfg.Database.MaxOpenConns,
		MaxIdleConns:    cfg.Database.MaxIdleConns,
		ConnMaxLifetime: cfg.Database.ConnMaxLifetime,
	})
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
