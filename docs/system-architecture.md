# System Architecture

## High-Level Architecture

```
┌─────────────────────────────────────────────────────────────────┐
│                          HTTP Clients                           │
│              (browsers, mobile apps, API clients)               │
└──────────────────────────┬──────────────────────────────────────┘
                           │ HTTP/HTTPS
┌──────────────────────────▼──────────────────────────────────────┐
│                    Echo HTTP Server                             │
│  ┌──────────────────────────────────────────────────────────┐  │
│  │  Middleware (Request ID, Panic Recovery, Logging)       │  │
│  │  - API Key Auth (X-API-Key header)                      │  │
│  │  - JWT Auth (Authorization Bearer token)                │  │
│  └──────────────────────────────────────────────────────────┘  │
│                           │                                     │
│  ┌────────────┬──────────┬──────┬──────┬──────────────────┐   │
│  ▼            ▼          ▼      ▼      ▼                  ▼   │
│ Health   AuthHandler  UserHandler LinkHandler  RedirectHandler  │
│Handler   (login,      (list,get)  (create,     (/:code)        │
│          register,               stats)                        │
│          refresh,                                              │
│          logout)                                               │
└──────────────────────────┬──────────────────────────────────────┘
                           │
        ┌──────────────────┼──────────────────┐
        │                  │                  │
        ▼                  ▼                  ▼
   AuthService      LinkService         AnalyticsService
   UserService      (cache-first)       (async recording)
        │                  │                  │
        │     ┌────────────┼────────────┐    │
        │     │            │            │    │
        ▼     ▼            ▼            ▼    ▼
    UserRepo LinkRepo  LinkCacheRepo ClickRepo RefreshTokenRepo
        │        │            │            │         │
        │        │ Redis      │            │         │
        │        │ (TTL)      │            │         │
        │        └────────────┘            │         │
        │                                  │         │
        └──────────────────┬───────────────┴────────┘
                           │
            ┌──────────────┴──────────────┐
            ▼                             ▼
        PostgreSQL                    Redis
        (users,links,clicks,      (link cache,
        refresh_tokens)            key expiry)
```

## Component Overview

### HTTP Layer (Echo Server)

**Responsibility**: Transport, request parsing, response serialization

- **Entry point**: `cmd/server/main.go` (wires all layers, manages graceful shutdown)
- **Router setup**: `internal/router/router.go` (route registration + middleware wiring)
- **Handlers**: `internal/handler/` (5 handler types: health, auth, user, link, redirect)

Each handler:
1. Parses the HTTP request
2. Calls a service method
3. Converts the result to an HTTP response (or error)
4. Never imports database types directly

### Service Layer (Business Logic)

**Responsibility**: Validation, domain rules, orchestration

Located in `internal/service/`:

| Service | Purpose | Dependencies |
|---------|---------|--------------|
| `AuthService` | Register, login, refresh, logout | UserRepo, RefreshTokenRepo, TokenIssuer |
| `UserService` | Get user by ID, read operations | UserRepository |
| `LinkService` | Create link, resolve code (cache-first) | LinkRepo, LinkCacheRepo, ShortcodeGen |
| `AnalyticsService` | Record clicks, fetch stats | LinkRepo, ClickRepo |

All services depend on repository **interfaces** (testable, mockable).

### Repository Layer (Data Access)

**Responsibility**: Abstract all data access behind interfaces

Located in `internal/repository/`:

| Repository | Storage | Usage |
|------------|---------|-------|
| `UserRepository` | PostgreSQL | User CRUD |
| `LinkRepository` | PostgreSQL | Link CRUD |
| `LinkCacheRepository` | Redis | Link code → URL resolution |
| `ClickRepository` | PostgreSQL | Click event logging |
| `RefreshTokenRepository` | PostgreSQL | Token storage & revocation |

Each repository has:
- **Interface** (contract, used by services)
- **Implementation** (GORM for PostgreSQL, go-redis for Redis)

### Middleware Layer

Located in `internal/middleware/`:

