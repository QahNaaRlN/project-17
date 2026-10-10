# 07. Хранилище данных

## 1. Принципы

| ID | Требование |
|---|---|
| ST-001 | PostgreSQL 16+ — источник истины. Redis и объектное хранилище содержат только производные данные или бинарные файлы, восстановимые или адресуемые из БД. |
| ST-002 | Каждая таблица с данными проекта ДОЛЖНА содержать `project_id`; каждый SQL-запрос (sqlc) ДОЛЖЕН фильтровать по нему. Включение Row-Level Security — после MVP. |
| ST-003 | Тела сущностей и документов IR хранятся в JSONB; идентичность, версии, указатели, ссылки, права, workflow и публикации — в реляционных таблицах. |
| ST-004 | Миграции схемы БД — goose, только вперёд; каждая миграция совместима с предыдущей версией приложения (expand → migrate → contract). |

**[Решение]** Таблицы `entities`, `compositions`, `components` и их `*_versions` из v0.1 объединены в `objects` / `object_versions` с полем `kind`. Указатели, Change Set, ссылки и публикации работают одинаково для всех видов объектов.

**[Решение]** Рабочая версия объекта в Change Set — одна изменяемая строка на пару (CS, объект), пересчитываемая каждой операцией. Неизменяемая история изменений — это операции; версия становится неизменяемой при фиксации (`state = 'committed'`) и только тогда получает номер.

## 2. DDL (основные таблицы)

