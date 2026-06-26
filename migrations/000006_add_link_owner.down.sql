DROP INDEX IF EXISTS idx_links_user_id_original_url;
ALTER TABLE links DROP COLUMN IF EXISTS user_id;
