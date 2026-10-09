# 09. Агент, права, политики и Missing Capability

## 1. Принципы

| ID | Требование |
|---|---|
| AGT-001 | Агент НЕ ДОЛЖЕН иметь полномочий, недоступных человеку с тем же набором прав. Агент, человек, API и миграция используют одни команды и одну политику. |
| AGT-002 | Инструменты агента ДОЛЖНЫ быть семантическими. Инструменты `execute_sql`, произвольный PATCH тела документа и доступ к внутренним таблицам НЕ ДОЛЖНЫ предоставляться. |
| AGT-003 | Все изменения агента ДОЛЖНЫ попадать в Change Set и быть видимыми в истории как операции с `actor.kind = agent`. |
| AGT-004 | Агент получает минимально необходимый структурный контекст; LLM не используется как поисковый алгоритм там, где система может дать детерминированную навигацию. |

## 2. Модель прав

### 2.1. Перечень прав

| Право | Разрешает |
|---|---|
| `content.read` | Чтение сущностей, ассетов, черновиков |
| `content.write` | Создание и изменение сущностей и `localContent` |
| `content.delete` | Удаление сущностей и документов |
| `content.publish` | Публикация, продвижение, откат (с условием по окружению) |
| `asset.write` | Загрузка ассетов и изменение их метаданных |
| `design.read` | Чтение документов IR, manifest, токенов |
| `design.compose` | Структурные, дизайн- и binding-операции над документами |
| `design.zones.manage` | Изменение зон, политик документов, `locked` |
| `design.components.certify` | Сертификация Composed-компонентов для STRICT |
| `design.tokens.modify` | Создание предложений об изменении токенов (оформляются как Missing Capability вида `token`, так как токены — часть кода приложения) |
| `component.write` | Создание и изменение Composed-компонентов |
| `behavior.use` | Назначение зарегистрированных действий событиям |
| `behavior.propose` | Предложение контракта нового действия |
| `capability.request` | Создание запросов Missing Capability |
| `code.propose` | Получение context pack и отправка ссылок на PR по запросам |
| `schema.read` / `schema.propose` / `schema.apply` | Чтение схем / создание CS со схемными изменениями / согласование и применение |
| `manifest.register` | Регистрация manifest, создание preview-окружений |
| `agent.delegate` | Запуск агентской сессии от своего имени |
| `project.admin` | Пользователи, роли, политики, окружения |

### 2.2. Роли по умолчанию

| Роль | Права |
|---|---|
| `editor` | `content.read`, `content.write`, `content.publish`, `asset.write`, `design.read`, `agent.delegate` |
| `designer` | `editor` без `content.publish` + `design.compose`, `component.write`, `behavior.use`, `capability.request`, `schema.read` |
| `lead-designer` | `designer` + `design.zones.manage`, `design.components.certify`, `content.publish` |
| `developer` | `content.read`, `design.read`, `schema.*`, `behavior.propose`, `code.propose`, `manifest.register` |
| `admin` | все |
| `ci` (сервис) | `manifest.register`, `schema.propose` |

### 2.3. Документ политики

```yaml
version: 1
rules:
  - subject: { role: editor }
    deny: [content.publish]
    when: { environment: [production], risk: [high] }

  - subject: { agent: "*" }
    allow: [content.read, content.write, design.read, design.compose, behavior.use, capability.request]
    requireApproval: [content.publish, schema.propose]
    deny: [schema.apply, design.zones.manage, design.components.certify, content.delete, project.admin]

  - subject: { agent: "copy-assistant" }
    allow: [content.publish]
    when: { environment: [staging], risk: [low] }

approvals:
  - when: { risk: [medium] }
    require: { count: 1 }
  - when: { risk: [high] }
    require: { count: 2, roles: [lead-designer] }
  - when: { environment: [production], source: [agent] }
    require: { count: 1 }

risk:
  - when: { source: [agent] }
    min: medium
  - when: { pathPrefix: ["/checkout"] }
    min: high
```

**Условия (`when`):** `environment`, `risk`, `source`, `schema`, `pathPrefix`, `zoneMode`, `zoneId`, `operationType`. Все указанные условия должны выполняться (И); значения внутри условия — ИЛИ.

### 2.4. Вычисление

