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

// Config is the top-level configuration container. Nested sections are given
// an envPrefix so their variables are namespaced (e.g. DB_HOST, SERVER_PORT).
type Config struct {
	Env       string          `env:"ENV" envDefault:"development"`
	Server    ServerConfig    `envPrefix:"SERVER_"`
	Database  DatabaseConfig  `envPrefix:"DB_"`
	Shortener ShortenerConfig `envPrefix:"SHORTENER_"`
	Redis     RedisConfig     `envPrefix:"REDIS_"`
	Keycloak  KeycloakConfig  `envPrefix:"KEYCLOAK_"`
	Quota     QuotaConfig     `envPrefix:"QUOTA_"`
	Kafka     KafkaConfig     `envPrefix:"KAFKA_"`
	R2        R2Config        `envPrefix:"R2_"`
	Tracing   TracingConfig   `envPrefix:"TRACING_"`
	Paddle    PaddleConfig    `envPrefix:"PADDLE_"`
	// ServiceVersion tags traces/metrics with the running build (git sha),
	// injected at deploy. Defaults to "dev" for local runs.
	ServiceVersion string `env:"SERVICE_VERSION" envDefault:"dev"`
}

// PaddleConfig holds Paddle Billing settings.
// Set PADDLE_ENABLED=true and provide credentials to activate billing features.
type PaddleConfig struct {
	Enabled       bool   `env:"ENABLED" envDefault:"false"`
	WebhookSecret string `env:"WEBHOOK_SECRET"` // pdl_ntf_...
	APIKey        string `env:"API_KEY"`        // server-side API key for portal sessions
	ClientToken   string `env:"CLIENT_TOKEN"`   // client-side token for Paddle.js
	BaseURL       string `env:"BASE_URL" envDefault:"https://vendors.paddle.com/api/2.0"`
}

// TracingConfig holds OpenTelemetry trace-export settings. Disabled by default
// so the app runs without a collector; enable in-cluster where Alloy/Tempo run.
type TracingConfig struct {
	// Enabled turns OTLP trace export on. Off = no TracerProvider installed.
	Enabled bool `env:"ENABLED" envDefault:"true"`
	// OTLPEndpoint is the OTLP gRPC target (the Alloy forwarder), host:port.
	OTLPEndpoint string `env:"OTLP_ENDPOINT" envDefault:"alloy.monitoring.svc.cluster.local:4317"`
	// SampleRatio is the head-sampling ratio: 1.0 keeps every trace; lower it
	// only under load testing. Decision is made once at the root and propagated.
	SampleRatio float64 `env:"SAMPLE_RATIO" envDefault:"1.0"`
}

// R2Config holds Cloudflare R2 (S3-compatible) settings.
// Endpoint is derived from AccountID: <AccountID>.r2.cloudflarestorage.com
type R2Config struct {
	AccountID       string `env:"ACCOUNT_ID"`
	AccessKeyID     string `env:"ACCESS_KEY_ID"`
	SecretAccessKey string `env:"SECRET_ACCESS_KEY"`
	Bucket          string `env:"BUCKET" envDefault:"bulk-uploads"`
}

// Enabled reports whether R2 credentials are configured.
func (r R2Config) Enabled() bool { return r.AccountID != "" && r.AccessKeyID != "" }

// KeycloakConfig holds the OIDC resource-server settings. The service validates
// Keycloak-issued access tokens; it no longer issues its own.
type KeycloakConfig struct {
	// Issuer is the public token issuer used to validate the `iss` claim,
	// e.g. https://auth.cd.me/realms/<realm>. Must match tokens byte-for-byte.
	Issuer string `env:"ISSUER"`
	// JWKSURL is the in-cluster certs endpoint the verifier fetches public keys
	// from, e.g. http://<keycloak-svc>/realms/<realm>/protocol/openid-connect/certs.
	// Build it by hand (internal host + the standard path); do NOT copy the
	// public jwks_uri from the .well-known document.
	JWKSURL string `env:"JWKS_URL"`
	// ClientID is the backend client used as the expected audience. Empty means
	// skip audience validation (oidc.Config.SkipClientIDCheck).
	ClientID string `env:"CLIENT_ID"`
}

