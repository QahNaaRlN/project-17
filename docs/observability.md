# Наблюдаемость: трассировки, метрики, логи и телеметрия SDK (OPS-020…023)

Статус: **одобрена пользователем 2026-10-28; решения §11 приняты**.
Дата: 2026-10-28. Требования: STU-590…609 (STU-570…589 заняты
[этапами pipeline 10–11](pipeline-visual-lighthouse.md)).
Основа: `origin/dev` на `107db06` (после [PR #102](https://github.com/QahNaaRlN/project-17/pull/102)).
Строка плана MVP: M6, пакет 6 «Наблюдаемость OPS-020…023».

## 1. Проблема и границы

Требования [12 §4](spec/12-security-ops.md#4-наблюдаемость) и NFR-018:

- OPS-020 — трассировки OpenTelemetry для каждого HTTP-запроса, команды, задачи и SQL-запроса; `traceId` в ошибках;
- OPS-021 — метрики: задержки и коды по эндпоинтам, длительность команд, очереди и возраст задач, этапы pipeline, hit ratio кэша Delivery, диагностики runtime по кодам, расход токенов LLM;
- OPS-022 — структурированные JSON-логи с `traceId`, `projectId`, `actorId`, без контента и секретов;
- OPS-023 — SDK отправляет диагностику runtime на `POST /delivery/v1/{project}/{env}/telemetry` с семплированием;
- NFR-018 — все сервисы экспортируют трассировки, метрики и логи через OpenTelemetry.

Сейчас сервер пишет JSON-логи `log/slog` с `requestId` (строка на HTTP-запрос и
отдельные события); трассировок и метрик нет; ошибки problem+json содержат
`requestId`, но не `traceId`; в логах нет `projectId`/`actorId`; приёма телеметрии
SDK нет. OpenTelemetry попадает в `go.mod` только косвенно.

Пакет добавляет:

- OpenTelemetry SDK в `cms`: трассировки HTTP, команд, задач River и SQL; экспорт OTLP;
- метрики OPS-021 через OpenTelemetry;
- корреляцию логов с трассировкой и акторами;
- приём диагностик runtime от SDK и их отправку из `@cms/core`;
- профиль Docker Compose с локальным бэкендом наблюдаемости.

Вне пакета: алерты и дашборды production (зависят от выбранного хостинга);
наблюдаемость Studio BFF и исполнителя проверок сверх логов и распространения
`traceparent` (решение §11.6); хранение диагностик runtime в БД.

## 2. Проверенная основа

| Основа | Состояние в dev | Чего не хватает |
|---|---|---|
| Логи | `slog.NewJSONHandler` в `app.go`, строка `http request` с `requestId`; журнал безопасности отдельно | `traceId`, `spanId`, `projectId`, `actorId` |
| Ошибки | problem+json с `requestId`, `instance` | `traceId` |
| HTTP | chi, middleware `RequestID`, `requestLogger`, `recoverer` | Спанов и метрик |
| Команды | `commandbus.Bus.Dispatch` — одна точка для HTTP, MCP и оркестратора (OPS-011) | Спана и длительности по типу |
| Задачи | River: очереди `default`, `capability_delivery`, `capability_github`; проверки этапов 8–11 в `checks` | Глубины очередей и возраста как метрик |
| Кэш Delivery | `cached()` в `httpapi/delivery.go`: попадание, промах, обход | Счётчика исходов |
| LLM | `agents.OpenRouter` читает `prompt_tokens`/`completion_tokens` | Метрики расхода |
| SDK | `@cms/runtime`: диагностики плана (`CMS_SOURCE_LIMIT` и др.), `onError` действий | Отправки на сервер |

## 3. Требования

| ID | Требование |
|---|---|
| STU-590 | `cms serve` и фоновые задачи инициализируют OpenTelemetry SDK (трассировки и метрики) по стандартным переменным `OTEL_EXPORTER_OTLP_ENDPOINT`, `OTEL_EXPORTER_OTLP_PROTOCOL`, `OTEL_SERVICE_NAME` (по умолчанию `cms`), `OTEL_TRACES_SAMPLER(_ARG)`. Без `OTEL_EXPORTER_OTLP_ENDPOINT` экспорт выключен, провайдеры no-op, накладные расходы минимальны. Ресурс: `service.name`, `service.version`, `deployment.environment` (`CMS_DEPLOYMENT_ENV`) |
| STU-591 | Спан на каждый HTTP-запрос (otelhttp поверх chi) с маршрутом-шаблоном (`/delivery/v1/{project}/{env}/page`, не фактическим путём), методом и кодом; входящий `traceparent` принимается только от доверенных клиентов (Studio BFF, исполнитель проверок по сервисному токену) — для анонимных Delivery-запросов начинается новая трасса (решение §11.5) |
| STU-592 | Спан `command {name}` в `commandbus.Dispatch` с атрибутами `cms.command`, `cms.project_id`, `cms.actor_kind`, исходом (`ok` или код ошибки); тело команды и параметры в атрибуты не пишутся |
| STU-593 | Спан на каждую задачу River (`job {kind}`) с очередью и номером попытки; задачи, поставленные из запроса, связываются с его трассой ссылкой (span link) через метаданные задачи |
| STU-594 | Спан на каждый SQL-запрос pgx (трассировщик `pgx.QueryTracer`) с именем запроса sqlc (`-- name:`) или `sql`; текст SQL и параметры в атрибуты не пишутся |
| STU-595 | problem+json содержит `traceId` (32 hex) рядом с `requestId`; заголовок ответа `traceparent` не возвращается анонимным клиентам Delivery |
| STU-596 | Метрики HTTP: `http.server.request.duration` (гистограмма, секунды) по маршруту-шаблону, методу и коду — семантические соглашения OpenTelemetry |
| STU-597 | `cms.command.duration` (гистограмма) по `cms.command` и исходу; `cms.pipeline.stage.duration` по этапу и статусу для этапов 1–11 (синхронные — время в `evaluate`, асинхронные — от постановки до завершения) |
| STU-598 | Наблюдаемые метрики очередей раз в 30 с: `cms.jobs.depth` и `cms.jobs.oldest_age` по очереди River и виду задачи (доступные и запланированные), `cms.checks.queued` и `cms.checks.oldest_age` по этапу 8–11 |
| STU-599 | `cms.delivery.cache.requests` по `kind` (`page`, `document`, …) и исходу (`hit`, `miss`, `bypass`, `error`); hit ratio считается в бэкенде |
| STU-600 | `cms.llm.tokens` по модели и типу (`prompt`, `completion`) и `cms.llm.requests` по модели и исходу — для каждого вызова провайдера агента |
| STU-601 | Ограничение кардинальности: метки — только из фиксированных наборов (маршрут-шаблон, имя команды, код ошибки из реестра, этап, вид задачи, модель из конфигурации); ID проектов и акторов в метки метрик не попадают (только в атрибуты спанов) |
| STU-602 | Логи: обработчик `slog` добавляет к каждой записи с контекстом `traceId`, `spanId`, `projectId`, `actorId` (если есть); строка `http request` пишет маршрут-шаблон; запрет на тела запросов, контент, токены и cookie сохраняется (SEC-005, проверка STU-515 в сценарии §2) |
| STU-603 | `POST /delivery/v1/{project}/{env}/telemetry` (решение §11.4): ключ доставки окружения; тело `{diagnostics: [{code, count, source?}]}`, ≤ 50 записей и 16 KiB; `code` — `[A-Z0-9_]{1,64}`, известные коды runtime, остальные — `OTHER`; `count` 1…10 000; ответ `204`. Сервер только увеличивает счётчик `cms.runtime.diagnostics` по `code`, `source` (`render`, `source`, `action`) и окружению; ничего не хранит |
| STU-604 | Лимит телеметрии: 10 запросов/с на ключ доставки (лимитер STU-531); превышение — `429` без записи в журнал безопасности; неверный ключ — `401` как у Delivery |
| STU-605 | `@cms/core`: опция `telemetry: {sampleRate}` (по умолчанию 0,1, 0 — выключено); диагностики плана рендера, ошибки источников данных и действий агрегируются по коду и отправляются не чаще раза в 30 с и при завершении процесса; ошибки отправки игнорируются и не влияют на рендер. Отправка только с серверной стороны (там, где есть ключ доставки) |
| STU-606 | Studio BFF и исполнитель проверок передают `traceparent` в запросы к CMS и пишут в свои логи `traceId` ответа об ошибке; собственных трассировок в этом пакете нет (решение §11.6) |
| STU-607 | `deploy/compose`: профиль `observability` — OpenTelemetry Collector или всё-в-одном бэкенд (решение §11.2), `cms` с `OTEL_EXPORTER_OTLP_ENDPOINT`; `docs/observability-local.md` — как открыть трассы и метрики |
| STU-608 | Автоматические проверки: Go — спаны HTTP, команды, задачи и SQL в in-memory экспортёре, связь задачи с запросом, `traceId` в problem+json, метрики через ручной ридер, кардинальность, приём и лимит телеметрии, поля логов; TS — агрегирование, семплирование и сбои отправки в `@cms/core`; сценарий §2 — `traceId` в ошибках и отсутствие секретов в логах (STU-515) |
| STU-609 | Документация: эта спецификация, 12 §4 (реализация), 08 (эндпоинт телеметрии, `traceId`), 10 (опция SDK), `docs/testing.md`, отчёт `docs/observability-acceptance.md` |

## 4. Поток

```
Studio BFF ──traceparent──▶ cms serve: HTTP span ─▶ command span ─▶ SQL spans
                                      │                 │
                                      │                 └─ job enqueue (link)
                                      ▼                         ▼
                       problem+json {traceId}        River worker: job span ─▶ SQL spans
store (SSR) ─@cms/core─▶ POST …/telemetry ─▶ cms.runtime.diagnostics{code}
                       OTLP ─▶ Collector ─▶ трассы, метрики (бэкенд §11.2)
```

## 5. Метрики

| Метрика | Тип | Метки |
|---|---|---|
| `http.server.request.duration` | гистограмма, с | `http.route`, `http.request.method`, `http.response.status_code` |
| `cms.command.duration` | гистограмма, с | `cms.command`, `cms.outcome` |
| `cms.pipeline.stage.duration` | гистограмма, с | `cms.stage`, `cms.status` |
| `cms.jobs.depth`, `cms.jobs.oldest_age` | gauge | `messaging.destination.name` (очередь), `cms.job_kind` |
| `cms.checks.queued`, `cms.checks.oldest_age` | gauge | `cms.stage` |
| `cms.delivery.cache.requests` | счётчик | `cms.delivery.kind`, `cms.cache.result` |
| `cms.llm.tokens`, `cms.llm.requests` | счётчик | `gen_ai.request.model`, `gen_ai.token.type` / `cms.outcome` |
| `cms.runtime.diagnostics` | счётчик | `cms.diagnostic.code`, `cms.diagnostic.source`, `deployment.environment` |

## 6. Отступления и связи

- NFR-018 «логи через OpenTelemetry»: логи остаются JSON в stdout с `traceId`; их
  собирает агент окружения (Collector filelog, Docker, хостинг). Экспорт логов по
  OTLP — после выбора хостинга (решение §11.3).
- 12 §5.1 упоминает отдельный процесс `worker`: задачи River выполняет `cms serve`;
  спаны задач от этого не зависят.

## 7. Порядок реализации после одобрения

1. Сервер: инициализация SDK и ресурс, HTTP, команды, SQL, задачи и связь, `traceId` в ошибках, поля логов (STU-590…595, 602).
2. Метрики (STU-596…601).
3. Приём телеметрии и `@cms/core` (STU-603…605).
4. Распространение `traceparent` в Studio BFF и исполнителе (STU-606).
5. Профиль Compose и инструкция (STU-607).
6. Тесты, документация, отчёт (STU-608, 609).

## 8. Критерии приёмки пакета

- В локальном профиле `observability` одна правка в Studio видна одной трассой: HTTP → команда → SQL; публикация — с задачей purge, связанной ссылкой.
- Ошибка API содержит `traceId`, по которому трасса находится в бэкенде.
- Метрики OPS-021 видны в бэкенде, метки ограничены §5; hit ratio кэша Delivery считается запросом.
- Диагностика runtime из витрины увеличивает `cms.runtime.diagnostics`; сбой или лимит телеметрии не влияет на рендер.
- Без `OTEL_EXPORTER_OTLP_ENDPOINT` поведение и задержки не меняются: нагрузочный прогон `Load` остаётся зелёным.
- Существующие проверки CI, пороги покрытия и E2E не ослаблены.

## 9. Связанные документы

[12 §4](spec/12-security-ops.md), [01 NFR-018](spec/01-requirements.md), [08 API](spec/08-api.md),
[10 runtime и SDK](spec/10-runtime-sdk-preview.md), [кэш Delivery](delivery-cache-limits.md),
[безопасность](security-hardening.md), [нагрузочные тесты](load-tests.md), [план MVP](spec/14-mvp-plan.md).

## 10. Риски

- SQL-спаны на каждый запрос увеличивают объём трасс в десятки раз; семплирование
  (§11.1) обязательно в production.
- Метки с высокой кардинальностью дорого стоят в любом бэкенде; STU-601 закрепляет
  фиксированные наборы.
- Телеметрия из витрин — новая точка входа; ключ доставки, лимит и отсутствие
  хранения ограничивают злоупотребление.

## 11. Принятые решения — 2026-10-28

Пользователь принял рекомендации 1–6:
- семплирование — родительское с долей (1а);
- локальный бэкенд — `grafana/otel-lgtm` (2а);
- логи — JSON в stdout с `traceId` (3а);
- телеметрию отправляет только серверная часть приложения (4а);
- `traceparent` — только от аутентифицированных клиентов (5а);
- Node-сервисы только передают `traceparent` (6а).

Варианты и обоснования:

1. **Семплирование трасс.**
   - (а) Родительское с долей: 100 % локально и в CI, 10 % по умолчанию в production
     (`OTEL_TRACES_SAMPLER=parentbased_traceidratio`, `_ARG=0.1`); ошибки 5xx не
     добираются отдельно.
   - (б) Всё 100 %, сокращение — в Collector (tail sampling).

   Рекомендация — (а): работает без дополнительной инфраструктуры; tail sampling
   можно добавить в Collector позже, не меняя код.
2. **Локальный бэкенд.**
   - (а) Всё-в-одном `grafana/otel-lgtm` (Collector, Tempo, Prometheus, Loki, Grafana) в профиле Compose.
   - (б) Collector с отладочным экспортёром в консоль и Jaeger для трасс.
   - (в) Без локального бэкенда, только экспорт.

   Рекомендация — (а): один контейнер, трассы и метрики в одном интерфейсе, критерии
   §8 проверяются руками.
3. **Логи.**
   - (а) JSON в stdout с `traceId`; OTLP-экспорт логов — после выбора хостинга.
   - (б) Сразу мост `otelslog` и экспорт логов по OTLP рядом со stdout.

   Рекомендация — (а): логи уже собираются платформой из stdout; двойной поток
   удваивает объём и риск утечки при ошибке фильтра. Отступление от NFR-018
   фиксируется (§6).
4. **Кто отправляет телеметрию SDK.**
   - (а) Только серверная часть приложения с ключом доставки.
   - (б) И браузер — публичный эндпоинт без ключа с лимитом по IP.

   Рекомендация — (а): эндпоинт не становится публичной точкой записи; ошибки
   гидратации в браузере можно передать через сервер приложения позже.
5. **Входящий `traceparent`.**
   - (а) Принимать только от аутентифицированных сервисных и человеческих сессий (Studio BFF, исполнитель, CI); Delivery без ключа — новая трасса.
   - (б) Принимать от всех.

   Рекомендация — (а): чужой `traceparent` позволяет навязывать семплирование и
   склеивать трассы; Delivery — публичная поверхность.
6. **Node-сервисы.**
   - (а) Studio BFF и исполнитель только передают `traceparent` и логируют `traceId`; собственные трассировки — отдельным пакетом.
   - (б) OpenTelemetry Node SDK в обоих сейчас.

   Рекомендация — (а): основная работа и задержки — в `cms`; Node SDK добавит
   зависимость и время старта, не закрывая новых требований OPS-020…023.
