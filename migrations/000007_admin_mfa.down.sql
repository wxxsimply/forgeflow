ALTER TABLE sessions DROP COLUMN IF EXISTS mfa_verified_at;

ALTER TABLE users
    DROP COLUMN IF EXISTS mfa_last_used_step,
    DROP COLUMN IF EXISTS mfa_recovery_code_hashes,
    DROP COLUMN IF EXISTS mfa_enabled_at,
    DROP COLUMN IF EXISTS mfa_pending_expires_at,
    DROP COLUMN IF EXISTS mfa_pending_secret_ciphertext,
    DROP COLUMN IF EXISTS mfa_secret_ciphertext;
