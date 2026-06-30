-- Subscription plan catalog. A future billing system extends this with pricing
-- and feature columns; seed the free "basic" plan (10 links/day) here.
CREATE TABLE IF NOT EXISTS plans (
    id               BIGSERIAL    PRIMARY KEY,
    code             VARCHAR(50)  NOT NULL,
    name             VARCHAR(255) NOT NULL,
    daily_link_quota INT          NOT NULL,
    price_cents      INT          NOT NULL DEFAULT 0,
    is_active        BOOLEAN      NOT NULL DEFAULT TRUE,
    created_at       TIMESTAMPTZ  NOT NULL DEFAULT now(),
    updated_at       TIMESTAMPTZ  NOT NULL DEFAULT now()
);

CREATE UNIQUE INDEX idx_plans_code ON plans (code);

INSERT INTO plans (code, name, daily_link_quota, price_cents)
VALUES ('basic', 'Basic', 10, 0);
