# Project Changelog

All notable changes to the go-shortener project are documented here.

## [Unreleased]

### Added
- **Advanced Analytics with Tiered Feature Gating** — Timeseries, referrer, and device analytics gated by subscription plan
  - 4 new database tables: `plan_features` (feature flags per plan), `click_stats_daily`, `click_stats_referrer`, `click_stats_device` (rollup tables)
  - Pro/Business plans seeded with `analytics.timeseries`, `analytics.referrers`, `analytics.devices` feature flags; Basic plan has none
  - New pkg: `pkg/useragent` — Parse user-agent to device category (Desktop, Mobile, Tablet, Bot)
  - New pkg: `pkg/referrer` — Extract referrer domain from request header
  - EntitlementService: Resolves user→plan→feature flag for authorization checks
  - Rollup writes integrated into Kafka consumer batch commit: parse UA/referrer at write-time, upsert aggregates (daily count, referrer breakdown, device breakdown)
  - New endpoint: `GET /api/links/:code/analytics?range=7d|30d|90d` — Owner-scoped, feature-gated (401 for non-owner, 403 if no entitlement), returns `{timeseries: [...], referrers: [...], devices: [...]}`
  - Frontend: Chart.js charts shown for Pro/Business users; upgrade prompt for Basic plan
  - Schema migration: 4 new tables with unique indexes on (link_id, date_utc), (link_id, referrer), (link_id, device_type), (plan_id, feature_key)

### Added
- **Terms & Conditions Acceptance Gate** — Versioned T&C acceptance with modal UX
  - DB migration: `user_agreements` table tracks per-user acceptance (user_id, version, accepted_at)
  - Backend endpoint `POST /api/agreements/accept/{version}` for acceptance tracking
  - Versioning: Billing rule changes bump T&C version; users re-accept on login if they haven't accepted latest
  - Modal UI: Shows current T&C version, decline option closes app, accept records timestamp
  - Unauthenticated users see T&C on first login; subsequent logins skip if version unchanged
  - Integration: Runs before subscription/billing operations; required for accessing billing features

- **Billing: Plan + Interval Change** — Allow users to change subscription tier AND billing interval (monthly ↔ yearly)
  - Renamed `UpgradeSubscription` → `ChangeSubscription` service method
  - Endpoint `POST /api/subscription/upgrade` now accepts `{plan_id, interval}` (was plan_id only)
  - Validation: downgrades blocked; same-tier + different-interval allowed; same tier + same interval rejected as no-op
  - Frontend: Button logic updated to show "Switch to yearly/monthly" for same-plan interval changes
  - Frontend: activeInterval toggle now initializes from current subscription (was hardcoded to monthly)

- **Link Management (CRUD)** — Owner-only link management with hard delete, enable/disable, and expiry editing
  - `DELETE /api/links/:code` — Hard delete (cascades clicks, returns 204)
  - `PUT /api/links/:code` — Update mutable state: `{expires_at: RFC3339|null, is_active: bool}` (returns 200)
  - `GET /api/links?status=active|disabled|expired` — Filter links by status (empty=all, mutually exclusive buckets)
  - New column `links.is_active BOOL NOT NULL DEFAULT true` (migration 000011)
  - Disabled links return 410 Gone on redirect; never cached
  - Frontend: Status badges (active/disabled/expired), row actions (Enable/Disable, edit expiry, Delete), status filter dropdown
  - Cache invalidation on all mutations; dedup cache evicted on delete

### Fixed
- **Quota Display Bug** — Business plan showed "9223372036854776000 links remaining" instead of "Unlimited"
  - Root cause: Frontend checked for 32-bit max (2147483647) but service returns 64-bit max (math.MaxInt = 9223372036854775807)
  - Fix: Update quota check to use 64-bit max threshold with `>=` guard

### Notes
- Swagger/OpenAPI docs pending regeneration (`make swag` not yet run)

---

## [Unreleased - Observability] — 2026-07-10

