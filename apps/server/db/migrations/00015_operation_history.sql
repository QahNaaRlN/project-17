-- +goose Up
ALTER TABLE operations ADD COLUMN batch_id uuid;
ALTER TABLE operations ADD COLUMN redo_of uuid REFERENCES operations(id);
ALTER TABLE operations ADD CONSTRAINT operation_history_link CHECK (undo_of IS NULL OR redo_of IS NULL);
CREATE INDEX operations_by_batch ON operations(changeset_id, batch_id, seq);
CREATE INDEX operations_by_redo ON operations(redo_of) WHERE redo_of IS NOT NULL;
ALTER TABLE changesets ADD COLUMN redo_barrier_seq integer NOT NULL DEFAULT 0;

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN
  IF EXISTS (SELECT 1 FROM operations WHERE batch_id IS NOT NULL OR redo_of IS NOT NULL)
     OR EXISTS (SELECT 1 FROM changesets WHERE redo_barrier_seq <> 0) THEN
    RAISE EXCEPTION 'operation history prevents downgrade';
  END IF;
END $$;
-- +goose StatementEnd
ALTER TABLE changesets DROP COLUMN redo_barrier_seq;
DROP INDEX operations_by_redo;
DROP INDEX operations_by_batch;
ALTER TABLE operations DROP CONSTRAINT operation_history_link;
ALTER TABLE operations DROP COLUMN redo_of;
ALTER TABLE operations DROP COLUMN batch_id;
