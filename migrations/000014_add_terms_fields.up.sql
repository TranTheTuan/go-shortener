ALTER TABLE users
    ADD COLUMN IF NOT EXISTS terms_accepted_at TIMESTAMPTZ,
    ADD COLUMN IF NOT EXISTS terms_version VARCHAR(50);

CREATE INDEX IF NOT EXISTS idx_users_terms_version ON users(terms_version);