| Middleware | Purpose | Scope |
|------------|---------|-------|
| `APIKey` | Validates `X-API-Key` header | `/api/*` routes (write operations) |
| `JWT` | Validates Bearer token, extracts user ID | `/auth/me`, `/auth/logout` |
| Echo built-ins | Request ID, panic recovery | All routes |

## Data Flow Diagrams

### 1. Create Short Link (POST /api/links)

```
Client Request (with X-API-Key)
    │
    ▼
APIKeyMiddleware ──► Validate key ──► Reject if no match
    │
    ▼ (if valid)
LinkHandler.Create
    │
    ├──► Parse JSON body
    │
    ├──► Call LinkService.Create(originalURL, expiresAt)
    │       │
    │       ├──► Validate URL
    │       │
    │       ├──► Check if URL exists (PostgreSQL)
    │       │    ├──► Found: return existing link
    │       │    └──► Not found: proceed to generate
    │       │
    │       ├──► Generate short code (max 5 attempts)
    │       │    ├──► Generate random base62
    │       │    ├──► Check uniqueness (PostgreSQL)
    │       │    ├──► If collision: retry
    │       │    └──► If success: break
    │       │
    │       ├──► Save link (PostgreSQL)
    │       │
    │       └──► Return Link object
    │
    └──► Format response {"data": {short_code, short_url, ...}}
```

**Result**: HTTP 201 with short link details  
**Cache**: Link is cached on first redirect (not on creation)

### 2. Redirect Short Link (GET /:code)

```
Client Request (public, no auth)
    │
    ▼
RedirectHandler.Redirect
    │
    ├──► Parse short code from URL
    │
    ├──► Call LinkService.Resolve(code)
    │       │
    │       ├──► Try Redis cache (LinkCacheRepository.Get)
    │       │    ├──► Hit: return URL
    │       │    └──► Miss: continue
    │       │
    │       ├──► Query PostgreSQL (LinkRepository.GetByCode)
    │       │    ├──► Not found: return 404
    │       │    ├──► Found but expired: return 410
    │       │    └──► Found and valid: cache + return URL
    │       │
    │       ├──► Cache in Redis (TTL from SHORTENER_CACHE_TTL)
    │       │
    │       └──► Return original URL
    │
    ├──► Record click (async)
    │    │
    │    └──► go AnalyticsService.RecordClick(...)
    │         └──► Insert into clicks table (or drop on crash)
    │
    └──► Return HTTP 302 Redirect to original URL
```

**Result**: HTTP 302 with Location header  
**Performance**: Sub-100ms on cache hit  
**Analytics**: Async click recording (acceptable loss)

### 3. Login (POST /auth/login)

```
Client Request (email + password)
    │
    ▼
AuthHandler.Login
    │
    ├──► Parse JSON body
    │
    ├──► Call AuthService.Login(email, password)
    │       │
    │       ├──► Query UserRepository.GetByEmail(email)
    │       │    └──► Not found: return "invalid credentials"
    │       │
    │       ├──► Verify password (bcrypt.CompareHashAndPassword)
    │       │    └──► Mismatch: return "invalid credentials"
    │       │
    │       ├──► Generate access token (JWT HS256)
    │       │    └──► Claims: user_id, exp (now + 15m), etc.
    │       │
    │       ├──► Generate refresh token (random 32-byte base64)
    │       │
    │       ├──► Hash refresh token (SHA256)
    │       │
    │       ├──► Store hash in refresh_tokens table
    │       │    └──► expires_at = now + 7d
    │       │
    │       └──► Return {access_token, refresh_token, ...}
    │
    └──► Format response {"data": {access_token, refresh_token, ...}}
```

**Result**: HTTP 200 with token pair  
**Security**: Password never logged; tokens not logged; hash stored (not raw)

### 4. Refresh Token (POST /auth/refresh)

```
Client Request (refresh_token)
    │
    ▼
AuthHandler.Refresh
    │
    ├──► Parse JSON body
    │
    ├──► Call AuthService.Refresh(refreshToken)
    │       │
    │       ├──► Hash token (SHA256)
    │       │
    │       ├──► Query RefreshTokenRepository.GetByHash(hash)
    │       │    ├──► Not found: return "invalid token"
    │       │    └──► Found but revoked: return "invalid token"
    │       │
    │       ├──► Check expiry (expires_at > now)
    │       │    └──► Expired: return "invalid token"
    │       │
    │       ├──► Mark old token as revoked
    │       │
    │       ├──► Issue new access + refresh tokens (same as login)
    │       │
    │       └──► Return {new_access_token, new_refresh_token, ...}
    │
    └──► Format response {"data": {...}}
```

