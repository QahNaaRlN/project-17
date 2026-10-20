-- +goose Up
-- STU-335..354: requiredBy anchor groups and automatic fulfillment.

-- STU-345: one token-less system actor per project performs replacements.
ALTER TABLE actors ADD COLUMN system boolean NOT NULL DEFAULT false,
  ADD CONSTRAINT actors_system_service CHECK (NOT system OR kind = 'service');
CREATE UNIQUE INDEX actors_project_system ON actors(project_id) WHERE system;
INSERT INTO actors (id, kind, project_id, display_name, system)
SELECT gen_random_uuid(), 'service', id, 'cms-system', true FROM projects;

-- STU-335: anchors of a request; the original anchor migrates 1:1.
CREATE TABLE capability_request_anchors (
  id uuid PRIMARY KEY,
  project_id uuid NOT NULL,
  request_id uuid NOT NULL,
  changeset_id uuid NOT NULL,
  document_id uuid NOT NULL,
  node_id text NOT NULL,
  event text NOT NULL,
  owner_id uuid NOT NULL REFERENCES actors(id),
  state text NOT NULL DEFAULT 'active' CHECK (state IN ('active', 'queued', 'replaced', 'awaiting_args', 'invalid_args', 'anchor_changed', 'changeset_in_review', 'changeset_closed', 'reverted', 'detached', 'failed')),
  revision bigint NOT NULL DEFAULT 1 CHECK (revision > 0),
  fulfillment_args jsonb,
  attempts int NOT NULL DEFAULT 0 CHECK (attempts >= 0),
  next_at timestamptz NOT NULL DEFAULT now(),
  last_error text NOT NULL DEFAULT '',
  replaced_batch_id uuid,
  original boolean NOT NULL DEFAULT false,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  UNIQUE (project_id, id),
  FOREIGN KEY (project_id, request_id) REFERENCES capability_requests(project_id, id),
  FOREIGN KEY (project_id, changeset_id) REFERENCES changesets(project_id, id),
  FOREIGN KEY (project_id, document_id) REFERENCES objects(project_id, id)
);
CREATE UNIQUE INDEX capability_anchor_place ON capability_request_anchors(project_id, changeset_id, document_id, node_id, event)
  WHERE state NOT IN ('detached', 'changeset_closed');
CREATE INDEX capability_anchor_request ON capability_request_anchors(project_id, request_id, created_at);
CREATE INDEX capability_anchor_due ON capability_request_anchors(next_at) WHERE state IN ('queued', 'changeset_in_review');
INSERT INTO capability_request_anchors (id, project_id, request_id, changeset_id, document_id, node_id, event, owner_id, state, original, created_at)
SELECT gen_random_uuid(), project_id, id, changeset_id, document_id, node_id, event, owner_id,
  CASE WHEN status IN ('fulfilled', 'rejected', 'cancelled') THEN 'detached' ELSE 'active' END, true, created_at
FROM capability_requests;

-- STU-339/341: the immutable basis of an automatic fulfillment.
ALTER TABLE capability_requests
  ADD COLUMN fulfillment_state text NOT NULL DEFAULT 'waiting' CHECK (fulfillment_state IN ('waiting', 'fulfilled')),
  ADD COLUMN fulfillment_reason text NOT NULL DEFAULT '',
  ADD COLUMN fulfillment_mode text CHECK (fulfillment_mode IN ('manual', 'automatic')),
  ADD COLUMN fulfillment_commit text,
  ADD COLUMN fulfillment_observation_id uuid,
  ADD COLUMN fulfillment_basis jsonb,
  ADD COLUMN fulfilled_at timestamptz;
UPDATE capability_requests SET fulfillment_state = 'fulfilled', fulfillment_mode = 'manual', fulfilled_at = updated_at WHERE status = 'fulfilled';

-- STU-338: activation outbox (project, environment, manifest); requests may be
-- re-evaluated on a merged observation or an explicit check.
CREATE TABLE capability_fulfillment_jobs (
  id uuid PRIMARY KEY,
  project_id uuid NOT NULL,
  environment_id uuid NOT NULL,
  manifest_id uuid NOT NULL,
  request_id uuid,
  state text NOT NULL DEFAULT 'queued' CHECK (state IN ('queued', 'running', 'done')),
  attempts int NOT NULL DEFAULT 0 CHECK (attempts >= 0),
  next_at timestamptz NOT NULL DEFAULT now(),
  lease_until timestamptz,
  last_error text NOT NULL DEFAULT '',
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  FOREIGN KEY (project_id, environment_id) REFERENCES environments(project_id, id),
  FOREIGN KEY (project_id, manifest_id) REFERENCES manifests(project_id, id),
  FOREIGN KEY (project_id, request_id) REFERENCES capability_requests(project_id, id)
);
CREATE UNIQUE INDEX capability_fulfillment_activation ON capability_fulfillment_jobs(project_id, environment_id, manifest_id) WHERE request_id IS NULL;
CREATE INDEX capability_fulfillment_due ON capability_fulfillment_jobs(next_at) WHERE state <> 'done';

CREATE TABLE capability_fulfillment_audit (
  id uuid PRIMARY KEY,
  project_id uuid NOT NULL,
  request_id uuid NOT NULL,
  anchor_id uuid,
  actor_id uuid REFERENCES actors(id),
  event text NOT NULL,
  reason text NOT NULL,
  data jsonb NOT NULL DEFAULT '{}',
  created_at timestamptz NOT NULL DEFAULT now(),
  FOREIGN KEY (project_id, request_id) REFERENCES capability_requests(project_id, id)
);

-- +goose Down
DROP TABLE capability_fulfillment_audit;
DROP TABLE capability_fulfillment_jobs;
ALTER TABLE capability_requests
  DROP COLUMN fulfilled_at,
  DROP COLUMN fulfillment_basis,
  DROP COLUMN fulfillment_observation_id,
  DROP COLUMN fulfillment_commit,
  DROP COLUMN fulfillment_mode,
  DROP COLUMN fulfillment_reason,
  DROP COLUMN fulfillment_state;
DROP TABLE capability_request_anchors;
-- System actors stay as ordinary service actors: history references them.
DROP INDEX actors_project_system;
ALTER TABLE actors DROP CONSTRAINT actors_system_service, DROP COLUMN system;
