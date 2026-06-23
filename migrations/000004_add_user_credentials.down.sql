-- Reverts the user credential columns.
DROP INDEX IF EXISTS idx_users_username;
ALTER TABLE users DROP COLUMN IF EXISTS password_hash;
ALTER TABLE users DROP COLUMN IF EXISTS username;
ALTER TABLE users ALTER COLUMN name SET NOT NULL;