**Result**: HTTP 200 with new token pair  
**Security**: Token rotation prevents token reuse; revocation prevents old token use

## Database Schema

### users table
```sql
id           BIGSERIAL PRIMARY KEY
username     VARCHAR(50) UNIQUE NOT NULL (added migration 4)
email        VARCHAR(255) UNIQUE NOT NULL
name         VARCHAR(255) (nullable, added migration 4)
password_hash VARCHAR(255) NOT NULL (added migration 4)
created_at   TIMESTAMPTZ NOT NULL DEFAULT now()
updated_at   TIMESTAMPTZ NOT NULL DEFAULT now()
```

### links table
```sql
id           BIGSERIAL PRIMARY KEY
short_code   VARCHAR(16) UNIQUE NOT NULL
original_url TEXT NOT NULL
expires_at   TIMESTAMPTZ (nullable)
created_at   TIMESTAMPTZ NOT NULL DEFAULT now()
```

### clicks table
```sql
id           BIGSERIAL PRIMARY KEY
link_id      BIGINT NOT NULL FK→links(id) CASCADE
clicked_at   TIMESTAMPTZ NOT NULL
referrer     VARCHAR(255)
ip_address   VARCHAR(45)          (IPv6: 39 chars + safety)
user_agent   TEXT
```

### refresh_tokens table
```sql
id           BIGSERIAL PRIMARY KEY
user_id      BIGINT NOT NULL FK→users(id) CASCADE
token_hash   VARCHAR(64) UNIQUE NOT NULL
expires_at   TIMESTAMPTZ NOT NULL
revoked_at   TIMESTAMPTZ         (nullable; NULL = active)
created_at   TIMESTAMPTZ NOT NULL DEFAULT now()
```

**Indexes**:
- `users(email)` — UNIQUE
- `users(username)` — UNIQUE
- `links(short_code)` — UNIQUE
- `clicks(link_id)` — FK relationship
- `refresh_tokens(token_hash)` — UNIQUE
- `refresh_tokens(user_id)` — FK relationship

## Caching Strategy

### Redis Link Cache

**What**: Short-code → original URL  
**Key format**: `link:{code}` (e.g., `link:Ab3xY7q`)  
**Value**: Original URL string  
**TTL**: 24 hours (configurable via `SHORTENER_CACHE_TTL`)  
**Set on**: First redirect (lazy caching)  
**Eviction**: Automatic on TTL; explicit on link deletion (if implemented)

**Flow**:
1. GET `/:code`
2. Try Redis: `linkCacheRepo.Get(code)`
3. If hit: return cached URL
4. If miss: query PostgreSQL, cache result, return

**Failure mode**: If Redis is down, fall back to PostgreSQL (no redirect failure)

### No Session Cache
- No in-memory session cache (stateless design)
- JWT tokens carry all needed data (no session lookup)
- Refresh tokens validated against PostgreSQL (revocation support)

## Security Model

### Authentication Mechanisms

| Endpoint | Method | Requirement |
|----------|--------|-------------|
| POST /auth/register | Public | None (email/username validation only) |
| POST /auth/login | Public | None |
| POST /auth/refresh | Public | Valid refresh token |
| POST /auth/logout | JWT | Valid access token |
| GET /auth/me | JWT | Valid access token |
| POST /api/links | API Key | Valid X-API-Key header |
| GET /api/links/:code/stats | API Key | Valid X-API-Key header |
| GET /:code | Public | None |

### API Key Security
- **Storage**: In-memory (from config, environment variable)
- **Transmission**: `X-API-Key` header (case-insensitive)
- **Validation**: Fail-closed (empty set rejects all requests)
- **Rotation**: Change environment variable + restart

Example config:
```env
SHORTENER_API_KEYS=dev-key-1,dev-key-2,prod-key-abc123
```

