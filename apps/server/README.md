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
| `CMS_SHUTDOWN_TIMEOUT` | `15s` | Время на завершение запросов и фоновых задач при остановке |
| `CMS_CDN_PURGE_URL` | — | Вебхук purge CDN: `POST {"keys": [...]}`; не задан — purge только пишется в журнал |
| `CMS_CDN_PURGE_TOKEN` | — | Токен вебхука (`Authorization: Bearer`) |
| `CMS_S3_ENDPOINT` | — | S3/MinIO host:port без схемы и пути; bucket закрыт |
| `CMS_S3_ACCESS_KEY` / `CMS_S3_SECRET_KEY` | — | Ключи сервера для bucket (задаются вместе с endpoint и bucket) |
| `CMS_S3_BUCKET` | — | Приватный bucket файлов и staging загрузок |
| `CMS_S3_SECURE` | `true` | TLS; `false` только для локального MinIO |
| `CMS_ASSET_PUBLIC_URL` | — | Origin gateway/CDN изображений без пути |
| `CMS_ASSET_SIGNING_KEY` | — | Отдельный hex ключ браузерных URL, не менее 32 байт |
| `CMS_IMGPROXY_URL` | — | Внутренний base URL imgproxy |
| `CMS_IMGPROXY_KEY` / `CMS_IMGPROXY_SALT` | — | Hex key ≥32 bytes / salt ≥16 bytes, совпадают с imgproxy |

Endpoint должен быть доступен и серверу, и клиенту: его host входит в подпись
pre-signed URL, заменять host после подписания нельзя. Для контейнеров используйте
общий DNS/reverse proxy. CORS разрешает PUT с домена Studio; lifecycle удаляет
только префикс `uploads/` через сутки. Адресуемые SHA-256 файлы имеют префикс
`projects/` и этим правилом не удаляются. Без S3 конфигурации остальной сервер
работает, команды загрузки возвращают `ASSET_STORAGE_UNAVAILABLE`.

Контракт загрузки, готовности, `asset.create` / `asset.replaceFile`, ограничения
обработки и следующий пакет описаны в [загрузке ассетов](../../docs/asset-upload-processing.md).

Выдача подписанных изображений CNT-050–052 и первый `@cms/runtime Image`
подключены отдельным [пакетом](../../docs/asset-delivery-images.md): модель по
delivery/preview ключу, gateway с повторной проверкой версии, закрытый imgproxy,
CDN cache/purge. Без конфигурации images API отвечает `ASSET_IMAGES_UNAVAILABLE`.
Локальный compose profile, права S3 reader и ограничения обработчика — в пакете.

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
| `POST` | `/api/v1/commands/rebase-changeset` | Перенести Change Set на текущий head; конфликты и их разрешение (`mine`/`theirs`) |
| `POST` | `/api/v1/commands/set-approval-policy` | Число согласований по риску |
| `POST` | `/api/v1/commands/publish` | Опубликовать согласованный Change Set в окружение |
| `POST` | `/api/v1/commands/promote` | Перенести версии публикации в другое окружение |
| `POST` | `/api/v1/commands/rollback` | Откатить публикацию |
| `GET` | `/api/v1/environments` | Окружения |
| `GET` | `/api/v1/changesets?state=` | Change Set'ы проекта |
| `GET` | `/api/v1/changesets/{id}` | Change Set и изменённые объекты |
| `GET` | `/api/v1/changesets/{id}/operations?afterSeq=` | Журнал операций |
| `GET` | `/api/v1/changesets/{id}/review` | Проверки, риск и согласования |
| `POST` | `/api/v1/commands/create-delivery-key` | Ключ доставки `cms_pub_…` окружения (показывается один раз) |
| `POST` | `/api/v1/commands/revoke-delivery-key` | Отозвать ключ доставки |
| `GET` | `/api/v1/delivery-keys` | Ключи доставки проекта |
| `POST` | `/api/v1/commands/create-preview-token` | Preview-токен (15 мин) для чернового чтения Delivery API |
| `GET` | `/api/v1/publications?environment=` | История публикаций |
| `GET` | `/api/v1/publications/{id}` | Публикация с перемещёнными указателями |
| `GET` | `/api/v1/documents/{id}?changesetId=` | Документ: рабочая версия в Change Set или head |
| `GET` | `/api/v1/documents/{id}?environment=` | Опубликованная в окружении версия документа (с маршрутом `path`) |

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

### Delivery API

Чтение опубликованного приложением по ключу доставки окружения (`Authorization: Bearer cms_pub_…`).
Ответы кэшируются CDN: `ETag`, `Cache-Control: public, s-maxage=300, …`, `Surrogate-Key`.
С `Authorization: Preview <токен>` (и необязательным `?changesetId=`) те же запросы читают черновик:
Change Set — из рабочих версий, остальное — из head; ответы с `diagnostics` и `Cache-Control: no-store`.

| Метод | Путь | Назначение |
|---|---|---|
| `GET` | `/delivery/v1/{project}/{env}/page?path=` | Страница по маршруту: документ и параметры маршрута |
| `GET` | `/delivery/v1/{project}/{env}/document/{id}` | Опубликованный документ по ID |
| `GET` | `/delivery/v1/{project}/{env}/routes` | Таблица маршрутов окружения |

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
| `internal/changes` | Change Set, рабочие версии, журнал операций, undo, переигрывание операций при rebase |
| `internal/workflow` | Подача на проверку, проверки, риск, согласования, политика согласований, rebase |
| `internal/delivery` | Delivery API: ключи доставки, страница по маршруту, документ, таблица маршрутов |
| `internal/jobs` | Фоновые задачи на очереди River: purge CDN по `Surrogate-Key` после публикации |
| `internal/publishing` | Публикация в окружение, продвижение между окружениями, head и опубликованные указатели, откат |
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

Интеграционным тестам нужен PostgreSQL: либо `CMS_TEST_DATABASE_URL` (любая база на сервере; тесты создают и удаляют свои базы), либо Docker — тогда тесты сами поднимут контейнер `postgres:16-alpine` из зеркала `mirror.gcr.io` (testcontainers). Образы берутся из зеркала Google, потому что Docker Hub ограничивает анонимные загрузки с раннеров CI.

Тест приватного S3 требует Docker и собирает MinIO из официальных исходников,
закреплённых на коммите `0d7408fc9969caf07de6a8c3a84f9fbb10a6739e`
(`RELEASE.2025-04-22T22-12-26Z`). Публичные готовые образы этой версии
недоступны. Первая сборка требует сети и занимает несколько минут; Docker
сохраняет её слои для следующих запусков.

## Регистрация manifest

Сервисная команда `register-manifest` проверяет контракт и влияние на документы,
автоматически активирует безопасные изменения без схем standard и сохраняет
схемные кандидаты. Preview использует отдельные снимки схем. Query API возвращает
активный manifest/схемы; детали Change Set — диагностики по окружениям.
[Поведение, ограничения и следующий пакет](../../docs/manifest-registration.md).

## Схемная публикация

Схемный кандидат содержит journal schema.apply; человек принимает его командой
claim-schema-changeset. После исправлений и согласования publish одновременно
активирует manifest, схемы и документы. Rollback возвращает их прежние указатели.
[Контракт, проверка прав и остающиеся этапы](../../docs/schema-publication.md).

Сущности, метаданные ассетов и проверка ссылок публикации подключены:
[команды, чтение и граница пакета](../../docs/entities-assets.md).
