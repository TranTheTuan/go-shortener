# Go Backend HTTP Server Template

A production-ready starting point for building HTTP backend services in Go.
It ships with a clean, layered architecture (handler → service → repository),
sensible defaults, and the plumbing most services need so you can start writing
business logic on day one.

## Features

- **Layered architecture** — clear separation between transport, business
  logic, and data access.
- **[Echo](https://echo.labstack.com/)** web framework with request ID,
  logging, and panic-recovery middleware.
- **[GORM](https://gorm.io/)** ORM with PostgreSQL and a connection pool.
- **Environment-based config** via [caarlos0/env](https://github.com/caarlos0/env)
  with sane defaults — runs out of the box.
- **Structured JSON logging** with the standard library `log/slog`.
- **Consistent JSON responses** and a structured application-error type.
- **SQL migrations** managed with [golang-migrate](https://github.com/golang-migrate/migrate)
  (run manually, not on startup, for full control).
- **Graceful shutdown** on `SIGINT`/`SIGTERM`.
- **Dockerfile** with a multi-stage build.

## Tech Stack

| Concern        | Choice                                  |
| -------------- | --------------------------------------- |
| Language       | Go 1.26                                 |
| HTTP framework | Echo v4                                 |
| ORM / Database | GORM + PostgreSQL                       |
| Config         | caarlos0/env                            |
| Migrations     | golang-migrate                          |
| Logging        | log/slog (JSON)                         |

## Project Structure

```
.
├── cmd/server/          # Application entrypoint (wiring + graceful shutdown)
├── configs/             # Configuration loading from the environment
├── internal/            # Private application code
│   ├── handler/         # HTTP transport layer (Echo handlers)
│   ├── service/         # Business logic and validation
│   ├── repository/      # Data access (GORM)
│   └── router/          # Route registration and middleware
├── pkg/                 # Reusable, importable packages
│   ├── apperror/        # Structured application error type
│   ├── database/        # PostgreSQL connection helper
│   └── response/        # Uniform JSON response helpers
├── migrations/          # SQL migrations (*.up.sql / *.down.sql)
├── Dockerfile
└── Makefile
```

### Request flow

```
HTTP request
   │
   ▼
router ──► handler ──► service ──► repository ──► PostgreSQL
                          │
                  validation & domain rules
```

Each layer depends only on the interface of the layer below it, which keeps the
code testable (mock a repository to test a service, mock a service to test a
handler) and makes the data store easy to swap.

## Getting Started

### Prerequisites

- [Go 1.26+](https://go.dev/dl/)
- A running [PostgreSQL](https://www.postgresql.org/) instance
- [golang-migrate](https://github.com/golang-migrate/migrate/tree/master/cmd/migrate)
  CLI (for migrations)

### Setup

```bash
# 1. Copy the environment template and adjust values
cp .env.example .env

# 2. Install dependencies
go mod download

# 3. Apply database migrations
make migrate-up

# 4. Run the server
make run
```

The server listens on `http://localhost:8080` by default.

### Using this as a template

This repo is meant to be cloned and renamed. Update the module path to your own:

```bash
go mod edit -module github.com/<org>/<repo>
grep -rl 'TranTheTuan/go-shortener' . | xargs sed -i 's#TranTheTuan/go-shortener#<org>/<repo>#g'
```

## Configuration

Configuration is read from environment variables (see [`.env.example`](.env.example)).
All values have defaults, so the server runs without any configuration.

| Variable                  | Default       | Description                       |
| ------------------------- | ------------- | -------------------------------- |
| `ENV`                     | `development` | Deployment environment           |
| `SERVER_HOST`             | `0.0.0.0`     | Bind address                     |
| `SERVER_PORT`             | `8080`        | HTTP port                        |
| `SERVER_READ_TIMEOUT`     | `5s`          | Request read timeout             |
| `SERVER_WRITE_TIMEOUT`    | `10s`         | Response write timeout           |
| `SERVER_IDLE_TIMEOUT`     | `120s`        | Keep-alive idle timeout          |
| `SERVER_SHUTDOWN_TIMEOUT` | `10s`         | Graceful shutdown grace period   |
| `DB_HOST`                 | `localhost`   | PostgreSQL host                  |
| `DB_PORT`                 | `5432`        | PostgreSQL port                  |
| `DB_USER`                 | `postgres`    | PostgreSQL user                  |
| `DB_PASSWORD`             | `postgres`    | PostgreSQL password              |
| `DB_NAME`                 | `app`         | Database name                    |
| `DB_SSLMODE`              | `disable`     | SSL mode                         |
| `DB_TIMEZONE`             | `UTC`         | Connection time zone             |
| `DB_MAX_OPEN_CONNS`       | `25`          | Max open connections             |
| `DB_MAX_IDLE_CONNS`       | `25`          | Max idle connections             |
| `DB_CONN_MAX_LIFETIME`    | `5m`          | Max connection lifetime          |
| `SHORTENER_BASE_URL`      | `http://localhost:8080` | Origin used to build short URLs |
| `SHORTENER_API_KEYS`      | _(empty)_     | Comma-separated keys for `X-API-Key` (empty = reject all writes) |
| `SHORTENER_CODE_LENGTH`   | `7`           | Length of generated short codes  |
| `AUTH_JWT_SECRET`         | `dev-insecure-change-me` | HS256 signing secret for access tokens. The default is rejected outside `development` |
| `AUTH_ACCESS_TTL`         | `15m`         | Access-token lifetime            |
| `AUTH_REFRESH_TTL`        | `168h`        | Refresh-token lifetime (7 days)  |
| `AUTH_BCRYPT_COST`        | `12`          | bcrypt work factor for password hashing |
| `QUOTA_DEFAULT_PLAN_CODE` | `basic`       | Plan applied when a user has no active subscription |
| `QUOTA_BASIC_FALLBACK_LIMIT` | `10`       | Daily limit used if the plans table is unreachable |
| `QUOTA_BREAKER_MAX_FAILURES` | `10`       | Consecutive Redis failures that trip the quota circuit breaker |
| `QUOTA_BREAKER_OPEN_TIMEOUT` | `5m`       | How long the breaker stays open before a half-open probe |

## Database Migrations

Migrations live in [`migrations/`](migrations/) and are run manually via the
Makefile — they are **not** executed on server startup, so you stay in control
of when schema changes are applied.

```bash
make migrate-create NAME=add_orders_table   # generate a new migration pair
make migrate-up                             # apply all pending migrations
make migrate-down NUM=1                      # roll back the last migration
make migrate-version                         # print the current version
```

## API

| Method | Path                      | Auth    | Description                        |
| ------ | ------------------------- | ------- | --------------------------------- |
| GET    | `/healthz`                | —       | Health check                      |
| POST   | `/auth/register`          | —       | Register a user (username, email, password) |
| POST   | `/auth/login`             | —       | Log in by email → access + refresh tokens |
| POST   | `/auth/refresh`           | —       | Exchange a refresh token for a new pair |
| POST   | `/auth/logout`            | —       | Revoke a refresh token            |
| GET    | `/auth/me`                | Bearer  | Get the authenticated user        |
| GET    | `/users`                  | —       | List users                        |
| GET    | `/users/:id`              | —       | Get a user by ID                  |
| POST   | `/api/links`              | JWT or API key | Create a short link (JWT → owned by user; subject to daily quota) |
| GET    | `/api/links/:code/stats`  | JWT or API key | Click stats for a short link      |
| GET    | `/:code`                  | —       | Redirect (302) to the original URL |

### URL shortener

Create a short link (the `X-API-Key` header must match one of `SHORTENER_API_KEYS`).
`expires_at` is optional (RFC 3339); omit it for a link that never expires.

```bash
curl -X POST localhost:8080/api/links \
  -H 'X-API-Key: dev-key-1' \
  -H 'Content-Type: application/json' \
  -d '{"url":"https://example.com","expires_at":"2030-01-01T00:00:00Z"}'
# → { "data": { "short_code": "Ab3xY7q", "short_url": "http://localhost:8080/Ab3xY7q", ... } }

curl -i localhost:8080/Ab3xY7q                               # 302 → https://example.com
curl localhost:8080/api/links/Ab3xY7q/stats -H 'X-API-Key: dev-key-1'
```

Visiting an expired link returns `410 Gone`; an unknown code returns `404`.

### Link ownership & daily quota

`POST /api/links` accepts **either** a JWT (`Authorization: Bearer`) **or** an
`X-API-Key`. With a JWT the link is owned by that user (`user_id`) and dedup is
scoped per-owner; with an API key the link is unowned. The redirect endpoint
stays public.

Authenticated users have a daily creation quota from their subscription plan —
the seeded **basic** plan allows **10 links/day** (resets at 00:00 UTC). The
11th returns `429 QUOTA_EXCEEDED`. Reusing a URL you already shortened returns
the existing link and does not consume quota. API-key (unowned) creation is not
quota-limited. Quota uses Redis behind a circuit breaker, so a Redis outage
fails open (links still created). Plans live in the `plans` table; a future
billing system extends `plans`/`subscriptions` to sell higher-quota tiers.

Responses use a uniform envelope:

```jsonc
// Success
{ "data": { "id": 1, "username": "alice", "email": "alice@example.com" } }

// Error
{ "error": { "code": "NOT_FOUND", "message": "user not found" } }
```

### Authentication

Register, then log in by **email** to receive a short-lived access token and a
refresh token. Send the access token as `Authorization: Bearer <token>` on
protected routes (e.g. `/auth/me`). Refresh tokens are stored hashed and rotate
on every use; `/auth/logout` revokes one.

```bash
# Register (username + email both unique; password ≥ 8 chars)
curl -X POST localhost:8080/auth/register \
  -H 'Content-Type: application/json' \
  -d '{"username":"alice","email":"alice@example.com","password":"s3cretpw!"}'

# Log in (by email) → tokens
curl -X POST localhost:8080/auth/login \
  -H 'Content-Type: application/json' \
  -d '{"email":"alice@example.com","password":"s3cretpw!"}'
# → { "data": { "access_token": "...", "refresh_token": "...", "token_type": "Bearer", "expires_in": 900 } }

# Call a protected route
curl localhost:8080/auth/me -H 'Authorization: Bearer <access_token>'

# Rotate tokens / log out
curl -X POST localhost:8080/auth/refresh -H 'Content-Type: application/json' -d '{"refresh_token":"..."}'
curl -X POST localhost:8080/auth/logout  -H 'Content-Type: application/json' -d '{"refresh_token":"..."}'
```

The `users` resource (read-only `GET` endpoints) demonstrates the handler →
service → repository flow; user creation is owned by `/auth/register`.

## Make Targets

Run `make help` to list all targets:

| Target            | Description                          |
| ----------------- | ------------------------------------ |
| `make run`        | Run the HTTP server                  |
| `make build`      | Build the binary into `./build/main` |
| `make tidy`       | Tidy module dependencies             |
| `make test`       | Run the test suite                   |
| `make migrate-*`  | Database migration commands          |

## Docker

```bash
docker build -t my-service .
docker run --rm -p 8080:8080 --env-file .env my-service
```

## License

Released under the [MIT License](LICENSE).
