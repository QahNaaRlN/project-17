# 12. Реализация сервера, безопасность и эксплуатация

## 1. Серверная архитектура

### 1.1. Стек

| Подсистема | Технология |
|---|---|
| Язык | Go (актуальная стабильная версия) |
| HTTP | `net/http` + chi |
| PostgreSQL | pgx v5, sqlc для запросов, goose для миграций |
| Фоновые задачи и outbox | River (очередь на PostgreSQL) |
| Кэш, rate limiting, presence | Redis 7 |
| Объектное хранилище | S3-совместимое (MinIO локально) |
| Трансформации изображений | imgproxy |
| Валидация JSON Schema | `santhosh-tekuri/jsonschema` |
| Наблюдаемость | OpenTelemetry SDK, `log/slog` (JSON) |

### 1.2. Модули

Модульный монолит: один бинарник `cms` с режимами `serve`, `worker`, `migrate`. Модули общаются только через публичные интерфейсы пакетов (в `internal/<module>/api.go`); прямой доступ к таблицам чужого модуля запрещён и проверяется линтером (`depguard`).

| Модуль | Ответственность |
|---|---|
| `commandbus` | Конверт команд, идемпотентность, авторизация, транзакции, outbox |
| `content` | Сущности, ассеты, rich text, локализация |
| `schema` | Версии схем, миграции, upcast |
| `composition` | Документы IR, применение операций, нормализация, анализ привязок |
| `design` | Режимы, токены, дизайн-валидация, статическая доступность |
| `manifest` | Регистрация, diff, совместимость, индекс кода |
| `changes` | Change Set, операции, rebase, undo/redo, diff |
| `workflow` | Pipeline проверок, риск, согласования |
| `publishing` | Публикация, продвижение, откат, маршруты, инвалидация |
| `delivery` | Delivery API, кэш, разрешение данных |
| `permissions` | Роли, политики, ABAC, агентские сессии |
| `agents` | MCP-сервер, оркестратор встроенного агента, Missing Capability, context pack |
| `search` | Полнотекстовый индекс и запросы |

### 1.3. Command Bus

```go
type Command interface {
    Name() string                    // "apply-operations"
    Validate() error
}

type Handler[C Command, R any] interface {
    Authorize(ctx context.Context, actor Actor, cmd C) (Requirements, error)
    Handle(ctx context.Context, tx pgx.Tx, actor Actor, cmd C) (R, []Event, error)
}

type Actor struct {
    ID          uuid.UUID
    Kind        ActorKind            // Human | Agent | Service | Migration
    OnBehalfOf  *uuid.UUID
    SessionID   *uuid.UUID
    Rights      RightSet             // эффективные права (с учётом делегирования)
    Source      Source               // studio | api | agent | migration | import
}
```

| ID | Требование |
|---|---|
| OPS-010 | Обработчик исполняется в одной транзакции с уровнем `READ COMMITTED` и явными блокировками строк; события пишутся в outbox в той же транзакции. |
| OPS-011 | HTTP-слой, MCP-сервер и оркестратор агента вызывают Command Bus одним и тем же способом. |

## 2. Структура monorepo

```
cms/
├── apps/
│   ├── server/                 # Go-модуль
│   │   ├── cmd/cms/
│   │   ├── internal/           # модули §1.2
│   │   ├── db/{migrations,queries}/
│   │   └── api/openapi.yaml
│   └── studio/                 # React + Vite
├── packages/
│   ├── ir/                     # JSON Schema IR (источник истины), фикстуры, codegen TS и Go
│   ├── sdk-core/
│   ├── design-runtime/
│   ├── sdk-react/
│   ├── component-registry/
│   └── mcp/
├── examples/
│   └── react-store/            # эталонное приложение MVP
├── deploy/
│   └── compose/
└── docs/
    └── spec/
```

Менеджер пакетов JS — pnpm workspaces; оркестрация задач — Turborepo. Go-типы IR генерируются из `packages/ir/schema` в `apps/server/internal/composition/irtypes` командой `make gen`.

## 3. Безопасность

### 3.1. Модель угроз (кратко)

| Угроза | Меры |
|---|---|
| Повышение прав агентом (в т. ч. prompt injection) | Пересечение прав с делегирующим (AGT-022), серверные политики, квоты, отсутствие опасных инструментов (AGT-050…052) |
| Обход согласования | Разделение обязанностей (PUB-002/003), привязка согласования к хэшу (PUB-004) |
| XSS через контент | Нет HTML в IR и rich text; белый список схем URL; санитизация SVG |
| Подмена preview / clickjacking | Проверка origin в протоколе, `frame-ancestors`, короткоживущие preview-токены |
| Утечка черновиков | Черновой режим только с preview-токеном, `no-store` |
| Межпроектный доступ | `project_id` во всех запросах, тесты изоляции |
| Вредоносные файлы | Проверка MIME по содержимому, лимиты, санитизация SVG, отдельный домен для ассетов |
| Компрометация CI-токена | Узкие права `ci`, ограничение окружениями, срок жизни, ротация |
| Утечка кода в CMS | Индекс кода без содержимого файлов (AGT-070) |

