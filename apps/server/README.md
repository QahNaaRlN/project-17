# CMS Core (Go)

Модульный монолит сервера CMS ([спецификация 12 §1](../../docs/spec/12-security-ops.md)). Один бинарник `cms` с подкомандами.

| Команда | Назначение |
|---|---|
| `cms serve` | HTTP-сервер: Command API, Query API, `/healthz`, `/readyz` |
| `cms migrate up\|down\|status\|version` | Миграции схемы БД (goose, встроены в бинарник) |
| `cms bootstrap -slug S -name N [-token-ttl 2160h]` | Новый проект: окружения `staging` и `production`, роль `admin`, сервисный актор и токен (показывается один раз) |
| `cms version` | Версия сборки |

## Конфигурация

| Переменная | По умолчанию | Описание |
|---|---|---|
| `CMS_DATABASE_URL` | — (обязательна) | Строка подключения PostgreSQL 16+ |
| `CMS_HTTP_ADDR` | `:8080` | Адрес HTTP-сервера |
| `CMS_LOG_LEVEL` | `info` | `debug`, `info`, `warn`, `error`; журнал — JSON в stdout |
| `CMS_SHUTDOWN_TIMEOUT` | `15s` | Время на завершение запросов при остановке |

## Быстрый старт

```bash
docker compose -f deploy/compose/docker-compose.yml up --build -d
docker compose -f deploy/compose/docker-compose.yml run --rm cms bootstrap -slug store -name "Магазин"
# → cms_svc_…

curl -X POST localhost:8080/api/v1/commands/create-environment \
  -H "Authorization: Bearer cms_svc_…" -H "X-CMS-Project: store" \
  -H "Idempotency-Key: $(uuidgen)" -H "Content-Type: application/json" \
  -d '{"payload": {"name": "qa", "kind": "standard"}, "reason": "окружение для QA"}'

curl localhost:8080/api/v1/environments -H "Authorization: Bearer cms_svc_…" -H "X-CMS-Project: store"
```

## API

| Метод | Путь | Назначение |
|---|---|---|
| `POST` | `/api/v1/commands/create-environment` | Окружение проекта |
| `POST` | `/api/v1/commands/create-changeset` | Новый Change Set |
| `POST` | `/api/v1/commands/apply-operations` | Пакет операций над документами (атомарно, с `expectedSeq`) |
| `POST` | `/api/v1/commands/undo` | Отмена последней операции Change Set |
| `POST` | `/api/v1/commands/abandon-changeset` | Закрыть Change Set без слияния |
| `POST` | `/api/v1/commands/submit-changeset` | Подать на проверку: проверки, риск, переход в `in_review`/`approved`/`failed` |
| `POST` | `/api/v1/commands/approve-changeset` | Согласовать (только человек, не автор) |
| `POST` | `/api/v1/commands/request-changes` | Запросить изменения (комментарий обязателен) |
| `POST` | `/api/v1/commands/reopen-changeset` | Вернуть в работу, сбросив согласования |
| `POST` | `/api/v1/commands/set-approval-policy` | Число согласований по риску |
| `POST` | `/api/v1/commands/publish` | Опубликовать согласованный Change Set в окружение |
| `POST` | `/api/v1/commands/rollback` | Откатить публикацию |
| `GET` | `/api/v1/environments` | Окружения |
| `GET` | `/api/v1/changesets?state=` | Change Set'ы проекта |
| `GET` | `/api/v1/changesets/{id}` | Change Set и изменённые объекты |
| `GET` | `/api/v1/changesets/{id}/operations?afterSeq=` | Журнал операций |
| `GET` | `/api/v1/changesets/{id}/review` | Проверки, риск и согласования |
| `GET` | `/api/v1/publications?environment=` | История публикаций |
| `GET` | `/api/v1/publications/{id}` | Публикация с перемещёнными указателями |
| `GET` | `/api/v1/documents/{id}?changesetId=` | Документ: рабочая версия в Change Set или head |
| `GET` | `/api/v1/documents/{id}?environment=` | Опубликованная в окружении версия документа |

Пример пакета операций:

```json
{
  "payload": {
    "changesetId": "…",
    "expectedSeq": 0,
    "operations": [
      { "type": "document.create", "payload": { "kind": "page", "root": { "id": "n_root", "type": "Container" } } },
      { "type": "node.insert", "target": "<id документа>", "payload": { "parentId": "n_root", "subtree": { "type": "Heading", "bindings": { "text": "$content.title" } } } }
    ]
  },
  "reason": "Новая страница коллекции"
}
```

## Устройство

| Пакет | Ответственность |
|---|---|
| `internal/app` | Подкоманды бинарника, запуск и остановка сервера |
| `internal/config` | Конфигурация из окружения |
| `internal/platform/postgres` | Пул, миграции, транзакции |
| `internal/auth` | Права (09-agent.md §2.1), API-токены, актор |
| `internal/commandbus` | Единая точка изменений: авторизация → проверка → идемпотентность → транзакция |
| `internal/httpapi` | Маршруты, ошибки `application/problem+json`, журнал запросов |
| `internal/projects` | Проекты, окружения, команда `create-environment`, bootstrap |
| `internal/changes` | Change Set, рабочие версии, журнал операций, undo |
| `internal/workflow` | Подача на проверку, проверки, риск, согласования, политика согласований |
| `internal/publishing` | Публикация в окружение, head и опубликованные указатели, откат |
| `internal/composition/ops` | Операции над документом IR как чистые функции с обратными операциями |
| `internal/composition/ir` | Формат IR: типы, валидатор, нормализация |
| `internal/store` | Запросы sqlc (сгенерировано из `db/queries`) |
| `db/migrations` | Миграции goose |

Новая команда объявляется как `commandbus.Command[P, R]` и регистрируется в `commandbus.Register`; HTTP-слой менять не нужно.

## Разработка

```bash
pnpm --filter @cms/server gen    # sqlc и типы IR
pnpm --filter @cms/server test   # тесты с -race и порогом покрытия
```

Интеграционным тестам нужен PostgreSQL: либо `CMS_TEST_DATABASE_URL` (любая база на сервере; тесты создают и удаляют свои базы), либо Docker — тогда тесты сами поднимут контейнер `postgres:16-alpine` (testcontainers).
