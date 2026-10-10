-- +goose Up
CREATE TABLE changeset_manifest_diagnostics (
 changeset_id uuid NOT NULL, project_id uuid NOT NULL, environment_id uuid NOT NULL,
 manifest_id uuid, checked_seq integer NOT NULL,
 diagnostics jsonb NOT NULL CHECK (jsonb_typeof(diagnostics) = 'object'),
 stale boolean NOT NULL DEFAULT false, checked_at timestamptz NOT NULL DEFAULT now(),
 PRIMARY KEY (changeset_id, environment_id),
 FOREIGN KEY (project_id, changeset_id) REFERENCES changesets(project_id, id),
 FOREIGN KEY (project_id, environment_id) REFERENCES environments(project_id, id),
 FOREIGN KEY (project_id, manifest_id) REFERENCES manifests(project_id, id)
);
-- +goose Down
DROP TABLE changeset_manifest_diagnostics;
