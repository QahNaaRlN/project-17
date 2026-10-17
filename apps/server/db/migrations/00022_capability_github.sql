-- +goose Up
-- STU-295..314: facts are independent from engineering/IR state.
CREATE TABLE capability_github_configs (
 id uuid PRIMARY KEY, project_id uuid NOT NULL, environment_id uuid NOT NULL,
 environment text NOT NULL, config_id text NOT NULL, fingerprint text NOT NULL,
 credential_id text NOT NULL, enabled boolean NOT NULL,
 revision bigint NOT NULL DEFAULT 1 CHECK(revision>0),
 changed_by uuid NOT NULL REFERENCES actors(id), changed_at timestamptz NOT NULL DEFAULT now(),
 UNIQUE(project_id,environment), UNIQUE(project_id,id),
 FOREIGN KEY(project_id,environment_id) REFERENCES environments(project_id,id)
);
CREATE TABLE capability_github_links (
 id uuid PRIMARY KEY, project_id uuid NOT NULL, request_id uuid NOT NULL,
 config_id uuid NOT NULL, config_revision bigint NOT NULL, fingerprint text NOT NULL,
 revision bigint NOT NULL DEFAULT 1 CHECK(revision>0), generation bigint NOT NULL DEFAULT 1 CHECK(generation>0),
 active boolean NOT NULL DEFAULT true, pull_number bigint NOT NULL CHECK(pull_number>0),
 initiated_by uuid NOT NULL REFERENCES actors(id),
 state text NOT NULL DEFAULT 'queued' CHECK(state IN ('queued','sending','retry_wait','idle','stopped')),
 last_error text NOT NULL DEFAULT '', attempts int NOT NULL DEFAULT 0 CHECK(attempts BETWEEN 0 AND 3),
 polls_left int NOT NULL DEFAULT 5 CHECK(polls_left BETWEEN 0 AND 5),
 window_until timestamptz NOT NULL DEFAULT now()+interval '30 minutes', next_at timestamptz NOT NULL DEFAULT now(),
 refresh_after timestamptz NOT NULL DEFAULT now()+interval '60 seconds', lease_token uuid, lease_until timestamptz,
 sync_id uuid NOT NULL, created_at timestamptz NOT NULL DEFAULT now(), updated_at timestamptz NOT NULL DEFAULT now(),
 UNIQUE(project_id,request_id), UNIQUE(project_id,id),
 FOREIGN KEY(project_id,request_id) REFERENCES capability_requests(project_id,id),
 FOREIGN KEY(project_id,config_id) REFERENCES capability_github_configs(project_id,id)
);
CREATE INDEX capability_github_due ON capability_github_links(next_at) WHERE active AND state IN ('queued','retry_wait','sending');
CREATE TABLE capability_github_observations (
 id uuid PRIMARY KEY, project_id uuid NOT NULL, request_id uuid NOT NULL, link_id uuid NOT NULL,
 generation bigint NOT NULL, config_revision bigint NOT NULL, fingerprint text NOT NULL, sync_id uuid NOT NULL,
 body jsonb NOT NULL, digest text NOT NULL, observed_at timestamptz NOT NULL DEFAULT now(),
 fresh_until timestamptz NOT NULL DEFAULT now()+interval '15 minutes', UNIQUE(link_id,sync_id),
 FOREIGN KEY(project_id,request_id) REFERENCES capability_requests(project_id,id),
 FOREIGN KEY(project_id,link_id) REFERENCES capability_github_links(project_id,id)
);
CREATE TABLE capability_github_audit (
 id uuid PRIMARY KEY, project_id uuid NOT NULL REFERENCES projects(id), request_id uuid, actor_id uuid REFERENCES actors(id),
 event text NOT NULL, generation bigint NOT NULL DEFAULT 0, sync_id uuid, attempt int NOT NULL DEFAULT 0 CHECK(attempt BETWEEN 0 AND 3), reason text NOT NULL, created_at timestamptz NOT NULL DEFAULT now(),
 FOREIGN KEY(project_id,request_id) REFERENCES capability_requests(project_id,id)
);
CREATE INDEX capability_github_budget ON capability_github_audit(project_id,created_at) WHERE event='refresh';
CREATE TABLE capability_github_credentials (
 credential_id text PRIMARY KEY, blocked_until timestamptz NOT NULL DEFAULT now(), lease_token uuid, lease_until timestamptz
);
-- +goose Down
DROP TABLE capability_github_credentials;
DROP TABLE capability_github_audit;
DROP TABLE capability_github_observations;
DROP TABLE capability_github_links;
DROP TABLE capability_github_configs;
