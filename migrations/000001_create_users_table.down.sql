-- Reverts 000001_create_users_table.up.sql.
DROP INDEX IF EXISTS idx_users_email;
DROP TABLE IF EXISTS users;
