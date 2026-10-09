-- +goose Up
-- Проверки, согласования, публикации и указатели опубликованных версий
-- (docs/spec/07-storage.md §2, 06 §5–7).

CREATE TABLE checks (
  id            uuid PRIMARY KEY,
  changeset_id  uuid NOT NULL REFERENCES changesets(id),
  content_hash  bytea NOT NULL,
  environment   text NOT NULL DEFAULT '',  -- пусто: проверка не зависит от окружения
  stage         text NOT NULL,
  status        text NOT NULL CHECK (status IN ('queued', 'running', 'passed', 'failed', 'warning', 'skipped')),
  blocking      boolean NOT NULL,
  details       jsonb NOT NULL DEFAULT '{}',
  artifacts     jsonb NOT NULL DEFAULT '[]',
  started_at    timestamptz,
  finished_at   timestamptz
);
CREATE INDEX checks_by_cs ON checks (changeset_id, content_hash);

CREATE TABLE approvals (
  id              uuid PRIMARY KEY,
  changeset_id    uuid NOT NULL REFERENCES changesets(id),
  approver_id     uuid NOT NULL REFERENCES actors(id),
  decision        text NOT NULL CHECK (decision IN ('approve', 'request_changes')),
  content_hash    bytea NOT NULL,
  environment     text NOT NULL DEFAULT '',
  comment         text,
  created_at      timestamptz NOT NULL DEFAULT now(),
  invalidated_at  timestamptz
);
CREATE INDEX approvals_by_cs ON approvals (changeset_id) WHERE invalidated_at IS NULL;

CREATE TABLE publications (
  id                     uuid PRIMARY KEY,
  project_id             uuid NOT NULL REFERENCES projects(id),
  environment_id         uuid NOT NULL REFERENCES environments(id),
  changeset_id           uuid REFERENCES changesets(id),
  kind                   text NOT NULL CHECK (kind IN ('publish', 'promote', 'rollback', 'unpublish')),
  source_publication_id  uuid REFERENCES publications(id),
  actor_id               uuid NOT NULL REFERENCES actors(id),
  reason                 text,
  created_at             timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX publications_by_env ON publications (environment_id, created_at DESC);

CREATE TABLE publication_items (
  publication_id       uuid NOT NULL REFERENCES publications(id),
  object_id            uuid NOT NULL REFERENCES objects(id),
  previous_version_id  uuid REFERENCES object_versions(id),
  current_version_id   uuid REFERENCES object_versions(id),
  PRIMARY KEY (publication_id, object_id)
);

CREATE TABLE published_pointers (
  environment_id  uuid NOT NULL REFERENCES environments(id),
  object_id       uuid NOT NULL REFERENCES objects(id),
  version_id      uuid NOT NULL REFERENCES object_versions(id),
  publication_id  uuid NOT NULL REFERENCES publications(id),
  PRIMARY KEY (environment_id, object_id)
);

-- +goose Down
DROP TABLE published_pointers;
DROP TABLE publication_items;
DROP TABLE publications;
DROP TABLE approvals;
DROP TABLE checks;
