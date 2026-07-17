# Phase 01 — Database + Config

**Status**: pending  
**Priority**: critical  
**Effort**: 30 min  

## Context

Add `terms_accepted_at` and `terms_version` columns to the `users` table, and expose `TERMS_VERSION` as a configuration parameter. This allows tracking which T&C version each user has accepted.

## Implementation Steps

### Migration File: `000014_add_terms_fields.up.sql`

```sql
ALTER TABLE users
    ADD COLUMN IF NOT EXISTS terms_accepted_at TIMESTAMPTZ,
    ADD COLUMN IF NOT EXISTS terms_version VARCHAR(50);

CREATE INDEX IF NOT EXISTS idx_users_terms_version ON users(terms_version);
```

**Rationale**: Nullable columns (users who haven't accepted yet will have NULL). Index on `terms_version` for future queries like "find all users on version X who need to re-accept".

### Migration File: `000014_add_terms_fields.down.sql`

```sql
DROP INDEX IF EXISTS idx_users_terms_version;

ALTER TABLE users
    DROP COLUMN IF EXISTS terms_accepted_at,
    DROP COLUMN IF EXISTS terms_version;
```

### Config: `configs/config.go`

Add to the `Config` struct (line ~28, after `Paddle PaddleConfig`):

```go
// TermsConfig holds T&C versioning settings.
Terms TermsConfig `envPrefix:"TERMS_"`
```

Then add the nested struct after the `PaddleConfig` type definition (line ~60+):

```go
// TermsConfig holds Terms & Conditions versioning.
type TermsConfig struct {
	// CurrentVersion is the active T&C version. Bump this to force users to re-accept.
	// Format: semantic version string (e.g., "1.0", "2.1"). Injected into /app-config.json.
	CurrentVersion string `env:"VERSION" envDefault:"1.0"`
}
```

## Verification

1. Run `go build ./...` → should compile cleanly
2. Read `configs/config.go` back: confirm `Terms TermsConfig` is in the struct and defaults are applied
3. Manual test: start server with `TERMS_VERSION=2.0` env var → confirm it loads without error and appears in app config (verified in Phase 04)

## Notes

- The index on `terms_version` is optional for MVP but good for future audit/reporting queries
- Nullable columns are intentional: existing users won't have a value until they accept after upgrade
- No need to backfill existing users (Phase 04 frontend handles the gate retroactively)
