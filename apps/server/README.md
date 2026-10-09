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
