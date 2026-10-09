-- +goose Up
-- Версионируемые объекты, Change Set и журнал операций (docs/spec/07-storage.md §2, 06 §1–3).

CREATE TABLE objects (
  id               uuid PRIMARY KEY,
  project_id       uuid NOT NULL REFERENCES projects(id),
  kind             text NOT NULL CHECK (kind IN ('entity', 'document', 'asset')),
  schema_name      text,
  doc_kind         text CHECK (doc_kind IN ('page', 'component')),
  head_version_id  uuid,  -- NULL, пока объект существует только в открытом Change Set
  head_path        text,
  created_at       timestamptz NOT NULL DEFAULT now(),
  deleted_at       timestamptz,
  CHECK ((kind = 'entity') = (schema_name IS NOT NULL)),
  CHECK ((kind = 'document') = (doc_kind IS NOT NULL))
);
CREATE UNIQUE INDEX objects_head_path ON objects (project_id, head_path)
  WHERE head_path IS NOT NULL AND deleted_at IS NULL;

CREATE TABLE changesets (
  id            uuid PRIMARY KEY,
  project_id    uuid NOT NULL REFERENCES projects(id),
  kind          text NOT NULL CHECK (kind IN ('standard', 'schema', 'migration', 'rollback')),
  title         text NOT NULL CHECK (length(title) BETWEEN 1 AND 200),
  description   text,
  owner_id      uuid NOT NULL REFERENCES actors(id),
  state         text NOT NULL CHECK (state IN ('open', 'checking', 'failed', 'in_review',
                                                'changes_requested', 'approved', 'merged', 'abandoned')),
  risk          text CHECK (risk IN ('low', 'medium', 'high')),
  targets       text[] NOT NULL DEFAULT '{}',
  seq           int  NOT NULL DEFAULT 0,
  content_hash  bytea,
  has_conflicts boolean NOT NULL DEFAULT false,
  created_at    timestamptz NOT NULL DEFAULT now(),
  updated_at    timestamptz NOT NULL DEFAULT now(),
  submitted_at  timestamptz,
  merged_at     timestamptz
);
CREATE INDEX changesets_open ON changesets (project_id, state) WHERE state NOT IN ('merged', 'abandoned');

CREATE TABLE object_versions (
  id                 uuid PRIMARY KEY,
  project_id         uuid NOT NULL REFERENCES projects(id),
  object_id          uuid NOT NULL REFERENCES objects(id),
  number             int,
  state              text NOT NULL CHECK (state IN ('working', 'committed')),
  changeset_id       uuid REFERENCES changesets(id),
  parent_version_id  uuid REFERENCES object_versions(id),
  schema_version     int,
  ir_version         text,
  path               text,
  body               jsonb NOT NULL,
  body_hash          bytea NOT NULL,
  created_by         uuid NOT NULL REFERENCES actors(id),
  created_at         timestamptz NOT NULL DEFAULT now(),
  committed_at       timestamptz,
  UNIQUE (object_id, number),
  CHECK ((state = 'committed') = (number IS NOT NULL))
);
CREATE UNIQUE INDEX object_versions_one_working ON object_versions (changeset_id, object_id)
  WHERE state = 'working';
ALTER TABLE objects ADD FOREIGN KEY (head_version_id) REFERENCES object_versions(id);

CREATE TABLE changeset_objects (
  changeset_id        uuid NOT NULL REFERENCES changesets(id),
  object_id           uuid NOT NULL REFERENCES objects(id),
  base_version_id     uuid REFERENCES object_versions(id),  -- NULL для объектов, созданных в CS
  working_version_id  uuid NOT NULL REFERENCES object_versions(id),
  PRIMARY KEY (changeset_id, object_id)
);
CREATE INDEX changeset_objects_by_object ON changeset_objects (object_id);

CREATE TABLE operations (
  id               uuid PRIMARY KEY,
  project_id       uuid NOT NULL REFERENCES projects(id),
  changeset_id     uuid NOT NULL REFERENCES changesets(id),
  seq              int  NOT NULL,
  actor_id         uuid NOT NULL REFERENCES actors(id),
  on_behalf_of     uuid REFERENCES actors(id),
  source           text NOT NULL CHECK (source IN ('studio', 'api', 'agent', 'migration', 'import')),
  target_object_id uuid NOT NULL REFERENCES objects(id),
  type             text NOT NULL,
  payload          jsonb NOT NULL,
  before           jsonb,
  after            jsonb,
  inverse          jsonb,  -- обратная операция {type, payload}; NULL — отмена не поддерживается
  reason           text,
  client_op_id     text,
  undo_of          uuid REFERENCES operations(id),
  status           text NOT NULL DEFAULT 'applied' CHECK (status IN ('applied', 'conflict', 'dropped')),
  created_at       timestamptz NOT NULL DEFAULT now(),
  UNIQUE (changeset_id, seq),
  UNIQUE (changeset_id, actor_id, client_op_id)
);
CREATE INDEX operations_by_target ON operations (target_object_id, created_at);
CREATE UNIQUE INDEX operations_undone_once ON operations (undo_of) WHERE undo_of IS NOT NULL;

-- +goose Down
DROP TABLE operations;
DROP TABLE changeset_objects;
ALTER TABLE objects DROP CONSTRAINT objects_head_version_id_fkey;
DROP TABLE object_versions;
DROP TABLE changesets;
DROP TABLE objects;
