-- +goose Up
-- Coalesced, transactional invalidations. Row lock orders committed revisions.
CREATE TABLE project_revisions (
  project_id uuid PRIMARY KEY REFERENCES projects(id),
  revision bigint NOT NULL CHECK (revision > 0)
);

CREATE TABLE presence_leases (
  project_id uuid NOT NULL REFERENCES projects(id),
  actor_id uuid NOT NULL REFERENCES actors(id),
  lease_id uuid NOT NULL,
  document_id uuid NOT NULL REFERENCES objects(id),
  changeset_id uuid NOT NULL REFERENCES changesets(id),
  environment text NOT NULL,
  expires_at timestamptz NOT NULL,
  PRIMARY KEY (project_id, actor_id, lease_id),
  FOREIGN KEY (project_id, environment) REFERENCES environments(project_id, name)
);
CREATE INDEX presence_document ON presence_leases(project_id, environment, document_id, expires_at);

-- +goose Down
-- Both tables are transport metadata, not ledger/history facts.
DROP TABLE presence_leases;
DROP TABLE project_revisions;
