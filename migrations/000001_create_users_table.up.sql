-- Creates the users table backing internal/repository.User.
CREATE TABLE IF NOT EXISTS users (
    id         BIGSERIAL PRIMARY KEY,
    name       VARCHAR(255) NOT NULL,
    email      VARCHAR(255) NOT NULL,
    created_at TIMESTAMPTZ  NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ  NOT NULL DEFAULT now()
);

-- Enforce unique emails (mirrors the `uniqueIndex` GORM tag).
CREATE UNIQUE INDEX IF NOT EXISTS idx_users_email ON users (email);
