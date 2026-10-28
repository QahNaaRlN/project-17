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
| `OTEL_EXPORTER_OTLP_ENDPOINT` | — | Адрес OTLP/HTTP (Collector); без него трассировки и метрики выключены ([наблюдаемость](../../docs/observability.md)) |
| `OTEL_TRACES_SAMPLER`, `OTEL_TRACES_SAMPLER_ARG` | все трассы | В production — `parentbased_traceidratio` и `0.1` |
| `OTEL_SERVICE_NAME`, `CMS_DEPLOYMENT_ENV` | `cms`, — | Ресурс трасс и метрик |
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
| `CMS_PREVIEW_APP_ORIGINS` | — | Точные origin preview-развёртываний PR через запятую (https, http только loopback); шаблоны `*.` отклоняются при старте |

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
| `POST` | `/api/v1/commands/delete-environment` | Архив preview-окружения (MF-032) |
| `POST` | `/api/v1/commands/create-changeset` | Новый Change Set |
| `POST` | `/api/v1/commands/apply-operations` | Пакет операций над документами (атомарно, с `expectedSeq`) |
| `POST` | `/api/v1/commands/undo` | Отмена последнего действия CS: группа или старая одиночная операция |
| `POST` | `/api/v1/commands/redo` | Повтор последнего отменённого действия CS |
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
| `GET` | `/api/v1/pages?environment=&after=` | Страницы для Studio, `design.read`, 50 записей + `nextCursor` |
| `GET` | `/api/v1/working-drafts?environment=&documentId=&after=` | Свои open/standard CS доступной страницы, `design.read`, 50 записей + `nextCursor` |
| `GET` | `/api/v1/changesets/{id}` | Change Set и изменённые объекты |
| `GET` | `/api/v1/changesets/{id}/operations?afterSeq=` | Журнал операций |
| `GET` | `/api/v1/changesets/{id}/history?beforeSeq=` | История целыми действиями и доступность undo/redo; design.read |
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

## Studio Agent / OpenRouter

Задайте `CMS_OPENROUTER_API_KEY` только в серверном окружении. Модель задаётся
через `CMS_OPENROUTER_MODEL` либо командой администратора `set-agent-config`
(`model`, `monthlyTokens`, `userMonthlyTokens`). Без ключа/модели запуск недоступен.
Ключ не хранится в настройках проекта и не передаётся в Studio или MCP.
Лимиты по умолчанию: 1 000 000 токенов проекта и 100 000 пользователя в месяц UTC.
Неизвестный расход сохраняет резерв; повтор провайдера после потери ответа запрещён.
Агент не публикует: правки остаются в CS с авторством и требуют другого согласующего.
Внешний MCP использует отдельный `cms_agt_` токен с TTL ≤8 часов, квотой и CS scope.
Каталог и ограничения: [Studio Agent](../../docs/studio-agent.md),
[stdio proxy](../../packages/mcp/README.md).

## Missing Capability

Миграция 00020 добавляет action-запросы, версии context pack, аудит и загрузки
индекса кода. `upload-code-index` доступна сервисному актору с manifest.register;
`register-manifest` связывает upload с точным hash в одной транзакции.
Запросы создаёт capability.request; разработчик с code.propose видит отправленные
карточки и ограниченный pack. Применение действия выполняет владелец CS отдельно
после его появления в активном manifest. Проверенные факты PR/CI добавлены
отдельным пакетом ниже и не меняют этот ручной процесс.
[Команды, Query API, права и приёмка](../../docs/studio-missing-capability.md).

### Подписанная доставка context pack

Миграция 00021 добавляет отдельный журнал доставки. Отсутствие
`CMS_CAPABILITY_WEBHOOK_CONFIG_FILE` оставляет ручной workflow доступным.
Для доставки оператор указывает абсолютный путь к приватному JSON-файлу
`{"endpoints":[{"endpointId":"engineering","projectId":"<UUID>","environment":"staging","url":"https://receiver.example/context","activeKeyId":"v1","keys":{"v1":"<base64 от 32–128 случайных байт>"}}]}`.
Файл и ключи не включать в репозиторий/публичные артефакты. CMS валидирует
структуру и scope при запуске. URL только HTTPS/443 с публичными DNS-ответами;
redirect, proxy, IP literals и закрытые адреса запрещены. Тестовый флаг
`CMS_CAPABILITY_WEBHOOK_ALLOW_LOOPBACK_HTTP=true` разрешает только literal
127.0.0.1/::1 с явным портом HTTP и не связан с настройками OIDC.

