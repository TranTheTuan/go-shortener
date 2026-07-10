// Command server is the single entrypoint for every role of the service. The
// first argument selects the role: "server" (default) runs the HTTP API,
// "analyze" runs the Kafka click-consumer worker (service.name
// go-shortener-consumer), and "bulk-worker" runs the bulk-job worker. One
// binary, one image: deploy as separate workloads by overriding the command
// (e.g. `main analyze`).
package main

import (
	"context"
	"errors"
	"log/slog"
	"os"

	"github.com/uptrace/opentelemetry-go-extra/otelgorm"
	"gorm.io/gorm"

	"github.com/TranTheTuan/go-shortener/configs"
	"github.com/TranTheTuan/go-shortener/pkg/database"
	"github.com/TranTheTuan/go-shortener/pkg/observability"
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
	// Wrap the JSON handler so every log line carries the active span's
	// trace_id/span_id (when tracing is on) for Loki↔Tempo correlation.
	slog.SetDefault(slog.New(observability.NewTraceHandler(slog.NewJSONHandler(os.Stdout, nil))))

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
	case "bulk-worker":
		err = runBulkWorker()
	default:
		slog.Error(`unknown mode (want "server", "analyze", or "bulk-worker")`, "mode", mode)
		os.Exit(2)
	}

	// context.Canceled is the normal result of a signal-triggered shutdown.
	if err != nil && !errors.Is(err, context.Canceled) {
		slog.Error("exited with error", "mode", mode, "error", err)
		os.Exit(1)
	}
}

// setupTracing installs the global TracerProvider for a role (service.name).
// No-op + no-op shutdown when TRACING_ENABLED is false.
func setupTracing(ctx context.Context, cfg configs.Config, serviceName string) (func(context.Context) error, error) {
	return observability.SetupTracing(ctx, observability.Config{
		Enabled:     cfg.Tracing.Enabled,
		Endpoint:    cfg.Tracing.OTLPEndpoint,
		SampleRatio: cfg.Tracing.SampleRatio,
		ServiceName: serviceName,
		Version:     cfg.ServiceVersion,
		Env:         cfg.Env,
	})
}

// openPostgres opens the Postgres connection shared by all roles and installs
// the OTel GORM plugin so DB queries appear as spans under the request trace.
func openPostgres(cfg configs.Config) (*gorm.DB, error) {
	db, err := database.NewPostgres(cfg.Database.DSN(), database.PostgresOptions{
		MaxOpenConns:    cfg.Database.MaxOpenConns,
		MaxIdleConns:    cfg.Database.MaxIdleConns,
		ConnMaxLifetime: cfg.Database.ConnMaxLifetime,
	})
	if err != nil {
		return nil, err
	}
	if err := db.Use(otelgorm.NewPlugin()); err != nil {
		return nil, err
	}
	return db, nil
}
