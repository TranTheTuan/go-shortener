DROP INDEX IF EXISTS idx_users_terms_version;

ALTER TABLE users
    DROP COLUMN IF EXISTS terms_accepted_at,
    DROP COLUMN IF EXISTS terms_version;