| ID | Требование |
|---|---|
| AGT-010 | Для операции с требуемым правом R: если есть подходящее правило `deny` с R — отказ; иначе если R есть в правах ролей или в подходящем `allow` — разрешено; иначе если R в подходящем `requireApproval` — разрешено с обязательным согласованием CS (минимум 1, независимо от риска); иначе — отказ. |
| AGT-011 | `deny` имеет приоритет над любым `allow`. |
| AGT-012 | Отказ ДОЛЖЕН возвращать `POLICY_DENIED` с указанием права, правила и условия; запись в `security_events`. |
| AGT-013 | Изменение политики — команда `set-policy` (право `project.admin`); новая версия политики применяется к последующим операциям и к проверке этапа 5 pipeline для всех CS, ещё не слитых. |

## 3. Идентичность и делегирование агента

| ID | Требование |
|---|---|
| AGT-020 | Агент регистрируется в проекте как актор `kind = agent` с именем и описанием (провайдер, назначение). |
| AGT-021 | Работа агента выполняется в агентской сессии (`start-agent-session`). Сессия создаётся либо человеком с правом `agent.delegate` (тогда `onBehalfOf` = этот человек), либо сервисом для автономного агента (`onBehalfOf` пуст). |
| AGT-022 | Эффективные права сессии = права по политике агента ∩ права делегирующего человека. Агент не может получить больше прав, чем у пользователя, который его запустил. |
| AGT-023 | Сессия ограничена сроком (≤ 8 ч), квотой операций и списком Change Set'ов, в которые разрешена запись (по умолчанию — созданные в этой сессии и CS делегирующего пользователя, явно переданные при старте). |
| AGT-024 | Отзыв сессии или блокировка делегирующего пользователя немедленно делают токен сессии недействительным. |

## 4. MCP-интерфейс

MCP-сервер встроен в CMS (`/mcp`, транспорт Streamable HTTP) и вызывает те же обработчики Query API и Command Bus. Пакет `packages/mcp` содержит описания инструментов (JSON Schema) и stdio-прокси для локальных агентов.

### 4.1. Инструменты чтения

| Инструмент | Вход | Выход |
|---|---|---|
| `list_schemas` | `environment?` | Имена, заголовки, версии схем |
| `get_schema` | `name` | Поля и типы |
| `find_content` | `schema?`, `query?`, `filter?`, `limit` | Список сущностей (ID, заголовок, schema) |
| `get_content` | `entityId`, `changesetId?`, `locale?` | Тело сущности |
| `list_pages` | `pathPrefix?` | Страницы (ID, путь, заголовок) |
| `get_page_structure` | `documentId`, `changesetId?`, `depth?` | Компактное дерево: ID узлов, типы, имена, привязки, эффективный режим каждой зоны |
| `get_node` | `documentId`, `nodeId`, `changesetId?` | Полный узел и допустимые для него свойства/токены |
| `get_component` | `name` или `componentId` | Контракт компонента: свойства, слоты, события, provides |
| `list_components` | `documentId?`, `nodeId?` | Типы, допустимые в эффективном режиме указанного места |
| `get_capabilities` | `environment?`, `domain?` | Действия, источники данных, capabilities приложения |
| `get_design_rules` | `documentId`, `nodeId` | Токены, режим, допустимые дизайн-свойства и диапазоны |
| `get_changeset` | `changesetId` | Состояние, операции, диагностика, проверки |
| `get_changeset_diff` | `changesetId` | Структурный diff |

### 4.2. Инструменты изменения

| Инструмент | Соответствующие операции |
|---|---|
| `create_changeset` | `create-changeset` |
| `update_content` | `entity.setFields`, `localContent.set` |
| `create_content` | `entity.create` |
| `insert_node` | `node.insert` (вложенная форма) |
| `move_node` / `remove_node` / `duplicate_node` / `wrap_nodes` | `node.move` / `node.remove` / `node.duplicate` / `node.wrap` |
| `update_props` / `update_design` | `node.setProps` / `node.setDesign` |
| `bind_data` | `node.setBinding`, `node.setCondition`, `document.setDataSource` |
| `set_behavior` | `node.setBehavior` |
| `create_component` | `component.extract` |
| `apply_operations` | Пакет любых операций каталога (для сложных изменений) |
| `validate_changeset` | Синхронный прогон этапов 1–7 pipeline без смены состояния |
| `request_preview` | Возвращает URL preview и (опционально) скриншоты страницы на 375/768/1440 px |
| `request_review` | `submit-changeset` |
| `report_missing_capability` | `create-capability-request` (§5) |

