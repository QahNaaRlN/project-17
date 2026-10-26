-- +goose Up
-- STU-480…499: asynchronous render checks (pipeline stages 8–9) leased by the check runner.
ALTER TABLE checks ADD COLUMN created_at timestamptz NOT NULL DEFAULT now();
ALTER TABLE checks ADD COLUMN lease_id uuid;
ALTER TABLE checks ADD COLUMN lease_expires_at timestamptz;
ALTER TABLE checks ADD COLUMN attempts int NOT NULL DEFAULT 0;
CREATE INDEX checks_queue ON checks (created_at, id) WHERE status = 'queued';
CREATE INDEX checks_leases ON checks (lease_expires_at) WHERE status = 'running';
-- Stages 8–9 apply to a project without a row. No command writes it; command test
-- projects turn the stages off (cmstest). A separate table keeps projects unchanged.
CREATE TABLE project_render_checks (
  project_id uuid PRIMARY KEY REFERENCES projects(id),
  enabled boolean NOT NULL
);

-- +goose Down
DROP TABLE project_render_checks;
DROP INDEX checks_leases;
DROP INDEX checks_queue;
ALTER TABLE checks DROP COLUMN attempts;
ALTER TABLE checks DROP COLUMN lease_expires_at;
ALTER TABLE checks DROP COLUMN lease_id;
ALTER TABLE checks DROP COLUMN created_at;
