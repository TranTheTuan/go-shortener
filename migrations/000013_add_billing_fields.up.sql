ALTER TABLE subscriptions
    ADD COLUMN IF NOT EXISTS paddle_subscription_id VARCHAR(255) UNIQUE,
    ADD COLUMN IF NOT EXISTS paddle_customer_id     VARCHAR(255),
    ADD COLUMN IF NOT EXISTS paddle_price_id        VARCHAR(255),
    ADD COLUMN IF NOT EXISTS billing_interval       VARCHAR(10),
    ADD COLUMN IF NOT EXISTS canceled_at            TIMESTAMPTZ;

ALTER TABLE users
    ADD COLUMN IF NOT EXISTS paddle_customer_id VARCHAR(255) UNIQUE;

ALTER TABLE plans
    ADD COLUMN IF NOT EXISTS paddle_price_id_monthly VARCHAR(255),
    ADD COLUMN IF NOT EXISTS paddle_price_id_yearly  VARCHAR(255);

INSERT INTO plans (code, name, daily_link_quota, price_cents, paddle_price_id_monthly, paddle_price_id_yearly)
VALUES
    ('pro',      'Pro',      500,  900,  'pri_01kxhz0kmh1hqbqjxt8tmr75dk', 'pri_01kxhz3qfv1v1qfs2q34nck4fv'),
    ('business', 'Business', -1,  2900, 'pri_01kxhz6evz5rhxg7gv89q6ycma', 'pri_01kxhzarp44a6np3xrvp7fneep')
ON CONFLICT (code) DO NOTHING;
