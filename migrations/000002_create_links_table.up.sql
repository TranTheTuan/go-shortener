-- Creates the links table backing internal/repository.Link.
CREATE TABLE IF NOT EXISTS links (
    id           BIGSERIAL PRIMARY KEY,
    short_code   VARCHAR(16)  NOT NULL,
    original_url TEXT         NOT NULL,
    expires_at   TIMESTAMPTZ,
    created_at   TIMESTAMPTZ  NOT NULL DEFAULT now()
);

-- Enforce unique short codes (mirrors the `uniqueIndex` GORM tag) and powers
-- fast lookups on redirect.
CREATE UNIQUE INDEX IF NOT EXISTS idx_links_short_code ON links (short_code);
