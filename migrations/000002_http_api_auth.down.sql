DROP TABLE IF EXISTS audit_log;
DROP TABLE IF EXISTS idempotency_keys;
DROP INDEX IF EXISTS runs_repository_created_idx;
DROP INDEX IF EXISTS runs_owner_created_idx;
ALTER TABLE runs DROP CONSTRAINT IF EXISTS runs_owner_fk;
ALTER TABLE runs DROP COLUMN IF EXISTS repository_id;
DROP TABLE IF EXISTS repositories;
DROP TABLE IF EXISTS sessions;
DROP TABLE IF EXISTS users;