### JWT Security
- **Algorithm**: HS256 (HMAC-SHA256)
- **Secret**: `AUTH_JWT_SECRET` (required in production)
- **Signing**: Server signs all tokens
- **Verification**: Echo JWT middleware validates on protected routes
- **Access token TTL**: 15m (short-lived, revocation not needed)
- **Refresh token TTL**: 7d (stored in DB, revocable)

### Password Security
- **Hashing**: bcrypt (cost factor = 12, configurable)
- **Storage**: `password_hash` in users table (never raw password)
- **Verification**: bcrypt.CompareHashAndPassword (timing-safe)
- **Rotation**: Only via explicit password change (not implemented yet)

### Token Storage
- **Access tokens**: Not stored (ephemeral, JWT carries identity)
- **Refresh tokens**: Stored as SHA256 hash (raw token never persisted)
- **Raw token location**: Only in client memory (on login response)

### Input Validation

| Field | Rule | Purpose |
|-------|------|---------|
| username | `^[a-zA-Z0-9_]{3,50}$` | Prevent confusion, limit injection surface |
| email | Basic format check + UNIQUE | Standard email validation |
| password | ≥8 characters | Prevent weak passwords |
| url | No validation | User's responsibility (we just store) |

## Performance Characteristics

### Redirect Performance (Cached)
- **Time breakdown**: DNS (client) + TLS (client) + HTTP round-trip + Redis lookup + Redirect
- **P95**: <100ms (Redis single-digit ms, HTTP latency dominates)
- **Bottleneck**: Network latency (not server logic)

### Link Creation Performance
- **Collision checking**: Max 5 retries (statistically: <1% need retry)
- **Time**: ~10-50ms (PostgreSQL write + cache set)
- **Bottleneck**: Database write

### Auth Operations Performance
- **Login**: ~50-100ms (bcrypt is intentionally slow; cost=12)
- **Refresh**: ~20-50ms (token generation + DB write)
- **Logout**: ~10-30ms (DB update)

### Database Connection Pooling
- **Max open connections**: 25 (default, configurable)
- **Max idle**: 25
- **Conn lifetime**: 5 minutes (refresh connection pool)
- **Growth**: Connections allocated on demand up to max

## Concurrency Model

### Request Handling
- **Model**: Goroutine per request (Echo/HTTP server handles this)
- **Concurrency**: Limited by connection pool + database connections
- **Shutdown**: Graceful (waits up to 10s for in-flight requests)

### Analytics Recording
- **Model**: Fire-and-forget goroutine
- **Failure mode**: Click loss on crash (acceptable, non-critical)
- **No buffering**: Each click spawns immediate goroutine

```go
// In RedirectHandler
go a.analyticsSvc.RecordClick(...)  // async, no error handling
```

## Deployment Architecture

### Single-Instance (Current)
```
Client → Load Balancer (optional) → Echo Server
                                         ├──► PostgreSQL
                                         └──► Redis
```

### Multi-Instance (Future)
```
Clients → Load Balancer → [Echo 1, Echo 2, ...]
              │                    ├──► PostgreSQL (shared)
              │                    └──► Redis (shared)
              └────────────────────────────┘
```

**Stateless design**: All instances can handle any request.  
**Shared cache**: Redis is shared (no per-instance cache).  
**Database**: PostgreSQL is bottleneck (connection pool tuning important).

## Observability

### Logging
- **Format**: JSON via `log/slog`
- **Levels**: Info, Warn, Error, Debug
- **Request tracking**: Request ID middleware adds ID to all logs
- **What to log**: Errors, important state changes, not secrets

### Pprof Debugging
- **Endpoint**: `localhost:6060` (development only, via environment config)
- **Profiles**: CPU, heap, goroutine, block contention
- **Disable**: Set `SERVER_PPROF_ADDR=""` in production

### Metrics (Not Built-in)
Future enhancements:
- Prometheus metrics (request count, latency histograms)
- Request rate per API key
- Cache hit ratio
- Database query latency

---

**Last Updated**: 2026-06-22  
**Diagrams**: ASCII for documentation clarity  
**Status**: Production-ready single-instance architecture