// KafkaConfig holds Kafka producer/consumer settings.
type KafkaConfig struct {
	Brokers           []string `env:"BROKERS" envSeparator:","`
	ClickTopic        string   `env:"CLICK_TOPIC" envDefault:"link-clicks"`
	ConsumerGroup     string   `env:"CONSUMER_GROUP" envDefault:"click-consumer"`
	BulkJobTopic      string   `env:"BULK_JOB_TOPIC" envDefault:"bulk-link-jobs"`
	BulkConsumerGroup string   `env:"BULK_CONSUMER_GROUP" envDefault:"bulk-job-consumer"`
	SASLMechanism     string   `env:"SASL_MECHANISM"`
	SASLUsername      string   `env:"SASL_USERNAME"`
	SASLPassword      string   `env:"SASL_PASSWORD"`
	TLSEnabled        bool     `env:"TLS_ENABLED" envDefault:"false"`
}

// Enabled reports whether Kafka brokers are configured.
func (k KafkaConfig) Enabled() bool { return len(k.Brokers) > 0 }

// SASLEnabled reports whether SASL authentication is configured.
func (k KafkaConfig) SASLEnabled() bool { return k.SASLMechanism != "" }

// QuotaConfig holds daily-link-quota settings.
type QuotaConfig struct {
	// DefaultPlanCode is the plan applied when a user has no active subscription.
	DefaultPlanCode string `env:"DEFAULT_PLAN_CODE" envDefault:"basic"`
	// BasicFallbackLimit is the last-resort daily limit when the plans table is
	// unreachable, so a DB hiccup never blocks creation outright.
	BasicFallbackLimit int `env:"BASIC_FALLBACK_LIMIT" envDefault:"10"`
	// BreakerMaxFailures is the number of consecutive Redis failures that trips
	// the circuit breaker open.
	BreakerMaxFailures int `env:"BREAKER_MAX_FAILURES" envDefault:"10"`
	// BreakerOpenTimeout is how long the breaker stays open before a half-open probe.
	BreakerOpenTimeout time.Duration `env:"BREAKER_OPEN_TIMEOUT" envDefault:"5m"`
}

// ShortenerConfig holds URL-shortener settings.
type ShortenerConfig struct {
	// BaseURL is the public origin used to build short URLs (e.g. https://sho.rt).
	BaseURL string `env:"BASE_URL" envDefault:"http://localhost:8080"`
	// CodeLength is the number of base62 characters in generated short codes.
	CodeLength int `env:"CODE_LENGTH" envDefault:"7"`
	// CacheTTL is the default Redis TTL for links without an expiry date.
	CacheTTL time.Duration `env:"CACHE_TTL" envDefault:"24h"`
	// L1CacheSize caps entries in the per-pod in-memory (L1) redirect cache that
	// fronts Redis. 0 disables L1 (Redis-only).
	L1CacheSize int `env:"L1_CACHE_SIZE" envDefault:"50000"`
	// L1CacheTTL bounds L1 staleness. L1 can't be invalidated across pods, so a
	// mutated link (disabled/deleted/re-expired) keeps resolving from a stale L1
	// entry — on pods other than the one that handled the mutation — for at most
	// this long. Keep it short; Redis (L2) stays authoritative (evicted at once).
	L1CacheTTL time.Duration `env:"L1_CACHE_TTL" envDefault:"10s"`
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
	// MetricsAddr serves the Prometheus /metrics endpoint. Empty disables it.
	// Bind 0.0.0.0 (Prometheus scrapes the pod IP) — do NOT route via ingress.
	MetricsAddr string `env:"METRICS_ADDR" envDefault:"0.0.0.0:9464"`
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
	// Fail closed: outside development, Keycloak issuer + JWKS must be configured
	// or no request can be authenticated.
	if c.Env != "development" {
		if c.Keycloak.Issuer == "" || c.Keycloak.JWKSURL == "" {
			return errors.New("config: KEYCLOAK_ISSUER and KEYCLOAK_JWKS_URL must be set outside development")
		}
	}
	return nil
}
