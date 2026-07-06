-- Adds a reversible on/off flag to links. Existing rows stay live (default
-- true). A disabled link stops redirecting (410) but keeps its analytics —
-- distinct from a hard delete.
ALTER TABLE links ADD COLUMN is_active BOOLEAN NOT NULL DEFAULT true;