Human project.admin отдельно выбирает получателя; автору нужны capability.request,
design.read и явно выданное capability.deliver. Типовые человеческие роли и
AgentRights этого права не получают. Submit, refresh и включение target не
отправляют пакеты. Автор отдельно подтверждает URL и сохранённый pack.
Delivered означает только durable receipt, не инженерное принятие запроса.
River использует очередь capability_delivery (2 workers), lease 30 с,
восстановление раз в 30 с, до 6 попыток за цикл и до 3 явно подтверждённых циклов.
Ротация: новый activeKeyId с сохранением прежних ключей до окончания старых
доставок. Старый keyId нельзя назначать новому секрету; hot reload отсутствует.
Логи попыток содержат ID/project, cycle/attempt, duration, HTTP/outcome;
периодический журнал очереди — depth/oldest_age_seconds. Body/подписи/секреты
и raw response в них не попадают.
[Протокол, DTO и ошибки](../../docs/studio-capability-delivery.md),
[локальная приёмка](../../docs/studio-capability-delivery-acceptance.md).

### Проверенные сведения GitHub PR/CI

Миграция 00022 и очередь `capability_github` (2 workers) хранят наблюдения
отдельно от инженерного статуса, IR, context pack и доставки. Подключение
необязательно: без `CMS_CAPABILITY_GITHUB_CONFIG_FILE` ручной путь остаётся доступен.

Оператор задаёт абсолютный путь к приватному JSON-файлу:

```json
{
  "repositories": [{
    "configId": "application",
    "projectId": "<UUID проекта>",
    "environment": "staging",
    "repositoryId": "<числовой GitHub repository ID строкой>",
    "owner": "<owner>",
    "name": "<repo>",
    "baseBranch": "dev",
    "credentialId": "github-read-v1",
    "token": "<read-only token; verify endpoint access>",
    "requiredChecks": [{
      "source": "check_run",
      "name": "Check (TS + Go)",
      "target": "head",
      "appId": "<числовой ID доверенного GitHub App строкой>"
    }]
  }]
}
```

Подставить реальные ID/имена, а обязательные проверки перечислить полностью
(для Project 17 также `Docker image`, GitHub Actions app ID 15368). В PR этого
проекта API прикрепляет проверки к head SHA, даже когда checkout использует
пробное слияние; поэтому исходная политика использует head. Для проверок
фактического merge-коммита отдельно выбрать integration; отсутствие результатов
на нём остаётся unknown. Токен ограничить выбранным репозиторием:
Metadata/Pull requests/Checks/Commit statuses/Contents — read.
Проверка 2026-10-18 выявила ограничение текущего V1-контракта: интерфейс
fine-grained PAT не предлагает Checks/read; тот же ключ читает приватный PR
(200), но его Checks endpoint возвращает 403. Публичные Checks читаются, что
не доказывает пригодность ключа для приватного репозитория. Не заменять ключ
на широкий классический PAT; приватный Checks-сценарий остаётся заблокированным
до подключения и внешней приёмки [GitHub App](../../docs/studio-capability-github-app.md).
[Результаты живой проверки](../../docs/studio-capability-github-acceptance.md).
Contents используется
только для проверки родителей commit, исходники не читаются. Файл/токен не
коммитить, не выдавать браузеру/агенту; для ротации изменить credentialId,
перезапустить CMS и явно подтвердить новую настройку. Горячей перезагрузки нет.

В Studio человек с project.admin открывает «Интеграция GitHub», выбирает запись
каталога и явно подтверждает доступ участников CMS к ограниченным метаданным,
в том числе приватных PR. Человек с code.propose привязывает номер PR к
отправленному запросу и явно запускает обновление. Link/unlink/refresh используют
Command Bus, reason, idempotency key и revision gates. Факты GitHub читаются через GET. Для App добавлены только фиксированный POST
выпуска installation token и GET проверки списка доступных репозиториев;
CMS не открывает/не сливает PR и не запускает CI. Сеть — фиксированный
`https://api.github.com`, без redirects/proxy/закрытых адресов.

