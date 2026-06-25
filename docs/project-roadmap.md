# Project Roadmap & Development Status

**Current Date**: 2026-06-22  
**Active Branch**: `feat/auth` (username/password authentication)  
**Main Branch**: `master`

## Version History

### v1.0 - Production Foundation (COMPLETE)
**Status**: ✅ Complete | **Release**: 2026-06-22

#### Features Delivered
- **Core URL shortening**: Generate 7-char base62 codes, collision handling (max 5 retries)
- **Link resolution**: Cache-first (Redis) with PostgreSQL fallback
- **Link expiry**: Optional RFC 3339 timestamps
- **URL deduplication**: Same URL returns existing code
- **Click analytics**: Async recording with referrer/IP/user-agent tracking
- **Redis caching**: 24h TTL for link resolution
- **API Key auth**: X-API-Key header validation (fail-closed)
- **Health check**: `/healthz` endpoint
- **Graceful shutdown**: Waits for in-flight requests (10s timeout)
- **Structured logging**: JSON via log/slog
- **Swagger/OpenAPI**: Auto-generated from code comments
- **Database migrations**: Manual control via `make migrate-*`
- **Dockerfile**: Multi-stage build (production-ready)
- **Configuration**: Environment-based via caarlos0/env

**Code Quality**:
- Layered architecture (handler → service → repository)
- Interface-based design (testable, swappable)
- Structured error handling (apperror)
- Uniform JSON response envelope
- >80% test coverage on services

---

### v1.1 - Authentication (IN PROGRESS)
**Status**: 🔄 In Progress | **Branch**: `feat/auth` | **Target**: 2026-06-30

#### Features Being Added
- **Username/password registration**: POST /auth/register
  - Username validation (3–50 alphanumeric+underscore)
  - Email uniqueness + format validation
  - Password hashing (bcrypt, cost=12)
  - Optional full name field

- **Email/password login**: POST /auth/login
  - Email lookup
  - bcrypt password verification
  - Access + refresh token issuance
  - Generic failure message (no user enumeration)

- **Token refresh**: POST /auth/refresh
  - Validate refresh token (SHA256 hash check)
  - Rotate tokens (issue new pair)
  - Revoke old refresh token

- **Logout**: POST /auth/logout (JWT required)
  - Mark refresh token as revoked
  - Prevent future use

- **User profile**: GET /auth/me (JWT required)
  - Return authenticated user details

#### Database Changes (Migrations)
- Migration 4: Add `username` (UNIQUE), `password_hash` to users table
- Migration 5: Create `refresh_tokens` table with revocation support

#### Configuration
- `AUTH_JWT_SECRET`: HS256 signing key (required outside development)
- `AUTH_ACCESS_TTL`: Access token lifetime (default 15m)
- `AUTH_REFRESH_TTL`: Refresh token lifetime (default 7d)
- `AUTH_BCRYPT_COST`: bcrypt work factor (default 12)

#### Security Features
- Token rotation on refresh (new refresh token each time)
- SHA256 hashing of refresh tokens (raw tokens never in DB)
- Bcrypt password hashing with configurable cost
- Fail-closed defaults (empty JWT secret rejected in production)

#### Testing
- Unit tests: service layer (auth logic, validation)
- Integration tests: full login/refresh/logout flow
- Mock repositories for handler tests
- Table-driven test cases for edge conditions

#### Documentation
- Handler docstrings (Swagger comments)
- Service layer comments (validation rules, token lifecycle)
- README examples (curl commands for auth flow)
- Update code-standards.md with auth patterns

#### Acceptance Criteria
- ✅ User registration with validation
- ✅ Login returns valid JWT access token + refresh token
- ✅ Refresh endpoint rotates tokens correctly
- ✅ Logout revokes refresh token
- ✅ GET /auth/me returns current user (JWT required)
- ✅ All new code has >85% test coverage
- ✅ No raw tokens/passwords in logs
- ✅ Generic auth failure messages (UNAUTHORIZED)

