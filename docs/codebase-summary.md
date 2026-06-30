# Codebase Summary

**Project**: Go URL Shortener (`github.com/TranTheTuan/go-shortener`)  
**Language**: Go 1.26  
**Last Updated**: 2026-06-30  
**Active Branch**: `master` (Keycloak OIDC v1.1)

## Project Overview

Production-ready HTTP backend service demonstrating clean Go architecture with URL shortening, click analytics, and user authentication. Serves as both a functional service and a reference implementation for building scalable Go services.

---

## Directory Structure & File Count

```
go-shortener/
├── cmd/                           # Binaries (2 files)
│   └── server/
│       └── main.go                # HTTP server entrypoint (135 lines)
│
├── configs/                       # Configuration (2 files)
│   ├── config.go                  # Config loading from environment (117 lines)
│   └── redis.go                   # Redis connection config (utilities)
│
├── internal/                      # Private application code (23 files)
│   ├── handler/                   # HTTP transport layer (5 handlers)
│   │   ├── auth_handler.go        # Auth endpoints: register, login, refresh, logout, me
│   │   ├── health_handler.go      # Health check (/healthz)
│   │   ├── link_handler.go        # Link management: create, stats
│   │   ├── redirect_handler.go    # Public redirect (/:code)
│   │   └── user_handler.go        # User management: list, get by ID
│   │
│   ├── service/                   # Business logic layer (5 services + tests)
│   │   ├── auth_service.go        # Register, login, refresh, logout (250 lines)
│   │   ├── auth_service_test.go   # Auth service unit tests
│   │   ├── user_service.go        # User read operations
│   │   ├── link_service.go        # Link CRUD + cache-first resolution (180 lines)
│   │   ├── link_service_test.go   # Link service unit tests
│   │   ├── analytics_service.go   # Click recording + stats (async)
│   │   ├── analytics_service_test.go
│   │   ├── mocks_test.go          # Mock repositories for testing
│   │   └── (no *service.go interface files; interfaces in repository)
│   │
│   ├── repository/                # Data access layer (6 repositories + tests)
│   │   ├── user_repository.go     # User CRUD (PostgreSQL, GORM)
│   │   ├── link_repository.go     # Link CRUD (PostgreSQL, GORM)
│   │   ├── link_cache_repository.go # Link cache (Redis, go-redis)
│   │   ├── click_repository.go    # Click event logging (PostgreSQL)
│   │   ├── refresh_token_repository.go # Refresh token storage (PostgreSQL)
│   │   ├── api_key_test.go        # Middleware tests
│   │   └── jwt_test.go
│   │
│   ├── middleware/                # HTTP middleware (2 files + tests)
│   │   ├── api_key.go             # X-API-Key validation (fail-closed)
│   │   ├── api_key_test.go
│   │   ├── jwt.go                 # Bearer token validation + UserIDFrom()
│   │   └── jwt_test.go
│   │
│   └── router/                    # Route setup (1 file)
│       └── router.go              # Echo wiring + route registration (72 lines)
│
├── pkg/                           # Reusable packages (9 files)
│   ├── apperror/                  # Structured error type (85 lines)
│   │   └── apperror.go            # Error type + constructors (BadRequest, NotFound, etc.)
│   │
│   ├── response/                  # HTTP response helpers (utilities)
│   │   └── response.go            # Success() + Error() envelope wrapper
│   │
│   ├── database/                  # Database connection factories (2 files)
│   │   ├── postgres.go            # PostgreSQL connection setup (GORM)
│   │   └── redis.go               # Redis client setup (go-redis)
│   │
│   ├── keycloak/                  # Keycloak OIDC token validation (2 files + tests)
│   │   ├── verifier.go            # go-oidc wrapper (RS256, lazy JWKS)
│   │   └── verifier_test.go       # Token verification tests
│   │
│   └── shortcode/                 # Random code generation (2 files + tests)
│       ├── shortcode.go           # Crypto-random base62 generator (35 lines)
│       └── shortcode_test.go
│
├── migrations/                    # SQL migrations (12 files: 6 up + 6 down)
│   ├── 000001_create_users_table.{up,down}.sql
│   ├── 000002_create_links_table.{up,down}.sql
│   ├── 000003_create_clicks_table.{up,down}.sql
│   ├── 000004_add_user_credentials.{up,down}.sql      # Username (kept)
│   ├── 000005_create_refresh_tokens_table.{up,down}.sql # Removed in 000009
│   └── 000009_keycloak_auth.{up,down}.sql             # Add keycloak_sub, drop password_hash + refresh_tokens
│
├── docs/                          # Documentation (Markdown, maintained)
│   ├── README.md                  # Swagger/docs placeholder
│   ├── project-overview-pdr.md    # Project vision & requirements
│   ├── code-standards.md          # Go coding standards for project
│   ├── codebase-summary.md        # This file
│   ├── system-architecture.md     # Architecture & data flows
│   ├── project-roadmap.md         # Feature roadmap & status
│   └── deployment-guide.md        # Deployment instructions
│
├── Dockerfile                     # Multi-stage Docker build
├── Makefile                       # Build targets (run, test, build, migrate, lint)
├── go.mod                         # Go module definition
├── go.sum                         # Dependency checksums
├── .env.example                   # Environment template
└── README.md                      # Project root documentation (main README)
```

