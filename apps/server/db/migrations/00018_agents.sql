-- +goose Up
CREATE TABLE agent_definitions (
  actor_id uuid PRIMARY KEY REFERENCES actors(id),
  project_id uuid NOT NULL REFERENCES projects(id),
  name text NOT NULL CHECK (length(name) BETWEEN 1 AND 100),
  description text NOT NULL CHECK (length(description)<=1000),
  UNIQUE(project_id,name)
);
CREATE TABLE agent_sessions (
  id uuid PRIMARY KEY,
  project_id uuid NOT NULL REFERENCES projects(id),
  agent_id uuid NOT NULL REFERENCES actors(id),
  started_by uuid NOT NULL REFERENCES actors(id),
  on_behalf_of uuid REFERENCES actors(id),
  token_hash bytea NOT NULL UNIQUE,
  capabilities text[] NOT NULL,
  ops_quota int NOT NULL CHECK (ops_quota BETWEEN 1 AND 1000),
  ops_used int NOT NULL DEFAULT 0 CHECK (ops_used>=0 AND ops_used<=ops_quota),
  expires_at timestamptz NOT NULL,
  created_at timestamptz NOT NULL DEFAULT now(),
  revoked_at timestamptz,
  CHECK (expires_at<=created_at+interval '8 hours')
);
CREATE TABLE agent_session_changesets (
  session_id uuid NOT NULL REFERENCES agent_sessions(id),
  changeset_id uuid NOT NULL REFERENCES changesets(id),
  PRIMARY KEY(session_id,changeset_id)
);
ALTER TABLE operations ADD COLUMN agent_session_id uuid REFERENCES agent_sessions(id);
ALTER TABLE operations ADD CONSTRAINT agent_provenance CHECK (
 (agent_session_id IS NULL) OR (source='agent')
);
CREATE TABLE security_events (
 id uuid PRIMARY KEY,
 project_id uuid NOT NULL REFERENCES projects(id),
 actor_id uuid NOT NULL REFERENCES actors(id),
 type text NOT NULL,
 data jsonb NOT NULL,
 created_at timestamptz NOT NULL DEFAULT now()
);
CREATE TABLE agent_runs (
 id uuid PRIMARY KEY,
 project_id uuid NOT NULL REFERENCES projects(id),
 owner_id uuid NOT NULL REFERENCES actors(id),
 session_id uuid NOT NULL UNIQUE REFERENCES agent_sessions(id),
 changeset_id uuid NOT NULL REFERENCES changesets(id),
 document_id uuid NOT NULL REFERENCES objects(id),
 environment text NOT NULL,
 model text NOT NULL,
 prompt text NOT NULL CHECK (length(prompt) BETWEEN 1 AND 4000),
 state text NOT NULL CHECK (state IN ('queued','running','completed','failed','cancelled','interrupted')),
 step_seq int NOT NULL DEFAULT 0,
 error_code text,
 created_at timestamptz NOT NULL DEFAULT now(),
 updated_at timestamptz NOT NULL DEFAULT now(),
 deadline timestamptz NOT NULL,
 FOREIGN KEY(project_id,environment) REFERENCES environments(project_id,name)
);
CREATE UNIQUE INDEX agent_run_active_cs ON agent_runs(project_id,changeset_id) WHERE state IN ('queued','running');
CREATE INDEX agent_runs_owner ON agent_runs(project_id,owner_id,document_id,changeset_id,created_at DESC);
CREATE TABLE agent_steps (
 run_id uuid NOT NULL REFERENCES agent_runs(id),
 seq int NOT NULL,
 kind text NOT NULL CHECK (kind IN ('started','tool','result','message','error','finished')),
 data jsonb NOT NULL,
 created_at timestamptz NOT NULL DEFAULT now(),
 PRIMARY KEY(run_id,seq)
);
CREATE TABLE agent_usage (
 id uuid PRIMARY KEY,
 project_id uuid NOT NULL REFERENCES projects(id),
 owner_id uuid NOT NULL REFERENCES actors(id),
 run_id uuid NOT NULL REFERENCES agent_runs(id),
 reserved int NOT NULL CHECK (reserved>0),
 prompt_tokens int CHECK(prompt_tokens>=0),
 completion_tokens int CHECK(completion_tokens>=0),
 state text NOT NULL CHECK(state IN ('pending','settled','unknown')),
 created_at timestamptz NOT NULL DEFAULT now(),
 CHECK ((state='settled')=(prompt_tokens IS NOT NULL AND completion_tokens IS NOT NULL))
);
CREATE INDEX agent_usage_month ON agent_usage(project_id,created_at);

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN
 IF EXISTS(SELECT 1 FROM agent_sessions) OR EXISTS(SELECT 1 FROM agent_definitions) OR EXISTS(SELECT 1 FROM security_events) THEN
  RAISE EXCEPTION 'agent session/history data prevents downgrade';
 END IF;
END $$;
-- +goose StatementEnd
DROP TABLE agent_usage;
DROP TABLE agent_steps;
DROP TABLE agent_runs;
DROP TABLE security_events;
ALTER TABLE operations DROP CONSTRAINT agent_provenance;
ALTER TABLE operations DROP COLUMN agent_session_id;
DROP TABLE agent_session_changesets;
DROP TABLE agent_sessions;
DROP TABLE agent_definitions;
