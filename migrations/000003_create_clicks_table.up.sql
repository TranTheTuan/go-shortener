-- Creates the clicks table backing internal/repository.Click. One row per
-- redirect, recorded asynchronously for analytics.
CREATE TABLE IF NOT EXISTS clicks (
    id         BIGSERIAL PRIMARY KEY,
    link_id    BIGINT       NOT NULL REFERENCES links(id) ON DELETE CASCADE,
    clicked_at TIMESTAMPTZ  NOT NULL DEFAULT now(),
    referrer   TEXT,
    ip_address VARCHAR(45),
    user_agent TEXT
);

-- Speeds up per-link analytics aggregation.
CREATE INDEX IF NOT EXISTS idx_clicks_link_id ON clicks (link_id);
