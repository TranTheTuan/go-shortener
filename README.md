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
| `SHORTENER_CODE_LENGTH`   | `7`           | Length of generated short codes  |
| `KEYCLOAK_ISSUER`         | _(required\*)_ | Public token issuer; validated against the access token's `iss` (e.g. `https://auth.cd.me/realms/<realm>`) |
| `KEYCLOAK_JWKS_URL`       | _(required\*)_ | In-cluster JWKS endpoint for public keys (build by hand; don't copy the public `jwks_uri`) |
| `KEYCLOAK_CLIENT_ID`      | _(empty)_     | Backend client validated as the token audience; empty skips the audience check |
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

\* `KEYCLOAK_ISSUER` and `KEYCLOAK_JWKS_URL` are required outside the `development` environment (startup fails fast otherwise).

| Method | Path                      | Auth    | Description                        |
| ------ | ------------------------- | ------- | --------------------------------- |
| GET    | `/healthz`                | —       | Health check                      |
| GET    | `/auth/me`                | Keycloak JWT | Get the authenticated user    |
| GET    | `/users`                  | Keycloak JWT | List users                    |
| GET    | `/users/:id`              | Keycloak JWT | Get a user by ID              |
| POST   | `/api/links`              | Keycloak JWT | Create a short link (owned by the token's user; subject to daily quota) |
| GET    | `/api/links/:code/stats`  | Keycloak JWT | Click stats for a short link  |
| GET    | `/:code`                  | —       | Redirect (302) to the original URL |

### URL shortener

Create a short link with a Keycloak access token (see [Authentication](#authentication)).
`expires_at` is optional (RFC 3339); omit it for a link that never expires.

```bash
curl -X POST localhost:8080/api/links \
  -H 'Authorization: Bearer <access_token>' \
  -H 'Content-Type: application/json' \
  -d '{"url":"https://example.com","expires_at":"2030-01-01T00:00:00Z"}'
# → { "data": { "short_code": "Ab3xY7q", "short_url": "http://localhost:8080/Ab3xY7q", ... } }

curl -i localhost:8080/Ab3xY7q                               # 302 → https://example.com
curl localhost:8080/api/links/Ab3xY7q/stats -H 'Authorization: Bearer <access_token>'
```

Visiting an expired link returns `410 Gone`; an unknown code returns `404`.

### Link ownership & daily quota

Every `/api/links` create is owned by the authenticated user (`user_id`, mapped
from the Keycloak `sub`); dedup is scoped per-owner. The redirect endpoint stays
public.

Each user has a daily creation quota from their subscription plan — the seeded
**basic** plan allows **10 links/day** (resets at 00:00 UTC). The 11th returns
`429 QUOTA_EXCEEDED`. Reusing a URL you already shortened returns the existing
link and does not consume quota. Quota uses Redis behind a circuit breaker, so a
Redis outage fails open (links still created). Plans live in the `plans` table;
a future billing system extends `plans`/`subscriptions` to sell higher-quota tiers.

Responses use a uniform envelope:

```jsonc
// Success
{ "data": { "id": 1, "username": "alice", "email": "alice@example.com" } }

// Error
{ "error": { "code": "NOT_FOUND", "message": "user not found" } }
```

### Authentication

Authentication is handled by **Keycloak**; this service is an OIDC *resource
server* — it only **validates** access tokens, it does not register/login users
or issue tokens. Obtain an access token from Keycloak (any standard OIDC flow)
and send it as `Authorization: Bearer <token>` on protected routes.

```bash
# Obtain a token from Keycloak (example: password grant for local testing)
TOKEN=$(curl -s https://auth.cd.me/realms/<realm>/protocol/openid-connect/token \
  -d grant_type=password -d client_id=url-shortener-backend \
  -d username=alice -d password=... | jq -r .access_token)

# Call a protected route
curl localhost:8080/auth/me     -H "Authorization: Bearer $TOKEN"
curl -X POST localhost:8080/api/links -H "Authorization: Bearer $TOKEN" \
  -H 'Content-Type: application/json' -d '{"url":"https://example.com"}'
```

The service validates the token's signature (RS256, via the realm JWKS fetched
in-cluster), `iss`, `exp`, and — when `KEYCLOAK_CLIENT_ID` is set — `aud`. On the
first authenticated request a local user is **JIT-provisioned** from the token's
`sub`/`email`/`preferred_username` claims and reused thereafter; link ownership
and quota key on that local user.

**Keycloak setup notes:**
- The backend client needs the `email` + `profile` scopes (the app stores `email` and `preferred_username`).
- Set `KEYCLOAK_CLIENT_ID` to bind tokens to your client: the token must carry it in `aud` **or** `azp`. Keycloak sets `azp` to the requesting client by default, so this works out of the box — no audience mapper required (add one only if you prefer matching on `aud`). Leave it empty to skip the check.

## Frontend

A minimal vanilla (no build step) single-page frontend lives in [`web/`](web/) and is
**embedded into the binary** (`go:embed`) and served by the Go server at the same
origin as the API — so there is no separate deploy and no API CORS:

| Route | Serves |
| ----- | ------ |
| `/` | `web/index.html` (also receives the OIDC callback) |
| `/static/*` | `web/static/` assets (`app.js`, `styles.css`, vendored `keycloak.js`) |
| `/app-config.json` | `{authUrl, realm, clientId}` derived from `KEYCLOAK_*` env (no hardcoding) |

It signs in with **keycloak-js** (Authorization Code + PKCE), then calls the API
with the access token. MVP: sign in/out, show profile, create + copy a short link,
and look up click stats by code. Open `http://localhost:8080/` after `make run`.

**Keycloak client setup** (reuses the same `go-shortener` public client as the backend):
- Enable *Standard flow* + PKCE (S256); it's a public client.
- Add the Go server origin to **Valid redirect URIs** (e.g. `http://localhost:8080/*`, your prod URL) and **Web origins** (the origin).
- The vendored `web/static/keycloak.js` should match your Keycloak server's major version (currently v26).

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
