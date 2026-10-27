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

Пакет [полной очереди работ](../studio-work-queue.md) описан отдельным
[контрактом OpenAPI 3.1](../../apps/server/api/work-queue.openapi.json):
`GET /api/v1/work-queue` и `/work-queue/{changesetId}`. Параметры поиска,
фильтров и cursor проверяются сервером и Studio BFF. Legacy `/work-items` сохранён.
Это описание реализованного пакета; общий контракт API-001 остаётся отдельной задачей.

Пакет [привязок контента](../studio-content-bindings.md), STU-203…218,
добавляет `document.setContent` внутри `apply-operations` и ограниченное разрешение
by:entity в `data.content` Delivery page. Параметр `locale` выбирает точную локаль
проекта; неизвестная локаль даёт `400 PARAM_INVALID`. `contentContext` отдельно
содержит локаль и версии сущности/использованных файлов, участвующие в ETag.
Каталог Studio поддерживает `q` (до 200 символов), schema и cursor.
Реализовано и слито в dev в [PR #57](https://github.com/QahNaaRlN/project-17/pull/57);
локальная приёмка и CI конечного SHA PR пройдены.

Пакет [источников данных](../studio-data-sources.md), STU-219…236,
реализует `document.setDataSource` внутри `apply-operations`, inverse/rebase
по ключу подключения и формы параметров. Он слит в dev в
[PR #58](https://github.com/QahNaaRlN/project-17/pull/58); CI PR и merge-коммита пройдены. Исполнение loader остаётся
на серверной стороне приложения; новый CMS endpoint исполнения не вводится.

Пакет [действий и условий](../studio-behavior-conditions.md), STU-237…254,
реализован и слит в dev в [PR #59](https://github.com/QahNaaRlN/project-17/pull/59), merge `35ccee4`.
Он использует node.setBehavior/node.setCondition, отдельные advisory-права,
строгие аргументы и повторные проверки доступа. Нового endpoint исполнения нет.

Ответ `GET /api/v1/changesets/{id}/review` сохраняет `canEdit` и добавляет
`canEditBehavior = own && open && standard && behavior.use`,
`canEditCondition = own && open && standard && design.compose`.
Отсутствие нового флага в Studio запрещает соответствующую запись. Флаги
не заменяют проверку Command Bus, primary content и locked-политик.

## 2. Аутентификация

| Актор | Механизм |
|---|---|
| Человек (Studio) | OIDC (Authorization Code + PKCE) → серверная сессия, cookie `HttpOnly; Secure; SameSite=Lax`; CSRF-токен для команд |
| Сервис / CI | API-токен `cms_svc_<random>` в заголовке `Authorization: Bearer` |
| Агент | Токен агентской сессии `cms_agt_<random>` с ограниченным сроком (≤ 8 ч) и квотой операций ([09-agent.md §3](09-agent.md#3-идентичность-и-делегирование-агента)) |
| Delivery (published) | Публичный ключ доставки окружения `cms_pub_<random>` (только чтение published) |
| Delivery (draft) | Preview-токен (подписанный, ≤ 15 мин, привязан к CS и окружению) |

## 3. Command API

Реализованный вход описан в [STU-080…084](../studio-identity.md): CMS обслуживает
`GET /auth/login`, `/auth/callback`, `/auth/session` и `POST /auth/logout`.
Studio проксирует их как `/api/studio/auth/{action}`; callback регистрируется у IdP
на публичном origin Studio. Cookie-команды требуют `Origin`, `X-CSRF-Token`;
`X-CMS-Actor` привязывает запрос редактора к ожидаемому человеку и отклоняет смену
пользователя между проверкой сессии и командой. `GET /api/v1/work-items` принимает
`environment` и UUID cursor `after`, возвращает до 50 доступных работ и `nextCursor`.
Команды `create-role`, `bind-human-role`, `unbind-human-role` требуют `project.admin`.

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

Операции изменения существующего entity/asset могут передать top-level
`expectedVersionId` (UUID). Он сравнивается с исходной working/head версией под
той же транзакционной блокировкой, что и публикация. Несовпадение даёт
`CONTENT_VERSION_CONFLICT` (409), вся группа откатывается. Studio всегда передаёт
его при изменении полей/метаданных, delete/restore и replaceFile. Создание объектов
не принимает этот параметр. Он не входит в журнал или inverse; повторы команды
используют исходные ключ, CS/seq, версию и тело (STU-104).

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
| `approve-changeset` | `changesetId`, `expectedSeq?`, `comment?` | человек с `content.publish`, не автор (PUB-001…003) |
| `request-changes` | `changesetId`, `expectedSeq?`, `comment` | человек с `content.publish`, не автор (PUB-001…003) |
| `reopen-changeset` | `changesetId`, `expectedSeq?` | владелец CS; сбрасывает согласования |
| `request-changes` | `changesetId`, `comment` | роль согласующего |
| `publish` | `changesetId`, `environment`, `expectedSeq?` | `content.publish` (окружение) |
| `promote` | `publicationId`, `toEnvironment`, `expectedPromoteSnapshot?` | `content.publish` (целевое окружение); со снимком — актуальный предпросмотр и причина ([STU-466](../studio-promote.md)) |
| `rollback` | `publicationId`, `resetHead?` | `content.publish` (окружение) |
| `claim-schema-changeset` | `changesetId`, `expectedSeq` | человек с `schema.apply`; открытый кандидат сервиса |
| `register-manifest` | manifest, `environment`, `codeIndexUploadId?` | `manifest.register` |
| `create-asset-upload` / `complete-asset-upload` | см. [05-content.md §6](05-content.md#61-загрузка) | `asset.write` |
| `create-preview-token` | `changesetId?`, `environment` | `content.read` |
| `create-delivery-key` / `revoke-delivery-key` | `environment`, `name` / `id` | `project.admin` |
| `create-capability-request` / `update-capability-request` | см. [09-agent.md §5](09-agent.md#5-missing-capability) | `capability.request` |
| `start-agent-session` / `end-agent-session` | см. [09-agent.md §3](09-agent.md#3-идентичность-и-делегирование-агента) | `agent.delegate` |
| `create-environment` / `delete-environment` | `name`, `kind`, `appUrl` | `project.admin`; для `preview` — `manifest.register` |
| `set-environment-app-url` | `environment`, `appUrl` | `project.admin`; адрес приложения стандартного окружения для этапов 8–9 ([STU-481](../pipeline-render-checks.md)) |
| `claim-check` | — | `checks.run`, только сервисный актор исполнителя; аренда проверки этапа 8–9 на 5 мин, страницы и preview-токен; пустая очередь — `check: null` ([STU-485](../pipeline-render-checks.md)) |
| `complete-check` | `checkId`, `leaseId`, `status`, `details` | `checks.run`; результат по действующей аренде, затем пересчёт состояния CS ([STU-486](../pipeline-render-checks.md)) |
| `set-policy` | `body` | `project.admin` |

## 4. Query API

| Запрос | Описание |
|---|---|
| `GET /api/v1/schemas?environment=` | Схемы активного manifest окружения; до первой активации — `409 MANIFEST_NOT_READY` |
| `GET /api/v1/manifest?environment=` | Активный manifest; до первой активации — `409 MANIFEST_NOT_READY` |
| `GET /api/v1/objects?kind=&schema=&q=&cursor=` | Поиск объектов (head); `q` — полнотекстовый |
| `GET /api/v1/objects/{id}?changesetId=` | Объект: head или рабочая версия в CS |
| `GET /api/v1/objects/{id}/versions` | Список версий |
| `GET /api/v1/objects/{id}/diff?from=&to=` | Структурный diff двух версий |
| `GET /api/v1/objects/{id}/usages` | Где используется (обратные ссылки) |
| `GET /api/v1/documents/{id}/outline?changesetId=` | Компактная структура документа (дерево типов, имена, привязки) — для агента и дерева слоёв |
| `GET /api/v1/changesets?state=&owner=` | Список CS |
| `GET /api/v1/pages?environment=&after=` | Каталог страниц: head и свои рабочие страницы; `design.read`, 50 записей и `nextCursor` (STU-040/044) |
| `GET /api/v1/working-drafts?environment=&documentId=&after=` | Свои open/standard CS выбранной страницы и окружения; `design.read`, 50 записей и `nextCursor` (STU-041/044) |
| `GET /api/v1/content-catalog?environment=&changesetId=&kind=&schema=&after=` | Сущности/ассеты из working текущего CS или head, фильтрация до LIMIT 50, tombstones CS, nextCursor и locales/defaultLocale проекта; content.read (STU-100/101) |
| `GET /api/v1/content/{id}?environment=&changesetId=` | Тот же working/head, контракт выбранного standard target CS; `{id,kind,schema?,schemaVersion?,versionId,deleted,data}`. Версия схемы проверяется без upcast; content.read (STU-100/101) |
| `GET /api/v1/changesets/{id}` | CS с объектами, проверками, согласованиями, `needsAttention` и `manifestDiagnostics` по окружениям (CHG-034); для CS схем — кандидат и исходный manifest (MF-025) |
| `GET /api/v1/changesets/{id}/operations?afterSeq=` | Операции |
| `GET /api/v1/changesets/{id}/history?beforeSeq=` | Метаданные 20 целых действий CS и canUndo/canRedo в одном снимке; design.read (STU-050) |
| `GET /api/v1/changesets/{id}/impact` | Анализ влияния |
| `GET /api/v1/changesets/{id}/review` | Состояние, риск, проверки, роли/счётчики/история согласований, needsAttention, краткие objects без тел и actions для текущего актора; согласованный снимок без блокировок записи (STU-070) |
| `GET /api/v1/changesets/{id}/diff?environment=` | Реализован: полный индекс сравнения standard CS, база/публикация → рабочие версии, snapshot; [STU-149…161](../studio-review-diff.md) |
| `GET /api/v1/changesets/{id}/diff/{objectId}?environment=&baseline=&snapshot=&after=&limit=` | Реализован: детали объекта из того же снимка, безопасные значения и курсорная пагинация; [контракт](../studio-review-diff.md#52-детали-объекта) |
| `GET /api/v1/publications?environment=` | История публикаций; записи активации и rollback содержат `previousManifestId`, `currentManifestId` (PUB-033) |
| `GET /api/v1/publication-history/{id}/promote-preview?toEnvironment=` | Предпросмотр переноса: объекты «сейчас в цели → версия публикации», блокеры, диагностики, зависимости и snapshot; [STU-462](../studio-promote.md) |
| `GET /api/v1/publication-history/{id}/promote-preview/diff?toEnvironment=&objectId=&snapshot=&limit=&cursor=` | Детали предпросмотра переноса из того же снимка; [STU-463](../studio-promote.md) |
| `GET /api/v1/capability-requests?status=` | Запросы Missing Capability |
| `GET /api/v1/security-events?type=&after=&limit=` | Журнал SEC-005 проекта для `project.admin`, новые первыми, ≤ 100 записей и `nextCursor`; [STU-509](../security-hardening.md) |
| `GET /api/v1/events` (SSE) | Инвалидация состояния проекта и presence для документа/CS/окружения; [контракт](../studio-realtime.md) |

Ответ `register-manifest` различает активированный manifest и сохранённого кандидата: `manifestHash`, `activation` (`active` или `pending`), для кандидата — `changesetId` (MF-025). Регистрация кандидата не меняет ответы запросов активного manifest и схем.

Пагинация — курсорная (`cursor`, `limit` ≤ 100); ответ содержит `nextCursor`.

При публикации кандидата смена исходного активного manifest возвращает `409 MANIFEST_BASE_CHANGED` (MF-027).

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
- Успешные ответы несут `ETag` (SHA-256 тела), `Cache-Control` по API-033 и `Surrogate-Key` вида `{project}:{env}:{objectId}`; `/page` и `/routes` зависят также от `{project}:{env}:routes`. Ключи включают проект и окружение, чтобы purge одного окружения не задевал другие; после публикации эти ключи сбрасываются задачей `cdn_purge` (06 §7.3.1). На совпавший `If-None-Match` — `304`. Ошибки отдаются с `Cache-Control: no-store`.
- Чтение сущностей и метаданных ассетов подключено; загрузка и обработка файлов описаны в [пакете загрузки](../asset-upload-processing.md).
- Выдача изображений CNT-050–052 подключена через подписанную модель и gateway; [контракт и конфигурация](../asset-delivery-images.md). Raw PDF/видео не выдаются этим endpoint.
- Пока не реализованы: локали и fallback (API-032), кэш ответов на origin (Redis, NFR-002) и лимиты запросов — [спецификация пакета M6](../delivery-cache-limits.md) одобрена, реализация идёт.

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

#### 5.3.1. Реализация (этап M2, вторая итерация Delivery API)

- `create-preview-token {environment, changesetId?}` (право `content.read`) выпускает JWT HS256 на 15 минут с полями `prj`, `env`, `cs`, `sub`, `exp`. Ключ подписи — `environments.preview_key` (32 случайных байта на окружение); смена ключа делает недействительными все токены окружения.
- Запросы с `Authorization: Preview <JWT>` читают черновик: объекты Change Set `cs` — из рабочих версий (с их маршрутами), остальные — из head, а не из published (API-040). Токен без `cs` даёт чтение head. Параметр `changesetId` необязателен, но если передан — должен совпадать с `cs` токена (`403 FORBIDDEN`); с ключом доставки он запрещён (`400 PARAM_INVALID`).
- Черновые `/page` и `/document` содержат `diagnostics` — результат валидации IR документа (пока уровень L1); ответы идут с `Cache-Control: no-store` без `ETag` и `Surrogate-Key`.
- Таблица маршрутов черновика — маршруты head, переопределённые рабочими версиями Change Set (в том числе снятые маршруты и новые страницы).

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
| 409 | `MANIFEST_NOT_READY`, `MANIFEST_BASE_CHANGED`, `CHANGESET_SEQ_CONFLICT`, `CHANGESET_STATE_INVALID`, `CHANGESET_EMPTY`, `CHANGESET_HAS_CONFLICTS`, `REBASE_REQUIRED`, `ROLLBACK_SUPERSEDED`, `PROMOTE_OUTDATED`, `PROMOTE_NOTHING`, `PROMOTE_SNAPSHOT_STALE`, `CHECKS_PENDING`, `CHECK_LEASE_LOST`, `CHECK_APP_URL_MISSING`, `UNIQUE_VIOLATION`, `PATH_TAKEN` |
| 413 | `LIMIT_EXCEEDED` |
| 422 | `VALIDATION_FAILED` (с `diagnostics`), `ENVIRONMENT_NOT_PUBLISHABLE`, `PROMOTE_SAME_ENVIRONMENT`, `PROMOTE_NOT_SUPPORTED`, `MANIFEST_BREAKING_IN_USE`, `SCHEMA_VERSION_AHEAD`, `REFERENCE_UNPUBLISHED`, `CHECKS_FAILED` |
| 429 | `RATE_LIMITED` (с `Retry-After`) |

## 7. Ограничения частоты

| Клиент | Лимит по умолчанию |
|---|---|
| Пользователь Studio | 50 команд/с, 200 запросов/с |
| Сервисный токен | 20 команд/с |
| Агентская сессия | 5 команд/с, квота операций на сессию (по умолчанию 2 000) |
| Delivery API (origin) | 500 запросов/с на ключ доставки |

### Подключённое чтение сущностей и метаданных ассетов

`GET /api/v1/asset-uploads/{id}` требует `asset.write` и возвращает статус
собственной загрузки актора в проекте: pending/processing/ready/rejected,
expiresAt, fileHash готового файла и errorCode отклонения. Запись файла в
Change Set остаётся отдельной операцией; см. [пакет загрузки](../asset-upload-processing.md).

`GET /api/v1/entities/{id}` и `/assets/{id}` (content.read) принимают `environment`
или `changesetId`; без окружения читается head. Delivery endpoints —
`/delivery/v1/{project}/{env}/entity/{id}` и `/asset/{id}`. Они используют
существующие ключи доступа, preview, ETag и surrogate keys. Метаданные ассета
не заменяют endpoint бинарного файла CNT-050. Подробности и оставшиеся проверки:
[сущности и ссылки](../entities-assets.md).

`GET /delivery/v1/{project}/{env}/asset/{id}/image` требует delivery/preview ключ,
отвечает `no-store` и возвращает подписанные варианты. Браузер получает байты
через `/assets/v1/{env}/{assetId}/{fileHash}` без передачи delivery key.
Gateway проверяет всю capability, текущую версию, файл, scope/preview key и срок.
Статусы: 403 ASSET_URL_INVALID, 404 ASSET_IMAGE_NOT_FOUND, 503
ASSET_IMAGES_UNAVAILABLE, 502 ASSET_IMAGE_UPSTREAM; любые ошибки `no-store`.
Параметры и CDN политика: [пакет выдачи](../asset-delivery-images.md).

### Missing Capability v1

Подключены команды create/update/submit-capability-request,
refresh-capability-context, resolve-capability-request и сервисная upload-code-index.
Query `/api/v1/capability-requests` поддерживает фильтры и стабильный cursor;
`/{id}`, `/{id}/context-preview`, `/{id}/context-pack?version=N` возвращают
доступную актору проекцию, предварительный контекст и неизменяемый JSON export.
Ответы no-store; скачивание — attachment с X-Context-Hash. Права проверяются
по текущей сессии; code.propose не открывает живые страницы/CS.
Точные payload, ошибки и состояния: [контракт пакета](../studio-missing-capability.md).

### Подписанная доставка context pack

Команды `set-capability-delivery-target`, `send-capability-context`,
`retry-capability-delivery`, `cancel-capability-delivery` используют Command Bus,
reason и Idempotency-Key. Настройка доступна human project.admin; отправка/повтор
— владельцу исходного CS с capability.request + design.read + capability.deliver.
Агенты и сервисы не могут отправлять, повторять, отменять или менять target.
Human project.admin может аварийно отменить доставку без доступа к pack.

Query (все private, no-store, RepeatableRead):
`/api/v1/capability-delivery-endpoints?environment=`,
`/api/v1/capability-delivery-target?environment=`,
`/api/v1/capability-requests/{id}/delivery-preview`,
`/api/v1/capability-requests/{id}/deliveries?limit=25&cursor=`,
`/api/v1/capability-deliveries/{id}`. Параметр environment фиксирует окружение;
Studio всегда передаёт его и проверяет scope перед записью. Внешние URL/секреты
не принимаются в командах. Проекции не содержат body/keyring/lease или живую
страницу для code.propose. Повтор той же identity возвращает прежний delivery,
включая terminal, и не пересылает пакет.

Ошибка `CAPABILITY_DELIVERY_` с суффиксом REVISION_CONFLICT, CONTEXT_STALE,
TARGET_CHANGED, STATE_INVALID, RETRY_EXHAUSTED — 409; FORBIDDEN — 403,
NOT_FOUND — 404, LIMIT_EXCEEDED — 413, UNAVAILABLE — 503.
Точные payload, HMAC bytes, квитанция и controlled outcomes —
[STU-275…294](../studio-capability-delivery.md).
