-- Adds authentication credentials to the users table (assumed empty).
-- username + password_hash back internal/repository.User auth fields; name
-- becomes nullable since registration no longer requires a display name.
ALTER TABLE users ADD COLUMN username      VARCHAR(255) NOT NULL;
ALTER TABLE users ADD COLUMN password_hash VARCHAR(255) NOT NULL;
ALTER TABLE users ALTER COLUMN name DROP NOT NULL;

-- Enforce unique usernames (mirrors the `uniqueIndex` GORM tag).
CREATE UNIQUE INDEX idx_users_username ON users (username);
