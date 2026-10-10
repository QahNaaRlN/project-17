-- +goose Up
ALTER TABLE operations ALTER COLUMN target_object_id DROP NOT NULL;
ALTER TABLE operations ADD COLUMN target_schema_name text;
ALTER TABLE operations ADD CONSTRAINT operation_target_kind CHECK (
 (type = 'schema.apply' AND target_object_id IS NULL AND target_schema_name IS NOT NULL)
 OR (type <> 'schema.apply' AND target_object_id IS NOT NULL AND target_schema_name IS NULL)
);
ALTER TABLE publications ADD COLUMN previous_manifest_id uuid;
ALTER TABLE publications ADD COLUMN current_manifest_id uuid;
ALTER TABLE publications ADD COLUMN manifest_changed boolean NOT NULL DEFAULT false;
ALTER TABLE publications ADD FOREIGN KEY (project_id, previous_manifest_id) REFERENCES manifests(project_id,id);
ALTER TABLE publications ADD FOREIGN KEY (project_id, current_manifest_id) REFERENCES manifests(project_id,id);
CREATE TABLE schema_changeset_claims (
 id uuid PRIMARY KEY, changeset_id uuid NOT NULL REFERENCES changesets(id),
 previous_owner_id uuid NOT NULL REFERENCES actors(id), owner_id uuid NOT NULL REFERENCES actors(id),
 created_at timestamptz NOT NULL DEFAULT now()
);
CREATE TRIGGER immutable_schema_claim BEFORE UPDATE OR DELETE ON schema_changeset_claims
 FOR EACH ROW EXECUTE FUNCTION reject_manifest_storage_mutation();
-- +goose Down
-- Preserve the audit journal if this lifecycle has already been used.
-- +goose StatementBegin
DO $$ BEGIN
 IF EXISTS (SELECT 1 FROM operations WHERE type='schema.apply')
 OR EXISTS (SELECT 1 FROM schema_changeset_claims)
 OR EXISTS (SELECT 1 FROM publications WHERE manifest_changed) THEN
  RAISE EXCEPTION 'schema publication data prevents downgrade' USING ERRCODE='23514';
 END IF;
END $$;
-- +goose StatementEnd
DROP TABLE schema_changeset_claims;
ALTER TABLE publications DROP COLUMN manifest_changed, DROP COLUMN current_manifest_id, DROP COLUMN previous_manifest_id;
ALTER TABLE operations DROP CONSTRAINT operation_target_kind;
ALTER TABLE operations DROP COLUMN target_schema_name;
ALTER TABLE operations ALTER COLUMN target_object_id SET NOT NULL;