**Total Go Files**: ~30 (12 handler/service, 6 repository, 8 pkg, 2 config, 1 cmd)  
**Total Test Files**: ~6 (*_test.go)  
**Total Lines of Code**: ~2,500-3,000 (excluding tests)  
**Total Documentation**: ~3,500 lines (in docs/ directory)

---

## Key Modules

### 1. HTTP Server & Routing
**Files**: `cmd/server/main.go`, `internal/router/router.go`

Responsibility: Startup, graceful shutdown, Echo instance setup, middleware wiring.

**Key Features**:
- Environment-based configuration loading
- PostgreSQL connection pooling
- Redis client initialization
- Dependency wiring (handler → service → repository)
- Signal handling (SIGINT/SIGTERM)
- Graceful shutdown (10s timeout for in-flight requests)
- JSON logging via log/slog

**Entry Points**:
- `main()`: Entrypoint
- `run()`: Application logic (returns error)

---

### 2. Configuration Management
**Files**: `configs/config.go`, `configs/redis.go`

Responsibility: Load config from environment, apply defaults, validate.

**Key Types**:
- `Config`: Top-level container (Env, Server, Database, Shortener, Redis, Auth)
- `ServerConfig`: HTTP server settings (host, port, timeouts)
- `DatabaseConfig`: PostgreSQL connection (host, port, user, credentials, pooling)
- `ShortenerConfig`: URL shortener settings (base URL, API keys, code length, cache TTL)
- `RedisConfig`: Redis connection (host, port, credentials)
- `AuthConfig`: Authentication settings (JWT secret, token TTL, bcrypt cost)

**Key Functions**:
- `Load()`: Parse environment + apply defaults
- `Config.validate()`: Check production invariants (reject dev JWT secret outside dev)

**Environment Parsing**:
- Uses `caarlos0/env` library
- Struct tags: `env:"VAR_NAME"`, `envPrefix:"PREFIX_"`, `envDefault:"value"`
- All variables have sensible defaults (server runs out-of-box)

---

### 3. Error Handling
**Files**: `pkg/apperror/apperror.go`

Responsibility: Structured application errors (HTTP status + code + message).

**Key Type**: `Error`
- `Status`: HTTP status code (400, 404, 409, 410, 500)
- `Code`: Machine-readable code (BAD_REQUEST, NOT_FOUND, CONFLICT, GONE, INTERNAL)
- `Message`: Human-readable message (client-safe, never internal details)
- `Err`: Wrapped internal cause (logged, never exposed)

**Constructors**:
- `New(status, code, message)`: Custom error
- `BadRequest(msg)`: HTTP 400
- `NotFound(msg)`: HTTP 404
- `Conflict(msg)`: HTTP 409 (duplicate)
- `Gone(msg)`: HTTP 410 (expired)
- `Internal(err)`: HTTP 500 (wraps internal error)

