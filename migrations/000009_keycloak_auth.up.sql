-- Switch authentication to Keycloak. Users are JIT-provisioned from token claims
-- keyed by keycloak_sub; self-issued credentials and refresh tokens are dropped.
ALTER TABLE users ADD COLUMN keycloak_sub VARCHAR(36);
CREATE UNIQUE INDEX idx_users_keycloak_sub ON users (keycloak_sub);
ALTER TABLE users DROP COLUMN password_hash;
DROP TABLE IF EXISTS refresh_tokens;
