-- +goose Up
-- Проекты, окружения, акторы, роли, токены, идемпотентность команд (docs/spec/07-storage.md §2).

CREATE TABLE projects (
  id          uuid PRIMARY KEY,
  slug        text NOT NULL UNIQUE CHECK (slug ~ '^[a-z0-9-]{2,64}$'),
  name        text NOT NULL CHECK (length(name) BETWEEN 1 AND 200),
  settings    jsonb NOT NULL DEFAULT '{}',
  created_at  timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE environments (
  id                  uuid PRIMARY KEY,
  project_id          uuid NOT NULL REFERENCES projects(id),
  name                text NOT NULL CHECK (name ~ '^[a-z0-9][a-z0-9/_-]{0,62}$'),
  kind                text NOT NULL CHECK (kind IN ('standard', 'preview')),
  active_manifest_id  uuid,  -- внешний ключ на manifests появится вместе с таблицей
  app_url             text,
  expires_at          timestamptz,
  created_at          timestamptz NOT NULL DEFAULT now(),
  UNIQUE (project_id, name)
);

CREATE TABLE actors (
  id            uuid PRIMARY KEY,
  kind          text NOT NULL CHECK (kind IN ('human', 'agent', 'service', 'migration')),
  project_id    uuid REFERENCES projects(id),
  display_name  text NOT NULL,
  oidc_subject  text UNIQUE,
  email         text,
  disabled_at   timestamptz,
  created_at    timestamptz NOT NULL DEFAULT now(),
  CHECK ((kind = 'human') = (project_id IS NULL))
);

CREATE TABLE roles (
  id            uuid PRIMARY KEY,
  project_id    uuid NOT NULL REFERENCES projects(id),
  name          text NOT NULL,
  capabilities  text[] NOT NULL,
  UNIQUE (project_id, name)
);

CREATE TABLE role_bindings (
  project_id  uuid NOT NULL REFERENCES projects(id),
  actor_id    uuid NOT NULL REFERENCES actors(id),
  role_id     uuid NOT NULL REFERENCES roles(id),
  PRIMARY KEY (project_id, actor_id, role_id)
);

CREATE TABLE api_tokens (
  id            uuid PRIMARY KEY,
  project_id    uuid NOT NULL REFERENCES projects(id),
  actor_id      uuid NOT NULL REFERENCES actors(id),
  token_hash    bytea NOT NULL UNIQUE,
  scopes        text[] NOT NULL,
  environments  text[],
  expires_at    timestamptz NOT NULL,
  last_used_at  timestamptz,
  revoked_at    timestamptz,
  created_at    timestamptz NOT NULL DEFAULT now()
);

-- Идемпотентность команд (API-010). Ключ действует 24 ч; просроченные записи удаляет
-- фоновая задача (до её появления — при следующей записи с тем же ключом).
CREATE TABLE idempotency_keys (
  actor_id      uuid NOT NULL REFERENCES actors(id),
  key           text NOT NULL CHECK (length(key) BETWEEN 1 AND 200),
  command       text NOT NULL,
  request_hash  bytea NOT NULL,
  status        int  NOT NULL,
  response      bytea NOT NULL,  -- байты ответа как есть: повтор возвращает идентичное тело
  created_at    timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (actor_id, key)
);

-- +goose Down
DROP TABLE idempotency_keys;
DROP TABLE api_tokens;
DROP TABLE role_bindings;
DROP TABLE roles;
DROP TABLE actors;
DROP TABLE environments;
DROP TABLE projects;