**Usage Pattern**:
- Services return `*apperror.Error` (domain errors)
- Handlers convert to HTTP responses via `response.Error(c, err)`
- Internal errors wrapped to prevent exposure: `apperror.Internal(databaseErr)`

---

### 4. HTTP Response Envelope
**Files**: `pkg/response/response.go`

Responsibility: Uniform JSON response formatting.

**Key Functions**:
- `Success(c, status, data)`: Wrap data in `{"data": ...}`
- `Error(c, err)`: Wrap error in `{"error": {"code": "...", "message": "..."}}`

**Response Format**:
```json
// Success (HTTP 200/201)
{"data": {"id": 1, "name": "Alice", ...}}

// Error (HTTP 400/404/500/etc)
{"error": {"code": "NOT_FOUND", "message": "user not found"}}
```

---

### 5. Database Layer
**Files**: `pkg/database/postgres.go`, `pkg/database/redis.go`

**PostgreSQL**:
- Connection factory: `NewPostgres(dsn, options)`
- GORM ORM (auto-migrations disabled; manual control)
- Connection pooling: configurable max open/idle/lifetime

**Redis**:
- Client factory: `SetupRedis(cfg)`
- go-redis/v9 library
- Connection pooling: pool size, min idle conns

---

### 6. Repository Layer
**Files**: `internal/repository/*.go` (6 implementations)

Responsibility: Data access abstractions (PostgreSQL + Redis).

| Repository | Storage | Key Methods |
|------------|---------|------------|
| `UserRepository` | PostgreSQL | GetByID, GetByEmail, GetByUsername, Create, Update, Delete, List |
| `LinkRepository` | PostgreSQL | GetByCode, GetByURL, Create, Update, Delete |
| `LinkCacheRepository` | Redis | Get (by code), Set (with TTL), Delete |
| `ClickRepository` | PostgreSQL | Create (insert click event), GetStats (by link) |
| `RefreshTokenRepository` | PostgreSQL | Create, GetByHash, UpdateRevokedAt, Delete |

**Interface-driven design**:
- Each repository defines an interface in same package
- Services depend on interfaces (testable, mockable)
- Multiple implementations possible (GORM, sqlc, etc.)

---

### 7. Service Layer
**Files**: `internal/service/*service.go` (5 services)

Responsibility: Business logic, validation, orchestration.

| Service | Methods | Logic |
|---------|---------|-------|
| `UserService` | GetByID, List | User read operations |
| `LinkService` | Create, Resolve | Link CRUD + cache-first resolution + collision retry |
| `AnalyticsService` | RecordClick, GetStats | Click recording (async) + stats |
| `AuthService` | Register, Login, Refresh, Logout | Auth flow + token rotation + bcrypt |
| (Implicit) | | All services take repository interfaces |

**Key Patterns**:
- Validation on service methods (not handlers)
- Error wrapping: internal errors → `apperror.Internal(err)`, validation errors → `apperror.BadRequest(msg)`
- Context passing for cancellation + deadline
- Mockable: tests inject mock repositories

---

### 8. Handler Layer
**Files**: `internal/handler/*handler.go` (5 handlers)

Responsibility: HTTP request parsing, service invocation, response formatting.

