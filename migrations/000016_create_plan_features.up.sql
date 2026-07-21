CREATE TABLE IF NOT EXISTS plan_features (
    id          BIGSERIAL   PRIMARY KEY,
    plan_id     BIGINT      NOT NULL REFERENCES plans(id) ON DELETE CASCADE,
    feature_key VARCHAR(64) NOT NULL,
    enabled     BOOLEAN     NOT NULL DEFAULT TRUE,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX idx_plan_features_plan_key ON plan_features (plan_id, feature_key);

INSERT INTO plan_features (plan_id, feature_key, enabled)
SELECT p.id, f.key, TRUE
FROM plans p
CROSS JOIN (VALUES
    ('analytics.timeseries'),
    ('analytics.referrers'),
    ('analytics.devices')
) AS f(key)
WHERE p.code IN ('pro', 'business')
ON CONFLICT (plan_id, feature_key) DO NOTHING;
