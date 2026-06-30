# Project Roadmap & Development Status

**Current Date**: 2026-06-30  
**Active Branch**: `master` (Keycloak OIDC resource server)  
**Latest**: v1.1 merged

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

### v1.1 - Keycloak OIDC Authentication (COMPLETE)
**Status**: ✅ Complete | **Merged**: 2026-06-30 | **Branch**: `master`

#### Features Delivered
- **Keycloak delegation**: Service is OIDC resource server (validates Keycloak tokens)
- **JIT provisioning**: Keycloak `sub` auto-mapped to local users on first authenticated request
- **Token validation**: go-oidc with in-cluster JWKS fetching (lazy initialization, no startup block)
- **User profile**: GET /auth/me returns authenticated user synced from Keycloak claims
- **Link ownership**: `/api/links` create owned by authenticated user (keyed on JIT-mapped local id)
- **Audience validation**: Optional `aud` check when `KEYCLOAK_CLIENT_ID` configured

#### Database Changes (Migrations)
- Migration 9: Add `keycloak_sub` (UNIQUE, nullable), drop `password_hash` + `refresh_tokens` table

#### Configuration (New)
- `KEYCLOAK_ISSUER`: Expected token issuer (e.g., `https://auth.cd.me/realms/<realm>`)
- `KEYCLOAK_JWKS_URL`: In-cluster JWKS endpoint (in-cluster DNS, not public jwks_uri)
- `KEYCLOAK_CLIENT_ID`: Backend client ID for audience validation (empty = skip `aud` check)

#### Security Features
- OAuth 2.0/OIDC standard (industry best practice)
- Token signature validation via JWKS (RS256 assumed)
- Issuer (`iss`) validation against public domain
- Expiry (`exp`) checked automatically
- Audience (`aud`) optionally validated
- No password storage (delegated to Keycloak)

#### Code Changes
- Removed: `AuthService`, self-issued JWT token management, refresh token logic, bcrypt
- Added: `pkg/keycloak/verifier.go`, `internal/middleware/keycloak.go`, `UserService.SyncFromKeycloak()`
- Modified: User repository (keycloak_sub field), router (Keycloak middleware on protected routes)
- Removed auth write endpoints: `/auth/register`, `/auth/login`, `/auth/refresh`, `/auth/logout`
- Kept: `/auth/me` (now returns synced local user from Keycloak identity)

#### Testing
- Keycloak middleware unit tests (mock TokenVerifier + UserService)
- JIT provisioning tests (new sub → create, existing sub → update)
- Token validation tests (valid token, expired, wrong issuer, wrong audience)
- Downstream tests unchanged (quota, dedup, link ownership all use local user_id)

#### Deployment Notes
- Keycloak backend client requires `email` + `profile` scopes
- Configure audience mapper so access tokens include backend client in `aud`
- Set `KEYCLOAK_ISSUER` to realm's public issuer (e.g., `https://auth.cd.me/realms/<realm>`)
- Set `KEYCLOAK_JWKS_URL` to in-cluster endpoint (e.g., `http://keycloak-keycloakx-http.keycloak.svc.cluster.local/...`)

#### Acceptance Criteria
- ✅ Valid Keycloak access token → create/list links, `/auth/me`, `/users` work
- ✅ Invalid/expired/foreign-aud token → 401
- ✅ User JIT-provisioned once, reused after
- ✅ Link ownership + daily quota behave exactly as before
- ✅ No self-issued tokens, no password storage, no X-API-Key
- ✅ App starts even if Keycloak briefly unavailable (lazy JWKS)

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

### Phase 9: Keycloak OIDC Authentication (COMPLETE) ✅
- Keycloak token validation (go-oidc)
- JIT provisioning (sync Keycloak identity to local users)
- `/auth/me` endpoint (returns synced user)
- Keycloak middleware (validates Bearer tokens)
- Link ownership (tied to Keycloak-mapped user)

---

## Upcoming Work (Post v1.1)

### v1.2 - Keycloak Role-Based Authorization (Planned)
**Timeline**: Q3 2026

- **Role mapping**: Check Keycloak `realm_access.roles` claims
- **Admin endpoints**: Restricted to `admin` role (user management, link stats)
- **Owner-only operations**: Link delete/update for creator
- **Fine-grained access**: Per-endpoint role checks

---

### v1.3 - Link Management Enhancements (Planned)
**Timeline**: Q3 2026

- **Delete link** (owner-only or admin)
- **Update link expiry**
- **Custom short codes** (alphanumeric, owner-created)
- **Draft/published states** (private links)

### v1.4 - Admin Dashboard (Planned)
**Timeline**: Q3/Q4 2026

- Web UI (React/Vue, separate repo)
- Link management: list, edit, delete
- Analytics visualization: charts, trends
- User management: list, promote to admin
- Rate limiting configuration

### v1.5 - Rate Limiting (Planned)
**Timeline**: Q4 2026

- Per-user rate limiting (Keycloak sub)
- Per-IP rate limiting (anonymous redirects)
- Endpoint-specific limits
- Metrics: current usage, reset time
- Configurable thresholds

### v1.6 - Observability (Planned)
**Timeline**: Q4 2026 / Q1 2027

- Prometheus metrics (request count, latency, cache hit ratio)
- Tracing integration (OpenTelemetry)
- Request-scoped logging improvements
- Health check details (database, cache status, Keycloak status)
- Alerting rules (example: cache down)

### v1.7 - Multi-Database Support (Planned)
**Timeline**: Q1 2027+

- MySQL driver (in addition to PostgreSQL)
- Migration tooling for both
- Database abstraction improvements
- Connection pool configuration per DB

### v1.8 - Keycloak Admin API Integration (Future)
**Timeline**: 2027+

- User provisioning from Keycloak (bulk import)
- Sync realm roles to local permissions
- Real-time user attribute updates
- Webhook handlers for Keycloak events

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
1. **Single instance**: No multi-instance token cache coordination
2. **No custom codes**: Users cannot specify short code
3. **No per-user rate limiting**: Quota is per-plan only (same for all users)
4. **No dashboard**: CLI/API only
5. **Pre-auth users orphaned**: Demo users with null `keycloak_sub` won't map to Keycloak

### Workarounds
- Run multiple instances behind load balancer (stateless)
- Share PostgreSQL + Redis (single writer allowed)
- Migrate demo users via script or eventual Keycloak admin API sync

### Planned Fixes
- Rate limiting (v1.5)
- Role-based authorization (v1.2)
- Admin dashboard (v1.4)
- Keycloak sync (v1.8)

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

**Last Updated**: 2026-06-30  
**Next Review**: 2026-07-30 (post v1.2 planning)  
**Maintained by**: @TranTheTuan