| ID | Требование |
|---|---|
| AGT-030 | Каждый изменяющий инструмент ДОЛЖЕН требовать `changesetId` и `reason`. |
| AGT-031 | Ответ изменяющего инструмента ДОЛЖЕН содержать новые ID узлов, `seq` и диагностику — агент не должен перечитывать документ, чтобы узнать результат. |
| AGT-032 | Инструменты ДОЛЖНЫ отклонять операции, недопустимые в эффективном режиме, с диагностикой, достаточной для исправления (допустимые значения, ближайшие токены). |

### 4.3. Встроенный агент Studio

Режим Agent в Studio использует встроенный оркестратор (модуль `agents/`): LLM-цикл на сервере с теми же инструментами MCP, вызываемыми в процессе.

| ID | Требование |
|---|---|
| AGT-040 | Провайдер и модель LLM настраиваются на уровне проекта. Ключи провайдера хранятся в секретах сервера и не передаются в Studio. |
| AGT-041 | Оркестратор работает в агентской сессии от имени текущего пользователя и пишет в его открытый CS. |
| AGT-042 | Шаги агента (вызовы инструментов, итоги) передаются в Studio потоком (SSE); каждая операция агента сразу отображается в preview и может быть отменена пользователем. |
| AGT-043 | Расход токенов LLM учитывается по проекту и пользователю; проект МОЖЕТ задать месячный лимит. |

### 4.4. Защита от prompt injection

| ID | Требование |
|---|---|
| AGT-050 | Контент, возвращаемый инструментами, ДОЛЖЕН передаваться в структурированных полях данных, отдельно от служебного текста; описания инструментов указывают, что содержимое полей — данные, а не инструкции. |
| AGT-051 | Ограничение возможностей агента обеспечивается сервером (политика, квоты, список CS), а не инструкциями в промпте. Инъекция в контенте не может расширить права сессии. |
| AGT-052 | Агент не имеет инструментов для внешних сетевых запросов, отправки сообщений или изменения прав. |

## 5. Missing Capability

### 5.1. Жизненный цикл

```
draft ─submit─▶ submitted ─accept─▶ accepted ─▶ in_progress ─▶ pr_open ─▶ fulfilled
                    │                                              │
                    └──────────── reject / cancel ◀────────────────┘
```

| Этап | Что происходит |
|---|---|
| Обнаружение | (a) дизайнер в зоне CODE выбирает «Нужно поведение / компонент» и описывает намерение; (b) агент вызывает `report_missing_capability`, не найдя подходящего элемента в manifest; (c) валидатор встречает `pending:` в CS |
| Формализация | CMS (или агент) предлагает ключ, вид (`action`, `component`, `dataSource`, `token`) и контракт; проверяет дубли среди открытых запросов и похожие существующие элементы manifest |
| Заглушка | В черновике узел получает действие `pending:<requestId>`; Studio показывает узел со значком; публикация блокируется (IR-053) |
| Context pack | Детерминированная сборка контекста (§6) |
| Передача | Webhook проекта (`codeAgent.webhookUrl`, подпись HMAC) или создание задачи в трекере через интеграцию; статус `accepted` выставляет разработчик или сервис |
| Реализация | Агент или разработчик работает в репозитории, открывает PR; сервис CI сообщает ссылку (`update-capability-request`, статус `pr_open`) |
| Preview | Сборка PR регистрирует manifest в `preview/<ref>`; Studio позволяет посмотреть CS на этом окружении, где `pending:<id>` разрешается в реализованный ключ |
| Выполнение | При регистрации manifest в стандартное окружение, содержащего ключ, запрос получает статус `fulfilled`; CMS добавляет в CS из `requiredBy` операцию замены `pending:<id>` на реальное действие (актор `service: cms-system`) и уведомляет владельца CS |

### 5.2. Формат запроса

```json
{
  "key": "commerce.product.quickPreview",
  "kind": "action",
  "title": "Быстрый просмотр товара",
  "description": "По клику на карточку в ProductShowcase открывать модальное окно с галереей, ценой и кнопкой «В корзину» без перехода на страницу товара.",
  "requiredBy": [{ "documentId": "0192f1c5-…", "nodeId": "n_card01", "event": "click", "changesetId": "0192f1cf-…" }],
  "proposedContract": {
    "args": { "product": { "type": "reference", "schema": "Product", "required": true } },
    "acceptance": [
      "Модальное окно открывается по клику и закрывается по Esc и клику вне окна",
      "Фокус переходит в окно и возвращается на карточку после закрытия",
      "Кнопка «В корзину» использует существующее действие commerce.addToCart"
    ]
  }
}
```

