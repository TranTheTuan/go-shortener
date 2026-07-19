-- Rename daily_link_quota to monthly_link_quota and update plan values
ALTER TABLE plans RENAME COLUMN daily_link_quota TO monthly_link_quota;

-- Update plan quota values: daily → monthly
UPDATE plans SET monthly_link_quota = 60 WHERE code = 'basic';
UPDATE plans SET monthly_link_quota = 1500 WHERE code = 'pro';
UPDATE plans SET monthly_link_quota = 50000 WHERE code = 'business';