```sql
-- Проекты и окружения ----------------------------------------------------
CREATE TABLE projects (
  id          uuid PRIMARY KEY,
  slug        text NOT NULL UNIQUE CHECK (slug ~ '^[a-z0-9-]{2,64}$'),
  name        text NOT NULL,
  settings    jsonb NOT NULL DEFAULT '{}',   -- locales, defaultLocale, defaultMode, maxMode, allowRawColors, limits
  created_at  timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE environments (
  id                  uuid PRIMARY KEY,
  project_id          uuid NOT NULL REFERENCES projects(id),
  name                text NOT NULL,                        -- staging, production, preview/feature-x
  kind                text NOT NULL CHECK (kind IN ('standard', 'preview')),
  active_manifest_id  uuid,
  app_url             text,                                  -- базовый URL приложения (preview, SSR-проверки)
  expires_at          timestamptz,                           -- для preview
  created_at          timestamptz NOT NULL DEFAULT now(),
  UNIQUE (project_id, name)
);

-- Акторы, роли, политики, токены -----------------------------------------
CREATE TABLE actors (
  id             uuid PRIMARY KEY,
  kind           text NOT NULL CHECK (kind IN ('human', 'agent', 'service', 'migration')),
  project_id     uuid REFERENCES projects(id),             -- NULL для людей (глобальные учётные записи)
  display_name   text NOT NULL,
  oidc_subject   text UNIQUE,
  email          text,
  disabled_at    timestamptz,
  created_at     timestamptz NOT NULL DEFAULT now(),
  CHECK ((kind = 'human') = (project_id IS NULL))
);

CREATE TABLE roles (
  id            uuid PRIMARY KEY,
  project_id    uuid NOT NULL REFERENCES projects(id),
  name          text NOT NULL,
  capabilities  text[] NOT NULL,                             -- права: content.write, design.compose …
  UNIQUE (project_id, name)
);

CREATE TABLE role_bindings (
  project_id  uuid NOT NULL REFERENCES projects(id),
  actor_id    uuid NOT NULL REFERENCES actors(id),
  role_id     uuid NOT NULL REFERENCES roles(id),
  PRIMARY KEY (project_id, actor_id, role_id)
);

CREATE TABLE policies (
  id          uuid PRIMARY KEY,
  project_id  uuid NOT NULL REFERENCES projects(id),
  version     int  NOT NULL,
  body        jsonb NOT NULL,                                -- см. 09-agent.md §2.3
  active      boolean NOT NULL DEFAULT false,
  created_by  uuid NOT NULL REFERENCES actors(id),
  created_at  timestamptz NOT NULL DEFAULT now(),
  UNIQUE (project_id, version)
);
CREATE UNIQUE INDEX policies_one_active ON policies (project_id) WHERE active;

CREATE TABLE delivery_keys (                                -- ключи доставки cms_pub_… (08 §2)
  id              uuid PRIMARY KEY,
  project_id      uuid NOT NULL REFERENCES projects(id),
  environment_id  uuid NOT NULL REFERENCES environments(id),
  name            text NOT NULL,
  token_hash      bytea NOT NULL UNIQUE,                     -- SHA-256 от секрета
  created_by      uuid NOT NULL REFERENCES actors(id),
  created_at      timestamptz NOT NULL DEFAULT now(),
  revoked_at      timestamptz
);

CREATE TABLE api_tokens (
  id            uuid PRIMARY KEY,
  project_id    uuid NOT NULL REFERENCES projects(id),
  actor_id      uuid NOT NULL REFERENCES actors(id),
  token_hash    bytea NOT NULL UNIQUE,                       -- SHA-256 от секрета
  scopes        text[] NOT NULL,                             -- подмножество прав роли
  environments  text[],                                      -- NULL = все
  expires_at    timestamptz NOT NULL,
  last_used_at  timestamptz,
  revoked_at    timestamptz,
  created_at    timestamptz NOT NULL DEFAULT now()
);

-- Идемпотентность команд (API-010; см. §4) --------------------------------
CREATE TABLE idempotency_keys (
  actor_id      uuid NOT NULL REFERENCES actors(id),
  key           text NOT NULL CHECK (length(key) BETWEEN 1 AND 200),
  command       text NOT NULL,
  request_hash  bytea NOT NULL,                              -- SHA-256 имени команды и компактного payload
  status        int  NOT NULL,
  response      bytea NOT NULL,                              -- тело ответа байт-в-байт
  created_at    timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (actor_id, key)
);

-- Manifest и схемы -------------------------------------------------------
CREATE TABLE manifests (
  id              uuid PRIMARY KEY,
  project_id      uuid NOT NULL REFERENCES projects(id),
  hash            text NOT NULL,
  app_version     text NOT NULL,
  body            jsonb NOT NULL,
  code_index_key  text,                                      -- ключ в объектном хранилище
  registered_by   uuid NOT NULL REFERENCES actors(id),
  created_at      timestamptz NOT NULL DEFAULT now(),
  UNIQUE (project_id, hash)
);
ALTER TABLE environments ADD FOREIGN KEY (active_manifest_id) REFERENCES manifests(id);

CREATE TABLE schema_versions (
  project_id   uuid NOT NULL REFERENCES projects(id),
  schema_name  text NOT NULL,
  version      int  NOT NULL,
  body         jsonb NOT NULL,
  manifest_id  uuid NOT NULL REFERENCES manifests(id),
  created_at   timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (project_id, schema_name, version)
);

-- Версионируемые объекты -------------------------------------------------
CREATE TABLE objects (
  id               uuid PRIMARY KEY,
  project_id       uuid NOT NULL REFERENCES projects(id),
  kind             text NOT NULL CHECK (kind IN ('entity', 'document', 'asset')),
  schema_name      text,                                     -- для entity
  doc_kind         text CHECK (doc_kind IN ('page', 'component')),
  head_version_id  uuid,
  head_path        text,                                     -- маршрут страницы в head
  created_at       timestamptz NOT NULL DEFAULT now(),
  deleted_at       timestamptz,
  CHECK ((kind = 'entity') = (schema_name IS NOT NULL)),
  CHECK ((kind = 'document') = (doc_kind IS NOT NULL))
);
-- Форма маршрута: параметры заменены на ':' — '/a/:x' и '/a/:y' конфликтуют.
CREATE FUNCTION route_shape(path text) RETURNS text LANGUAGE sql IMMUTABLE STRICT
  AS $$ SELECT regexp_replace(path, ':[^/]+', ':', 'g') $$;
CREATE UNIQUE INDEX objects_head_route ON objects (project_id, route_shape(head_path))
  WHERE head_path IS NOT NULL AND deleted_at IS NULL;
CREATE INDEX objects_by_schema ON objects (project_id, schema_name) WHERE kind = 'entity';

CREATE TABLE object_versions (
  id                 uuid PRIMARY KEY,
  project_id         uuid NOT NULL,
  object_id          uuid NOT NULL REFERENCES objects(id),
  number             int,                                    -- NULL для рабочей версии
  state              text NOT NULL CHECK (state IN ('working', 'committed')),
  changeset_id       uuid,                                   -- CS, создавший версию
  parent_version_id  uuid REFERENCES object_versions(id),
  schema_version     int,                                    -- для entity
  ir_version         text,                                   -- для document
  path               text,                                   -- маршрут страницы в этой версии
  body               jsonb NOT NULL,
  body_hash          bytea NOT NULL,                         -- SHA-256 канонического JSON
  created_by         uuid NOT NULL REFERENCES actors(id),
  created_at         timestamptz NOT NULL DEFAULT now(),
  committed_at       timestamptz,
  UNIQUE (object_id, number),
  CHECK ((state = 'committed') = (number IS NOT NULL))
);
CREATE UNIQUE INDEX object_versions_one_working ON object_versions (changeset_id, object_id)
  WHERE state = 'working';
ALTER TABLE objects ADD FOREIGN KEY (head_version_id) REFERENCES object_versions(id);

-- Исходящие ссылки версии: целостность и анализ влияния
CREATE TABLE version_references (
  from_version_id  uuid NOT NULL REFERENCES object_versions(id) ON DELETE CASCADE,
  to_object_id     uuid NOT NULL REFERENCES objects(id),
  ref_kind         text NOT NULL CHECK (ref_kind IN ('entity', 'asset', 'component', 'page_link')),
  pointer          text NOT NULL,                            -- JSON Pointer места ссылки
  PRIMARY KEY (from_version_id, pointer)
);
CREATE INDEX version_references_to ON version_references (to_object_id);

-- Использование элементов manifest документами (для MF-020)
CREATE TABLE version_manifest_usage (
  from_version_id  uuid NOT NULL REFERENCES object_versions(id) ON DELETE CASCADE,
  element_kind     text NOT NULL,                            -- component, action, dataSource, token, formatter
  element_name     text NOT NULL,
  PRIMARY KEY (from_version_id, element_kind, element_name)
);
CREATE INDEX version_manifest_usage_el ON version_manifest_usage (element_kind, element_name);

-- Уникальные значения полей среди head-версий (CNT-003)
CREATE TABLE unique_values (
  project_id   uuid NOT NULL,
  schema_name  text NOT NULL,
  field        text NOT NULL,
  locale       text NOT NULL DEFAULT '',
  value_hash   bytea NOT NULL,
  object_id    uuid NOT NULL REFERENCES objects(id),
  PRIMARY KEY (project_id, schema_name, field, locale, value_hash)
);

-- Change Set и операции --------------------------------------------------
CREATE TABLE changesets (
  id            uuid PRIMARY KEY,
  project_id    uuid NOT NULL REFERENCES projects(id),
  kind          text NOT NULL CHECK (kind IN ('standard', 'schema', 'migration', 'rollback')),
  title         text NOT NULL,
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

CREATE TABLE changeset_collaborators (
  changeset_id  uuid NOT NULL REFERENCES changesets(id),
  actor_id      uuid NOT NULL REFERENCES actors(id),
  PRIMARY KEY (changeset_id, actor_id)
);

CREATE TABLE changeset_objects (
  changeset_id        uuid NOT NULL REFERENCES changesets(id),
  object_id           uuid NOT NULL REFERENCES objects(id),
  base_version_id     uuid REFERENCES object_versions(id),   -- NULL для созданных в CS
  working_version_id  uuid NOT NULL REFERENCES object_versions(id),
  PRIMARY KEY (changeset_id, object_id)
);
CREATE INDEX changeset_objects_by_object ON changeset_objects (object_id);

CREATE TABLE operations (
  id               uuid PRIMARY KEY,
  project_id       uuid NOT NULL,
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
  inverse          jsonb,                                    -- обратная операция {type, payload}; NULL — отмена не поддерживается
  reason           text,
  client_op_id     text,
  undo_of          uuid REFERENCES operations(id),
  status           text NOT NULL DEFAULT 'applied' CHECK (status IN ('applied', 'conflict', 'dropped')),
  created_at       timestamptz NOT NULL DEFAULT now(),
  updated_at       timestamptz NOT NULL DEFAULT now(),
  UNIQUE (changeset_id, seq),
  UNIQUE (changeset_id, actor_id, client_op_id)
);
CREATE INDEX operations_by_target ON operations (target_object_id, created_at);
CREATE INDEX operations_created_brin ON operations USING brin (created_at);

-- Проверки и согласования ------------------------------------------------
CREATE TABLE checks (
  id            uuid PRIMARY KEY,
  changeset_id  uuid NOT NULL REFERENCES changesets(id),
  content_hash  bytea NOT NULL,
  environment   text NOT NULL DEFAULT '',                    -- пусто: проверка не зависит от окружения
  stage         text NOT NULL,                               -- schema, ir, bindings, a11y_static, policy, …
  status        text NOT NULL CHECK (status IN ('queued', 'running', 'passed', 'failed', 'warning', 'skipped')),
  blocking      boolean NOT NULL,
  details       jsonb NOT NULL DEFAULT '{}',                 -- диагностика
  artifacts     jsonb NOT NULL DEFAULT '[]',                 -- ключи объектного хранилища
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

-- Публикации -------------------------------------------------------------
CREATE TABLE publications (
  id                     uuid PRIMARY KEY,
  project_id             uuid NOT NULL REFERENCES projects(id),
  environment_id         uuid NOT NULL REFERENCES environments(id),
  changeset_id           uuid REFERENCES changesets(id),
  kind                   text NOT NULL CHECK (kind IN ('publish', 'promote', 'rollback', 'unpublish')),
  source_publication_id  uuid REFERENCES publications(id),   -- для promote и rollback
  actor_id               uuid NOT NULL REFERENCES actors(id),
  reason                 text,                                -- причина из конверта команды (API-011)
  created_at             timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE publication_items (
  publication_id       uuid NOT NULL REFERENCES publications(id),
  object_id            uuid NOT NULL REFERENCES objects(id),
  previous_version_id  uuid REFERENCES object_versions(id),  -- NULL: объект не был опубликован
  current_version_id   uuid REFERENCES object_versions(id),  -- NULL: снят с публикации
  PRIMARY KEY (publication_id, object_id)
);

CREATE TABLE published_pointers (
  environment_id  uuid NOT NULL REFERENCES environments(id),
  object_id       uuid NOT NULL REFERENCES objects(id),
  version_id      uuid NOT NULL REFERENCES object_versions(id),
  publication_id  uuid NOT NULL REFERENCES publications(id),
  PRIMARY KEY (environment_id, object_id)
);

CREATE TABLE routes (
  environment_id  uuid NOT NULL REFERENCES environments(id),
  path            text NOT NULL,                             -- '/products/:slug'
  object_id       uuid NOT NULL REFERENCES objects(id),
  PRIMARY KEY (environment_id, path)
);
CREATE UNIQUE INDEX routes_shape ON routes (environment_id, route_shape(path));
CREATE UNIQUE INDEX routes_object ON routes (environment_id, object_id);  -- маршрут опубликованной версии

-- Ассеты -----------------------------------------------------------------
CREATE TABLE asset_files (
  project_id   uuid NOT NULL REFERENCES projects(id),
  sha256       bytea NOT NULL,
  storage_key  text NOT NULL,
  mime_type    text NOT NULL,
  size_bytes   bigint NOT NULL,
  width        int,
  height       int,
  duration_ms  int,
  blurhash     text,
  status       text NOT NULL CHECK (status IN ('pending', 'ready', 'rejected')),
  created_at   timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (project_id, sha256)
);

-- Missing Capability и агенты -------------------------------------------
CREATE TABLE capability_requests (
  id                     uuid PRIMARY KEY,
  project_id             uuid NOT NULL REFERENCES projects(id),
  key                    text NOT NULL,                      -- commerce.product.quickPreview
  title                  text NOT NULL,
  description            text NOT NULL,
  status                 text NOT NULL CHECK (status IN ('draft', 'submitted', 'accepted', 'in_progress',
                                                         'pr_open', 'fulfilled', 'rejected', 'cancelled')),
  required_by            jsonb NOT NULL DEFAULT '[]',        -- [{documentId, nodeId, changesetId}]
  proposed_contract      jsonb,                              -- предполагаемая сигнатура действия/компонента
  context_pack_key       text,
  external_refs          jsonb NOT NULL DEFAULT '[]',        -- ссылки на задачи, PR
  fulfilled_manifest_id  uuid REFERENCES manifests(id),
  created_by             uuid NOT NULL REFERENCES actors(id),
  created_at             timestamptz NOT NULL DEFAULT now(),
  updated_at             timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX capability_requests_open ON capability_requests (project_id, key)
  WHERE status NOT IN ('fulfilled', 'rejected', 'cancelled');

CREATE TABLE agent_sessions (
  id              uuid PRIMARY KEY,
  project_id      uuid NOT NULL REFERENCES projects(id),
  agent_id        uuid NOT NULL REFERENCES actors(id),
  on_behalf_of    uuid REFERENCES actors(id),
  capabilities    text[] NOT NULL,                           -- эффективные права (пересечение)
  ops_quota       int NOT NULL,
  ops_used        int NOT NULL DEFAULT 0,
  expires_at      timestamptz NOT NULL,
  created_at      timestamptz NOT NULL DEFAULT now(),
  revoked_at      timestamptz
);

-- Поиск ------------------------------------------------------------------
CREATE TABLE search_index (
  project_id   uuid NOT NULL,
  object_id    uuid NOT NULL REFERENCES objects(id),
  locale       text NOT NULL DEFAULT '',
  kind         text NOT NULL,
  schema_name  text,
  title        text,
  tsv          tsvector NOT NULL,
  PRIMARY KEY (object_id, locale)
);
CREATE INDEX search_index_tsv ON search_index USING gin (tsv);

-- Журнал безопасности ----------------------------------------------------
CREATE TABLE security_events (
  id          uuid NOT NULL,
  project_id  uuid,
  actor_id    uuid,
  type        text NOT NULL,                                 -- login, token.created, policy.changed, access.denied …
  data        jsonb NOT NULL,
  ip          inet,
  created_at  timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (id, created_at)
) PARTITION BY RANGE (created_at);
```

