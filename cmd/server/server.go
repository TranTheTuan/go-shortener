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

	paddlesdk "github.com/PaddleHQ/paddle-go-sdk/v5"
	"github.com/redis/go-redis/extra/redisotel/v9"

	"github.com/TranTheTuan/go-shortener/configs"
	"github.com/TranTheTuan/go-shortener/internal/events"
	"github.com/TranTheTuan/go-shortener/internal/handler"
	"github.com/TranTheTuan/go-shortener/internal/repository"
	"github.com/TranTheTuan/go-shortener/internal/router"
	"github.com/TranTheTuan/go-shortener/internal/service"
	"github.com/TranTheTuan/go-shortener/internal/worker"
	"github.com/TranTheTuan/go-shortener/pkg/database"
	"github.com/TranTheTuan/go-shortener/pkg/keycloak"
	"github.com/TranTheTuan/go-shortener/pkg/metrics"
	paddlepkg "github.com/TranTheTuan/go-shortener/pkg/paddle"
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

	// Install tracing first so the GORM/Redis instrumentation below binds to the
	// live TracerProvider. Deferred (LIFO) so span flush runs LAST on exit —
	// after Echo + metrics teardown — and on EVERY return path, including error
	// exits where buffered spans matter most.
	tpShutdown, err := setupTracing(context.Background(), cfg, "go-shortener-server")
	if err != nil {
		return err
	}
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = tpShutdown(ctx)
	}()

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
	// Trace actual Redis calls. L1 cache hits skip Redis, so no span there —
	// the redirect hot path stays untouched.
	if err := redisotel.InstrumentTracing(rdb.Client); err != nil {
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
	userSvc := service.NewUserService(userRepo, planRepo, cfg.Quota.DefaultPlanCode, cfg.Terms.CurrentVersion)

	linkRepo := repository.NewLinkRepository(db)
	clickRepo := repository.NewClickRepository(db)
	// Tiered redirect cache: per-pod in-memory L1 fronting Redis (L2). L1 spares
	// Redis the round trip for hot codes; its short TTL bounds cross-pod staleness.
	linkCacheRepo := repository.NewTieredLinkCache(
		repository.NewLinkCacheRepository(rdb),
		cfg.Shortener.L1CacheSize, cfg.Shortener.L1CacheTTL,
	)
	linkSvc := service.NewLinkService(linkRepo, linkCacheRepo, cfg.Shortener.CodeLength, cfg.Shortener.CacheTTL)

	clickStatsRepo := repository.NewClickStatsRepository(db)
	planFeatureRepo := repository.NewPlanFeatureRepository(db)
	subRepo := repository.NewSubscriptionRepository(db)
	entitlementSvc := service.NewEntitlementService(planFeatureRepo, subRepo, planRepo, cfg.Quota.DefaultPlanCode)
	analyticsSvc := service.NewAnalyticsService(linkRepo, clickRepo, clickStatsRepo, entitlementSvc)

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
	quotaSvc := service.NewQuotaService(rdb, breaker, planRepo, subRepo, cfg.Quota.DefaultPlanCode, cfg.Quota.BasicFallbackLimit)

	// Metrics: OTel MeterProvider + Prometheus exporter, served on a dedicated
	// in-cluster port (scraped by Prometheus; never routed via ingress).
	var metricsSrv *http.Server
	var metricsShutdown func(context.Context) error
	if cfg.Server.MetricsAddr != "" {
		reg, shutdown, err := metrics.Setup(breaker.IsOpen)
		if err != nil {
			return fmt.Errorf("metrics setup: %w", err)
		}
		metricsShutdown = shutdown
		mux := http.NewServeMux()
		mux.Handle("/metrics", metrics.Handler(reg))
		metricsSrv = &http.Server{
			Addr:              cfg.Server.MetricsAddr,
			Handler:           mux,
			ReadHeaderTimeout: 5 * time.Second,
		}
		go func() {
			slog.Info("metrics server starting", "addr", cfg.Server.MetricsAddr)
			if err := metricsSrv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
				slog.Error("metrics server failed", "error", err)
			}
		}()
	}

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
		bulkSvc := service.NewBulkJobService(bulkRepo, r2, cfg.Shortener.BaseURL, quotaSvc)
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

	// Paddle billing — only wired when PADDLE_ENABLED=true.
	var webhookHandler *handler.WebhookHandler
	var subscriptionHandler *handler.SubscriptionHandler
	var paddleVerifier *paddlesdk.WebhookVerifier
	var webhookQueue chan service.PaddleEvent
	var billingSvc service.BillingService
	if cfg.Paddle.Enabled {
		paddleVerifier = paddlepkg.NewVerifier(cfg.Paddle.WebhookSecret)
		paddleSDK, err := paddlepkg.NewClient(cfg.Paddle.APIKey, cfg.Paddle.BaseURL)
		if err != nil {
			return fmt.Errorf("paddle SDK: %w", err)
		}
		webhookQueue = make(chan service.PaddleEvent, 100)
		billingSvc = service.NewBillingService(planRepo, subRepo, userRepo, quotaSvc, paddleSDK, cfg.Quota.DefaultPlanCode)
		webhookHandler = handler.NewWebhookHandler(webhookQueue)
		subscriptionHandler = handler.NewSubscriptionHandler(billingSvc, quotaSvc, planRepo)
	}

	e := router.New(router.Handlers{
		Health:       handler.NewHealthHandler(),
		User:         handler.NewUserHandler(userSvc),
		Link:         handler.NewLinkHandler(linkSvc, analyticsSvc, dedupCache, cfg.Shortener.BaseURL),
		Redirect:     handler.NewRedirectHandler(linkSvc, producer),
		Auth:         handler.NewAuthHandler(userSvc),
		Frontend:     handler.NewFrontendHandler(cfg.Keycloak.Issuer, cfg.Keycloak.ClientID, cfg.Paddle.ClientToken, cfg.Terms.CurrentVersion),
		BulkJob:      bulkHandler,
		Webhook:      webhookHandler,
		Subscription: subscriptionHandler,
	}, router.Deps{
		Verifier:       verifier,
		Users:          userSvc,
		Dedup:          dedupCache,
		Quota:          quotaSvc,
		PaddleVerifier: paddleVerifier,
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
	if webhookQueue != nil {
		go worker.RunWebhookWorker(ctx, webhookQueue, billingSvc)
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
	// Stop serving /metrics BEFORE tearing down the provider, so a late scrape
	// never hits a shut-down MeterProvider.
	if metricsSrv != nil {
		_ = metricsSrv.Shutdown(shutdownCtx)
	}
	if metricsShutdown != nil {
		_ = metricsShutdown(shutdownCtx)
	}
	// Span flush is handled by the deferred tpShutdown (runs after this returns,
	// LIFO — last, as intended).

	slog.Info("server stopped gracefully")
	return nil
}
