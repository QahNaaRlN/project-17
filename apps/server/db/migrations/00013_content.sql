-- +goose Up
-- CNT-012: tombstone is versioned and rolled back with content pointers.
ALTER TABLE object_versions ADD COLUMN deleted boolean NOT NULL DEFAULT false;

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN
 IF EXISTS (SELECT 1 FROM object_versions WHERE deleted) THEN
  RAISE EXCEPTION 'content tombstones prevent downgrade';
 END IF;
END $$;
-- +goose StatementEnd
ALTER TABLE object_versions DROP COLUMN deleted;
