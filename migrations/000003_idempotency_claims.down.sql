DELETE FROM idempotency_keys WHERE run_id IS NULL;
ALTER TABLE idempotency_keys DROP COLUMN status;
ALTER TABLE idempotency_keys ALTER COLUMN run_id SET NOT NULL;
