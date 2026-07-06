# Project Changelog

All notable changes to the go-shortener project are documented here.

## [Unreleased]

### Added
- **Link Management (CRUD)** — Owner-only link management with hard delete, enable/disable, and expiry editing
  - `DELETE /api/links/:code` — Hard delete (cascades clicks, returns 204)
  - `PUT /api/links/:code` — Update mutable state: `{expires_at: RFC3339|null, is_active: bool}` (returns 200)
  - `GET /api/links?status=active|disabled|expired` — Filter links by status (empty=all, mutually exclusive buckets)
  - New column `links.is_active BOOL NOT NULL DEFAULT true` (migration 000011)
  - Disabled links return 410 Gone on redirect; never cached
  - Frontend: Status badges (active/disabled/expired), row actions (Enable/Disable, edit expiry, Delete), status filter dropdown
  - Cache invalidation on all mutations; dedup cache evicted on delete

### Security
- Owner-only authorization on all link mutations (404 for non-owner/missing/unowned links)
- Keycloak OIDC authentication required for link management endpoints

### Notes
- Swagger/OpenAPI docs pending regeneration (`make swag` not yet run)

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
