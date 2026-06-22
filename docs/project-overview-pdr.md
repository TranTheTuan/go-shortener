# Go URL Shortener - Project Overview & PDR

## Product Vision

**Go URL Shortener** is a production-ready HTTP backend service that demonstrates clean Go architecture and industry best practices. It provides a fully-featured URL shortening API with click analytics, user authentication, and Redis caching—serving as both a functional service and a reference implementation for building scalable Go services.

### Target Audience
- Backend developers building API services in Go
- Teams needing a battle-tested service template
- Developers learning layered architecture patterns
- Organizations requiring a URL shortener with analytics

## Core Features

### 1. URL Shortening (Complete)
- Generate cryptographic random base62 short codes (default 7 chars)
- Configure code length via `SHORTENER_CODE_LENGTH`
- Collision handling with automatic retry (max 5 attempts)
- Deduplication: identical URLs return existing short code
- Optional expiry dates (RFC 3339 format)
- Cache-first resolution (Redis → PostgreSQL fallback)

### 2. Click Analytics (Complete)
- Async click recording (goroutine-based, acceptable loss on crash)
- Metrics per short code: total clicks, breakdown by referrer/IP/user-agent
- Real-time stats retrieval via `/api/links/:code/stats`
- Data persistence in PostgreSQL with 45-char IP field

### 3. Authentication (Complete - feat/auth)
- **Username/password auth**: bcrypt password hashing (configurable cost)
- **JWT tokens**: HS256 access tokens (default 15min TTL)
- **Token rotation**: Refresh tokens with rotation on use
- **Token revocation**: SHA256 hashing (raw tokens never stored in DB)
- **Logout**: Explicit refresh token revocation
- **User profile**: `/auth/me` returns current user

### 4. Caching (Complete)
- Redis cache for link resolution (prefix: `link:{code}`)
- Configurable TTL per environment
- Graceful degradation to PostgreSQL on cache miss
- Automatic cache invalidation on link expiry

### 5. Security (Complete)
- **API Key auth**: `X-API-Key` header (fail-closed: empty key set rejects all writes)
- **JWT auth**: Bearer token in Authorization header
- **Password security**: bcrypt with configurable work factor (default 12)
- **Token storage**: Never store raw tokens; SHA256 hash in DB
- **User enumeration prevention**: Generic auth failure message
- **HTTPS ready**: No enforcement (let reverse proxy handle)

## Non-Functional Requirements

### Performance
- Sub-100ms response time for short link redirects (cached)
- Connection pooling: max 25 open, 25 idle PostgreSQL connections
- Redis for cache-first link resolution
- Pprof debug endpoint enabled in development

### Scalability
- Stateless design (runs behind load balancer)
- Database connection limits configurable
- Redis pub/sub ready for multi-instance scenarios
- Graceful shutdown (waits for in-flight requests)

### Reliability
- Graceful shutdown on SIGINT/SIGTERM
- Structured JSON logging via log/slog
- Request ID tracking via middleware
- Recovery middleware catches panics
- Database connection timeout management

### Developer Experience
- Environment-based config (no code changes needed)
- SQL migrations managed manually via `make migrate-*`
- Layered architecture (handler → service → repository)
- Interface-based design (easy to mock/test)
- Swagger/OpenAPI spec auto-generated from code

### Maintenance
- Clean separation of concerns (HTTP / business logic / data)
- Consistent error handling via `apperror` package
- Uniform JSON response envelope (`{"data":...}` / `{"error":...}`)
- Well-commented code explaining non-obvious patterns

## Technical Goals

1. **Production-ready baseline**: No shortcuts; actual password hashing, token storage, error handling
2. **Best-practices template**: Reference implementation for Go HTTP services
3. **Testability**: Interface-driven design, mockable repositories
4. **Observability**: Structured logging, request tracking, pprof support
5. **Security-first**: Fail-closed defaults, no credentials in logs, bcrypt passwords

## Architecture Highlights

### Layered Design
```
HTTP Request
   ↓
Router (middleware: request ID, panic recovery)
   ↓
Handler (parse request, call service)
   ↓
Service (business logic, validation)
   ↓
Repository (data access via interface)
   ↓
PostgreSQL / Redis
```

Each layer depends on interfaces of lower layers → testable, swappable, maintainable.

### Key Patterns
- **Error handling**: Structured `apperror.Error` type (HTTP status + machine-readable code + message)
- **Response envelope**: All responses wrapped in `{"data":...}` or `{"error":{...}}`
- **Token security**: Refresh tokens hashed (SHA256) in DB; raw token never persisted
- **Cache strategy**: Redis with fallback to DB; async analytics to prevent write bottleneck
- **Validation**: Regex for username (3–50 alphanumeric+underscore), password length (≥8), email format

## Database Schema

| Table | Purpose | Key Constraints |
|-------|---------|-----------------|
| `users` | User accounts | email + username UNIQUE, bcrypt password |
| `links` | Short URLs | short_code UNIQUE (16 chars), expires_at nullable |
| `clicks` | Analytics events | link_id FK, referrer/ip/user_agent tracked |
| `refresh_tokens` | Auth tokens | token_hash UNIQUE (SHA256), revocable, expires |

## Deployment Model

- **Environment**: Configurable (development, staging, production)
- **Database**: PostgreSQL 12+
- **Cache**: Redis 6+
- **Secrets**: `AUTH_JWT_SECRET` (required outside dev), API keys comma-separated
- **Server**: HTTP (no TLS; use reverse proxy)
- **Shutdown**: Graceful (drains in-flight requests, timeout configurable)

## Success Metrics

- **Functionality**: All API endpoints working; auth flow complete
- **Performance**: <100ms P95 redirect latency (cached)
- **Reliability**: 99.9% availability (graceful shutdown + error recovery)
- **Code quality**: >80% test coverage on services
- **Security**: No credentials in logs; all passwords bcrypted; tokens hashed

## Known Limitations & Future Work

### Current Scope
- Single-instance (no distributed cache invalidation)
- Basic analytics (no time-series, aggregation)
- No rate limiting
- No custom short codes (always generated)

### Potential Enhancements
- Rate limiting per API key / IP
- Admin dashboard for link management
- Custom code support (owner-only)
- Metrics export (Prometheus)
- Multi-database support (MySQL fallback)
- WebSocket support for real-time analytics

## Getting Started

See `/README.md` for:
- Prerequisites (Go 1.26+, PostgreSQL, Redis, golang-migrate)
- Local setup (`cp .env.example .env && make run`)
- Docker setup
- Make targets

---

**Last Updated**: 2026-06-22  
**Version**: 1.0 (feat/auth branch)  
**Status**: Feature-complete for v1; auth system in active development