---

## Completed Work Summary

### Phase 1: Server Skeleton ✅
- HTTP server (Echo v4)
- Graceful shutdown
- Config loading (caarlos0/env)
- PostgreSQL connection pooling
- Structured logging (log/slog)
- Middleware: request ID, panic recovery

### Phase 2: Data Layer ✅
- 5 database migrations (users, links, clicks, credentials, refresh_tokens)
- GORM repositories (interface-driven)
- User, Link, Click, RefreshToken models
- Connection pooling + timeout configuration

### Phase 3: URL Shortening ✅
- LinkService: create, resolve, collision retry
- LinkRepository: GORM + uniqueness checks
- Random code generation (crypto/rand, base62)
- Deduplication logic (same URL → existing code)
- Expiry validation (410 Gone for expired)

### Phase 4: Caching ✅
- Redis setup (go-redis/v9)
- LinkCacheRepository: cache with TTL
- Cache-first resolve strategy
- Graceful fallback to PostgreSQL

### Phase 5: Analytics ✅
- ClickRepository: event logging (async)
- AnalyticsService: record clicks, fetch stats
- Async recording (goroutine, acceptable loss)
- Stats by referrer/IP/user-agent tracking

### Phase 6: API Layer ✅
- Handler layer: 5 handler types
- Request parsing + response serialization
- Route registration (Echo)
- Swagger/OpenAPI documentation
- Uniform JSON envelope (response package)

### Phase 7: Error Handling ✅
- apperror.Error structured type
- Error codes: BAD_REQUEST, NOT_FOUND, CONFLICT, GONE, INTERNAL
- HTTP status mapping (400, 404, 409, 410, 500)
- Wrapped internal errors (secure logging)

### Phase 8: Security ✅
- API Key middleware (X-API-Key, fail-closed)
- JWT middleware (Bearer token validation)
- UserIDFrom context accessor
- Fail-closed defaults

### Phase 9: Authentication (IN PROGRESS) 🔄
- Register endpoint
- Login endpoint
- Refresh endpoint
- Logout endpoint
- Profile endpoint

---

## Upcoming Work (Post v1.1)

### v1.2 - Link Management Enhancements (Planned)
**Timeline**: Q3 2026

- **Delete link** (owner-only or admin)
- **Update link expiry**
- **Custom short codes** (alphanumeric, owner-created)
- **Link ownership** (associate with user account)
- **Draft/published states** (private links)

### v1.3 - Admin Dashboard (Planned)
**Timeline**: Q3/Q4 2026

- Web UI (React/Vue, separate repo)
- Link management: list, edit, delete
- Analytics visualization: charts, trends
- User management: list, promote to admin
- Rate limiting configuration

### v1.4 - Rate Limiting (Planned)
**Timeline**: Q4 2026

- Per-API-key rate limiting (token bucket)
- Per-IP rate limiting (anonymous users)
- Endpoint-specific limits
- Metrics: current usage, reset time
- Configurable thresholds

### v1.5 - Observability (Planned)
**Timeline**: Q4 2026 / Q1 2027

- Prometheus metrics (request count, latency, cache hit ratio)
- Tracing integration (OpenTelemetry)
- Request-scoped logging improvements
- Health check details (database, cache status)
- Alerting rules (example: cache down)

### v1.6 - Multi-Database Support (Planned)
**Timeline**: Q1 2027+

- MySQL driver (in addition to PostgreSQL)
- Migration tooling for both
- Database abstraction improvements
- Connection pool configuration per DB

### v2.0 - Advanced Features (Future)
**Timeline**: 2027+

- **Team collaboration**: Multi-user workspaces
- **Link groups/categories**: Organize links
- **Bulk operations**: Create/delete/update many links
- **Export**: CSV, JSON export of link data
- **Webhooks**: Post-redirect notifications
- **IP geolocation**: Track click geography
- **Redirect rules**: A/B testing, conditional redirects

