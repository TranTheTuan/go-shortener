# System Architecture

## High-Level Architecture

```
┌─────────────────────────────────────────────────────────────────┐
│                          HTTP Clients                           │
│  (browsers, mobile apps, API clients with Keycloak tokens)     │
└──────────────────────────┬──────────────────────────────────────┘
                           │ HTTP/HTTPS (with Authorization Bearer)
                           │
                    ┌──────▼──────┐
                    │  Keycloak   │
                    │  (auth.cd.me)
                    │  OIDC Issuer│
                    └──────┬──────┘
                           │ JWKS (cached in-cluster)
┌──────────────────────────▼──────────────────────────────────────┐
│                    Echo HTTP Server                             │
│  ┌──────────────────────────────────────────────────────────┐  │
│  │  Middleware (Request ID, Panic Recovery, Logging)       │  │
│  │  - Keycloak Auth (Bearer token validation + JIT prov.)  │  │
│  └──────────────────────────────────────────────────────────┘  │
│                           │                                     │
│  ┌────────────┬──────────┬──────┬──────┬──────────────────┐   │
│  ▼            ▼          ▼      ▼      ▼                  ▼   │
│ Health   AuthHandler  UserHandler LinkHandler  RedirectHandler  │
│Handler   (/auth/me,   (list,get)  (create,     (/:code)        │
│          returns              stats)                        │
│          synced user)                                       │
└──────────────────────────┬──────────────────────────────────────┘
                           │
        ┌──────────────────┼──────────────────┐
        │                  │                  │
        ▼                  ▼                  ▼
   UserService      LinkService         AnalyticsService
   (sync from KC)   (cache-first)       (async recording)
        │                  │                  │
        │     ┌────────────┼────────────┐    │
        │     │            │            │    │
        ▼     ▼            ▼            ▼    ▼
    UserRepo LinkRepo  LinkCacheRepo ClickRepo
        │        │            │            │
        │        │ Redis      │            │
        │        │ (TTL)      │            │
        │        └────────────┘            │
        │                                  │
        └──────────────────┬───────────────┘
                           │
            ┌──────────────┴──────────────┐
            ▼                             ▼
        PostgreSQL                    Redis
        (users w/                 (link cache,
        keycloak_sub,             key expiry)
        links, clicks)            
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
| `Keycloak` | Validates Bearer token (Keycloak-issued RS256), JIT-provisions user | `/api/*`, `/auth/me`, `/users/*` |
| Echo built-ins | Request ID, panic recovery, logging | All routes |

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

### 3. Get User Profile (GET /auth/me)

```
Client Request (with Bearer token from Keycloak)
    │
    ▼
KeycloakMiddleware.Verify
    │
    ├──► Extract Bearer token from Authorization header
    │
    ├──► Validate signature (RS256 via JWKS)
    │    └──► Invalid: return 401
    │
    ├──► Validate expiry, issuer (iss), audience (aud)
    │    └──► Invalid: return 401
    │
    ├──► Extract Identity (sub, email, preferred_username)
    │
    ├──► Call UserService.SyncFromKeycloak(Identity)
    │       │
    │       ├──► Query UserRepository.GetByKeycloakSub(sub)
    │       │    ├──► Found: update email/username if changed, return existing user
    │       │    └──► Not found: Create new user (get-or-create pattern)
    │       │
    │       └──► Return local User (int64 id)
    │
    ├──► Set local user ID in context
    │
    └──► Next handler
        │
        ▼
    AuthHandler.Me
        │
        ├──► Extract local user ID from context
        │
        ├──► Format response {"data": {id, username, email, ...}}
```

**Result**: HTTP 200 with synced user details  
**Security**: Password never stored; identity delegated to Keycloak  
**JIT Provisioning**: User auto-created on first Keycloak request

## Database Schema

### users table
```sql
id           BIGSERIAL PRIMARY KEY
username     VARCHAR(50) UNIQUE NOT NULL
email        VARCHAR(255) UNIQUE NOT NULL
keycloak_sub VARCHAR(36) UNIQUE         (nullable for orphaned demo users; added migration 9)
name         VARCHAR(255) (nullable)
created_at   TIMESTAMPTZ NOT NULL DEFAULT now()
updated_at   TIMESTAMPTZ NOT NULL DEFAULT now()
```

### links table
```sql
id           BIGSERIAL PRIMARY KEY
short_code   VARCHAR(16) UNIQUE NOT NULL
original_url TEXT NOT NULL
user_id      BIGINT NOT NULL FK→users(id) CASCADE
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

**Indexes**:
- `users(email)` — UNIQUE
- `users(username)` — UNIQUE
- `users(keycloak_sub)` — UNIQUE (added migration 9)
- `links(short_code)` — UNIQUE
- `links(user_id)` — FK relationship
- `clicks(link_id)` — FK relationship

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
| GET /auth/me | Keycloak JWT | Valid Keycloak access token (RS256) |
| POST /api/links | Keycloak JWT | Valid Keycloak access token |
| GET /api/links/:code/stats | Keycloak JWT | Valid Keycloak access token |
| GET /users | Keycloak JWT | Valid Keycloak access token |
| GET /users/:id | Keycloak JWT | Valid Keycloak access token |
| GET /:code | Public | None |
| GET /healthz | Public | None |

### Keycloak OIDC Security
- **Algorithm**: RS256 (RSA signatures, keys from Keycloak JWKS)
- **JWKS endpoint**: In-cluster (e.g., `http://keycloak-svc.../certs`)
- **Token issuer (iss)**: Public domain (e.g., `https://auth.cd.me/realms/<realm>`)
- **Token expiry (exp)**: Validated on every request (short-lived, e.g., 5-10m)
- **Audience (aud)**: Optional validation (requires audience mapper on Keycloak client)
- **Lazy JWKS fetch**: First token triggers download; cached thereafter; app starts even if Keycloak briefly down

### JIT Provisioning
- **Model**: Get-or-create local user from Keycloak identity
- **Key**: Keycloak `sub` (UUID, unique per realm)
- **Attributes**: `email`, `preferred_username` from token claims (pre-validated by Keycloak)
- **Orphaned demo users**: Pre-migration users have null `keycloak_sub` (won't map to Keycloak; acceptable for dev/template)

### Identity Flow
```
Keycloak sub (UUID) ──► users.keycloak_sub (UNIQUE, JIT get-or-create) ──► local int64 user_id
```
Downstream (link ownership, quota) sees only int64 user_id; Keycloak identity abstracted away.

### Input Validation

| Field | Rule | Purpose |
|-------|------|---------|
| email | From Keycloak claim | Pre-validated by Keycloak (assume trusted) |
| username | From `preferred_username` claim | Pre-validated by Keycloak |
| keycloak_sub | UUID from `sub` claim | Unique Keycloak identity |
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
- **Token validation**: ~5-20ms (JWKS cached locally, signature check only)
- **JIT provisioning**: ~10-50ms (get-or-create user DB lookup)
- **GET /auth/me**: ~20-60ms (token validation + user lookup)

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
- Request rate per user
- Cache hit ratio
- Database query latency
- Keycloak token validation latency

---

**Last Updated**: 2026-06-30  
**Diagrams**: ASCII for documentation clarity  
**Status**: Production-ready single-instance architecture  
**Auth Model**: Keycloak OIDC resource server (v1.1+)
