ALTER TABLE idempotency_keys ALTER COLUMN run_id DROP NOT NULL;
ALTER TABLE idempotency_keys ADD COLUMN status text NOT NULL DEFAULT 'completed'
    CHECK (status IN ('pending', 'completed'));