| Handler | Endpoints | Methods |
|---------|-----------|---------|
| `HealthHandler` | GET /healthz | Health() |
| `UserHandler` | GET /users, GET /users/:id | List, Get |
| `LinkHandler` | POST /api/links, GET /api/links/:code/stats | Create, Stats |
| `RedirectHandler` | GET /:code | Redirect (302) |
| `AuthHandler` | POST /auth/* | Register, Login, Refresh, Logout, Me |

**Pattern**:
1. Parse request body
2. Call service method
3. Convert result to response (or error)
4. Return via `response.Success()` or `response.Error()`

---

### 9. Middleware
**Files**: `internal/middleware/*.go` (1 middleware + tests)

| Middleware | Purpose | Scope |
|-----------|---------|-------|
| `Keycloak` | Validate Bearer token (Keycloak-issued), JIT-provision user | Protected routes (`/api/*`, `/auth/me`, etc.) |

**Keycloak Middleware**:
- Extracts Bearer token from Authorization header
- Validates signature (RS256) + expiry + issuer + audience (optional)
- Calls `UserService.SyncFromKeycloak` to get-or-create local user from Keycloak identity
- Sets local user ID in context (downstream sees only int64 id, not Keycloak sub)
- Accessor: `UserIDFrom(c)` retrieves local user ID from context
- Returns 401 on invalid/expired/wrong-aud token; 500 on JIT DB error

---

### 10. Keycloak Token Verification
**Files**: `pkg/keycloak/verifier.go`, `pkg/keycloak/verifier_test.go`

Responsibility: Keycloak OIDC token validation (RS256, lazy JWKS fetching).

**Key Type**: `TokenVerifier` interface
- Constructor: `NewVerifier(cfg KeycloakConfig)` → wraps go-oidc
- Methods: `Verify(ctx, rawToken)` → (*Identity, error)

**Identity Struct**:
- `Sub`: Keycloak subject UUID (unique user identifier)
- `Email`: User email claim
- `Username`: preferred_username claim

**Verification Details**:
- Algorithm: RS256 (HMAC signature, keys from Keycloak JWKS)
- JWKS endpoint: In-cluster (lazy fetch, auto-refresh, cached)
- Validates: signature, issuer (iss), expiry (exp), audience (aud, optional)
- No startup block: JWKS fetched on first token validation

---

### 11. Short Code Generation
**Files**: `pkg/shortcode/shortcode.go`, `pkg/shortcode/shortcode_test.go`

Responsibility: Cryptographic random base62 code generation.

**Key Function**: `Generate(n int) string`
- Uses `crypto/rand` (secure randomness)
- Alphabet: 0-9, a-z, A-Z (62 chars)
- Default length: 7 (configurable via `SHORTENER_CODE_LENGTH`)

**Collision handling**: Service retries generation (max 5 attempts) on uniqueness check failure.

---

### 12. Database Schema
**Files**: `migrations/000{1-5}*.sql` (5 migrations)

```
Migration 1: users table
  - id, name, email (UNIQUE), created_at, updated_at

Migration 2: links table
  - id, short_code (UNIQUE), original_url, expires_at, created_at

Migration 3: clicks table
  - id, link_id (FK), clicked_at, referrer, ip_address, user_agent

Migration 4: Alter users
  - Add username (UNIQUE), password_hash
  - Make name nullable

Migration 5: refresh_tokens table
  - id, user_id (FK), token_hash (UNIQUE), expires_at, revoked_at, created_at
```

---

## Dependencies

### Direct Dependencies (go.mod)

| Module | Version | Purpose |
|--------|---------|---------|
| `github.com/labstack/echo/v4` | v4.15.4 | HTTP web framework |
| `gorm.io/gorm` | v1.31.1 | ORM framework |
| `gorm.io/driver/postgres` | v1.6.0 | PostgreSQL driver for GORM |
| `github.com/redis/go-redis/v9` | v9.20.1 | Redis client |
| `github.com/coreos/go-oidc/v3` | v3.x | OIDC token validation (Keycloak) |
| `github.com/caarlos0/env/v10` | v10.0.0 | Environment config parsing |
| `github.com/swaggo/echo-swagger` | v1.5.2 | Swagger/OpenAPI UI |
| `github.com/swaggo/swag` | v1.16.2 | Swagger code generation |

### Transitive Dependencies
- Echo dependencies: labstack/gommon, fasttemplate, etc.
- Database: jackc/pgx, jinzhu/inflection, etc.
- Redis: cespare/xxhash, etc.
- JWT: go-json, etc.
- Swagger: openapi packages, yaml, etc.

**Total**: ~50 transitive dependencies (vendoring optional)

---

## Recent Features & Branches

### Completed (master branch)
- ✅ Base HTTP server with graceful shutdown
- ✅ URL shortening with collision retry
- ✅ Click analytics (async recording)
- ✅ Redis caching (cache-first link resolution)
- ✅ API Key authentication (fail-closed)
- ✅ Swagger/OpenAPI documentation
- ✅ Database migrations (manual control)
- ✅ Error handling (structured apperror type)

### In Progress (Next: v1.2 role-based authorization)
- 🔄 Keycloak realm roles mapping
- 🔄 Admin endpoint authorization
- 🔄 Fine-grained access control

### Future (Planned)
- 📅 Rate limiting per API key
- 📅 Admin dashboard (web UI)
- 📅 Custom short codes
- 📅 Link ownership & permissions
- 📅 Prometheus metrics
- 📅 Multi-database support (MySQL)

---

## Testing Strategy

### Unit Tests (Service Layer)
- Mock repositories via hand-written mocks
- Test validation rules (email format, username regex, password length)
- Test error cases (not found, conflict, etc.)
- Test token generation/rotation
- Table-driven tests for multiple scenarios

### Files**:
- `internal/service/*_test.go`: Service unit tests
- `internal/service/mocks_test.go`: Mock repository implementations
- `internal/middleware/*_test.go`: Middleware unit tests
- `pkg/token/token_test.go`: JWT tests
- `pkg/shortcode/shortcode_test.go`: Code generation tests

### Test Coverage
- Target: >85% on services
- Mocks: Repository interfaces (allow easy swapping)
- No live database/Redis in unit tests

### Integration Tests
- Run with real PostgreSQL + Redis (post v1.1)
- Test full auth flow: register → login → refresh → logout
- Test cache hit/miss scenarios
- Test link expiry validation

---

## Code Quality

### Standards Enforced
- **Go version**: 1.26 (no legacy code)
- **Error handling**: Structured `apperror.Error` (no plain `errors.New()`)
- **Response format**: Uniform envelope (no ad-hoc responses)
- **Interfaces**: Repository interfaces for testability
- **File size**: Keep under 200 LOC per file
- **Naming**: snake_case for files, camelCase for variables, PascalCase for exports

### Tools
- `gofmt`: Auto-format (via IDE)
- `golangci-lint`: Linting (non-blocking, prioritize functionality)
- Tests: Run via `make test` before commit

---

## Build & Deployment

### Makefile Targets
```bash
make run                # Run server (development)
make build              # Build binary to ./build/main
make test               # Run tests
make tidy               # Tidy dependencies
make lint               # Run linter
make migrate-up         # Apply migrations
make migrate-down NUM=1 # Rollback 1 migration
make migrate-create     # Create new migration
```

### Build Artifacts
- **Binary**: `./build/main` (Linux 64-bit by default)
- **Docker image**: Multi-stage build (small final image)
- **Database**: Migrations in `migrations/` directory

---

## Security Posture

| Concern | Implementation |
|---------|-----------------|
| **Passwords** | Delegated to Keycloak (not stored locally) |
| **Tokens** | RS256 (Keycloak-issued, validated locally via JWKS) |
| **JIT identity** | Keycloak sub → local user (get-or-create on first request) |
| **Token validation** | Signature (RS256), expiry, issuer, audience (optional) |
| **Auth errors** | Generic 401 (no user enumeration) |
| **Claims** | Email + username from Keycloak claims (pre-validated) |
| **Secrets** | Keycloak ISSUER/JWKS URLs in config; never tokens in logs |
| **Database** | Parameterized queries via GORM (SQL injection safe) |

---

## Known Limitations

1. **Single-instance**: No multi-instance token cache coordination
2. **No custom codes**: All codes generated randomly
3. **No per-user rate limiting**: Quota fixed per plan (same for all users)
4. **Async analytics loss**: Clicks not guaranteed (fire-and-forget)
5. **No caching invalidation**: Expired links remain cached until TTL
6. **Demo users orphaned**: Pre-migration users with null keycloak_sub won't map to Keycloak

---

## Next Steps

1. **Deploy v1.1** (Keycloak OIDC): Staging + production rollout
2. **Implement v1.2**: Role-based authorization (Keycloak realm roles)
3. **Implement v1.3**: Link management (delete, update, custom codes)
4. **Add rate limiting** (v1.5): Per-user quota enforcement
5. **Observability** (v1.6): Prometheus metrics + Keycloak health checks

---

**Last Updated**: 2026-06-30  
**Maintainer**: @TranTheTuan  
**License**: MIT  
**Auth Model**: Keycloak OIDC resource server (v1.1+)
