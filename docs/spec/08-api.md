# 08. API

## 1. Обзор интерфейсов

| Интерфейс | Назначение | Клиенты | Префикс |
|---|---|---|---|
| Command API | Все изменения состояния | Studio, CI, агент (через MCP), скрипты | `POST /api/v1/commands/{name}` |
| Query API | Чтение для Studio и инструментов | Studio, агент, скрипты | `GET /api/v1/...` |
| Delivery API | Чтение опубликованного / чернового контента и IR приложением | SDK приложения, CDN | `GET /delivery/v1/...` |
| Asset API | Выдача ассетов и трансформаций | Браузер, CDN | `GET /assets/v1/...` |
| MCP | Инструменты для агентов поверх Command/Query API | Агенты | `/mcp` (Streamable HTTP) |

**[Решение]** GraphQL в MVP не реализуется. Delivery API возвращает документ вместе со всеми нужными ему данными, вычисленными статическим анализом привязок (IR-044), поэтому выбор полей на клиенте в MVP не требуется. GraphQL для внешних потребителей контента — после MVP.

| ID | Требование |
|---|---|
| API-001 | Все интерфейсы ДОЛЖНЫ быть описаны в OpenAPI 3.1 (`apps/server/api/openapi.yaml`); клиент `@cms/core` и серверные заглушки генерируются из описания. |
| API-002 | Бизнес-правила НЕ ДОЛЖНЫ дублироваться между интерфейсами: MCP и Studio вызывают те же обработчики Command Bus. |

## 2. Аутентификация

