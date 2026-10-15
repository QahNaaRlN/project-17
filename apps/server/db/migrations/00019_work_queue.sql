-- +goose Up
CREATE INDEX changesets_work_queue ON changesets (project_id, created_at DESC, id DESC)
WHERE kind='standard' AND state IN ('open','failed','changes_requested','in_review','approved');

-- +goose Down
DROP INDEX changesets_work_queue;
