// Package configs loads application configuration from the environment,
// applying sensible defaults so the server runs out of the box. Parsing is
// driven by struct tags via github.com/caarlos0/env.
package configs

import (
	"errors"
	"fmt"
	"time"

	"github.com/caarlos0/env/v10"
)

// defaultDevJWTSecret is the placeholder signing secret. It is accepted only in
// the development environment; any other environment must override it.
const defaultDevJWTSecret = "dev-insecure-change-me"

// Config is the top-level configuration container. Nested sections are given
// an envPrefix so their variables are namespaced (e.g. DB_HOST, SERVER_PORT).
type Config struct {
	Env       string          `env:"ENV" envDefault:"development"`
	Server    ServerConfig    `envPrefix:"SERVER_"`
	Database  DatabaseConfig  `envPrefix:"DB_"`
	Shortener ShortenerConfig `envPrefix:"SHORTENER_"`
	Redis     RedisConfig     `envPrefix:"REDIS_"`
	Auth      AuthConfig      `envPrefix:"AUTH_"`
}

// AuthConfig holds authentication settings.
type AuthConfig struct {
	// JWTSecret signs access tokens (HS256). The development default is rejected
	// in any other environment (fail-closed).
	JWTSecret string `env:"JWT_SECRET" envDefault:"dev-insecure-change-me"`
	// AccessTTL is the lifetime of an access token.
	AccessTTL time.Duration `env:"ACCESS_TTL" envDefault:"15m"`
	// RefreshTTL is the lifetime of a refresh token.
	RefreshTTL time.Duration `env:"REFRESH_TTL" envDefault:"168h"`
	// BcryptCost is the bcrypt work factor for password hashing.
	BcryptCost int `env:"BCRYPT_COST" envDefault:"12"`
}

// ShortenerConfig holds URL-shortener settings.
type ShortenerConfig struct {
	// BaseURL is the public origin used to build short URLs (e.g. https://sho.rt).
	BaseURL string `env:"BASE_URL" envDefault:"http://localhost:8080"`
	// APIKeys is the set of keys accepted on the X-API-Key header. An empty set
	// rejects every write request (fail-closed).
	APIKeys []string `env:"API_KEYS" envSeparator:","`
	// CodeLength is the number of base62 characters in generated short codes.
	CodeLength int `env:"CODE_LENGTH" envDefault:"7"`
	// CacheTTL is the default Redis TTL for links without an expiry date.
	CacheTTL time.Duration `env:"CACHE_TTL" envDefault:"24h"`
}

// ServerConfig holds the HTTP server settings.
type ServerConfig struct {
	Host            string        `env:"HOST" envDefault:"0.0.0.0"`
	Port            int           `env:"PORT" envDefault:"8080"`
	ReadTimeout     time.Duration `env:"READ_TIMEOUT" envDefault:"5s"`
	WriteTimeout    time.Duration `env:"WRITE_TIMEOUT" envDefault:"10s"`
	IdleTimeout     time.Duration `env:"IDLE_TIMEOUT" envDefault:"120s"`
	ShutdownTimeout time.Duration `env:"SHUTDOWN_TIMEOUT" envDefault:"10s"`
	// PprofAddr is the address for the pprof debug server. Empty string disables it.
	PprofAddr string `env:"PPROF_ADDR" envDefault:"localhost:6060"`
}

// Addr returns the address the HTTP server should listen on.
func (s ServerConfig) Addr() string {
	return fmt.Sprintf("%s:%d", s.Host, s.Port)
}

// DatabaseConfig holds the PostgreSQL connection settings.
type DatabaseConfig struct {
	Host     string `env:"HOST" envDefault:"localhost"`
	Port     int    `env:"PORT" envDefault:"5432"`
	User     string `env:"USER" envDefault:"postgres"`
	Password string `env:"PASSWORD" envDefault:"postgres"`
	Name     string `env:"NAME" envDefault:"app"`
	SSLMode  string `env:"SSLMODE" envDefault:"disable"`
	TimeZone string `env:"TIMEZONE" envDefault:"UTC"`

	MaxOpenConns    int           `env:"MAX_OPEN_CONNS" envDefault:"25"`
	MaxIdleConns    int           `env:"MAX_IDLE_CONNS" envDefault:"25"`
	ConnMaxLifetime time.Duration `env:"CONN_MAX_LIFETIME" envDefault:"5m"`
}

// DSN builds a PostgreSQL data source name suitable for GORM's postgres driver.
func (d DatabaseConfig) DSN() string {
	return fmt.Sprintf(
		"host=%s port=%d user=%s password=%s dbname=%s sslmode=%s TimeZone=%s",
		d.Host, d.Port, d.User, d.Password, d.Name, d.SSLMode, d.TimeZone,
	)
}

// Load parses configuration from environment variables into a Config,
// applying the defaults declared in the struct tags.
func Load() (Config, error) {
	var cfg Config
	if err := env.Parse(&cfg); err != nil {
		return Config{}, fmt.Errorf("config: parse environment: %w", err)
	}
	if err := cfg.validate(); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

// validate enforces invariants that struct-tag defaults cannot express.
func (c Config) validate() error {
	// Fail closed: outside development the JWT secret must be set to a real value.
	if c.Env != "development" &&
		(c.Auth.JWTSecret == "" || c.Auth.JWTSecret == defaultDevJWTSecret) {
		return errors.New("config: AUTH_JWT_SECRET must be set to a non-default value outside development")
	}
	return nil
}
