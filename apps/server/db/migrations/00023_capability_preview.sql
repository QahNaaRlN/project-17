-- +goose Up
-- STU-315..334: preview/<ref> environments bound to a verified GitHub PR.
-- Archived environments keep their row and history under a tombstone name, which
-- releases the original name for re-creation (MF-032) without weakening the
-- (project_id, name) key that realtime/agent tables reference.
ALTER TABLE environments
  ADD COLUMN source_base_environment_id uuid,
  ADD COLUMN source_config_id uuid,
  ADD COLUMN source_repository_id text,
  ADD COLUMN source_pull_number bigint CHECK (source_pull_number > 0),
  ADD COLUMN registration_seq int NOT NULL DEFAULT 0 CHECK (registration_seq >= 0),
  ADD COLUMN last_registered_at timestamptz,
  ADD COLUMN archived_at timestamptz,
  ADD COLUMN archived_name text,
  ADD COLUMN archive_reason text CHECK (archive_reason IN ('deleted', 'ttl', 'pr_closed', 'pr_merged')),
  ADD CONSTRAINT environments_source_complete CHECK (num_nonnulls(source_base_environment_id, source_config_id, source_repository_id, source_pull_number) IN (0, 4)),
  ADD CONSTRAINT environments_source_preview CHECK (source_base_environment_id IS NULL OR kind = 'preview'),
  ADD CONSTRAINT environments_archive_preview CHECK (archived_at IS NULL OR kind = 'preview'),
  ADD CONSTRAINT environments_archive_complete CHECK (num_nonnulls(archived_at, archived_name, archive_reason) IN (0, 3)),
  ADD CONSTRAINT environments_source_base FOREIGN KEY (project_id, source_base_environment_id) REFERENCES environments(project_id, id),
  ADD CONSTRAINT environments_source_config FOREIGN KEY (project_id, source_config_id) REFERENCES capability_github_configs(project_id, id);
CREATE UNIQUE INDEX environments_active_source ON environments(project_id, source_base_environment_id, source_repository_id, source_pull_number)
  WHERE archived_at IS NULL AND source_base_environment_id IS NOT NULL;
CREATE INDEX environments_preview_expiry ON environments(expires_at) WHERE kind = 'preview' AND archived_at IS NULL;
-- MF-032 applies to every ephemeral environment; existing ones get a full TTL window.
UPDATE environments SET expires_at = now() + interval '14 days' WHERE kind = 'preview' AND expires_at IS NULL;

ALTER TABLE presence_leases DROP CONSTRAINT presence_leases_project_id_environment_fkey,
  ADD CONSTRAINT presence_leases_project_id_environment_fkey FOREIGN KEY (project_id, environment) REFERENCES environments(project_id, name) ON UPDATE CASCADE;
ALTER TABLE agent_runs DROP CONSTRAINT agent_runs_project_id_environment_fkey,
  ADD CONSTRAINT agent_runs_project_id_environment_fkey FOREIGN KEY (project_id, environment) REFERENCES environments(project_id, name) ON UPDATE CASCADE;

CREATE TABLE preview_registrations (
  id uuid PRIMARY KEY,
  project_id uuid NOT NULL,
  environment_id uuid NOT NULL,
  seq int NOT NULL CHECK (seq > 0),
  manifest_id uuid NOT NULL,
  commit text NOT NULL CHECK (commit ~ '^([0-9a-f]{40}|[0-9a-f]{64})$'),
  app_url text,
  registered_by uuid NOT NULL REFERENCES actors(id),
  registered_at timestamptz NOT NULL DEFAULT now(),
  UNIQUE (environment_id, seq),
  FOREIGN KEY (project_id, environment_id) REFERENCES environments(project_id, id),
  FOREIGN KEY (project_id, manifest_id) REFERENCES manifests(project_id, id)
);

CREATE TABLE capability_preview_args (
  project_id uuid NOT NULL,
  request_id uuid NOT NULL,
  environment_id uuid NOT NULL,
  manifest_id uuid NOT NULL,
  args jsonb NOT NULL,
  revision bigint NOT NULL DEFAULT 1 CHECK (revision > 0),
  set_by uuid NOT NULL REFERENCES actors(id),
  set_at timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (project_id, request_id, environment_id),
  FOREIGN KEY (project_id, request_id) REFERENCES capability_requests(project_id, id),
  FOREIGN KEY (project_id, environment_id) REFERENCES environments(project_id, id),
  FOREIGN KEY (project_id, manifest_id) REFERENCES manifests(project_id, id)
);

CREATE TABLE preview_environment_audit (
  id uuid PRIMARY KEY,
  project_id uuid NOT NULL,
  environment_id uuid NOT NULL,
  actor_id uuid REFERENCES actors(id),
  event text NOT NULL,
  reason text NOT NULL,
  data jsonb NOT NULL DEFAULT '{}',
  created_at timestamptz NOT NULL DEFAULT now(),
  FOREIGN KEY (project_id, environment_id) REFERENCES environments(project_id, id)
);

-- +goose Down
DROP TABLE preview_environment_audit;
DROP TABLE capability_preview_args;
DROP TABLE preview_registrations;
ALTER TABLE agent_runs DROP CONSTRAINT agent_runs_project_id_environment_fkey,
  ADD CONSTRAINT agent_runs_project_id_environment_fkey FOREIGN KEY (project_id, environment) REFERENCES environments(project_id, name);
ALTER TABLE presence_leases DROP CONSTRAINT presence_leases_project_id_environment_fkey,
  ADD CONSTRAINT presence_leases_project_id_environment_fkey FOREIGN KEY (project_id, environment) REFERENCES environments(project_id, name);
DROP INDEX environments_preview_expiry;
DROP INDEX environments_active_source;
ALTER TABLE environments
  DROP CONSTRAINT environments_source_config,
  DROP CONSTRAINT environments_source_base,
  DROP CONSTRAINT environments_archive_complete,
  DROP CONSTRAINT environments_archive_preview,
  DROP CONSTRAINT environments_source_preview,
  DROP CONSTRAINT environments_source_complete,
  DROP COLUMN archive_reason,
  DROP COLUMN archived_name,
  DROP COLUMN archived_at,
  DROP COLUMN last_registered_at,
  DROP COLUMN registration_seq,
  DROP COLUMN source_pull_number,
  DROP COLUMN source_repository_id,
  DROP COLUMN source_config_id,
  DROP COLUMN source_base_environment_id;
