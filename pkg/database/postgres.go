// Package database provides helpers for establishing database connections.
package database

import (
	"fmt"
	"time"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

// PostgresOptions configures the connection pool.
type PostgresOptions struct {
	MaxOpenConns    int
	MaxIdleConns    int
	ConnMaxLifetime time.Duration
}

// NewPostgres opens a GORM connection to PostgreSQL using the given DSN,
// configures the underlying connection pool, and verifies connectivity.
func NewPostgres(dsn string, opts PostgresOptions) (*gorm.DB, error) {
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{
		// TranslateError maps driver-specific errors onto GORM sentinels such
		// as gorm.ErrDuplicatedKey so the repository can detect conflicts.
		TranslateError: true,
	})
	if err != nil {
		return nil, fmt.Errorf("database: open postgres: %w", err)
	}

	sqlDB, err := db.DB()
	if err != nil {
		return nil, fmt.Errorf("database: access sql.DB: %w", err)
	}

	sqlDB.SetMaxOpenConns(opts.MaxOpenConns)
	sqlDB.SetMaxIdleConns(opts.MaxIdleConns)
	sqlDB.SetConnMaxLifetime(opts.ConnMaxLifetime)

	if err := sqlDB.Ping(); err != nil {
		return nil, fmt.Errorf("database: ping: %w", err)
	}

	return db, nil
}
