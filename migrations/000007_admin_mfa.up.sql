ALTER TABLE users
    ADD COLUMN mfa_secret_ciphertext bytea NULL,
    ADD COLUMN mfa_pending_secret_ciphertext bytea NULL,
    ADD COLUMN mfa_pending_expires_at timestamptz NULL,
    ADD COLUMN mfa_enabled_at timestamptz NULL,
    ADD COLUMN mfa_recovery_code_hashes jsonb NOT NULL DEFAULT '[]'::jsonb
        CHECK (jsonb_typeof(mfa_recovery_code_hashes) = 'array'),
    ADD COLUMN mfa_last_used_step bigint NOT NULL DEFAULT -1;

ALTER TABLE sessions
    ADD COLUMN mfa_verified_at timestamptz NULL;
