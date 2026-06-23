// Command server is the HTTP API entrypoint. It loads configuration, connects
// to the database, wires the handler/service/repository layers together, and
// runs the Echo server with graceful shutdown.
package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	_ "net/http/pprof" // registers pprof handlers on http.DefaultServeMux
	"os"
	"os/signal"
	"syscall"

	"github.com/TranTheTuan/go-shortener/configs"
	"github.com/TranTheTuan/go-shortener/internal/handler"
	"github.com/TranTheTuan/go-shortener/internal/repository"
	"github.com/TranTheTuan/go-shortener/internal/router"
	"github.com/TranTheTuan/go-shortener/internal/service"
	"github.com/TranTheTuan/go-shortener/pkg/database"
	"github.com/TranTheTuan/go-shortener/pkg/token"
)

// @title                      Go URL Shortener API
// @version                    1.0
// @description                URL shortener with click analytics, built on Echo + GORM + PostgreSQL.
// @description                All responses use a uniform envelope: success payloads under `data`, errors under `error`.
// @BasePath                   /
// @securityDefinitions.apikey ApiKeyAuth
// @in                         header
// @name                       X-API-Key
// @securityDefinitions.apikey BearerAuth
// @in                         header
// @name                       Authorization
// @description                Bearer access token. Format: "Bearer {token}".
func main() {
	if err := run(); err != nil {
		slog.Error("server exited with error", "error", err)
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

	// Connect to PostgreSQL. Schema migrations are run manually via the
	// Makefile (`make migrate-up`), not on server startup.
	db, err := database.NewPostgres(cfg.Database.DSN(), database.PostgresOptions{
		MaxOpenConns:    cfg.Database.MaxOpenConns,
		MaxIdleConns:    cfg.Database.MaxIdleConns,
		ConnMaxLifetime: cfg.Database.ConnMaxLifetime,
	})
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
	userSvc := service.NewUserService(userRepo)

	linkRepo := repository.NewLinkRepository(db)
	clickRepo := repository.NewClickRepository(db)
	linkCacheRepo := repository.NewLinkCacheRepository(rdb)
	linkSvc := service.NewLinkService(linkRepo, linkCacheRepo, cfg.Shortener.CodeLength, cfg.Shortener.CacheTTL)
	analyticsSvc := service.NewAnalyticsService(linkRepo, clickRepo)

	issuer := token.NewIssuer(cfg.Auth.JWTSecret, cfg.Auth.AccessTTL)
	refreshRepo := repository.NewRefreshTokenRepository(db)
	authSvc := service.NewAuthService(userRepo, refreshRepo, issuer, cfg.Auth.RefreshTTL, cfg.Auth.BcryptCost)

	e := router.New(router.Handlers{
		Health:   handler.NewHealthHandler(),
		User:     handler.NewUserHandler(userSvc),
		Link:     handler.NewLinkHandler(linkSvc, analyticsSvc, cfg.Shortener.BaseURL),
		Redirect: handler.NewRedirectHandler(linkSvc, analyticsSvc),
		Auth:     handler.NewAuthHandler(authSvc, userSvc),
	}, cfg.Shortener.APIKeys, issuer)
	e.Server.ReadTimeout = cfg.Server.ReadTimeout
	e.Server.WriteTimeout = cfg.Server.WriteTimeout
	e.Server.IdleTimeout = cfg.Server.IdleTimeout

	// Trap interrupt/termination signals for graceful shutdown.
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

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
