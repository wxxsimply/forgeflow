CREATE TABLE users (
    id uuid PRIMARY KEY,
    email text NOT NULL,
    normalized_email text NOT NULL UNIQUE,
    password_hash text NOT NULL,
    role text NOT NULL CHECK (role IN ('admin', 'operator', 'viewer')),
    status text NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'disabled')),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE sessions (
    id uuid PRIMARY KEY,
    user_id uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    token_hash bytea NOT NULL UNIQUE CHECK (octet_length(token_hash) = 32),
    csrf_hash bytea NOT NULL CHECK (octet_length(csrf_hash) = 32),
    source_ip text NOT NULL DEFAULT '',
    user_agent text NOT NULL DEFAULT '',
    created_at timestamptz NOT NULL DEFAULT now(),
    last_seen_at timestamptz NOT NULL DEFAULT now(),
    expires_at timestamptz NOT NULL,
    idle_expires_at timestamptz NOT NULL,
    revoked_at timestamptz NULL
);

CREATE INDEX sessions_user_active_idx ON sessions (user_id, created_at DESC)
    WHERE revoked_at IS NULL;
CREATE INDEX sessions_expiry_idx ON sessions (expires_at, idle_expires_at)
    WHERE revoked_at IS NULL;

CREATE TABLE repositories (
    id uuid PRIMARY KEY,
    owner_id uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    name text NOT NULL,
    local_path text NOT NULL,
    default_branch text NOT NULL DEFAULT 'HEAD',
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (owner_id, name)
);

ALTER TABLE runs
    ADD COLUMN repository_id uuid NULL REFERENCES repositories(id) ON DELETE SET NULL,
    ADD CONSTRAINT runs_owner_fk FOREIGN KEY (owner_id) REFERENCES users(id) ON DELETE SET NULL;

CREATE INDEX runs_owner_created_idx ON runs (owner_id, created_at DESC, id DESC);
CREATE INDEX runs_repository_created_idx ON runs (repository_id, created_at DESC);

CREATE TABLE idempotency_keys (
    owner_id uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    key text NOT NULL,
    request_hash bytea NOT NULL CHECK (octet_length(request_hash) = 32),
    run_id uuid NOT NULL REFERENCES runs(id) ON DELETE CASCADE,
    created_at timestamptz NOT NULL DEFAULT now(),
    expires_at timestamptz NOT NULL,
    PRIMARY KEY (owner_id, key)
);

CREATE INDEX idempotency_expiry_idx ON idempotency_keys (expires_at);

CREATE TABLE audit_log (
    sequence bigserial PRIMARY KEY,
    actor_id uuid NULL REFERENCES users(id) ON DELETE SET NULL,
    action text NOT NULL,
    resource_type text NOT NULL,
    resource_id text NOT NULL,
    request_id text NOT NULL,
    source_ip text NOT NULL DEFAULT '',
    details jsonb NOT NULL DEFAULT '{}'::jsonb,
    created_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX audit_log_actor_created_idx ON audit_log (actor_id, created_at DESC);
CREATE INDEX audit_log_resource_idx ON audit_log (resource_type, resource_id, created_at DESC);
