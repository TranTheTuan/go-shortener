// Command server is the HTTP API entrypoint. It loads configuration, connects
// to the database, wires the handler/service/repository layers together, and
// runs the Echo server with graceful shutdown.
package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"github.com/TranTheTuan/YOUR-REPO-NAME/configs"
	"github.com/TranTheTuan/YOUR-REPO-NAME/internal/handler"
	"github.com/TranTheTuan/YOUR-REPO-NAME/internal/repository"
	"github.com/TranTheTuan/YOUR-REPO-NAME/internal/router"
	"github.com/TranTheTuan/YOUR-REPO-NAME/internal/service"
	"github.com/TranTheTuan/YOUR-REPO-NAME/pkg/database"
)

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

	// Wire dependencies: repository -> service -> handler.
	userRepo := repository.NewUserRepository(db)
	userSvc := service.NewUserService(userRepo)

	e := router.New(router.Handlers{
		Health: handler.NewHealthHandler(),
		User:   handler.NewUserHandler(userSvc),
	})
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
