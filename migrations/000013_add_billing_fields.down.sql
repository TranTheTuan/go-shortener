ALTER TABLE plans
    DROP COLUMN IF EXISTS paddle_price_id_monthly,
    DROP COLUMN IF EXISTS paddle_price_id_yearly;

ALTER TABLE subscriptions
    DROP COLUMN IF EXISTS paddle_subscription_id,
    DROP COLUMN IF EXISTS paddle_customer_id,
    DROP COLUMN IF EXISTS paddle_price_id,
    DROP COLUMN IF EXISTS billing_interval,
    DROP COLUMN IF EXISTS canceled_at;

ALTER TABLE users DROP COLUMN IF EXISTS paddle_customer_id;

DELETE FROM plans WHERE code IN ('pro', 'business');
