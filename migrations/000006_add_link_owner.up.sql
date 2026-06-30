-- Associates each short link with the user who created it. Nullable: links
-- created via the static X-API-Key have no owner. ON DELETE SET NULL keeps a
-- link redirecting even if its owner is deleted.
ALTER TABLE links ADD COLUMN user_id BIGINT REFERENCES users(id) ON DELETE SET NULL;

-- Supports per-owner dedup lookups (WHERE user_id = ? AND original_url = ?).
CREATE INDEX idx_links_user_id_original_url ON links (user_id, original_url);