| Актор | Механизм |
|---|---|
| Человек (Studio) | OIDC (Authorization Code + PKCE) → серверная сессия, cookie `HttpOnly; Secure; SameSite=Lax`; CSRF-токен для команд |
| Сервис / CI | API-токен `cms_svc_<random>` в заголовке `Authorization: Bearer` |
| Агент | Токен агентской сессии `cms_agt_<random>` с ограниченным сроком (≤ 8 ч) и квотой операций ([09-agent.md §3](09-agent.md#3-идентичность-и-делегирование-агента)) |
| Delivery (published) | Публичный ключ доставки окружения `cms_pub_<random>` (только чтение published) |
| Delivery (draft) | Preview-токен (подписанный, ≤ 15 мин, привязан к CS и окружению) |

## 3. Command API

### 3.1. Конверт

```http
POST /api/v1/commands/apply-operations
Content-Type: application/json
Idempotency-Key: 6c1f0e7a-…
X-CMS-Project: store

{
  "payload": {
    "changesetId": "0192f1cf-…",
    "expectedSeq": 16,
    "operations": [
      { "clientOpId": "c-7f2a", "type": "node.setDesign", "target": "0192f1c5-…",
        "payload": { "nodeId": "n_grid01", "set": { "gap": { "base": "md", "lg": "xl" } } },
        "coalesceKey": "setDesign:n_grid01:gap" }
    ]
  },
  "reason": "Увеличить воздух между колонками на десктопе"
}
```

Ответ `200`:

```json
{
  "result": { "seq": 17, "operations": [{ "id": "0192f1d0-…", "seq": 17, "coalesced": false }] },
  "changeset": { "id": "0192f1cf-…", "state": "open", "seq": 17, "risk": "medium" },
  "diagnostics": [ { "code": "BINDING_PATH_UNRESOLVED", "severity": "warning", "…": "…" } ],
  "versions": [ { "objectId": "0192f1c5-…", "bodyHash": "sha256:…" } ]
}
```

| ID | Требование |
|---|---|
| API-010 | Каждая команда ДОЛЖНА принимать `Idempotency-Key`; повтор с тем же ключом в течение 24 ч возвращает сохранённый ответ без повторного исполнения. |
| API-011 | Команды, создающие операции, ДОЛЖНЫ принимать `reason` (обязательно для агентов и для риска `high`). |
| API-012 | Обработка команды в Command Bus: аутентификация → определение актора → авторизация (права + ABAC) → валидация payload → исполнение в транзакции → запись операций → outbox → ответ. |

### 3.2. Каталог команд

| Команда | Payload | Право |
|---|---|---|
| `create-changeset` | `title`, `description?`, `targets?` | любое право записи |
| `apply-operations` | `changesetId`, `expectedSeq`, `operations[]` (≤ 200) | по каталогу операций |
| `undo` / `redo` | `changesetId`, `expectedSeq` | автор CS |
| `import-document` | `changesetId`, `document` (вложенная форма), `path?` | `design.compose` |
| `rebase-changeset` | `changesetId`, `expectedSeq`, `resolutions?: [{operationId, choice: mine\|theirs}]` (≤ 1000) | автор CS |
| `submit-changeset` | `changesetId`, `targets` | автор CS |
| `reopen-changeset` | `changesetId` | автор CS |
| `abandon-changeset` | `changesetId` | автор CS, `project.admin` |
| `approve-changeset` | `changesetId`, `environment`, `comment?` | роль согласующего (PUB-001…003) |
| `request-changes` | `changesetId`, `comment` | роль согласующего |
| `publish` | `changesetId`, `environment` | `content.publish` (окружение) |
| `promote` | `publicationId`, `toEnvironment` | `content.publish` (целевое окружение) |
| `rollback` | `publicationId`, `resetHead?` | `content.publish` (окружение) |
| `register-manifest` | manifest, `environment`, `codeIndexUploadId?` | `manifest.register` |
| `create-asset-upload` / `complete-asset-upload` | см. [05-content.md §6](05-content.md#61-загрузка) | `asset.write` |
| `create-preview-token` | `changesetId?`, `environment` | `content.read` |
| `create-delivery-key` / `revoke-delivery-key` | `environment`, `name` / `id` | `project.admin` |
| `create-capability-request` / `update-capability-request` | см. [09-agent.md §5](09-agent.md#5-missing-capability) | `capability.request` |
| `start-agent-session` / `end-agent-session` | см. [09-agent.md §3](09-agent.md#3-идентичность-и-делегирование-агента) | `agent.delegate` |
| `create-environment` / `delete-environment` | `name`, `kind`, `appUrl` | `project.admin`; для `preview` — `manifest.register` |
| `set-policy` | `body` | `project.admin` |

## 4. Query API

| Запрос | Описание |
|---|---|
| `GET /api/v1/schemas?environment=` | Схемы активного manifest окружения |
| `GET /api/v1/manifest?environment=` | Активный manifest |
| `GET /api/v1/objects?kind=&schema=&q=&cursor=` | Поиск объектов (head); `q` — полнотекстовый |
| `GET /api/v1/objects/{id}?changesetId=` | Объект: head или рабочая версия в CS |
| `GET /api/v1/objects/{id}/versions` | Список версий |
| `GET /api/v1/objects/{id}/diff?from=&to=` | Структурный diff двух версий |
| `GET /api/v1/objects/{id}/usages` | Где используется (обратные ссылки) |
| `GET /api/v1/documents/{id}/outline?changesetId=` | Компактная структура документа (дерево типов, имена, привязки) — для агента и дерева слоёв |
| `GET /api/v1/changesets?state=&owner=` | Список CS |
| `GET /api/v1/changesets/{id}` | CS с объектами, проверками, согласованиями |
| `GET /api/v1/changesets/{id}/operations?afterSeq=` | Операции |
| `GET /api/v1/changesets/{id}/impact` | Анализ влияния |
| `GET /api/v1/publications?environment=` | История публикаций |
| `GET /api/v1/capability-requests?status=` | Запросы Missing Capability |
| `GET /api/v1/events` (SSE) | Поток событий проекта для Studio: изменения CS, проверки, публикации, presence |

Пагинация — курсорная (`cursor`, `limit` ≤ 100); ответ содержит `nextCursor`.

## 5. Delivery API

### 5.1. Страница по маршруту

```http
GET /delivery/v1/{project}/{env}/page?path=/collections/summer&locale=ru
Authorization: Bearer cms_pub_…
```

```json
{
  "page": { "objectId": "0192f1c5-…", "versionId": "0192f1c6-…", "path": "/collections/:slug",
            "params": { "slug": "summer" } },
  "document": { "irVersion": "1.0", "kind": "page", "root": "n_root", "nodes": { "…": "…" } },
  "components": { "0192f1c7-…": { "versionId": "…", "document": { "…": "…" } } },
  "data": {
    "content": { "id": "0192…", "schema": "Collection", "title": "Летняя коллекция", "handle": "summer" },
    "entities": { "0192…": { "…": "…" } },
    "assets": { "0192…": { "url": "https://cdn…", "width": 2400, "height": 1600, "blurhash": "…", "alt": "…" } },
    "links": { "0192…": "/products/linen-shirt" }
  },
  "dataSources": { "featured": { "source": "commerce.products.list", "params": { "collection": "summer", "limit": 4 } } },
  "requires": { "manifestHash": "sha256:…", "components": ["ProductCard"], "actions": ["commerce.addToCart"] },
  "locale": "ru",
  "fallbacks": ["data.entities.0192….lead"]
}
```

| ID | Требование |
|---|---|
| API-030 | Ответ ДОЛЖЕН содержать документ, транзитивно используемые Composed-компоненты (с версиями, разрешёнными для окружения), разрешённые сущности, ассеты и ссылки, необходимые привязкам. |
| API-031 | Параметры источников данных, зависящие от `$content` и `$context.route`, ДОЛЖНЫ возвращаться вычисленными; исполнение источников — на стороне приложения. Параметры, зависящие от `$item`/`$props`, вычисляются SDK. |
| API-032 | Значения выдаются в запрошенной локали с fallback (CNT-031); rich text — в формате RichText v1. |
| API-033 | Ответ published ДОЛЖЕН содержать `ETag`, `Cache-Control: public, s-maxage=300, stale-while-revalidate=60, stale-if-error=86400` и `Surrogate-Key` со всеми ID объектов, от которых он зависит. |
| API-034 | Если путь не найден — `404` с телом `{ "redirect"?: … }`. |

### 5.2. Прочие ресурсы Delivery API

| Запрос | Описание |
|---|---|
| `GET /delivery/v1/{project}/{env}/routes` | Таблица маршрутов (для генерации маршрутизатора / sitemap) |
| `GET /delivery/v1/{project}/{env}/entities/{id}?locale=` | Опубликованная сущность |
| `GET /delivery/v1/{project}/{env}/entities?schema=&filter[field]=&sort=&cursor=` | Простые выборки опубликованных сущностей (REST для простых ресурсов) |
| `GET /delivery/v1/{project}/{env}/document/{id}` | Документ по ID (компоненты, фрагменты) |

### 5.2.1. Реализация (этап M2, первая итерация)

- Ключ доставки `cms_pub_<random>` выдаёт команда `create-delivery-key {environment, name}`; секрет показывается один раз, хранится SHA-256. `revoke-delivery-key {id}` отзывает ключ, `GET /api/v1/delivery-keys` — список ключей проекта (право `project.admin`). Ключ действует только для своего проекта и окружения: чужие `{project}/{env}` в пути — `403 FORBIDDEN`, неизвестный или отозванный ключ — `401 UNAUTHENTICATED`.
- Реализованы `GET …/page?path=`, `GET …/document/{id}` и `GET …/routes`. Путь страницы сопоставляется с таблицей маршрутов окружения по сегментам; при нескольких совпадениях выигрывает маршрут, у которого раньше встречается литеральный сегмент (`/products/sale` сильнее `/products/:slug`). Хвостовой `/` в запросе отбрасывается.
- Ответ `/page` содержит `page {objectId, versionId, path, params}` и `document`; `components`, `data` и `dataSources` пока пустые — они появятся вместе с Composed-компонентами и контентом. 404 отдаётся как problem+json без `redirect` (редиректы — вместе с таблицей редиректов).
- Успешные ответы несут `ETag` (SHA-256 тела), `Cache-Control` по API-033 и `Surrogate-Key` (ID объектов; у `/routes` — ещё ключ `routes`); на совпавший `If-None-Match` — `304`. Ошибки отдаются с `Cache-Control: no-store`.
- Пока не реализованы: черновой режим и preview-токены (§5.3), локали и fallback (API-032), сущности и ассеты, кэш Redis и инвалидация CDN по `Surrogate-Key` (PUB-021), лимиты запросов.

### 5.3. Черновой режим

Тот же набор запросов с параметром `changesetId` и preview-токеном:

```http
GET /delivery/v1/store/staging/page?path=/collections/summer&changesetId=0192f1cf-…
Authorization: Preview eyJhbGciOi…
```

| ID | Требование |
|---|---|
| API-040 | В черновом режиме объекты CS берутся из рабочих версий, остальные — из head (а не из published). Ответ содержит `Cache-Control: no-store`. |
| API-041 | Preview-токен — JWT (HS256, ключ окружения), поля `prj`, `env`, `cs`, `sub`, `exp` (≤ 15 мин). Studio обновляет его до истечения. |
| API-042 | Черновые ответы содержат `diagnostics` текущего состояния CS. |

## 6. Ошибки

Формат — RFC 9457 (`application/problem+json`):

```json
{
  "type": "https://cms.dev/errors/rebase-required",
  "title": "Требуется rebase Change Set",
  "status": 409,
  "code": "REBASE_REQUIRED",
  "detail": "Head объекта 0192f1c5 изменился после начала работы",
  "instance": "/api/v1/commands/publish",
  "traceId": "4bf92f3577b34da6a3ce929d0e0e4736",
  "diagnostics": []
}
```

| HTTP | Коды |
|---|---|
| 400 | `PAYLOAD_INVALID`, `OPERATION_INVALID` |
| 401 | `UNAUTHENTICATED`, `TOKEN_EXPIRED` |
| 403 | `FORBIDDEN`, `POLICY_DENIED`, `APPROVAL_SELF`, `APPROVAL_FORBIDDEN_ACTOR`, `AGENT_QUOTA_EXCEEDED` |
| 404 | `NOT_FOUND` |
| 409 | `CHANGESET_SEQ_CONFLICT`, `CHANGESET_STATE_INVALID`, `CHANGESET_EMPTY`, `CHANGESET_HAS_CONFLICTS`, `REBASE_REQUIRED`, `ROLLBACK_SUPERSEDED`, `PROMOTE_OUTDATED`, `PROMOTE_NOTHING`, `UNIQUE_VIOLATION`, `PATH_TAKEN` |
| 413 | `LIMIT_EXCEEDED` |
| 422 | `VALIDATION_FAILED` (с `diagnostics`), `ENVIRONMENT_NOT_PUBLISHABLE`, `PROMOTE_SAME_ENVIRONMENT`, `PROMOTE_NOT_SUPPORTED`, `MANIFEST_BREAKING_IN_USE`, `SCHEMA_VERSION_AHEAD`, `REFERENCE_UNPUBLISHED` |
| 429 | `RATE_LIMITED` (с `Retry-After`) |

## 7. Ограничения частоты

| Клиент | Лимит по умолчанию |
|---|---|
| Пользователь Studio | 50 команд/с, 200 запросов/с |
| Сервисный токен | 20 команд/с |
| Агентская сессия | 5 команд/с, квота операций на сессию (по умолчанию 2 000) |
| Delivery API (origin) | 500 запросов/с на ключ доставки |
