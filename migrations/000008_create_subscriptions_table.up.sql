-- A user's subscription to a plan. Absence of an active row = the user is on
-- the default basic plan (resolved in the service layer). Billing later inserts
-- rows for paid plans. The partial unique index allows at most one active
-- subscription per user.
CREATE TABLE IF NOT EXISTS subscriptions (
    id                   BIGSERIAL   PRIMARY KEY,
    user_id              BIGINT      NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    plan_id              BIGINT      NOT NULL REFERENCES plans(id),
    status               VARCHAR(20) NOT NULL DEFAULT 'active',
    current_period_start TIMESTAMPTZ NOT NULL DEFAULT now(),
    current_period_end   TIMESTAMPTZ,
    created_at           TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at           TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE UNIQUE INDEX idx_subscriptions_active_user ON subscriptions (user_id) WHERE status = 'active';
CREATE INDEX idx_subscriptions_user_id ON subscriptions (user_id);
