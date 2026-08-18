CREATE TABLE runs (
    id uuid PRIMARY KEY,
    owner_id uuid NULL,
    status text NOT NULL,
    version bigint NOT NULL CHECK (version >= 0),
    current_node_id text NOT NULL,
    task text NOT NULL,
    repository_path text NOT NULL,
    base_revision text NOT NULL,
    budget jsonb NOT NULL,
    state_json jsonb NOT NULL,
    created_at timestamptz NOT NULL,
    updated_at timestamptz NOT NULL
);

CREATE TABLE checkpoints (
    run_id uuid NOT NULL REFERENCES runs(id) ON DELETE CASCADE,
    version bigint NOT NULL CHECK (version > 0),
    node_id text NOT NULL,
    state_json jsonb NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (run_id, version)
);

CREATE TABLE run_events (
    run_id uuid NOT NULL REFERENCES runs(id) ON DELETE CASCADE,
    sequence bigint NOT NULL CHECK (sequence > 0),
    event_id uuid NOT NULL UNIQUE,
    trace_id uuid NOT NULL,
    type text NOT NULL,
    node_id text NOT NULL DEFAULT '',
    message text NOT NULL,
    payload jsonb NOT NULL,
    created_at timestamptz NOT NULL,
    PRIMARY KEY (run_id, sequence)
);

CREATE INDEX run_events_created_idx ON run_events (run_id, created_at);

CREATE TABLE approvals (
    id uuid PRIMARY KEY,
    run_id uuid NOT NULL REFERENCES runs(id) ON DELETE CASCADE,
    type text NOT NULL,
    risk text NOT NULL,
    status text NOT NULL,
    request_json jsonb NOT NULL,
    decision_comment text NOT NULL DEFAULT '',
    requested_at timestamptz NOT NULL,
    resolved_at timestamptz NULL,
    updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX approvals_run_status_idx ON approvals (run_id, status);

CREATE TABLE node_executions (
    run_id uuid NOT NULL REFERENCES runs(id) ON DELETE CASCADE,
    node_id text NOT NULL,
    iteration integer NOT NULL CHECK (iteration >= 0),
    idempotency_key text NOT NULL,
    status text NOT NULL,
    attempts integer NOT NULL CHECK (attempts >= 0),
    execution_json jsonb NOT NULL,
    updated_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (run_id, node_id, iteration, idempotency_key)
);

CREATE TABLE outbox (
    id uuid PRIMARY KEY,
    topic text NOT NULL,
    dedupe_key text NOT NULL UNIQUE,
    payload jsonb NOT NULL,
    available_at timestamptz NOT NULL DEFAULT now(),
    published_at timestamptz NULL,
    attempts integer NOT NULL DEFAULT 0 CHECK (attempts >= 0),
    last_error text NOT NULL DEFAULT '',
    created_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX outbox_ready_idx ON outbox (available_at, created_at) WHERE published_at IS NULL;

CREATE TABLE jobs (
    id uuid PRIMARY KEY,
    type text NOT NULL,
    run_id uuid NULL REFERENCES runs(id) ON DELETE CASCADE,
    dedupe_key text NOT NULL UNIQUE,
    payload jsonb NOT NULL,
    status text NOT NULL CHECK (status IN ('queued', 'leased', 'retry', 'completed', 'dead')),
    attempt integer NOT NULL DEFAULT 0 CHECK (attempt >= 0),
    max_attempts integer NOT NULL CHECK (max_attempts > 0),
    available_at timestamptz NOT NULL DEFAULT now(),
    lease_id uuid NULL,
    lease_owner text NULL,
    lease_until timestamptz NULL,
    last_error text NOT NULL DEFAULT '',
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    completed_at timestamptz NULL
);

CREATE INDEX jobs_lease_ready_idx ON jobs (available_at, created_at)
    WHERE status IN ('queued', 'retry', 'leased');
CREATE INDEX jobs_run_idx ON jobs (run_id, created_at);

CREATE TABLE artifacts (
    id uuid PRIMARY KEY,
    run_id uuid NOT NULL REFERENCES runs(id) ON DELETE CASCADE,
    kind text NOT NULL,
    storage_key text NOT NULL UNIQUE,
    sha256 text NOT NULL CHECK (sha256 ~ '^[a-f0-9]{64}$'),
    size_bytes bigint NOT NULL CHECK (size_bytes >= 0),
    content_type text NOT NULL,
    metadata jsonb NOT NULL DEFAULT '{}'::jsonb,
    created_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX artifacts_run_kind_idx ON artifacts (run_id, kind, created_at);

CREATE TABLE model_calls (
    run_id uuid NOT NULL REFERENCES runs(id) ON DELETE CASCADE,
    sequence integer NOT NULL CHECK (sequence > 0),
    node_id text NOT NULL,
    agent text NOT NULL,
    model text NOT NULL,
    status text NOT NULL,
    call_json jsonb NOT NULL,
    created_at timestamptz NOT NULL,
    PRIMARY KEY (run_id, sequence)
);

CREATE TABLE tool_calls (
    call_id uuid PRIMARY KEY,
    run_id uuid NOT NULL REFERENCES runs(id) ON DELETE CASCADE,
    node_id text NOT NULL,
    tool_name text NOT NULL,
    status text NOT NULL,
    call_json jsonb NOT NULL,
    created_at timestamptz NOT NULL
);
