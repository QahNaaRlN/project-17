-- +goose Up
-- MF-027/PUB-004: immutable validation binding; registration/activation is a later stage.
ALTER TABLE changesets ADD CONSTRAINT changesets_project_id_id_key UNIQUE (project_id, id);
ALTER TABLE environments ADD CONSTRAINT environments_project_id_id_key UNIQUE (project_id, id);
CREATE TABLE changeset_manifest_candidates (
  changeset_id uuid PRIMARY KEY,
  project_id uuid NOT NULL,
  environment_id uuid NOT NULL,
  manifest_id uuid NOT NULL,
  base_manifest_id uuid,
  FOREIGN KEY (project_id, changeset_id) REFERENCES changesets(project_id, id),
  FOREIGN KEY (project_id, environment_id) REFERENCES environments(project_id, id),
  FOREIGN KEY (project_id, manifest_id) REFERENCES manifests(project_id, id),
  FOREIGN KEY (project_id, base_manifest_id) REFERENCES manifests(project_id, id)
);
CREATE TRIGGER immutable_candidate_binding BEFORE UPDATE OR DELETE ON changeset_manifest_candidates
  FOR EACH ROW EXECUTE FUNCTION reject_manifest_storage_mutation();

-- +goose Down
DROP TABLE changeset_manifest_candidates;
ALTER TABLE environments DROP CONSTRAINT environments_project_id_id_key;
ALTER TABLE changesets DROP CONSTRAINT changesets_project_id_id_key;
