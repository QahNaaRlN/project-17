-- +goose Up
-- Публичные ключи доставки окружения cms_pub_… (docs/spec/08-api.md §2, §5): только чтение
-- опубликованного через Delivery API. Хранится SHA-256 секрета.

CREATE TABLE delivery_keys (
  id              uuid PRIMARY KEY,
  project_id      uuid NOT NULL REFERENCES projects(id),
  environment_id  uuid NOT NULL REFERENCES environments(id),
  name            text NOT NULL CHECK (length(name) BETWEEN 1 AND 100),
  token_hash      bytea NOT NULL UNIQUE,
  created_by      uuid NOT NULL REFERENCES actors(id),
  created_at      timestamptz NOT NULL DEFAULT now(),
  revoked_at      timestamptz
);
CREATE INDEX delivery_keys_by_project ON delivery_keys (project_id, created_at);

-- +goose Down
DROP TABLE delivery_keys;