### Added
- **OpenTelemetry Metrics (Prometheus)** — Observable production-ready monitoring
  - `/metrics` endpoint on `0.0.0.0:9464` (configurable via `SERVER_METRICS_ADDR`, in-cluster only)
  - HTTP RED middleware: `http_server_request_duration_seconds` (histogram), `http_server_active_requests` (gauge), labeled by method/route-template/status
  - Domain counters: `redirects_total`, `link_cache_lookups_total`, `quota_rejections_total`, `click_events_total`; observable: `redis_breaker_open`
  - Cardinality rule: route labels use template only (e.g., `/api/links/:code`), never user_id/short_code/URL
  - Kubernetes ServiceMonitor integration (label `release: proxy-monitor`)
  - Grafana dashboard JSON (`../go-shortener-infra/monitoring/grafana-dashboard-go-shortener.json`)
  - Go runtime metrics included

- **Distributed Tracing (OpenTelemetry → Grafana Tempo)** — Third observability pillar
  - OTLP gRPC export → Alloy DaemonSet (forward-only) → Tempo (filesystem, 72h retention)
  - Head-based sampling (ParentBased + TraceIDRatioBased, default 100% keep); decision propagated via W3C `tracecontext`
  - Auto-instrumentation: `otelecho` (HTTP), `redisotel` (Redis), `otelgorm` (GORM), `kotel` (Kafka)
  - **L1-cache protection**: `GET /:code` excluded from tracing to preserve hot-path performance
  - Async trace continuity: bulk-job producer→Kafka→consumer→PG renders as single trace via baggage propagation
  - Correlation: `slog_trace_handler` stamps `trace_id`/`span_id` on logs → Loki derived field jumps to Tempo; Tempo `tracesToLogsV2` returns to logs
  - Config: `TRACING_ENABLED` (opt-in, default false), `TRACING_OTLP_ENDPOINT`, `TRACING_SAMPLE_RATIO`, `SERVICE_VERSION`
  - New pkg: `pkg/observability/` (tracing.go, slog_trace_handler.go)
  - Infrastructure: Alloy manifests + Tempo config in `../hdp-infra/monitoring/` and `../go-shortener-infra/`

### Notes
- Workers not yet instrumented; server-side only
- Tempo metrics-generator (service graph) deferred to phase 2
- Tracing fully opt-in; no-op when `TRACING_ENABLED=false`

---

## [v1.1] — 2026-07-06 (Master branch)

### Added
- **Keycloak OIDC Integration** — Replace username/password auth with federated identity
  - Bearer token validation (RS256 via JWKS)
  - JIT user provisioning (get-or-create from Keycloak sub)
  - Token expiry + issuer validation
  - New column `users.keycloak_sub VARCHAR(36) UNIQUE` (migration 000009)

### Changed
- Auth endpoints now return synced Keycloak user (no local password storage)
- Refresh token flow removed (clients use Keycloak tokens directly)

### Removed
- Local password hashing (bcrypt, salt storage)
- Refresh token table (migration 000009 rolls back migration 000005)
- Username/password login endpoints

---

## [v1.0] — 2026-06-30

### Added
- **Core URL Shortener** — Create, list, and redirect short links
  - POST /api/links — Create link with optional expiry
  - GET /api/links/:code/stats — Click statistics
  - GET /:code — Public redirect (302)
  - GET /api/links — List user's links with counts
  - X-API-Key authentication

### Analytics
- Async click recording (Fire-and-forget goroutine)
- Per-link click stats (count, last click)
- Click metadata: referrer, IP, user-agent

### Caching
- Redis link cache (code → original URL)
- Cache-first resolution (Redis hit → 302; miss → PostgreSQL lookup → cache + 302)
- 24-hour TTL (configurable)
- Expired links return 410 Gone

### Infrastructure
- PostgreSQL backend (GORM ORM)
- Graceful shutdown (10s timeout)
- Request ID middleware
- Structured JSON logging (log/slog)
- Database migrations (manual control)

### Documentation
- Swagger/OpenAPI UI
- README with API examples
- Architecture guide (design decisions, data flows)

---

**Last Updated**: 2026-07-06  
**Maintainer**: @TranTheTuan  
**License**: MIT