После жеста — одна синхронизация и до пяти фоновых обновлений, не чаще 5 минут;
окно 30 минут, свежесть снимка 15 минут. Ручной cooldown 60 секунд, 20 sync/час
на проект, до 3 попыток на sync, общие ограничения по credentialId. Recovery
каждые 30 секунд. Ошибка сохраняет last-known, а не зелёный текущий результат.
Смена head/base/SHA, повтор CI и разные издатели проверок учитываются отдельно.
Merged/success не означает deployed/fulfilled и не снимает блок публикации pending.
[Контракты и границы](../../docs/studio-capability-github.md).


Для приватных Checks вместо `token` можно задать `installation` с числовыми
`appId`, `installationId` и абсолютным `privateKeyFile` (RSA PEM, права 600).
Ключ считывается при старте; App выдаёт краткоживущий токен ровно на один
репозиторий и пять прав чтения. Cache, обновление и отзыв обрабатывает CMS.
Секреты не выдаются браузеру; установка App во внешнем GitHub подтверждается
отдельно. [Полная инструкция и границы](../../docs/studio-capability-github-app.md).

### Preview/<ref> для проверенного PR

Миграция 00023 добавляет источник preview-окружения (base standard, конфигурация
GitHub, repository ID, номер PR), журнал регистраций `preview_registrations`,
аргументы предпросмотра и аудит. CI создаёт `preview/pr-<N>` командой
`create-environment` с `source`, загружает code index и вызывает
`register-manifest` с `headSha` (полный commit индекса) и необязательным `appUrl`
из `CMS_PREVIEW_APP_ORIGINS`. Сопоставление с запросом вычисляется при чтении по
head SHA наблюдения GitHub; Delivery preview подставляет действие вместо
`pending:<id>` только в ответе preview-окружения. Публикация с заглушкой по-прежнему
блокируется. `delete-environment`, River-задача `capability_preview_expiry` (раз в час,
14 дней без регистраций) и проверенное наблюдение закрытого/слитого PR архивируют
окружение. [Пакет](../../docs/studio-capability-preview.md).

### Автоматическое выполнение запроса

Миграция 00024 добавляет системного актора `cms-system` на проект (без токенов),
привязки запроса (группа requiredBy) и outbox оценки. Активация standard-manifest,
проверенное наблюдение «PR слит» и команда `check-capability-fulfillment` ставят
оценку; River-задача `capability_fulfillment` (каждые 10 с) проверяет происхождение
версии (commit индекса = merge SHA или его потомок по GitHub compare) и заменяет
`pending:<id>` в открытых CS владельцев через общий конвейер операций. Публикация
по-прежнему требует проверки и согласования человеком.
[Пакет](../../docs/studio-capability-fulfillment.md),
[приёмка](../../docs/studio-capability-fulfillment-acceptance.md).

### Запросы недостающих компонентов

Миграция 00025 добавляет вид запроса `component`: контракт свойств/слотов, место
вставки и начальные значения. Отправка вставляет узел-заглушку с полем `pending`
(IR-053), он проверяется по предложенному контракту и блокирует публикацию
(`COMPONENT_PENDING_CAPABILITY`). Выполнение — те же условия, что у действий; замена —
операция `node.clearPending` от `cms-system`. Приложения, отклоняющие страницы с
диагностиками runtime, должны допускать `CMS_COMPONENT_PENDING_CAPABILITY` в preview.
[Пакет](../../docs/studio-capability-component.md),
[приёмка](../../docs/studio-capability-component-acceptance.md).

### Запросы недостающих источников данных и токенов

Миграция 00026 добавляет виды `dataSource` и `token`. Заглушка источника —
подключение страницы с меткой `pending` (не загружается, `CMS_SOURCE_PENDING`),
заглушка токена — дизайн-значение `pending:<id>` (в preview черновика —
предлагаемое значение). Публикация блокируется; выполнение — общие условия,
замена — снятие метки или запись имени токена от `cms-system`.
[Пакет](../../docs/studio-capability-data-token.md),
[приёмка](../../docs/studio-capability-data-token-acceptance.md).
