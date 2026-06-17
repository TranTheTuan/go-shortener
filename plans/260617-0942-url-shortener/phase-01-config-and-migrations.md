# Phase 01 — Config + DB Migrations

**Priority:** P0 · **Status:** pending · **Depends:** none

Add shortener config + create `links` and `clicks` tables.

## Files

- Modify: `configs/config.go`, `.env.example`
- Create: `migrations/000002_create_links_table.up.sql` / `.down.sql`
- Create: `migrations/000003_create_clicks_table.up.sql` / `.down.sql`

## Steps

### 1. Config (`configs/config.go`)

Add `Shortener ShortenerConfig` to `Config` with `envPrefix:"SHORTENER_"`:

```go
type Config struct {
	Env       string          `env:"ENV" envDefault:"development"`
	Server    ServerConfig    `envPrefix:"SERVER_"`
	Database  DatabaseConfig  `envPrefix:"DB_"`
	Shortener ShortenerConfig `envPrefix:"SHORTENER_"`
}

// ShortenerConfig holds URL-shortener settings.
type ShortenerConfig struct {
	BaseURL    string   `env:"BASE_URL" envDefault:"http://localhost:8080"`
	APIKeys    []string `env:"API_KEYS" envSeparator:","`
	CodeLength int      `env:"CODE_LENGTH" envDefault:"7"`
}
```

Note: `caarlos0/env` parses `[]string` via `envSeparator`. Empty `API_KEYS` ⇒ empty slice ⇒ middleware rejects all (fail-closed). Document in `.env.example`.

### 2. `.env.example`

Append:
```
# URL shortener
SHORTENER_BASE_URL=http://localhost:8080
SHORTENER_API_KEYS=dev-key-1,dev-key-2
SHORTENER_CODE_LENGTH=7
```

### 3. Migration `000002_create_links_table.up.sql`

```sql
CREATE TABLE IF NOT EXISTS links (
    id           BIGSERIAL PRIMARY KEY,
    short_code   VARCHAR(16)  NOT NULL,
    original_url TEXT         NOT NULL,
    expires_at   TIMESTAMPTZ,
    created_at   TIMESTAMPTZ  NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX IF NOT EXISTS idx_links_short_code ON links (short_code);
```
`.down.sql`: `DROP TABLE IF EXISTS links;`

### 4. Migration `000003_create_clicks_table.up.sql`

```sql
CREATE TABLE IF NOT EXISTS clicks (
    id         BIGSERIAL PRIMARY KEY,
    link_id    BIGINT       NOT NULL REFERENCES links(id) ON DELETE CASCADE,
    clicked_at TIMESTAMPTZ  NOT NULL DEFAULT now(),
    referrer   TEXT,
    ip_address VARCHAR(45),
    user_agent TEXT
);
CREATE INDEX IF NOT EXISTS idx_clicks_link_id ON clicks (link_id);
```
`.down.sql`: `DROP TABLE IF EXISTS clicks;`

## Todo

- [ ] Add `ShortenerConfig` + field to `Config`
- [ ] Update `.env.example`
- [ ] Create links migration (up/down)
- [ ] Create clicks migration (up/down)
- [ ] `go build ./...` passes

## Success Criteria

- `make migrate-up` applies both migrations cleanly; `make migrate-down NUM=2` rolls back
- Config loads with defaults
