CREATE TABLE registration_codes (
    normalized_email text PRIMARY KEY,
    code_hash bytea NOT NULL CHECK (octet_length(code_hash) = 32),
    expires_at timestamptz NOT NULL,
    resend_after timestamptz NOT NULL,
    attempts integer NOT NULL DEFAULT 0 CHECK (attempts >= 0 AND attempts <= 5),
    created_at timestamptz NOT NULL DEFAULT now(),
    CHECK (resend_after <= expires_at)
);

CREATE INDEX registration_codes_expires_at_idx ON registration_codes (expires_at);