### 3.2. Требования

| ID | Требование |
|---|---|
| SEC-001 | Пароли не хранятся; аутентификация людей — только через OIDC. |
| SEC-002 | Секреты токенов хранятся только как SHA-256; показываются один раз при создании. |
| SEC-003 | Studio отдаётся с CSP: `default-src 'self'; frame-src <app-origins>; connect-src 'self'; script-src 'self'`; без инлайн-скриптов. |
| SEC-004 | Ассеты отдаются с отдельного домена (cookie-less), с `Content-Disposition: attachment` для неизображений и `X-Content-Type-Options: nosniff`. |
| SEC-005 | Все отказы авторизации, входы, создание токенов, изменения ролей и политик пишутся в `security_events`. |
| SEC-006 | Вебхуки (исходящие) подписываются HMAC-SHA256 с меткой времени; получатель отклоняет запросы старше 5 мин. |
| SEC-007 | Зависимости сканируются в CI (`govulncheck`, `pnpm audit`); образы — сканером уязвимостей. |
| SEC-008 | Персональные данные в CMS минимальны (учётные записи); удаление пользователя анонимизирует `display_name`/`email` в акторе, сохраняя операции. |

## 4. Наблюдаемость

| ID | Требование |
|---|---|
| OPS-020 | Трассировки OpenTelemetry для каждого HTTP-запроса, команды, задачи и SQL-запроса; `traceId` возвращается в ошибках. |
| OPS-021 | Метрики: задержки и коды по эндпоинтам; длительность команд по типам; длина очередей и возраст задач; длительность этапов pipeline; hit ratio кэша Delivery; число диагностик runtime по кодам (из SDK); расход токенов LLM. |
| OPS-022 | Логи — структурированный JSON с `traceId`, `projectId`, `actorId`; без содержимого контента и секретов. |
| OPS-023 | SDK отправляет диагностику runtime (неизвестные типы, ошибки источников, ошибки действий) на `POST /delivery/v1/{project}/{env}/telemetry` с семплированием. |

## 5. Инфраструктура

### 5.1. Локальная и первая серверная конфигурация

```yaml
# deploy/compose/docker-compose.yml (сокращённо)
services:
  cms:
    image: cms/server
    command: ["serve"]
    environment: [DATABASE_URL, REDIS_URL, S3_ENDPOINT, S3_BUCKET, OIDC_ISSUER, PUBLIC_URL]
    depends_on: [postgres, redis, minio]
  cms-worker:
    image: cms/server
    command: ["worker"]
  studio:
    image: cms/studio            # статика за тем же доменом, что и API
  postgres:
    image: postgres:16
  redis:
    image: redis:7
  minio:
    image: minio/minio
  imgproxy:
    image: darthsim/imgproxy
```

### 5.2. Production

| Компонент | Размещение |
|---|---|
| CMS API (`serve`) | ≥ 2 экземпляра за балансировщиком, без состояния |
| Worker | ≥ 1 экземпляр; масштабируется по длине очереди |
| PostgreSQL | Управляемый или с репликой и PITR (OPS-001) |
| Redis | Управляемый, без требования персистентности |
| Объектное хранилище | S3-совместимое с версионированием |
| CDN | Перед Delivery API и ассетами; поддержка purge по surrogate keys |

Kubernetes, Kafka и микросервисы не вводятся до подтверждённой необходимости.

## 6. Стратегия тестирования

| Уровень | Что покрывает |
|---|---|
| Conformance IR | Общие фикстуры `packages/ir/fixtures/{valid,invalid}` для валидаторов Go и TS |
| Unit (Go) | Применение операций, rebase, вычисление режимов и политик, upcast схем, diff |
| Property-based (Go) | Операция + обратная операция = исходный документ; rebase без конфликтов коммутирует с независимыми операциями |
| Интеграционные (Go + PostgreSQL в testcontainers) | Публикация, откат, конкурентные CS, изоляция проектов |
| Контрактные | OpenAPI ↔ обработчики; MCP-инструменты ↔ команды |
| E2E (Playwright) | Сквозной сценарий MVP на `examples/react-store` ([14-mvp-plan.md](14-mvp-plan.md)) |
| Нагрузочные (k6) | NFR-001…007 |
