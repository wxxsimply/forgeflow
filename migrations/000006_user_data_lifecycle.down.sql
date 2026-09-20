DROP TABLE user_deletion_requests;
DROP TABLE user_data_exports;

ALTER TABLE prompt_releases DROP CONSTRAINT prompt_releases_promoted_by_fkey;
ALTER TABLE prompt_releases ALTER COLUMN promoted_by SET NOT NULL;
ALTER TABLE prompt_releases ADD CONSTRAINT prompt_releases_promoted_by_fkey
    FOREIGN KEY (promoted_by) REFERENCES users(id);

ALTER TABLE eval_runs DROP CONSTRAINT eval_runs_created_by_fkey;
ALTER TABLE eval_runs ALTER COLUMN created_by SET NOT NULL;
ALTER TABLE eval_runs ADD CONSTRAINT eval_runs_created_by_fkey
    FOREIGN KEY (created_by) REFERENCES users(id);

ALTER TABLE runs DROP CONSTRAINT runs_owner_fk;
ALTER TABLE runs ADD CONSTRAINT runs_owner_fk
    FOREIGN KEY (owner_id) REFERENCES users(id) ON DELETE SET NULL;

ALTER TABLE users DROP CONSTRAINT users_status_check;
ALTER TABLE users ADD CONSTRAINT users_status_check
    CHECK (status IN ('active', 'disabled'));
