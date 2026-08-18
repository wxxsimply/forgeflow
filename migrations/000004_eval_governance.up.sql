CREATE TABLE eval_runs (
    id uuid PRIMARY KEY,
    created_by uuid NOT NULL REFERENCES users(id),
    dataset text NOT NULL,
    dataset_version text NOT NULL,
    status text NOT NULL CHECK (status IN ('completed', 'failed')),
    report_json jsonb NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX eval_runs_created_idx ON eval_runs (created_at DESC, id DESC);

CREATE TABLE prompt_releases (
    id uuid PRIMARY KEY,
    agent text NOT NULL,
    version text NOT NULL,
    prompt_sha256 text NOT NULL CHECK (prompt_sha256 ~ '^[a-f0-9]{64}$'),
    eval_run_id uuid NOT NULL REFERENCES eval_runs(id),
    promoted_by uuid NOT NULL REFERENCES users(id),
    rollback_of uuid NULL REFERENCES prompt_releases(id),
    comment text NOT NULL DEFAULT '',
    active boolean NOT NULL DEFAULT true,
    created_at timestamptz NOT NULL DEFAULT now()
);

CREATE UNIQUE INDEX prompt_releases_one_active_agent_idx ON prompt_releases (agent) WHERE active;
CREATE INDEX prompt_releases_agent_created_idx ON prompt_releases (agent, created_at DESC);