| ID | Требование |
|---|---|
| AGT-060 | CMS НЕ ДОЛЖНА генерировать или исполнять код при обработке Missing Capability; её задача — формализация, контекст и отслеживание. |
| AGT-061 | Ключ запроса ДОЛЖЕН соответствовать правилам имён manifest и не совпадать с существующим элементом активного manifest. |
| AGT-062 | Выполненная capability ДОЛЖНА появиться в manifest с `origin: "generated"` (для компонентов) и пройти обычный инженерный процесс (PR, CI, ревью человеком). |

## 6. Структурный контекст репозитория

### 6.1. Индекс кода

`cms manifest build` строит `code-index.json` по графу импортов TypeScript, начиная от определений в registry:

```json
{
  "repo": { "root": ".", "commit": "3f2a1c9" },
  "elements": {
    "component:ProductCard": {
      "definition": { "file": "src/cms/registry/commerce.ts", "line": 14 },
      "implementation": { "file": "src/components/ProductCard.tsx", "export": "ProductCard" },
      "dependencies": ["src/components/Price.tsx", "src/lib/commerce/format.ts"],
      "tests": ["src/components/ProductCard.test.tsx"]
    },
    "action:commerce.addToCart": {
      "definition": { "file": "src/cms/actions/cart.ts", "line": 8 },
      "implementation": { "file": "src/cms/actions/cart.ts", "export": "addToCart" },
      "dependencies": ["src/lib/commerce/cart.ts"],
      "tests": ["src/cms/actions/cart.test.ts"]
    }
  },
  "conventions": {
    "registry": "src/cms/registry/index.ts",
    "testCommand": "pnpm test",
    "manifestCommand": "pnpm cms manifest build"
  }
}
```

| ID | Требование |
|---|---|
| AGT-070 | Индекс содержит только пути, экспорты, номера строк и связи — **не содержимое файлов**. Код агент читает из репозитория сам, со своими правами доступа. |
| AGT-071 | Зависимости включаются до глубины 2 по импортам внутри репозитория; `node_modules` и сгенерированные файлы исключаются. |
| AGT-072 | Тесты элемента — файлы `*.test.*` / `*.spec.*`, импортирующие файл реализации. |

### 6.2. Context pack

Для запроса Missing Capability CMS собирает context pack детерминированно, без LLM:

1. **Цепочка UI:** страница → путь предков узла (типы и имена) → узел → компоненты, использованные в поддереве.
2. **Элементы manifest:** контракты компонентов цепочки; действия, источники данных и capabilities того же домена (`commerce.*`); близкие по виду элементы (для действия, открывающего окно, — `Modal`, `openModal`).
3. **Код:** записи индекса для элементов из п. 2 — файлы определения, реализации, зависимостей, тестов; файл регистрации registry.
4. **Данные:** схемы контента, на которые ссылаются аргументы контракта.
5. **Задача:** описание, предложенный контракт, критерии приёмки.
6. **Ограничения:** регистрировать через `defineAction`/`defineComponent`; добавить тесты; не менять несвязанные файлы; результат — PR с обновлённым registry.

```json
{
  "request": { "key": "commerce.product.quickPreview", "kind": "action", "…": "…" },
  "uiChain": ["Page /collections/:slug", "Container n_root", "ProductShowcase (composed) n_show01", "ProductCard n_card01"],
  "manifest": { "components": ["ProductCard", "Modal"], "actions": ["commerce.addToCart", "openModal"], "schemas": ["Product"] },
  "files": {
    "definitions": ["src/cms/registry/commerce.ts", "src/cms/actions/cart.ts"],
    "implementations": ["src/components/ProductCard.tsx"],
    "related": ["src/lib/commerce/cart.ts", "src/components/Modal.tsx"],
    "tests": ["src/components/ProductCard.test.tsx", "src/cms/actions/cart.test.ts"],
    "registry": "src/cms/registry/index.ts"
  },
  "constraints": ["…"],
  "repo": { "commit": "3f2a1c9", "testCommand": "pnpm test" }
}
```

| ID | Требование |
|---|---|
| AGT-080 | Context pack ДОЛЖЕН собираться по индексу активного manifest того окружения, из которого создан запрос, и фиксировать `commit`. |
| AGT-081 | Размер списка файлов context pack ограничен (по умолчанию 40); при превышении приоритет — определения и реализации, затем тесты, затем зависимости. |