Очередь фоновых задач и outbox — таблицы библиотеки River (создаются её миграциями).

## 3. Производные индексы и их обновление

| Индекс | Обновляется |
|---|---|
| `version_references`, `version_manifest_usage` | При каждом пересчёте рабочей версии и фиксации; извлекаются статическим анализом тела |
| `unique_values` | При фиксации версии, ставшей head |
| `routes` | В транзакции публикации / отката |
| `search_index` | Фоновой задачей после сдвига head (задержка ≤ 30 с); конфигурация `tsvector` по локали (`russian`, `english`, `simple`) |

## 4. Redis

| Ключ | Назначение | TTL |
|---|---|---|
| `dlv:{project}:{env}:{kind}:{key}:{locale}` | Кэш ответов Delivery API | 1 ч + явная инвалидация |
| `tag:{project}:{env}:obj:{objectId}` | Множество ключей кэша, зависящих от объекта | как у ключей |
| `rl:{tokenId}:{window}` | Rate limiting | окно |
| `presence:{project}:{documentId}` | Кто открыл документ (hash actorId → время) | 30 с, продлевается heartbeat |

Потеря Redis НЕ ДОЛЖНА приводить к потере данных или нарушению корректности: кэш перестраивается.

**[Решение]** Ключи идемпотентности команд (API-010) хранятся в PostgreSQL (таблица `idempotency_keys`), а не в Redis: проверка, исполнение команды и сохранение ответа выполняются в одной транзакции, параллельные запросы с одним ключом сериализуются `pg_advisory_xact_lock`. Ответ хранится как `bytea`, чтобы повтор возвращал байт-в-байт то же тело (JSONB нормализует JSON). Записи старше 24 ч не учитываются.

## 5. Объектное хранилище

```
projects/{projectId}/assets/{sha256}
projects/{projectId}/manifests/{manifestId}/code-index.json.gz
projects/{projectId}/checks/{changesetId}/{checkId}/{artifact}
projects/{projectId}/context-packs/{capabilityRequestId}.json
```

Бакеты закрыты; доступ — через pre-signed URL (загрузка) и CDN с подписанными URL (выдача ассетов).

## 6. Резервное копирование

| ID | Требование |
|---|---|
| OPS-001 | Непрерывное архивирование WAL + ежедневный базовый бэкап (pgBackRest или средство управляемой БД); проверка восстановления — ежемесячно. |
| OPS-002 | Версионирование бакетов объектного хранилища; удаление ассетов — отложенное (30 дней). |