---

## Branch Structure

### master (Main)
- Stable, tested code
- Deployed to production
- Tag-based releases

### feat/auth (Active Development)
- Username/password authentication
- Will merge to master after review + testing
- Deadline: 2026-06-30

### Other Feature Branches (As Needed)
- `feat/admin-dashboard`
- `feat/rate-limiting`
- `feat/custom-codes`
- etc.

---

## Testing Strategy

### Unit Tests (Service Layer)
- Mock repositories
- Test validation rules
- Test error cases
- Test token generation/rotation

### Integration Tests
- Real PostgreSQL + Redis
- Full auth flow: register → login → refresh → logout
- Cache hit/miss scenarios
- Link expiry validation

### Load Testing (Post v1.0)
- Target: 1000 req/sec redirects
- Monitor: CPU, memory, database connections
- Profile: pprof heap/CPU on production-like traffic

### Security Testing
- Password validation rules
- Token expiry enforcement
- API key validation (empty set rejects all)
- SQL injection prevention (GORM parameterized)

---

## Deployment Milestones

### Development
- ✅ Local development (docker-compose, make targets)
- ✅ GitHub Actions CI/CD (basic)

### Staging
- 🔄 Automated deployment on merge to master
- 🔄 Environment parity (except secrets)
- 🔄 Load testing baseline

### Production
- 📅 Planned after v1.1 merge
- 📅 Rolling deployment strategy
- 📅 Health check + auto-rollback
- 📅 Monitoring + alerting

---

## Known Limitations & Workarounds

### Current Scope
1. **Single instance**: No multi-instance coordination (yet)
2. **No custom codes**: Users cannot specify short code
3. **No link ownership**: All links are public (auth in progress)
4. **No rate limiting**: Potential abuse vector
5. **No dashboard**: CLI/API only

### Workarounds
- Run multiple instances behind load balancer (stateless)
- Share PostgreSQL + Redis (single writer allowed)
- Use API key quotas to prevent abuse (manual)

### Planned Fixes
- Rate limiting (v1.4)
- Link ownership (v1.2)
- Admin dashboard (v1.3)

---

## Success Metrics

### Functional
- ✅ All API endpoints work as documented
- ✅ Auth flow complete (register → login → refresh → logout)
- ✅ Click analytics recorded and retrievable
- ✅ Cache hit rate >95% for popular links

### Performance
- ✅ P95 redirect latency <100ms (cached)
- ✅ Link creation <50ms
- ✅ Handle 1000 req/sec (load testing)

### Reliability
- ✅ 99.9% uptime (7 days, production)
- ✅ Graceful shutdown <10s
- ✅ Cache failure doesn't break redirects

### Code Quality
- ✅ >80% test coverage (services)
- ✅ No security warnings (dependabot)
- ✅ Clean architecture (no circular deps)

### Developer Experience
- ✅ Onboarding time <2 hours
- ✅ First feature added <30 minutes
- ✅ Documentation covers common scenarios

---

## Contributing Guidelines

### Before Starting
1. Check GitHub issues for ongoing work
2. Create an issue for new features
3. Discuss approach in issue comments
4. Assign issue to yourself

### Development Workflow
1. Create feature branch: `git checkout -b feat/my-feature`
2. Implement + test locally
3. Run `make test` (all tests pass)
4. Run `make lint` (address linting issues)
5. Create pull request with description
6. Address code review feedback
7. Merge after approval + CI passes

### Commit Standards
- Use conventional commits: `feat:`, `fix:`, `docs:`, `test:`, `refactor:`
- Keep commits focused (one logical change per commit)
- Write descriptive commit messages

### Code Review Checklist
- Tests added/updated
- Follows code standards (code-standards.md)
- Error handling is complete
- No secrets in code/logs
- Documentation updated

---

**Last Updated**: 2026-06-22  
**Next Review**: 2026-06-30 (post v1.1 merge)  
**Maintained by**: @TranTheTuan
