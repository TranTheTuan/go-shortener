// Command server is the single entrypoint for both roles of the service. The
// first argument selects the role — "server" (default) runs the HTTP API,
// "consumer" runs the Kafka click-consumer worker. One binary, one image:
// deploy as two workloads by overriding the command (e.g. `main consumer`).
package main

import (
	"context"
	"errors"
	"log/slog"
	"os"

	"gorm.io/gorm"

	"github.com/TranTheTuan/go-shortener/configs"
	"github.com/TranTheTuan/go-shortener/pkg/database"
)

// @title                      Go URL Shortener API
// @version                    1.0
// @description                URL shortener with click analytics, built on Echo + GORM + PostgreSQL.
// @description                All responses use a uniform envelope: success payloads under `data`, errors under `error`.
// @BasePath                   /
// @securityDefinitions.apikey BearerAuth
// @in                         header
// @name                       Authorization
// @description                Keycloak access token. Format: "Bearer {token}".
func main() {
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, nil)))

	mode := "server"
	if len(os.Args) > 1 {
		mode = os.Args[1]
	}

	var err error
	switch mode {
	case "server":
		err = runServer()
	case "analyze":
		err = runAnalyzeConsumer()
	default:
		slog.Error(`unknown mode (want "server" or "analyze")`, "mode", mode)
		os.Exit(2)
	}

	// context.Canceled is the normal result of a signal-triggered shutdown.
	if err != nil && !errors.Is(err, context.Canceled) {
		slog.Error("exited with error", "mode", mode, "error", err)
		os.Exit(1)
	}
}

// openPostgres opens the Postgres connection shared by both roles.
func openPostgres(cfg configs.Config) (*gorm.DB, error) {
	return database.NewPostgres(cfg.Database.DSN(), database.PostgresOptions{
		MaxOpenConns:    cfg.Database.MaxOpenConns,
		MaxIdleConns:    cfg.Database.MaxIdleConns,
		ConnMaxLifetime: cfg.Database.ConnMaxLifetime,
	})
}
