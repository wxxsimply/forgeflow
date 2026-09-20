ALTER TABLE users DROP CONSTRAINT users_status_check;
ALTER TABLE users ADD CONSTRAINT users_status_check
    CHECK (status IN ('active', 'disabled', 'deletion_pending'));

ALTER TABLE runs DROP CONSTRAINT runs_owner_fk;
ALTER TABLE runs ADD CONSTRAINT runs_owner_fk
    FOREIGN KEY (owner_id) REFERENCES users(id) ON DELETE CASCADE;

ALTER TABLE eval_runs DROP CONSTRAINT eval_runs_created_by_fkey;
ALTER TABLE eval_runs ALTER COLUMN created_by DROP NOT NULL;
ALTER TABLE eval_runs ADD CONSTRAINT eval_runs_created_by_fkey
    FOREIGN KEY (created_by) REFERENCES users(id) ON DELETE SET NULL;

ALTER TABLE prompt_releases DROP CONSTRAINT prompt_releases_promoted_by_fkey;
ALTER TABLE prompt_releases ALTER COLUMN promoted_by DROP NOT NULL;
ALTER TABLE prompt_releases ADD CONSTRAINT prompt_releases_promoted_by_fkey
    FOREIGN KEY (promoted_by) REFERENCES users(id) ON DELETE SET NULL;

CREATE TABLE user_data_exports (
    id uuid PRIMARY KEY,
    owner_id uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    created_at timestamptz NOT NULL DEFAULT now(),
    expires_at timestamptz NOT NULL,
    downloaded_at timestamptz NULL,
    CHECK (expires_at > created_at)
);

CREATE INDEX user_data_exports_owner_created_idx
    ON user_data_exports (owner_id, created_at DESC);
CREATE INDEX user_data_exports_expiry_idx ON user_data_exports (expires_at);

CREATE TABLE user_deletion_requests (
    id uuid PRIMARY KEY,
    owner_id uuid NULL REFERENCES users(id) ON DELETE SET NULL,
    owner_hash text NOT NULL UNIQUE CHECK (owner_hash ~ '^[a-f0-9]{64}$'),
    status text NOT NULL CHECK (status IN ('pending', 'processing', 'failed', 'completed')),
    artifact_manifest jsonb NOT NULL DEFAULT '[]'::jsonb,
    deleted_artifact_ids jsonb NOT NULL DEFAULT '[]'::jsonb,
    attempts integer NOT NULL DEFAULT 0 CHECK (attempts >= 0),
    last_error text NOT NULL DEFAULT '',
    backup_purge_after timestamptz NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    completed_at timestamptz NULL
);

CREATE INDEX user_deletion_requests_status_idx
    ON user_deletion_requests (status, updated_at);
