-- Rollback: rename column back and revert values
UPDATE plans SET monthly_link_quota = 10 WHERE code = 'basic';
UPDATE plans SET monthly_link_quota = 500 WHERE code = 'pro';

ALTER TABLE plans RENAME COLUMN monthly_link_quota TO daily_link_quota;
