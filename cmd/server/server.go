package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	_ "net/http/pprof" // registers pprof handlers on http.DefaultServeMux
	"os/signal"
	"syscall"
	"time"

	"github.com/TranTheTuan/go-shortener/configs"
	"github.com/TranTheTuan/go-shortener/internal/events"
	"github.com/TranTheTuan/go-shortener/internal/handler"
	"github.com/TranTheTuan/go-shortener/internal/repository"
	"github.com/TranTheTuan/go-shortener/internal/router"
	"github.com/TranTheTuan/go-shortener/internal/service"
	"github.com/TranTheTuan/go-shortener/pkg/database"
	"github.com/TranTheTuan/go-shortener/pkg/keycloak"
	"github.com/TranTheTuan/go-shortener/pkg/redisbreaker"
	"github.com/TranTheTuan/go-shortener/pkg/storage"
)

// runServer loads config, wires the handler/service/repository layers, and runs
// the Echo HTTP API with graceful shutdown.
func runServer() error {
	cfg, err := configs.Load()
	if err != nil {
		return err
	}

	// Connect to PostgreSQL. Schema migrations are run manually via the
	// Makefile (`make migrate-up`), not on server startup.
	db, err := openPostgres(cfg)
	if err != nil {
		return err
	}

	rdb, err := database.SetupRedis(cfg.Redis)
	if err != nil {
		return err
	}

	if cfg.Server.PprofAddr != "" {
		go func() {
			slog.Info("pprof server starting", "addr", cfg.Server.PprofAddr)
			if err := http.ListenAndServe(cfg.Server.PprofAddr, nil); err != nil {
				slog.Error("pprof server failed", "error", err)
			}
		}()
	}

	// Wire dependencies: repository -> service -> handler.
	userRepo := repository.NewUserRepository(db)
	planRepo := repository.NewPlanRepository(db)
	userSvc := service.NewUserService(userRepo, planRepo, cfg.Quota.DefaultPlanCode)

	linkRepo := repository.NewLinkRepository(db)
	clickRepo := repository.NewClickRepository(db)
	linkCacheRepo := repository.NewLinkCacheRepository(rdb)
	linkSvc := service.NewLinkService(linkRepo, linkCacheRepo, cfg.Shortener.CodeLength, cfg.Shortener.CacheTTL)
	analyticsSvc := service.NewAnalyticsService(linkRepo, clickRepo)

	// Wire the click producer: Kafka when brokers are configured, inline fallback otherwise.
	var producer events.ClickProducer
	if cfg.Kafka.Enabled() {
		p, err := events.NewKafkaProducer(cfg.Kafka)
		if err != nil {
			return fmt.Errorf("kafka producer: %w", err)
		}
		producer = p
	} else {
		producer = events.NewInlineProducer(clickRepo)
	}
	defer producer.Close()

	// Keycloak token verifier. The background context backs lazy JWKS fetch +
	// key rotation; construction does not call Keycloak (app boots regardless).
	verifier := keycloak.NewVerifier(context.Background(), cfg.Keycloak.Issuer, cfg.Keycloak.JWKSURL, cfg.Keycloak.ClientID)

	// Quota + per-owner dedup: Redis access guarded by a circuit breaker.
	breaker := redisbreaker.New(cfg.Quota.BreakerMaxFailures, cfg.Quota.BreakerOpenTimeout)
	dedupCache := service.NewDedupCache(rdb, breaker, cfg.Shortener.CacheTTL)
	subRepo := repository.NewSubscriptionRepository(db)
	quotaSvc := service.NewQuotaService(rdb, breaker, planRepo, subRepo, cfg.Quota.DefaultPlanCode, cfg.Quota.BasicFallbackLimit)

	// Bulk upload — optional; only wired when R2 credentials are present.
	var bulkHandler *handler.BulkJobHandler
	var bulkRepo repository.BulkJobRepository
	var bulkProducer events.BulkJobProducer
	if cfg.R2.Enabled() {
		r2, err := storage.NewR2Client(cfg.R2)
		if err != nil {
			return fmt.Errorf("r2 client: %w", err)
		}
		bulkRepo = repository.NewBulkJobRepository(db)
		bulkSvc := service.NewBulkJobService(bulkRepo, r2, cfg.Shortener.BaseURL)
		bulkHandler = handler.NewBulkJobHandler(bulkSvc)
		if cfg.Kafka.Enabled() {
			bp, err := events.NewBulkJobProducer(cfg.Kafka)
			if err != nil {
				return fmt.Errorf("bulk job producer: %w", err)
			}
			bulkProducer = bp
			defer bulkProducer.Close()
		}
	} else {
		slog.Warn("R2 not configured; bulk-upload endpoints disabled")
	}

	e := router.New(router.Handlers{
		Health:   handler.NewHealthHandler(),
		User:     handler.NewUserHandler(userSvc),
		Link:     handler.NewLinkHandler(linkSvc, analyticsSvc, dedupCache, cfg.Shortener.BaseURL),
		Redirect: handler.NewRedirectHandler(linkSvc, producer),
		Auth:     handler.NewAuthHandler(userSvc),
		Frontend: handler.NewFrontendHandler(cfg.Keycloak.Issuer, cfg.Keycloak.ClientID),
		BulkJob:  bulkHandler,
	}, router.Deps{
		Verifier: verifier,
		Users:    userSvc,
		Dedup:    dedupCache,
		Quota:    quotaSvc,
	})
	e.Server.ReadTimeout = cfg.Server.ReadTimeout
	e.Server.WriteTimeout = cfg.Server.WriteTimeout
	e.Server.IdleTimeout = cfg.Server.IdleTimeout

	// Trap interrupt/termination signals for graceful shutdown.
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	if bulkProducer != nil {
		go outboxRelay(ctx, bulkRepo, bulkProducer, 5*time.Second)
	}

	// Run the server in the background.
	serverErr := make(chan error, 1)
	go func() {
		slog.Info("server starting", "addr", cfg.Server.Addr(), "env", cfg.Env)
		if err := e.Start(cfg.Server.Addr()); err != nil && !errors.Is(err, http.ErrServerClosed) {
			serverErr <- err
		}
	}()

	// Block until the server fails or a shutdown signal arrives.
	select {
	case err := <-serverErr:
		return err
	case <-ctx.Done():
		slog.Info("shutdown signal received")
	}

	// Give in-flight requests time to complete before exiting.
	shutdownCtx, cancel := context.WithTimeout(context.Background(), cfg.Server.ShutdownTimeout)
	defer cancel()

	if err := e.Shutdown(shutdownCtx); err != nil {
		return err
	}

	slog.Info("server stopped gracefully")
	return nil
}
